/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestInjectOpenShellConfig(t *testing.T) {
	config := map[string]any{}
	resolved := resolvedOpenShell{
		Enabled:        true,
		Endpoint:       "http://openshell.openshell-alice.svc.cluster.local:8080",
		SandboxImage:   "quay.io/example/openclaw-openshell-sandbox:latest",
		Mode:           clawv1alpha1.OpenShellModeRemote,
		TimeoutSeconds: 180,
	}

	injectOpenShellConfig(config, resolved)

	defaults := config["agents"].(map[string]any)["defaults"].(map[string]any)
	assert.Equal(t, map[string]any{
		"mode":            "all",
		"backend":         "openshell",
		"scope":           "session",
		"workspaceAccess": "rw",
	}, defaults["sandbox"])

	entries := config["plugins"].(map[string]any)["entries"].(map[string]any)
	plugin := entries["openshell"].(map[string]any)
	assert.Equal(t, true, plugin["enabled"])

	pluginConfig := plugin["config"].(map[string]any)
	assert.Equal(t, "/opt/openshell/bin/openshell", pluginConfig["command"])
	assert.Equal(t, "openshell", pluginConfig["gateway"])
	assert.Equal(t, "quay.io/example/openclaw-openshell-sandbox:latest", pluginConfig["from"])
	assert.Equal(t, "remote", pluginConfig["mode"])
	assert.Equal(t, resolved.Endpoint, pluginConfig["gatewayEndpoint"])
	assert.Equal(t, int32(180), pluginConfig["timeoutSeconds"])
}

func TestOpenShellTargetFromEndpoint(t *testing.T) {
	target, err := openShellTargetFromEndpoint(
		"http://openshell.openshell-alice.svc.cluster.local:8080",
		"openclaw-alice",
	)
	require.NoError(t, err)
	assert.Equal(t, openShellEgressTarget{
		Name:      "openshell",
		Namespace: "openshell-alice",
		Port:      8080,
	}, target)
	assert.Equal(t, []string{
		"openshell",
		"openshell.openshell-alice.svc",
		"openshell.openshell-alice.svc.cluster.local",
	}, openShellNoProxyHosts(target.Name, target.Namespace))
}

func TestInjectOpenShellEgressRule(t *testing.T) {
	np := &unstructured.Unstructured{}
	np.SetKind(NetworkPolicyKind)
	np.SetName(getEgressNetworkPolicyName(testInstanceName))
	np.Object["spec"] = map[string]any{"egress": []any{}}

	err := injectOpenShellEgressRule(
		[]*unstructured.Unstructured{np},
		testInstanceName,
		resolvedOpenShell{
			Enabled: true,
			EgressTarget: &openShellEgressTarget{
				Name:        "openshell",
				Namespace:   "openshell-alice",
				Port:        8080,
				PodSelector: map[string]string{"gateway": "openshell"},
			},
		},
	)
	require.NoError(t, err)

	egress, found, err := unstructured.NestedSlice(np.Object, "spec", "egress")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, egress, 1)

	rule := egress[0].(map[string]any)
	ports := rule["ports"].([]any)
	assert.Equal(t, int64(8080), ports[0].(map[string]any)["port"])

	to := rule["to"].([]any)
	peer := to[0].(map[string]any)
	namespaceSelector := peer["namespaceSelector"].(map[string]any)
	assert.Equal(
		t,
		"openshell-alice",
		namespaceSelector["matchLabels"].(map[string]any)["kubernetes.io/metadata.name"],
	)
	podSelector := peer["podSelector"].(map[string]any)
	assert.Equal(t, map[string]string{"gateway": "openshell"}, podSelector["matchLabels"])
}

func TestConfigureGatewayOpenShellNoProxy(t *testing.T) {
	objects := makeTestDeploymentForPlugins()
	instance := &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
	}
	resolved := resolvedOpenShell{
		Enabled: true,
		NoProxyHosts: []string{
			"openshell",
			"openshell.openshell-alice.svc",
			"openshell.openshell-alice.svc.cluster.local",
		},
	}

	require.NoError(t, configureGatewayOpenShellNoProxy(objects, instance, resolved))

	gateway := getGatewayContainerFromUnstructured(t, objects[0])
	envVars := gateway["env"].([]any)
	envMap := map[string]string{}
	for _, e := range envVars {
		entry := e.(map[string]any)
		envMap[entry["name"].(string)] = entry["value"].(string)
	}
	assert.Equal(
		t,
		"openshell,openshell.openshell-alice.svc,openshell.openshell-alice.svc.cluster.local",
		envMap["NO_PROXY"],
	)
	assert.Equal(t, envMap["NO_PROXY"], envMap["no_proxy"])
}

func getGatewayContainerFromUnstructured(t *testing.T, deployment *unstructured.Unstructured) map[string]any {
	t.Helper()

	containers, found, err := unstructured.NestedSlice(
		deployment.Object,
		"spec", "template", "spec", "containers",
	)
	require.NoError(t, err)
	require.True(t, found)
	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if container["name"] == ClawGatewayContainerName {
			return container
		}
	}
	t.Fatalf("container %q not found", ClawGatewayContainerName)
	return nil
}
