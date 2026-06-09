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

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sort"
	"time"
)

func (s *server) listClaws(ctx context.Context, identity userIdentity, namespace string) ([]stateResponse, error) {
	var list map[string]any
	if err := s.kubeJSON(ctx, identity, http.MethodGet, apiPath("apis/claw.sandbox.redhat.com/v1alpha1/namespaces", namespace, "claws"), nil, &list); err != nil {
		return nil, err
	}
	items, _, _ := nestedSlice(list, "items")
	claws := make([]stateResponse, 0, len(items))
	for _, item := range items {
		claw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		claws = append(claws, stateFromClaw(claw))
	}
	sort.Slice(claws, func(i, j int) bool {
		return claws[i].Name < claws[j].Name
	})
	return claws, nil
}

func (s *server) getState(ctx context.Context, identity userIdentity, namespace, name string) (stateResponse, error) {
	var claw map[string]any
	err := s.kubeJSON(ctx, identity, http.MethodGet, apiPath("apis/claw.sandbox.redhat.com/v1alpha1/namespaces", namespace, "claws", name), nil, &claw)
	if err != nil {
		return stateResponse{}, err
	}
	return stateFromClaw(claw), nil
}

func stateFromClaw(claw map[string]any) stateResponse {
	ready, reason, message := readyCondition(claw)
	gatewayURL, _, _ := nestedString(claw, "status", "gatewayURL")
	if gatewayURL == "" {
		gatewayURL, _, _ = nestedString(claw, "status", "url")
	}
	providers := credentialProviders(claw)
	provider := ""
	if len(providers) > 0 {
		provider = providers[0]
	}
	name, _, _ := nestedString(claw, "metadata", "name")
	createdAt, _, _ := nestedString(claw, "metadata", "creationTimestamp")
	model, _, _ := nestedString(claw, "spec", "config", "raw", "agents", "defaults", "model", "primary")
	agentName := firstAgentName(claw)

	return stateResponse{
		Name:        name,
		Exists:      true,
		Ready:       ready,
		Reason:      reason,
		Message:     message,
		GatewayURL:  gatewayURL,
		Provider:    provider,
		Providers:   providers,
		Model:       model,
		AgentName:   agentName,
		CreatedAt:   createdAt,
		SecretNames: credentialSecretNames(claw),
	}
}

func (s *server) applySecret(ctx context.Context, identity userIdentity, req provisionRequest) error {
	option := providers[req.Provider]
	body := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      secretNameForRequest(req),
			"namespace": req.Namespace,
			"labels": map[string]string{
				managedByLabel: managedByValue,
				instanceLabel:  req.Name,
				providerLabel:  req.Provider,
			},
		},
		"type": "Opaque",
		"data": map[string]string{
			option.SecretKey: base64.StdEncoding.EncodeToString([]byte(req.APIKey)),
		},
	}
	return s.apply(ctx, identity, apiPath("api/v1/namespaces", req.Namespace, "secrets", secretNameForRequest(req)), body)
}

func (s *server) ensureProject(ctx context.Context, identity userIdentity, namespace string) error {
	if err := s.kubeJSON(ctx, identity, http.MethodGet, apiPath("api/v1/namespaces", namespace), nil, nil); err == nil {
		return nil
	} else {
		var apiErr apiError
		if !errors.As(err, &apiErr) || (apiErr.StatusCode != http.StatusNotFound && apiErr.StatusCode != http.StatusForbidden) {
			return err
		}
	}

	body := map[string]any{
		"apiVersion": "project.openshift.io/v1",
		"kind":       "ProjectRequest",
		"metadata": map[string]string{
			"name": namespace,
		},
	}
	err := s.kubeJSON(ctx, identity, http.MethodPost, "/apis/project.openshift.io/v1/projectrequests", body, nil)
	var apiErr apiError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return nil
	}
	return err
}

func (s *server) applyClaw(ctx context.Context, identity userIdentity, req provisionRequest) error {
	credentials, rawConfig := s.currentClawSpec(ctx, identity, req.Namespace, req.Name)
	credentials = upsertProvisionCredential(credentials, req)
	rawConfig = applyAgentConfig(rawConfig, req.AgentName, req.Model)

	body := map[string]any{
		"apiVersion": "claw.sandbox.redhat.com/v1alpha1",
		"kind":       "Claw",
		"metadata": map[string]any{
			"name":      req.Name,
			"namespace": req.Namespace,
			"labels": map[string]string{
				managedByLabel: managedByValue,
			},
		},
		"spec": map[string]any{
			"credentials": credentials,
			"config": map[string]any{
				"raw":       rawConfig,
				"mergeMode": "merge",
			},
		},
	}
	return s.apply(ctx, identity, apiPath("apis/claw.sandbox.redhat.com/v1alpha1/namespaces", req.Namespace, "claws", req.Name), body)
}

