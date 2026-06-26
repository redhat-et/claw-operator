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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func testClawWithTraces(endpoint, samplingRatio string) *clawv1alpha1.Claw {
	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Traces = &clawv1alpha1.TracesSpec{
		Enabled:       true,
		Endpoint:      endpoint,
		SamplingRatio: samplingRatio,
	}
	return instance
}

func testClawWithLogs(endpoint string) *clawv1alpha1.Claw {
	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Logs = &clawv1alpha1.LogsSpec{
		Enabled:  true,
		Endpoint: endpoint,
	}
	return instance
}

func TestTracesAndLogsHelpers(t *testing.T) {
	t.Run("nil spec", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		assert.False(t, tracesEnabled(instance))
		assert.False(t, logsEnabled(instance))
		assert.Equal(t, "", tracesEndpoint(instance))
		assert.Equal(t, "", logsEndpoint(instance))
		assert.Equal(t, "1", tracesSamplingRatio(instance))
	})

	t.Run("traces enabled", func(t *testing.T) {
		instance := testClawWithTraces("http://collector.obs.svc:4318", "")
		assert.True(t, tracesEnabled(instance))
		assert.False(t, logsEnabled(instance))
		assert.Equal(t, "http://collector.obs.svc:4318", tracesEndpoint(instance))
		assert.Equal(t, "http://collector.obs.svc:4318", logsEndpoint(instance))
	})

	t.Run("logs enabled with own endpoint", func(t *testing.T) {
		instance := testClawWithTraces("http://traces.svc:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{
			Enabled:  true,
			Endpoint: "http://logs.svc:4318",
		}
		assert.True(t, tracesEnabled(instance))
		assert.True(t, logsEnabled(instance))
		assert.Equal(t, "http://traces.svc:4318", tracesEndpoint(instance))
		assert.Equal(t, "http://logs.svc:4318", logsEndpoint(instance))
	})

	t.Run("logs falls back to traces endpoint", func(t *testing.T) {
		instance := testClawWithTraces("http://collector.svc:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true}
		assert.Equal(t, "http://collector.svc:4318", logsEndpoint(instance))
	})

	t.Run("custom sampling ratio", func(t *testing.T) {
		instance := testClawWithTraces("", "0.1")
		assert.Equal(t, "0.1", tracesSamplingRatio(instance))
	})

	t.Run("otlpEndpoint prefers traces", func(t *testing.T) {
		instance := testClawWithTraces("http://traces.svc:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true, Endpoint: "http://logs.svc:4318"}
		assert.Equal(t, "http://traces.svc:4318", otlpEndpoint(instance))
	})

	t.Run("otlpEndpoint falls back to explicit logs endpoint when no traces", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true, Endpoint: "http://loki.svc:4318"}
		assert.Equal(t, "http://loki.svc:4318", otlpEndpoint(instance))
	})

	t.Run("otlpEndpoint empty when no signal has endpoint", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		assert.Equal(t, "", otlpEndpoint(instance))
	})

}

func TestInjectDiagnosticsConfig(t *testing.T) {
	t.Run("traces enabled sets diagnostics.otel keys", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithTraces("http://collector:4318", "")

		injectDiagnosticsConfig(config, instance)

		diag := config["diagnostics"].(map[string]any)
		otel := diag["otel"].(map[string]any)
		assert.Equal(t, true, otel["traces"])
		assert.Equal(t, "http://collector:4318", otel["endpoint"])
		_, hasLogs := otel["logs"]
		assert.False(t, hasLogs)
	})

	t.Run("logs enabled sets logs key", func(t *testing.T) {
		config := map[string]any{}
		instance := testClawWithLogs("http://logs:4318")

		injectDiagnosticsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, true, otel["logs"])
		_, hasTraces := otel["traces"]
		assert.False(t, hasTraces)
	})

	t.Run("both enabled sets both keys", func(t *testing.T) {
		config := map[string]any{}
		instance := testClawWithTraces("http://c:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true}

		injectDiagnosticsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, true, otel["traces"])
		assert.Equal(t, true, otel["logs"])
	})

	t.Run("preserves existing endpoint", func(t *testing.T) {
		config := map[string]any{
			"diagnostics": map[string]any{
				"otel": map[string]any{
					"endpoint": "http://custom:4318",
				},
			},
		}
		instance := testClawWithTraces("", "")

		injectDiagnosticsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, "http://custom:4318", otel["endpoint"])
	})

	t.Run("noop when nothing enabled", func(t *testing.T) {
		config := map[string]any{}
		instance := &clawv1alpha1.Claw{}

		injectDiagnosticsConfig(config, instance)

		_, hasDiag := config["diagnostics"]
		assert.False(t, hasDiag)
	})

	t.Run("logs-only with own endpoint sets endpoint from logs", func(t *testing.T) {
		config := map[string]any{}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true, Endpoint: "http://loki.svc:4318"}

		injectDiagnosticsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, true, otel["logs"])
		assert.Equal(t, "http://loki.svc:4318", otel["endpoint"])
		_, hasTraces := otel["traces"]
		assert.False(t, hasTraces)
	})

	t.Run("metrics-only with traces endpoint sets all three signals", func(t *testing.T) {
		config := map[string]any{}
		instance := testClawWithTraces("http://collector.svc:4318", "")
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}

		injectDiagnosticsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, true, otel["traces"])
		assert.Equal(t, true, otel["metrics"])
		assert.Equal(t, "http://collector.svc:4318", otel["endpoint"])
	})
}

