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

// otlpEndpoint returns the best available OTLP endpoint for the gateway SDK.
// Prefers spec.traces.endpoint; falls back to an explicit spec.logs.endpoint
// when traces is not configured. Metrics share whatever endpoint is available.
func otlpEndpoint(instance *clawv1alpha1.Claw) string {
	if ep := tracesEndpoint(instance); ep != "" {
		return ep
	}
	if instance.Spec.Logs != nil && instance.Spec.Logs.Endpoint != "" {
		return instance.Spec.Logs.Endpoint
	}
	return ""
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
	if metricsEnabled(instance) && otlpEndpoint(instance) == "" {
		return fmt.Errorf("spec.traces.endpoint or spec.logs.endpoint is required when spec.metrics.enabled is true")
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

// injectDiagnosticsConfig injects diagnostics.otel config keys when traces,
// logs, or metrics are enabled. The endpoint is resolved via otlpEndpoint —
// the gateway sends OTLP directly to an externally-deployed OTel Collector.
func injectDiagnosticsConfig(config map[string]any, instance *clawv1alpha1.Claw) {
	tracesOn := tracesEnabled(instance)
	logsOn := logsEnabled(instance)
	metricsOn := metricsEnabled(instance)
	if !tracesOn && !logsOn && !metricsOn {
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

	// enabled activates the diagnostics-otel plugin's OTLP exporter.
	// Without this key the plugin loads but does not initialize any exporters.
	otel["enabled"] = true
	if tracesOn {
		otel["traces"] = true
	}
	if logsOn {
		otel["logs"] = true
	}
	if metricsOn {
		otel["metrics"] = true
	}
	// Set the collector endpoint from the best available signal endpoint.
	// otlpEndpoint prefers traces, then falls back to an explicit logs endpoint.
	if _, set := otel["endpoint"]; !set {
		if ep := otlpEndpoint(instance); ep != "" {
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

			// Point the SDK at the external OTel Collector.
			// Uses the best available endpoint across all enabled signals.
			if ep := otlpEndpoint(instance); ep != "" {
				envVars = setOrAppendEnv(envVars, "OTEL_EXPORTER_OTLP_ENDPOINT", ep)

				// The gateway runs behind a MITM proxy (HTTP_PROXY). OTLP traffic to
				// the collector must bypass it or the proxy will return 403 (no
				// credential rule for the collector host). Append the collector hostname
				// to NO_PROXY / no_proxy so the OTel SDK connects directly.
				if u, err := url.Parse(ep); err == nil && u.Hostname() != "" {
					envVars = appendToNoProxy(envVars, "NO_PROXY", u.Hostname())
					envVars = appendToNoProxy(envVars, "no_proxy", u.Hostname())
				}
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

// appendToNoProxy finds the NO_PROXY or no_proxy env var and appends the given
// hostname if it is not already present. The MITM proxy respects this variable
// via NODE_USE_ENV_PROXY, so appending the OTel Collector hostname causes the
// Node.js SDK to connect to the collector directly rather than through the proxy.
func appendToNoProxy(envVars []any, name, hostname string) []any {
	for i, e := range envVars {
		env, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if env["name"] != name {
			continue
		}
		existing, _ := env["value"].(string)
		// Check if already present (exact hostname or as part of a CSV entry).
		for _, h := range strings.Split(existing, ",") {
			if strings.TrimSpace(h) == hostname {
				return envVars
			}
		}
		if existing != "" {
			env["value"] = existing + "," + hostname
		} else {
			env["value"] = hostname
		}
		envVars[i] = env
		return envVars
	}
	// NO_PROXY not yet set — create a new entry.
	return append(envVars, map[string]any{"name": name, "value": hostname})
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
