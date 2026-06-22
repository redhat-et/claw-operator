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
	"fmt"
	"regexp"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// AnnotationKeyConfirmOverwrite must be set to "true" on a Claw CR when
// spec.config.mergeMode is "overwrite". This guards against accidental loss
// of user config — overwrite wipes the PVC config on every pod restart.
const AnnotationKeyConfirmOverwrite = "claw.sandbox.redhat.com/confirm-overwrite"

// calverRE matches CalVer date-versions of the form YYYY.M.D with an optional
// additional dot-separated patch segment (e.g. "2026.6.8" or "2026.12.31.1").
var calverRE = regexp.MustCompile(`^\d{4}\.\d{1,2}\.\d{1,2}(\.\d+)*$`)

// labelValueRE matches characters invalid in Kubernetes label values (non-alphanumeric/.-_).
var labelValueRE = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// bearerOnlyProviders is the set of known providers that support only bearer
// authentication. Combining any of these with type: gcp is invalid because
// they have no Vertex AI SDK integration and no GCP credential path.
var bearerOnlyProviders = map[string]bool{
	"openai":       true,
	"xai":          true,
	"openrouter":   true,
	"openai-codex": true,
}

// SetupWebhookWithManager registers validating and mutating webhooks for Claw.
//
// +kubebuilder:webhook:path=/mutate-claw-sandbox-redhat-com-v1alpha1-claw,mutating=true,failurePolicy=fail,sideEffects=None,groups=claw.sandbox.redhat.com,resources=claws,verbs=create;update,versions=v1alpha1,name=mclaw.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-claw-sandbox-redhat-com-v1alpha1-claw,mutating=false,failurePolicy=fail,sideEffects=None,groups=claw.sandbox.redhat.com,resources=claws,verbs=create;update,versions=v1alpha1,name=vclaw.kb.io,admissionReviewVersions=v1
func (r *Claw) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithDefaulter(&ClawDefaulter{}).
		WithValidator(&ClawValidator{}).
		Complete()
}

// ClawValidator validates Claw resources at admission time.
type ClawValidator struct{}

var _ admission.CustomValidator = &ClawValidator{}

func (v *ClawValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, validateClaw(obj.(*Claw))
}

func (v *ClawValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return nil, validateClaw(newObj.(*Claw))
}

