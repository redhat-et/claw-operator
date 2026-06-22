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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// -- helpers --

func newClaw() *Claw {
	return &Claw{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
}

func withAnnotation(c *Claw, key, val string) *Claw {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[key] = val
	return c
}

func withCred(c *Claw, cred CredentialSpec) {
	c.Spec.Credentials = append(c.Spec.Credentials, cred)
}

func apiKeyCred(name, provider string) CredentialSpec {
	return CredentialSpec{
		Name:     name,
		Type:     CredentialTypeAPIKey,
		Provider: provider,
		SecretRef: []SecretRefEntry{
			{Name: "my-secret", Key: "api-key"},
		},
		APIKey: &APIKeyConfig{Header: "x-api-key"},
	}
}

func gcpCred(name, provider string) CredentialSpec {
	return CredentialSpec{
		Name:     name,
		Type:     CredentialTypeGCP,
		Provider: provider,
		SecretRef: []SecretRefEntry{
			{Name: "my-secret", Key: "sa.json"},
		},
		GCP: &GCPConfig{Project: "my-project", Location: "us-central1"},
	}
}

// ctxWithCreateRequest wraps a context with a synthetic admission Create request.
func ctxWithCreateRequest(username string) context.Context {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			UserInfo:  authv1.UserInfo{Username: username},
		},
	}
	return admission.NewContextWithRequest(context.Background(), req)
}

// -- validator tests --

func TestValidateCredentialsDuplicateName(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	withCred(c, apiKeyCred("gemini", "google"))
	withCred(c, apiKeyCred("gemini", "anthropic")) // duplicate name

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for duplicate credential name, got nil")
	}
	if !containsMsg(err.Error(), "duplicate credential name") {
		t.Errorf("expected 'duplicate credential name' in error, got: %v", err)
	}
}

func TestValidateCredentialsGCPWithBearerOnlyProvider(t *testing.T) {
	bearers := []string{"openai", "xai", "openrouter", "openai-codex"}
	v := &ClawValidator{}
	for _, provider := range bearers {
		t.Run(provider, func(t *testing.T) {
			c := newClaw()
			withCred(c, gcpCred("cred-"+provider, provider))
			_, err := v.ValidateCreate(context.Background(), c)
			if err == nil {
				t.Fatalf("expected error for type:gcp + provider:%s, got nil", provider)
			}
			if !containsMsg(err.Error(), "bearer authentication") {
				t.Errorf("expected 'bearer authentication' in error, got: %v", err)
			}
		})
	}
}

func TestValidateCredentialsGCPWithGoogleAllowed(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	withCred(c, gcpCred("vertex-google", "google"))
	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected type:gcp + provider:google to be valid, got: %v", err)
	}
}

func TestValidateCredentialsGCPWithAnthropicAllowed(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	withCred(c, gcpCred("vertex-anthropic", "anthropic"))
	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected type:gcp + provider:anthropic to be valid, got: %v", err)
	}
}

func TestValidateCredentialsEmptySecretRefName(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	withCred(c, CredentialSpec{
		Name: "bad-cred",
		Type: CredentialTypeAPIKey,
		SecretRef: []SecretRefEntry{
			{Name: "", Key: "api-key"}, // empty name
		},
		APIKey: &APIKeyConfig{Header: "x-api-key"},
	})
	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for empty secretRef.name, got nil")
	}
	if !containsMsg(err.Error(), "secretRef.name must not be empty") {
		t.Errorf("expected 'secretRef.name must not be empty' in error, got: %v", err)
	}
}

func TestValidateMergeModeOverwriteWithoutAnnotation(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	c.Spec.Config = &ConfigSpec{MergeMode: ConfigModeOverwrite}
	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for mergeMode:overwrite without annotation, got nil")
	}
	if !containsMsg(err.Error(), AnnotationKeyConfirmOverwrite) {
		t.Errorf("expected annotation key in error message, got: %v", err)
	}
}

func TestValidateMergeModeOverwriteWithAnnotation(t *testing.T) {
	v := &ClawValidator{}
	c := withAnnotation(newClaw(), AnnotationKeyConfirmOverwrite, "true")
	c.Spec.Config = &ConfigSpec{MergeMode: ConfigModeOverwrite}
	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected mergeMode:overwrite with annotation to be valid, got: %v", err)
	}
}

func TestValidateMergeModeOverwriteWrongAnnotationValue(t *testing.T) {
	v := &ClawValidator{}
	c := withAnnotation(newClaw(), AnnotationKeyConfirmOverwrite, "yes")
	c.Spec.Config = &ConfigSpec{MergeMode: ConfigModeOverwrite}
	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for annotation value 'yes' (not 'true'), got nil")
	}
}

func TestValidateAgentFilesGitURLNotHTTPS(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	c.Spec.AgentFiles = &AgentFilesSpec{
		Git: &AgentFilesGitSource{URL: "http://github.com/org/repo"},
	}
	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for non-https git URL, got nil")
	}
	if !containsMsg(err.Error(), "https://") {
		t.Errorf("expected 'https://' in error, got: %v", err)
	}
}

func TestValidateAgentFilesGitURLHTTPS(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	c.Spec.AgentFiles = &AgentFilesSpec{
		Git: &AgentFilesGitSource{URL: "https://github.com/org/repo"},
	}
	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected https:// git URL to be valid, got: %v", err)
	}
}