func makeTestDeploymentForDiagnostics() []*unstructured.Unstructured {
	dep := &unstructured.Unstructured{}
	dep.SetKind(DeploymentKind)
	dep.SetName(getClawDeploymentName(testInstanceName))
	dep.Object["spec"] = map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name": ClawGatewayContainerName,
						"env":  []any{},
					},
				},
			},
		},
	}
	return []*unstructured.Unstructured{dep}
}

func TestInjectOTelEnvVars(t *testing.T) {
	t.Run("injects resource attributes and sampler", func(t *testing.T) {
		objects := makeTestDeploymentForDiagnostics()
		instance := testClawWithTraces("http://c:4318", "0.5")

		require.NoError(t, injectOTelEnvVars(objects, instance))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		gateway := containers[0].(map[string]any)
		envVars := gateway["env"].([]any)

		envMap := make(map[string]any)
		for _, e := range envVars {
			env := e.(map[string]any)
			envMap[env["name"].(string)] = env
		}

		assert.Equal(t, "openclaw", envMap["OTEL_SERVICE_NAME"].(map[string]any)["value"])

		podName := envMap["POD_NAME"].(map[string]any)
		valueFrom := podName["valueFrom"].(map[string]any)
		fieldRef := valueFrom["fieldRef"].(map[string]any)
		assert.Equal(t, "metadata.name", fieldRef["fieldPath"])

		podNs := envMap["POD_NAMESPACE"].(map[string]any)
		valueFromNs := podNs["valueFrom"].(map[string]any)
		fieldRefNs := valueFromNs["fieldRef"].(map[string]any)
		assert.Equal(t, "metadata.namespace", fieldRefNs["fieldPath"])

		resAttrs := envMap["OTEL_RESOURCE_ATTRIBUTES"].(map[string]any)
		assert.Contains(t, resAttrs["value"], "service.instance.id=$(POD_NAME)")
		assert.Contains(t, resAttrs["value"], "k8s.namespace.name=$(POD_NAMESPACE)")

		assert.Equal(t, "parentbased_traceidratio",
			envMap["OTEL_TRACES_SAMPLER"].(map[string]any)["value"])
		assert.Equal(t, "0.5",
			envMap["OTEL_TRACES_SAMPLER_ARG"].(map[string]any)["value"])
	})

	t.Run("omits sampler when traces not enabled", func(t *testing.T) {
		objects := makeTestDeploymentForDiagnostics()
		instance := testClawWithLogs("http://c:4318")

		require.NoError(t, injectOTelEnvVars(objects, instance))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		gateway := containers[0].(map[string]any)
		envVars := gateway["env"].([]any)

		for _, e := range envVars {
			env := e.(map[string]any)
			assert.NotEqual(t, "OTEL_TRACES_SAMPLER", env["name"])
		}
	})

	t.Run("sets OTEL_EXPORTER_OTLP_ENDPOINT from logs endpoint when no traces", func(t *testing.T) {
		objects := makeTestDeploymentForDiagnostics()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{Enabled: true, Endpoint: "http://loki.svc:4318"}

		require.NoError(t, injectOTelEnvVars(objects, instance))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		gateway := containers[0].(map[string]any)
		envVars := gateway["env"].([]any)
		envMap := make(map[string]string)
		for _, e := range envVars {
			env := e.(map[string]any)
			if v, ok := env["value"].(string); ok {
				envMap[env["name"].(string)] = v
			}
		}
		assert.Equal(t, "http://loki.svc:4318", envMap["OTEL_EXPORTER_OTLP_ENDPOINT"])
	})

	t.Run("returns error when deployment not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithTraces("", "")

		err := injectOTelEnvVars(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestInjectTelemetryEgressRules(t *testing.T) {
	makeEgressNP := func() []*unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetKind(NetworkPolicyKind)
		np.SetName(getEgressNetworkPolicyName(testInstanceName))
		np.Object["spec"] = map[string]any{
			"egress": []any{
				map[string]any{
					"ports": []any{
						map[string]any{"port": int64(443), "protocol": "TCP"},
					},
				},
			},
		}
		return []*unstructured.Unstructured{np}
	}

	t.Run("adds egress rule for external endpoint", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://external-collector.example.com:4318", "")

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2)
	})

	t.Run("adds in-cluster egress rule with namespace selector", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://collector.observability.svc:4318", "")

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2)

		rule := egress[1].(map[string]any)
		to := rule["to"].([]any)
		require.Len(t, to, 1)
		nsSel := to[0].(map[string]any)["namespaceSelector"].(map[string]any)
		labels := nsSel["matchLabels"].(map[string]any)
		assert.Equal(t, "observability", labels["kubernetes.io/metadata.name"])
	})

	t.Run("adds in-cluster same-namespace egress rule with pod selector", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://collector:4318", "")

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2)

		rule := egress[1].(map[string]any)
		to := rule["to"].([]any)
		require.Len(t, to, 1)
		_, hasPodSel := to[0].(map[string]any)["podSelector"]
		assert.True(t, hasPodSel)
	})

	t.Run("noop when no endpoints", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("", "")

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 1)
	})

	t.Run("deduplicates same endpoint for traces and logs", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://external.example.com:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{
			Enabled:  true,
			Endpoint: "http://external.example.com:4318",
		}

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2)
	})

	t.Run("adds separate rules for different endpoint types", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://traces.example.com:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{
			Enabled:  true,
			Endpoint: "http://collector.observability.svc:4318",
		}

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 3, "expected base + external + in-cluster rules")
	})

	t.Run("deduplicates external endpoints on same port", func(t *testing.T) {
		objects := makeEgressNP()
		instance := testClawWithTraces("http://traces.example.com:4318", "")
		instance.Spec.Logs = &clawv1alpha1.LogsSpec{
			Enabled:  true,
			Endpoint: "http://logs.example.com:4318",
		}

		require.NoError(t, injectTelemetryEgressRules(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2, "same-port external endpoints should dedup to one rule")
	})
}

