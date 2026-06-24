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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func tracesEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.Traces != nil && instance.Spec.Traces.Enabled
}

func logsEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.Logs != nil && instance.Spec.Logs.Enabled
}

func tracesEndpoint(instance *clawv1alpha1.Claw) string {
	if instance.Spec.Traces != nil {
		return instance.Spec.Traces.Endpoint
	}
	return ""
}

func logsEndpoint(instance *clawv1alpha1.Claw) string {
	if instance.Spec.Logs != nil && instance.Spec.Logs.Endpoint != "" {
		return instance.Spec.Logs.Endpoint
	}
	return tracesEndpoint(instance)
}

func tracesSamplingRatio(instance *clawv1alpha1.Claw) string {
	if instance.Spec.Traces != nil && instance.Spec.Traces.SamplingRatio != "" {
		return instance.Spec.Traces.SamplingRatio
	}
	return "1"
}

// injectObservabilityResources handles metrics Service/NP and telemetry egress rules.
// The OTel Collector is expected to be deployed externally (e.g., via the OTel Operator);
// the gateway sends OTLP directly to it via OTEL_EXPORTER_OTLP_ENDPOINT.
func injectObservabilityResources(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if tracesEnabled(instance) && tracesEndpoint(instance) == "" {
		return fmt.Errorf("spec.traces.endpoint is required when spec.traces.enabled is true")
	}
	if logsEnabled(instance) && logsEndpoint(instance) == "" {
		return fmt.Errorf("spec.logs requires either spec.logs.endpoint or spec.traces.endpoint when enabled")
	}
	if metricsEnabled(instance) {
		if err := addMetricsPortToService(objects, instance); err != nil {
			return fmt.Errorf("failed to add metrics port to Service: %w", err)
		}
		if err := addMetricsIngressRule(objects, instance); err != nil {
			return fmt.Errorf("failed to add metrics ingress rule: %w", err)
		}
	}
	if tracesEnabled(instance) || logsEnabled(instance) {
		if err := injectTelemetryEgressRules(objects, instance); err != nil {
			return fmt.Errorf("failed to inject telemetry egress rules: %w", err)
		}
	}
	return nil
}

// injectDiagnosticsConfig injects diagnostics.otel config keys when traces or
// logs are enabled. The endpoint is set to spec.traces.endpoint — the gateway
// sends OTLP directly to an externally-deployed OTel Collector.
func injectDiagnosticsConfig(config map[string]any, instance *clawv1alpha1.Claw) {
	tracesOn := tracesEnabled(instance)
	logsOn := logsEnabled(instance)
	if !tracesOn && !logsOn {
		return
	}

	diagnostics, _ := config["diagnostics"].(map[string]any)
	if diagnostics == nil {
		diagnostics = map[string]any{}
		config["diagnostics"] = diagnostics
	}

	otel, _ := diagnostics["otel"].(map[string]any)
	if otel == nil {
		otel = map[string]any{}
		diagnostics["otel"] = otel
	}

	if tracesOn {
		otel["traces"] = true
	}
	if logsOn {
		otel["logs"] = true
	}
	// Point directly at the external OTel Collector (not the removed sidecar).
	if _, set := otel["endpoint"]; !set {
		if ep := tracesEndpoint(instance); ep != "" {
			otel["endpoint"] = ep
		}
	}
}