func (v *ClawValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ClawDefaulter injects safe defaults into Claw resources at admission time.
type ClawDefaulter struct{}

var _ admission.CustomDefaulter = &ClawDefaulter{}

// Default sets:
//   - spec.auth.mode = "token" when spec.auth is nil
//   - spec.config.mergeMode = "merge" when spec.config exists but mergeMode is unset
//   - claw.redhat.com/created-by label from the admission user, on Create only
func (d *ClawDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	claw := obj.(*Claw)

	// Stamp the creating user only at creation time (immutable once set).
	req, err := admission.RequestFromContext(ctx)
	if err == nil && req.Operation == admissionv1.Create {
		username := sanitizeLabelValue(req.UserInfo.Username)
		if username != "" {
			if claw.Labels == nil {
				claw.Labels = make(map[string]string)
			}
			if _, exists := claw.Labels["claw.redhat.com/created-by"]; !exists {
				claw.Labels["claw.redhat.com/created-by"] = username
			}
		}
	}

	// Default auth mode when the whole auth block is absent.
	if claw.Spec.Auth == nil {
		claw.Spec.Auth = &AuthSpec{Mode: AuthModeToken}
	}

	// Default mergeMode when the config block is present but mergeMode is unset.
	if claw.Spec.Config != nil && claw.Spec.Config.MergeMode == "" {
		claw.Spec.Config.MergeMode = ConfigModeMerge
	}

	return nil
}

// validateClaw runs all admission-time validation rules against a Claw CR.
func validateClaw(claw *Claw) error {
	var errs field.ErrorList //nolint:prealloc

	errs = append(errs, validateCredentials(claw)...)
	errs = append(errs, validateMergeMode(claw)...)
	errs = append(errs, validateAgentFilesGitURL(claw)...)
	errs = append(errs, validateVersion(claw)...)

	if len(errs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		GroupVersion.WithKind("Claw").GroupKind(),
		claw.Name,
		errs,
	)
}

// validateCredentials checks:
//   - no duplicate credential names (rule 5)
//   - no invalid type/provider combination, e.g. type:gcp + provider:openai (rule 1)
//   - no empty secretRef.name entries (rule 2, defense-in-depth over CRD MinLength=1)
func validateCredentials(claw *Claw) field.ErrorList {
	var errs field.ErrorList
	credPath := field.NewPath("spec", "credentials")
	seenNames := map[string]bool{}

	for i, cred := range claw.Spec.Credentials {
		idx := credPath.Index(i)

		// Rule 5: duplicate name
		if seenNames[cred.Name] {
			errs = append(errs, field.Invalid(idx.Child("name"), cred.Name,
				"duplicate credential name"))
		}
		seenNames[cred.Name] = true

		// Rule 1: type:gcp is incompatible with bearer-only providers
		if cred.Type == CredentialTypeGCP && bearerOnlyProviders[cred.Provider] {
			errs = append(errs, field.Invalid(idx.Child("type"), string(cred.Type),
				fmt.Sprintf("provider %q uses bearer authentication and cannot be combined with type gcp",
					cred.Provider)))
		}

		// Rule 2: secretRef.name must not be empty
		for j, ref := range cred.SecretRef {
			if ref.Name == "" {
				errs = append(errs, field.Required(
					idx.Child("secretRef").Index(j).Child("name"),
					"secretRef.name must not be empty"))
			}
		}
	}
	return errs
}

// validateMergeMode rejects mergeMode:overwrite unless the confirmation annotation is present.
func validateMergeMode(claw *Claw) field.ErrorList {
	if claw.Spec.Config == nil || claw.Spec.Config.MergeMode != ConfigModeOverwrite {
		return nil
	}
	if claw.Annotations[AnnotationKeyConfirmOverwrite] == "true" {
		return nil
	}
	return field.ErrorList{
		field.Forbidden(
			field.NewPath("spec", "config", "mergeMode"),
			fmt.Sprintf("mergeMode %q wipes all user config on every pod restart; "+
				"add annotation %s=true to confirm this is intentional",
				ConfigModeOverwrite, AnnotationKeyConfirmOverwrite),
		),
	}
}

// validateAgentFilesGitURL rejects git URLs that do not use HTTPS.
// The CRD schema already enforces this pattern, but the webhook provides an
// earlier, clearer rejection message before the object reaches etcd.
func validateAgentFilesGitURL(claw *Claw) field.ErrorList {
	if claw.Spec.AgentFiles == nil || claw.Spec.AgentFiles.Git == nil {
		return nil
	}
	url := claw.Spec.AgentFiles.Git.URL
	if url != "" && !strings.HasPrefix(url, "https://") {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "agentFiles", "git", "url"), url,
				"must start with https://"),
		}
	}
	return nil
}

// validateVersion rejects spec.version values that do not match the CalVer
// date-version format (YYYY.M.D or YYYY.M.D.patch).
func validateVersion(claw *Claw) field.ErrorList {
	v := claw.Spec.Version
	if v == "" || calverRE.MatchString(v) {
		return nil
	}
	return field.ErrorList{
		field.Invalid(
			field.NewPath("spec", "version"), v,
			`must be a CalVer date-version (e.g. "2026.6.8")`),
	}
}

// sanitizeLabelValue replaces characters invalid in Kubernetes label values with
// underscores, truncates to 63 characters, then strips any leading/trailing
// non-alphanumeric characters to satisfy the label value format requirement
// (must start and end with [A-Za-z0-9]). Returns empty string if nothing remains.
func sanitizeLabelValue(s string) string {
	result := labelValueRE.ReplaceAllString(s, "_")
	if len(result) > 63 {
		result = result[:63]
	}
	result = strings.TrimLeft(result, "._-")
	result = strings.TrimRight(result, "._-")
	return result
}