func (s *server) currentClawSpec(ctx context.Context, identity userIdentity, namespace, name string) ([]any, map[string]any) {
	var claw map[string]any
	err := s.kubeJSON(ctx, identity, http.MethodGet, apiPath("apis/claw.sandbox.redhat.com/v1alpha1/namespaces", namespace, "claws", name), nil, &claw)
	if err != nil {
		return nil, map[string]any{}
	}
	credentials, _, _ := nestedSlice(claw, "spec", "credentials")
	raw, _, _ := nestedMap(claw, "spec", "config", "raw")
	return credentials, cloneMap(raw)
}

func upsertCredential(credentials []any, instanceName, provider string) []any {
	return upsertProvisionCredential(credentials, provisionRequest{Name: instanceName, Provider: provider})
}

func upsertProvisionCredential(credentials []any, req provisionRequest) []any {
	option := providers[req.Provider]
	next := make([]any, 0, len(credentials)+1)
	replaced := false
	for _, credential := range credentials {
		credentialMap, ok := credential.(map[string]any)
		if !ok {
			continue
		}
		name, _ := credentialMap["name"].(string)
		if name == option.CredentialName {
			next = append(next, providerCredentialForRequest(req))
			replaced = true
			continue
		}
		next = append(next, credentialMap)
	}
	if !replaced {
		next = append(next, providerCredentialForRequest(req))
	}
	return next
}

func providerCredential(instanceName, provider string) map[string]any {
	return providerCredentialForRequest(provisionRequest{Name: instanceName, Provider: provider})
}

func providerCredentialForRequest(req provisionRequest) map[string]any {
	option := providers[req.Provider]
	credential := map[string]any{
		"name":     option.CredentialName,
		"provider": option.CredentialProvider,
		"secretRef": []map[string]string{
			{"name": secretNameForRequest(req), "key": option.SecretKey},
		},
	}
	if option.CredentialType != "" {
		credential["type"] = option.CredentialType
	}
	if option.RequiresGCP {
		credential["gcp"] = map[string]string{
			"project":  req.GCPProject,
			"location": req.GCPLocation,
		}
	}
	return credential
}

func applyAgentConfig(raw map[string]any, agentName, model string) map[string]any {
	config := cloneMap(raw)
	agents := ensureMap(config, "agents")
	defaults := ensureMap(agents, "defaults")
	defaults["model"] = map[string]any{"primary": model}
	models := ensureMap(defaults, "models")
	models[model] = map[string]any{"alias": model}
	agents["list"] = []any{
		map[string]any{
			"id":        "default",
			"name":      agentName,
			"identity":  map[string]string{"name": agentName},
			"workspace": "~/.openclaw/workspace",
			"model":     map[string]any{"primary": model},
		},
	}
	return config
}

func (s *server) restartDeployments(ctx context.Context, identity userIdentity, namespace, name string) error {
	restartedAt := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"openclaw-deployer.redhat.com/restartedAt": restartedAt,
					},
				},
			},
		},
	}
	for _, deployment := range []string{name, name + "-proxy"} {
		if err := s.mergePatch(ctx, identity, apiPath("apis/apps/v1/namespaces", namespace, "deployments", deployment), patch); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) deleteManagedSecret(ctx context.Context, identity userIdentity, namespace, name, secretName string) error {
	var secret map[string]any
	secretPath := apiPath("api/v1/namespaces", namespace, "secrets", secretName)
	if err := s.kubeJSON(ctx, identity, http.MethodGet, secretPath, nil, &secret); err != nil {
		return err
	}
	managedBy, _, _ := nestedString(secret, "metadata", "labels", managedByLabel)
	instance, _, _ := nestedString(secret, "metadata", "labels", instanceLabel)
	if managedBy != managedByValue || instance != name {
		return nil
	}
	return s.delete(ctx, identity, secretPath)
}

func readyCondition(claw map[string]any) (bool, string, string) {
	conditions, _, _ := nestedSlice(claw, "status", "conditions")
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok || condition["type"] != "Ready" {
			continue
		}
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		status, _ := condition["status"].(string)
		return status == "True", reason, message
	}
	return false, "", "Waiting for status"
}

func credentialProviders(claw map[string]any) []string {
	credentials, _, _ := nestedSlice(claw, "spec", "credentials")
	providers := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		credentialMap, ok := credential.(map[string]any)
		if !ok {
			continue
		}
		provider, _ := credentialMap["provider"].(string)
		if provider != "" {
			providers = append(providers, provider)
		}
	}
	return providers
}

func credentialSecretNames(claw map[string]any) []string {
	credentials, _, _ := nestedSlice(claw, "spec", "credentials")
	secretNames := []string{}
	for _, credential := range credentials {
		credentialMap, ok := credential.(map[string]any)
		if !ok {
			continue
		}
		secretRefs, _, _ := nestedSlice(credentialMap, "secretRef")
		for _, ref := range secretRefs {
			refMap, ok := ref.(map[string]any)
			if !ok {
				continue
			}
			name, _ := refMap["name"].(string)
			if name != "" {
				secretNames = appendUnique(secretNames, name)
			}
		}
	}
	return secretNames
}

func firstAgentName(claw map[string]any) string {
	agents, _, _ := nestedSlice(claw, "spec", "config", "raw", "agents", "list")
	if len(agents) == 0 {
		return ""
	}
	first, ok := agents[0].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := first["name"].(string)
	return name
}