func TestValidateVersionCalver(t *testing.T) {
	v := &ClawValidator{}

	valid := []string{"2026.6.8", "2026.12.31", "2026.6.8.1", "2025.1.1"}
	for _, ver := range valid {
		t.Run("valid:"+ver, func(t *testing.T) {
			c := newClaw()
			c.Spec.Version = ver
			_, err := v.ValidateCreate(context.Background(), c)
			if err != nil {
				t.Errorf("expected version %q to be valid, got: %v", ver, err)
			}
		})
	}

	invalid := []string{"latest", "dev-abc", "v1.2.3", "2026-6-8", "2026.6"}
	for _, ver := range invalid {
		t.Run("invalid:"+ver, func(t *testing.T) {
			c := newClaw()
			c.Spec.Version = ver
			_, err := v.ValidateCreate(context.Background(), c)
			if err == nil {
				t.Errorf("expected version %q to be invalid, got nil error", ver)
			}
		})
	}
}

func TestValidateVersionEmpty(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	// empty version is allowed (operator uses its built-in default)
	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected empty version to be valid, got: %v", err)
	}
}

func TestValidateUpdateCallsValidation(t *testing.T) {
	v := &ClawValidator{}
	old := newClaw()
	updated := newClaw()
	updated.Spec.Version = "not-calver"
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Fatal("expected ValidateUpdate to reject invalid version, got nil")
	}
}

func TestValidateDeleteAlwaysAllowed(t *testing.T) {
	v := &ClawValidator{}
	c := newClaw()
	_, err := v.ValidateDelete(context.Background(), c)
	if err != nil {
		t.Errorf("expected delete to always pass, got: %v", err)
	}
}

// -- defaulter tests --

func TestDefaulterSetsAuthModeWhenAbsent(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	if err := d.Default(context.Background(), c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if c.Spec.Auth == nil {
		t.Fatal("expected Spec.Auth to be set")
	}
	if c.Spec.Auth.Mode != AuthModeToken {
		t.Errorf("expected auth mode %q, got %q", AuthModeToken, c.Spec.Auth.Mode)
	}
}

func TestDefaulterDoesNotOverwriteExistingAuthMode(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	c.Spec.Auth = &AuthSpec{Mode: AuthModePassword}
	if err := d.Default(context.Background(), c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if c.Spec.Auth.Mode != AuthModePassword {
		t.Errorf("expected auth mode to remain %q, got %q", AuthModePassword, c.Spec.Auth.Mode)
	}
}

func TestDefaulterSetsMergeModeWhenConfigPresentButEmpty(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	c.Spec.Config = &ConfigSpec{} // present but MergeMode unset
	if err := d.Default(context.Background(), c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if c.Spec.Config.MergeMode != ConfigModeMerge {
		t.Errorf("expected mergeMode %q, got %q", ConfigModeMerge, c.Spec.Config.MergeMode)
	}
}

func TestDefaulterDoesNotOverwriteExistingMergeMode(t *testing.T) {
	d := &ClawDefaulter{}
	c := withAnnotation(newClaw(), AnnotationKeyConfirmOverwrite, "true")
	c.Spec.Config = &ConfigSpec{MergeMode: ConfigModeOverwrite}
	if err := d.Default(context.Background(), c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if c.Spec.Config.MergeMode != ConfigModeOverwrite {
		t.Errorf("expected mergeMode to remain %q, got %q", ConfigModeOverwrite, c.Spec.Config.MergeMode)
	}
}

func TestDefaulterSetsCreatedByLabelOnCreate(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	ctx := ctxWithCreateRequest("system:serviceaccount:claw-operator:controller")
	if err := d.Default(ctx, c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	got, ok := c.Labels["claw.redhat.com/created-by"]
	if !ok {
		t.Fatal("expected 'claw.redhat.com/created-by' label to be set")
	}
	if got == "" {
		t.Error("expected created-by label value to be non-empty")
	}
}

func TestDefaulterDoesNotOverwriteExistingCreatedByLabel(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	c.Labels = map[string]string{"claw.redhat.com/created-by": "alice"}
	ctx := ctxWithCreateRequest("bob")
	if err := d.Default(ctx, c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if got := c.Labels["claw.redhat.com/created-by"]; got != "alice" {
		t.Errorf("expected created-by to remain 'alice', got %q", got)
	}
}

func TestDefaulterNoCreatedByLabelWithoutAdmissionContext(t *testing.T) {
	d := &ClawDefaulter{}
	c := newClaw()
	// plain context — no admission request
	if err := d.Default(context.Background(), c); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}
	if _, ok := c.Labels["claw.redhat.com/created-by"]; ok {
		t.Error("did not expect created-by label when no admission request in context")
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"alice", "alice"},
		{"system:serviceaccount:ns:sa", "system_serviceaccount_ns_sa"},
		{"user@example.com", "user_example.com"},
		// Kubernetes label values must start and end with [A-Za-z0-9].
		{"@admin", "admin"},  // leading non-alnum stripped
		{"admin:", "admin"},  // trailing non-alnum stripped
		{":admin:", "admin"}, // both stripped
		{strings.Repeat("a", 70), strings.Repeat("a", 63)}, // truncated
		// Truncation + trailing non-alnum: 63rd char must not leave a trailing _.
		{strings.Repeat("a", 62) + ":", strings.Repeat("a", 62)},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeLabelValue(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if len(got) > 63 {
				t.Errorf("sanitizeLabelValue(%q) length %d > 63", tc.input, len(got))
			}
		})
	}
}

// containsMsg reports whether the error string contains the given substring.
func containsMsg(s, substr string) bool {
	return strings.Contains(s, substr)
}