func TestRequiredDiagnosticsPlugins(t *testing.T) {
	t.Run("traces enabled includes otel plugin", func(t *testing.T) {
		instance := testClawWithTraces("", "")
		plugins := requiredDiagnosticsPlugins(instance)
		assert.Contains(t, plugins, "@openclaw/diagnostics-otel")
		assert.NotContains(t, plugins, "@openclaw/diagnostics-prometheus")
	})

	t.Run("logs enabled includes otel plugin", func(t *testing.T) {
		instance := testClawWithLogs("")
		plugins := requiredDiagnosticsPlugins(instance)
		assert.Contains(t, plugins, "@openclaw/diagnostics-otel")
	})

	t.Run("metrics enabled includes prometheus plugin", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		plugins := requiredDiagnosticsPlugins(instance)
		assert.Contains(t, plugins, "@openclaw/diagnostics-prometheus")
		assert.NotContains(t, plugins, "@openclaw/diagnostics-otel")
	})

	t.Run("nothing enabled returns empty", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		plugins := requiredDiagnosticsPlugins(instance)
		assert.Empty(t, plugins)
	})

	t.Run("both traces and metrics includes both plugins", func(t *testing.T) {
		instance := testClawWithTraces("", "")
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		plugins := requiredDiagnosticsPlugins(instance)
		assert.Contains(t, plugins, "@openclaw/diagnostics-otel")
		assert.Contains(t, plugins, "@openclaw/diagnostics-prometheus")
	})
}
