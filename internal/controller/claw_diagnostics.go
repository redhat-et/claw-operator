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
	"net/url"
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

func otelSidecarNeeded(instance *clawv1alpha1.Claw) bool {
	return metricsEnabled(instance) || tracesEnabled(instance) || logsEnabled(instance)
}

// injectObservabilityResources handles OTel collector config, metrics Service/NP,
// and telemetry egress rules.
func injectObservabilityResources(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if otelSidecarNeeded(instance) {
		if err := injectOTelCollectorConfig(objects, instance); err != nil {
			return fmt.Errorf("failed to inject OTel collector config: %w", err)
		}
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

// injectDiagnosticsConfig injects diagnostics.otel config keys for the
// OTel Collector sidecar when traces or logs are enabled.
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
	if _, set := otel["endpoint"]; !set {
		otel["endpoint"] = "http://localhost:4318"
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
// for external traces/logs collector endpoints.
func injectTelemetryEgressRules(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	endpoints := collectTelemetryEndpoints(instance)
	if len(endpoints) == 0 {
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

		for _, ep := range endpoints {
			parsed, err := url.Parse(ep)
			if err != nil {
				continue
			}
			port, err := resolvePort(parsed)
			if err != nil {
				continue
			}

			rule := map[string]any{
				"ports": []any{
					map[string]any{
						"port":     int64(port),
						"protocol": "TCP",
					},
				},
			}
			egress = append(egress, rule)
		}

		return unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress")
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

func collectTelemetryEndpoints(instance *clawv1alpha1.Claw) []string {
	seen := make(map[string]bool)
	var endpoints []string

	tEp := tracesEndpoint(instance)
	if tEp != "" && isExternalEndpoint(tEp, instance) {
		if !seen[tEp] {
			endpoints = append(endpoints, tEp)
			seen[tEp] = true
		}
	}

	lEp := logsEndpoint(instance)
	if lEp != "" && isExternalEndpoint(lEp, instance) {
		if !seen[lEp] {
			endpoints = append(endpoints, lEp)
			seen[lEp] = true
		}
	}

	return endpoints
}

func isExternalEndpoint(rawURL string, instance *clawv1alpha1.Claw) bool {
	target, err := classifyServiceURL(rawURL, instance.Namespace)
	if err != nil {
		return false
	}
	return target.External
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
