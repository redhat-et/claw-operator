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
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	openShellPluginPackage        = "@openclaw/openshell-sandbox"
	openShellPluginID             = "openshell"
	defaultOpenShellPluginCommand = "/opt/openshell/bin/openshell"
	defaultOpenShellPluginGateway = "openshell"
	defaultOpenShellPluginMode    = clawv1alpha1.OpenShellModeRemote
	defaultOpenShellTimeout       = int32(180)
)

type resolvedOpenShell struct {
	Enabled        bool
	Endpoint       string
	SandboxImage   string
	Mode           clawv1alpha1.OpenShellMode
	TimeoutSeconds int32
	NoProxyHosts   []string
	EgressTarget   *openShellEgressTarget
}

type openShellEgressTarget struct {
	Name      string
	Namespace string
	Port      int
}

func openShellEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.OpenShell != nil && instance.Spec.OpenShell.Enabled
}

func (r *ClawResourceReconciler) resolveOpenShell(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) (resolvedOpenShell, ctrl.Result, error) {
	if !openShellEnabled(instance) {
		meta.RemoveStatusCondition(&instance.Status.Conditions, clawv1alpha1.ConditionTypeOpenShellConfigured)
		return resolvedOpenShell{}, ctrl.Result{}, nil
	}

	spec := instance.Spec.OpenShell
	resolved := resolvedOpenShell{
		Enabled:        true,
		SandboxImage:   valueOrDefault(spec.SandboxImage, defaultOpenShellSandboxImage),
		Mode:           spec.Mode,
		TimeoutSeconds: defaultOpenShellTimeout,
	}
	if resolved.Mode == "" {
		resolved.Mode = defaultOpenShellPluginMode
	}
	if spec.TimeoutSeconds != nil {
		resolved.TimeoutSeconds = *spec.TimeoutSeconds
	}

	var serviceName, serviceNamespace string
	servicePort := 0
	if spec.GatewayRef != nil {
		refNamespace := valueOrDefault(spec.GatewayRef.Namespace, instance.Namespace)
		gateway := &clawv1alpha1.OpenShellGateway{}
		key := types.NamespacedName{Name: spec.GatewayRef.Name, Namespace: refNamespace}
		if err := r.Get(ctx, key, gateway); err != nil {
			return resolvedOpenShell{}, ctrl.Result{}, fmt.Errorf("resolve OpenShell gatewayRef %s/%s: %w", refNamespace, spec.GatewayRef.Name, err)
		}
		if gateway.Status.Endpoint == "" {
			setCondition(instance, clawv1alpha1.ConditionTypeOpenShellConfigured,
				metav1.ConditionFalse, clawv1alpha1.ConditionReasonProvisioning,
				fmt.Sprintf("OpenShellGateway %s/%s has no endpoint yet", refNamespace, spec.GatewayRef.Name))
			setCondition(instance, clawv1alpha1.ConditionTypeReady,
				metav1.ConditionFalse, clawv1alpha1.ConditionReasonProvisioning,
				fmt.Sprintf("OpenShellGateway %s/%s is not ready", refNamespace, spec.GatewayRef.Name))
			return resolvedOpenShell{}, ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		resolved.Endpoint = gateway.Status.Endpoint
		serviceName = valueOrDefault(gateway.Status.ServiceName, gateway.Name)
		serviceNamespace = gateway.Namespace
		servicePort = int(gateway.Spec.ServicePort)
		if servicePort == 0 {
			servicePort = int(openShellGatewayConfigFor(gateway).ServicePort)
		}
	} else {
		resolved.Endpoint = strings.TrimSpace(spec.GatewayEndpoint)
	}

	if resolved.Endpoint == "" {
		return resolvedOpenShell{}, ctrl.Result{}, fmt.Errorf("spec.openshell requires gatewayRef or gatewayEndpoint")
	}

	if serviceName == "" {
		target, err := openShellTargetFromEndpoint(resolved.Endpoint, instance.Namespace)
		if err != nil {
			return resolvedOpenShell{}, ctrl.Result{}, err
		}
		serviceName = target.Name
		serviceNamespace = target.Namespace
		servicePort = target.Port
	}
	if servicePort == 0 {
		servicePort = 8080
	}
	resolved.EgressTarget = &openShellEgressTarget{
		Name:      serviceName,
		Namespace: serviceNamespace,
		Port:      servicePort,
	}
	resolved.NoProxyHosts = openShellNoProxyHosts(serviceName, serviceNamespace)
	setCondition(instance, clawv1alpha1.ConditionTypeOpenShellConfigured,
		metav1.ConditionTrue, clawv1alpha1.ConditionReasonConfigured,
		"OpenShell sandbox backend configured")
	return resolved, ctrl.Result{}, nil
}

func openShellTargetFromEndpoint(rawEndpoint, defaultNamespace string) (openShellEgressTarget, error) {
	parsed, err := url.Parse(rawEndpoint)
	if err != nil {
		return openShellEgressTarget{}, fmt.Errorf("parse OpenShell gatewayEndpoint %q: %w", rawEndpoint, err)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return openShellEgressTarget{}, fmt.Errorf("OpenShell gatewayEndpoint %q has no hostname", rawEndpoint)
	}
	parts := strings.Split(hostname, ".")
	target := openShellEgressTarget{
		Name:      parts[0],
		Namespace: defaultNamespace,
	}
	switch {
	case len(parts) == 1:
	case len(parts) >= 2 && (strings.HasSuffix(hostname, ".svc") || strings.HasSuffix(hostname, ".svc.cluster.local")):
		target.Namespace = parts[1]
	default:
		return openShellEgressTarget{}, fmt.Errorf("OpenShell gatewayEndpoint %q must use in-cluster service DNS", rawEndpoint)
	}
	if parsed.Port() == "" {
		target.Port = 8080
	} else {
		port, err := resolvePort(parsed)
		if err != nil {
			return openShellEgressTarget{}, err
		}
		target.Port = port
	}
	return target, nil
}

func openShellNoProxyHosts(serviceName, namespace string) []string {
	if namespace == "" {
		return []string{serviceName}
	}
	return []string{
		serviceName,
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	}
}

func injectOpenShellConfig(config map[string]any, resolved resolvedOpenShell) {
	if !resolved.Enabled {
		return
	}
	agents := ensureNestedMap(config, "agents")
	defaults := ensureNestedMap(agents, "defaults")
	defaults["sandbox"] = map[string]any{
		"mode":            "all",
		"backend":         openShellPluginID,
		"scope":           "session",
		"workspaceAccess": "rw",
	}

	entries := ensureNestedMap(ensureNestedMap(config, "plugins"), "entries")
	entries[openShellPluginID] = map[string]any{
		"enabled": true,
		"config": map[string]any{
			"command":         defaultOpenShellPluginCommand,
			"gateway":         defaultOpenShellPluginGateway,
			"from":            resolved.SandboxImage,
			"mode":            string(resolved.Mode),
			"gatewayEndpoint": resolved.Endpoint,
			"timeoutSeconds":  resolved.TimeoutSeconds,
		},
	}
}

func injectOpenShellGatewayEgressRule(objects []*unstructured.Unstructured, instanceName string, resolved resolvedOpenShell) error {
	if !resolved.Enabled || resolved.EgressTarget == nil {
		return nil
	}

	npName := getEgressNetworkPolicyName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}
		egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress rules from NetworkPolicy: %w", err)
		}
		if !found {
			egress = []any{}
		}
		egress = append(egress, buildOpenShellGatewayEgressRule(*resolved.EgressTarget))
		if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
			return fmt.Errorf("failed to set egress rules on NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

func buildOpenShellGatewayEgressRule(target openShellEgressTarget) map[string]any {
	return map[string]any{
		"to": []any{
			map[string]any{
				"namespaceSelector": map[string]any{
					"matchLabels": map[string]any{
						"kubernetes.io/metadata.name": target.Namespace,
					},
				},
				"podSelector": map[string]any{
					"matchLabels": openShellGatewaySelector(target.Name),
				},
			},
		},
		"ports": []any{
			map[string]any{
				"port":     int64(target.Port),
				"protocol": "TCP",
			},
		},
	}
}
