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
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"
)

const managerNetworkPolicyName = "claw-operator-allow-metrics-traffic"
const managerWebhookNetworkPolicyName = "claw-operator-allow-webhook-traffic"

func TestDefaultManifestsIncludeManagerNetworkPolicy(t *testing.T) {
	objects := buildKustomizeObjectsForTest(t, "../../config/default")

	np := findNetworkPolicyForTest(t, objects, managerNetworkPolicyName)
	assertManagerMetricsNetworkPolicy(t, np)

	np = findNetworkPolicyForTest(t, objects, managerWebhookNetworkPolicyName)
	assertManagerWebhookNetworkPolicy(t, np)
}

func TestOLMManifestsIncludeManagerNetworkPolicy(t *testing.T) {
	objects := buildKustomizeObjectsForTest(t, "../../config/manifests")

	np := findNetworkPolicyForTest(t, objects, managerNetworkPolicyName)
	assertManagerMetricsNetworkPolicy(t, np)

	np = findNetworkPolicyForTest(t, objects, managerWebhookNetworkPolicyName)
	assertManagerWebhookNetworkPolicy(t, np)
}

func buildKustomizeObjectsForTest(t *testing.T, path string) []runtime.RawExtension {
	t.Helper()

	resMap, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), path)
	require.NoError(t, err)

	yamlOutput, err := resMap.AsYaml()
	require.NoError(t, err)

	return splitYAMLDocuments(yamlOutput)
}

func splitYAMLDocuments(yamlOutput []byte) []runtime.RawExtension {
	docs := bytes.Split(yamlOutput, []byte("\n---\n"))
	objects := make([]runtime.RawExtension, 0, len(docs))
	for _, doc := range docs {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}
		objects = append(objects, runtime.RawExtension{Raw: doc})
	}
	return objects
}

func findNetworkPolicyForTest(t *testing.T, objects []runtime.RawExtension, name string) *networkingv1.NetworkPolicy {
	t.Helper()

	for _, obj := range objects {
		var meta struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		require.NoError(t, yaml.Unmarshal(obj.Raw, &meta))
		if meta.Kind != NetworkPolicyKind || meta.Metadata.Name != name {
			continue
		}

		var np networkingv1.NetworkPolicy
		require.NoError(t, yaml.Unmarshal(obj.Raw, &np))
		return &np
	}

	require.Failf(t, "NetworkPolicy not found", "expected %s in rendered manifests", name)
	return nil
}

func assertManagerMetricsNetworkPolicy(t *testing.T, np *networkingv1.NetworkPolicy) {
	t.Helper()

	rule := assertManagerIngressNetworkPolicy(t, np, 8443)

	require.Len(t, rule.From, 2)
	require.NotNil(t, rule.From[0].NamespaceSelector)
	assert.Equal(t, map[string]string{
		"kubernetes.io/metadata.name": "openshift-monitoring",
	}, rule.From[0].NamespaceSelector.MatchLabels)
	require.NotNil(t, rule.From[1].PodSelector)
	assert.Equal(t, map[string]string{
		"claw.sandbox.redhat.com/metrics-reader": "true",
	}, rule.From[1].PodSelector.MatchLabels)
}

func assertManagerWebhookNetworkPolicy(t *testing.T, np *networkingv1.NetworkPolicy) {
	t.Helper()

	rule := assertManagerIngressNetworkPolicy(t, np, 9443)

	require.Len(t, rule.From, 2)
	require.NotNil(t, rule.From[0].NamespaceSelector)
	assert.Equal(t, map[string]string{
		"kubernetes.io/metadata.name": "openshift-kube-apiserver",
	}, rule.From[0].NamespaceSelector.MatchLabels)

	require.NotNil(t, rule.From[1].NamespaceSelector)
	assert.Equal(t, map[string]string{
		"kubernetes.io/metadata.name": "kube-system",
	}, rule.From[1].NamespaceSelector.MatchLabels)
	require.NotNil(t, rule.From[1].PodSelector)
	assert.Equal(t, map[string]string{
		"component": "kube-apiserver",
	}, rule.From[1].PodSelector.MatchLabels)
}

func assertManagerIngressNetworkPolicy(t *testing.T, np *networkingv1.NetworkPolicy, port int32) networkingv1.NetworkPolicyIngressRule {
	t.Helper()

	assert.Equal(t, "claw-operator", np.Namespace)
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name": "claw-operator",
		"control-plane":          "controller-manager",
	}, np.Spec.PodSelector.MatchLabels)
	require.Len(t, np.Spec.PolicyTypes, 1)
	assert.Equal(t, networkingv1.PolicyTypeIngress, np.Spec.PolicyTypes[0])

	require.Len(t, np.Spec.Ingress, 1)
	rule := np.Spec.Ingress[0]
	require.Len(t, rule.Ports, 1)
	assert.Equal(t, port, rule.Ports[0].Port.IntVal)
	return rule
}