// injectOTelEnvVars injects OTel resource attribute environment variables
// into the gateway container for multi-instance discrimination.
func injectOTelEnvVars(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(
			obj.Object, "spec", "template", "spec", "containers",
		)
		if err != nil || !found {
			return fmt.Errorf("failed to get containers from claw deployment: %w", err)
		}

		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(cm, "name")
			if name != ClawGatewayContainerName {
				continue
			}

			envSlice, _, _ := unstructured.NestedSlice(cm, "env")
			envVars := make([]any, len(envSlice))
			copy(envVars, envSlice)

			envVars = setOrAppendEnv(envVars, "OTEL_SERVICE_NAME", "openclaw")
			envVars = setOrAppendEnvDownwardAPI(envVars, "POD_NAME", "metadata.name")
			envVars = setOrAppendEnvDownwardAPI(envVars, "POD_NAMESPACE", "metadata.namespace")
			envVars = setOrAppendEnv(envVars,
				"OTEL_RESOURCE_ATTRIBUTES",
				"service.instance.id=$(POD_NAME),k8s.namespace.name=$(POD_NAMESPACE)",
			)

			// Point the SDK at the external OTel Collector directly.
			if ep := tracesEndpoint(instance); ep != "" {
				envVars = setOrAppendEnv(envVars, "OTEL_EXPORTER_OTLP_ENDPOINT", ep)
			}

			if tracesEnabled(instance) {
				envVars = setOrAppendEnv(envVars,
					"OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
				envVars = setOrAppendEnv(envVars,
					"OTEL_TRACES_SAMPLER_ARG", tracesSamplingRatio(instance))
			}

			if err := unstructured.SetNestedSlice(cm, envVars, "env"); err != nil {
				return fmt.Errorf("failed to set env on gateway container: %w", err)
			}
			containers[i] = cm
			return unstructured.SetNestedSlice(
				obj.Object, containers, "spec", "template", "spec", "containers",
			)
		}
		return fmt.Errorf("gateway container not found in claw deployment")
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

func setOrAppendEnvDownwardAPI(envVars []any, name, fieldPath string) []any {
	for i, e := range envVars {
		env, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if env["name"] == name {
			env["valueFrom"] = map[string]any{
				"fieldRef": map[string]any{"fieldPath": fieldPath},
			}
			delete(env, "value")
			envVars[i] = env
			return envVars
		}
	}
	return append(envVars, map[string]any{
		"name": name,
		"valueFrom": map[string]any{
			"fieldRef": map[string]any{"fieldPath": fieldPath},
		},
	})
}

// injectTelemetryEgressRules adds egress rules to the gateway NetworkPolicy
// for traces/logs collector endpoints. External endpoints get port-only rules;
// in-cluster endpoints get podSelector/namespaceSelector rules (same pattern
// as injectMcpGatewayEgressRules).
func injectTelemetryEgressRules(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	targets, err := classifyTelemetryEndpoints(instance)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	npName := getEgressNetworkPolicyName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		egress, _, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress from NetworkPolicy: %w", err)
		}

		for _, t := range targets {
			if t.External {
				egress = append(egress, map[string]any{
					"ports": []any{
						map[string]any{
							"port":     int64(t.Port),
							"protocol": "TCP",
						},
					},
				})
			} else {
				egress = append(egress, buildInClusterEgressRule(t))
			}
		}

		return unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress")
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

func classifyTelemetryEndpoints(instance *clawv1alpha1.Claw) ([]egressTarget, error) {
	seen := make(map[string]bool)
	var targets []egressTarget

	for _, ep := range []string{tracesEndpoint(instance), logsEndpoint(instance)} {
		if ep == "" {
			continue
		}
		target, err := classifyServiceURL(ep, instance.Namespace)
		if err != nil {
			return nil, fmt.Errorf("invalid telemetry endpoint %q: %w", ep, err)
		}
		key := dedupKey(target)
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target)
	}

	return targets, nil
}

// buildCollectorConfig generates the OTel Collector YAML configuration
// based on which signals (metrics, traces, logs) are enabled.
func buildCollectorConfig(instance *clawv1alpha1.Claw) string {
	port := metricsPort(instance)
	tEp := tracesEndpoint(instance)
	lEp := logsEndpoint(instance)
	hasMetrics := metricsEnabled(instance)
	hasTraces := tracesEnabled(instance) && tEp != ""
	hasLogs := logsEnabled(instance) && lEp != ""

	var b strings.Builder
	b.WriteString("receivers:\n")
	b.WriteString("  otlp:\n")
	b.WriteString("    protocols:\n")
	b.WriteString("      http:\n")
	b.WriteString("        endpoint: 127.0.0.1:4318\n")

	b.WriteString("exporters:\n")
	if hasMetrics {
		fmt.Fprintf(&b, "  prometheus:\n    endpoint: 0.0.0.0:%d\n", port)
	}

	sameEndpoint := hasTraces && hasLogs && tEp == lEp
	if hasTraces && hasLogs && !sameEndpoint {
		fmt.Fprintf(&b, "  otlp_http/traces:\n    endpoint: %s\n", tEp)
		fmt.Fprintf(&b, "  otlp_http/logs:\n    endpoint: %s\n", lEp)
	} else if hasTraces || hasLogs {
		ep := tEp
		if ep == "" {
			ep = lEp
		}
		fmt.Fprintf(&b, "  otlp_http:\n    endpoint: %s\n", ep)
	}

	b.WriteString("service:\n")
	b.WriteString("  pipelines:\n")
	if hasMetrics {
		b.WriteString("    metrics:\n")
		b.WriteString("      receivers: [otlp]\n")
		b.WriteString("      exporters: [prometheus]\n")
	}
	if hasTraces {
		b.WriteString("    traces:\n")
		b.WriteString("      receivers: [otlp]\n")
		if sameEndpoint {
			b.WriteString("      exporters: [otlp_http]\n")
		} else if hasLogs {
			b.WriteString("      exporters: [otlp_http/traces]\n")
		} else {
			b.WriteString("      exporters: [otlp_http]\n")
		}
	}
	if hasLogs {
		b.WriteString("    logs:\n")
		b.WriteString("      receivers: [otlp]\n")
		if sameEndpoint {
			b.WriteString("      exporters: [otlp_http]\n")
		} else if hasTraces {
			b.WriteString("      exporters: [otlp_http/logs]\n")
		} else {
			b.WriteString("      exporters: [otlp_http]\n")
		}
	}

	return b.String()
}
