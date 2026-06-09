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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

var (
	namespaceRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsCharRE   = regexp.MustCompile(`[^a-z0-9-]+`)
	providers   = map[string]providerOption{
		"anthropic": {
			CredentialName:     "anthropic",
			CredentialProvider: "anthropic",
			ModelProvider:      "anthropic",
			SecretKey:          apiKeySecretKey,
		},
		"anthropic-vertex": {
			CredentialName:     "anthropic-vertex",
			CredentialProvider: "anthropic",
			CredentialType:     "gcp",
			ModelProvider:      "anthropic-vertex",
			SecretKey:          gcpSecretKey,
			RequiresGCP:        true,
		},
		"google": {
			CredentialName:     "google",
			CredentialProvider: "google",
			ModelProvider:      "google",
			SecretKey:          apiKeySecretKey,
		},
		"google-vertex": {
			CredentialName:     "google-vertex",
			CredentialProvider: "google",
			CredentialType:     "gcp",
			ModelProvider:      "google",
			SecretKey:          gcpSecretKey,
			RequiresGCP:        true,
		},
		"openai": {
			CredentialName:     "openai",
			CredentialProvider: "openai",
			ModelProvider:      "openai",
			SecretKey:          apiKeySecretKey,
		},
		"openrouter": {
			CredentialName:     "openrouter",
			CredentialProvider: "openrouter",
			ModelProvider:      "openrouter",
			SecretKey:          apiKeySecretKey,
		},
		"xai": {
			CredentialName:     "xai",
			CredentialProvider: "xai",
			ModelProvider:      "xai",
			SecretKey:          apiKeySecretKey,
		},
	}
)

type providerOption struct {
	CredentialName     string
	CredentialProvider string
	CredentialType     string
	ModelProvider      string
	SecretKey          string
	RequiresGCP        bool
}

func currentUser(r *http.Request) (string, error) {
	for _, header := range []string{
		"X-Forwarded-User",
		"X-Auth-Request-User",
		"X-Forwarded-Preferred-Username",
		"X-Forwarded-Email",
	} {
		if user := strings.TrimSpace(r.Header.Get(header)); user != "" {
			return user, nil
		}
	}
	if user := strings.TrimSpace(os.Getenv("DEVELOPER_USERNAME")); user != "" {
		return user, nil
	}
	return "", errors.New("OpenShift username was not forwarded to the deployer")
}

func currentIdentity(r *http.Request) (userIdentity, error) {
	user, err := currentUser(r)
	if err != nil {
		return userIdentity{}, err
	}
	return userIdentity{
		Name:   user,
		Groups: impersonationGroups(r),
	}, nil
}

func impersonationGroups(r *http.Request) []string {
	groups := []string{}
	for _, header := range []string{"X-Forwarded-Groups", "X-Auth-Request-Groups"} {
		for _, value := range r.Header.Values(header) {
			for _, group := range strings.Split(value, ",") {
				group = strings.TrimSpace(group)
				if group != "" {
					groups = appendUnique(groups, group)
				}
			}
		}
	}
	groups = appendUnique(groups, "system:authenticated")
	groups = appendUnique(groups, "system:authenticated:oauth")
	return groups
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		if child, ok := value.(map[string]any); ok {
			dst[key] = cloneMap(child)
			continue
		}
		dst[key] = value
	}
	return dst
}

func ensureMap(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = map[string]any{}
		parent[key] = child
	}
	return child
}

func validateNamespace(namespace string) error {
	return validateResourceName(namespace, "namespace")
}

func validateResourceName(name, field string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(name) > 63 || !namespaceRE.MatchString(name) {
		return fmt.Errorf("%s must be a valid Kubernetes resource name", field)
	}
	return nil
}

func validateGCPServiceAccountJSON(value string) error {
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return errors.New(`valid JSON with type "service_account" or "authorized_user" is required`)
	}
	if payload.Type != "service_account" && payload.Type != "authorized_user" {
		return errors.New(`valid JSON with type "service_account" or "authorized_user" is required`)
	}
	return nil
}

func normalizeModelRef(provider, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultModelForProvider(provider)
	}
	modelProvider := modelProviderFor(provider)
	if strings.Contains(model, "/") {
		if provider == "openrouter" && !strings.HasPrefix(model, "openrouter/") {
			return "openrouter/" + model
		}
		if provider == "anthropic-vertex" && strings.HasPrefix(model, "anthropic/") {
			return "anthropic-vertex/" + strings.TrimPrefix(model, "anthropic/")
		}
		return model
	}
	if provider == "openrouter" {
		return "openrouter/" + model
	}
	return modelProvider + "/" + model
}

func modelProviderFor(provider string) string {
	if option, ok := providers[provider]; ok && option.ModelProvider != "" {
		return option.ModelProvider
	}
	return provider
}

func defaultModelForProvider(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "anthropic-vertex":
		return "claude-sonnet-4-6"
	case "google":
		return "gemini-3.1-pro-preview"
	case "google-vertex":
		return "gemini-3.1-pro-preview"
	case "openai":
		return "gpt-5.5"
	case "openrouter":
		return "openrouter/anthropic/claude-sonnet-4-6"
	case "xai":
		return "grok-4.3"
	default:
		return "anthropic/claude-sonnet-4-6"
	}
}

func secretName(name, provider string) string {
	return "openclaw-" + name + "-" + provider + "-api-key"
}

func secretNameForRequest(req provisionRequest) string {
	option := providers[req.Provider]
	if option.RequiresGCP {
		return "openclaw-" + req.Name + "-" + option.CredentialName + "-gcp"
	}
	return secretName(req.Name, option.CredentialName)
}

func agentNameFromClawName(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	if len(words) == 0 {
		return "OpenClaw"
	}
	return strings.Join(words, " ")
}

func allowedNamespaceForUser(username, suffix string) string {
	name := strings.ToLower(strings.TrimSpace(username))
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	name = dnsCharRE.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if suffix != "" && !strings.HasSuffix(name, suffix) {
		name += suffix
	}
	return name
}

func apiPath(parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, subpart := range strings.Split(part, "/") {
			if subpart != "" {
				escaped = append(escaped, url.PathEscape(subpart))
			}
		}
	}
	return "/" + path.Join(escaped...)
}

func nestedString(obj map[string]any, fields ...string) (string, bool, error) {
	v, ok, err := nestedValue(obj, fields...)
	if err != nil || !ok {
		return "", ok, err
	}
	s, ok := v.(string)
	return s, ok, nil
}

func nestedSlice(obj map[string]any, fields ...string) ([]any, bool, error) {
	v, ok, err := nestedValue(obj, fields...)
	if err != nil || !ok {
		return nil, ok, err
	}
	s, ok := v.([]any)
	return s, ok, nil
}

func nestedMap(obj map[string]any, fields ...string) (map[string]any, bool, error) {
	v, ok, err := nestedValue(obj, fields...)
	if err != nil || !ok {
		return nil, ok, err
	}
	m, ok := v.(map[string]any)
	return m, ok, nil
}

func nestedValue(obj map[string]any, fields ...string) (any, bool, error) {
	var current any = obj
	for _, field := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("field %q is not an object", field)
		}
		current, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	return current, true, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
