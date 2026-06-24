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
	"path/filepath"
	"strings"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigMap key prefixes for workspace files and skills.
const (
	workspaceKeyPrefix = "_ws_"
	skillKeyPrefix     = "_skill_"
	pathSeparatorCode  = "--"
)

// builtinSkillNames lists operator-managed skill directory names that cannot
// be used as user skill names.
var builtinSkillNames = map[string]bool{
	"platform":   true,
	"kubernetes": true,
}

// encodeWorkspacePath encodes a workspace-relative path for use as a
// ConfigMap key by replacing "/" with "--".
func encodeWorkspacePath(p string) string {
	return strings.ReplaceAll(p, "/", pathSeparatorCode)
}

// validateWorkspaceFiles checks that all workspace file paths are safe and do
// not conflict with operator-managed paths.
func validateWorkspaceFiles(files map[string]string) error {
	for p := range files {
		if p == "" {
			return fmt.Errorf("workspace file path must not be empty")
		}
		if filepath.IsAbs(p) {
			return fmt.Errorf("workspace file path %q is invalid: must not be absolute", p)
		}
		if strings.Contains(p, "..") {
			return fmt.Errorf("workspace file path %q is invalid: must not contain \"..\"", p)
		}
		if strings.Contains(p, pathSeparatorCode) {
			return fmt.Errorf("workspace file path %q is invalid: must not contain %q (reserved as path encoding delimiter)", p, pathSeparatorCode)
		}
		cleaned := filepath.Clean(p)
		if strings.HasPrefix(cleaned, "skills/platform/") || cleaned == "skills/platform" {
			return fmt.Errorf("workspace file path %q conflicts with operator-managed platform skill", p)
		}
		if strings.HasPrefix(cleaned, "skills/kubernetes/") || cleaned == "skills/kubernetes" {
			return fmt.Errorf("workspace file path %q conflicts with operator-managed kubernetes skill", p)
		}
	}
	return nil
}

// validateSkillNames checks that all skill names are valid directory components
// and do not conflict with builtin operator skills.
func validateSkillNames(skills map[string]string) error {
	for name := range skills {
		if name == "" {
			return fmt.Errorf("skill name must not be empty")
		}
		if name == "." || name == ".." {
			return fmt.Errorf("skill name %q is invalid: must not be %q", name, name)
		}
		if strings.Contains(name, "/") {
			return fmt.Errorf("skill name %q is invalid: must not contain \"/\"", name)
		}
		if strings.Contains(name, pathSeparatorCode) {
			return fmt.Errorf("skill name %q is invalid: must not contain %q (reserved as path encoding delimiter)", name, pathSeparatorCode)
		}
		if builtinSkillNames[name] {
			return fmt.Errorf("skill name %q conflicts with builtin operator skill", name)
		}
	}
	return nil
}

// validateSkills validates all skill sources and checks for cross-field collisions.
func validateSkills(instance *clawv1alpha1.Claw) error {
	if instance.Spec.Skills == nil {
		return nil
	}
	s := instance.Spec.Skills
	if err := validateSkillNames(s.Content); err != nil {
		return err
	}

	allNames := make(map[string]string, len(s.Content))
	for name := range s.Content {
		allNames[name] = "spec.skills.content"
	}

	if err := validateSkillImages(s.Images, allNames); err != nil {
		return err
	}
	for _, si := range s.Images {
		allNames[si.Name] = "spec.skills.images"
	}

	return validateSkillConfigMaps(s.ConfigMaps)
}

// validateSkillImages checks that all skill image entries have valid names,
// non-empty image references, no duplicates, and no collisions with other skill sources.
func validateSkillImages(skillImages []clawv1alpha1.SkillImageSpec, allNames map[string]string) error {
	if len(skillImages) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(skillImages))
	for _, si := range skillImages {
		if si.Name == "" {
			return fmt.Errorf("skill image name must not be empty")
		}
		if si.Name == "." || si.Name == ".." {
			return fmt.Errorf("skill image name %q is invalid: must not be %q", si.Name, si.Name)
		}
		if strings.Contains(si.Name, "/") {
			return fmt.Errorf("skill image name %q is invalid: must not contain \"/\"", si.Name)
		}
		if strings.Contains(si.Name, pathSeparatorCode) {
			return fmt.Errorf(
				"skill image name %q is invalid: must not contain %q (reserved as path encoding delimiter)",
				si.Name, pathSeparatorCode,
			)
		}
		if builtinSkillNames[si.Name] {
			return fmt.Errorf("skill image name %q conflicts with builtin operator skill", si.Name)
		}
		if seen[si.Name] {
			return fmt.Errorf("duplicate skill image name %q", si.Name)
		}
		seen[si.Name] = true
		if source, exists := allNames[si.Name]; exists {
			return fmt.Errorf(
				"skill image name %q conflicts with %s entry",
				si.Name, source,
			)
		}
		if si.Image == "" {
			return fmt.Errorf("skill image %q has empty image reference", si.Name)
		}
		for _, ps := range si.ImagePullSecrets {
			if ps.Name == "" {
				return fmt.Errorf("skill image %q has imagePullSecret with empty name", si.Name)
			}
		}
	}
	return nil
}

// validateSkillConfigMaps checks that ConfigMap refs have non-empty names
// and no duplicates. Key-level collision detection happens at injection time.
func validateSkillConfigMaps(refs []clawv1alpha1.SkillConfigMapRef) error {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.Name == "" {
			return fmt.Errorf("skill configMap ref name must not be empty")
		}
		if seen[ref.Name] {
			return fmt.Errorf("duplicate skill configMap ref %q", ref.Name)
		}
		seen[ref.Name] = true
	}
	return nil
}

// injectWorkspaceFiles validates workspace file paths and writes _ws_ prefixed
// keys into the gateway ConfigMap.
func injectWorkspaceFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil || len(instance.Spec.Workspace.Files) == 0 {
		return nil
	}

	if err := validateWorkspaceFiles(instance.Spec.Workspace.Files); err != nil {
		return err
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	for p, content := range instance.Spec.Workspace.Files {
		key := workspaceKeyPrefix + encodeWorkspacePath(p)
		if err := unstructured.SetNestedField(cmObj.Object, content, "data", key); err != nil {
			return fmt.Errorf("failed to set workspace file %q in ConfigMap: %w", p, err)
		}
	}
	return nil
}

// injectSkillFiles writes _skill_ prefixed keys into the gateway ConfigMap
// for inline content skills. Validation is done separately by validateSkills.
func injectSkillFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Skills == nil || len(instance.Spec.Skills.Content) == 0 {
		return nil
	}

	if err := validateSkillNames(instance.Spec.Skills.Content); err != nil {
		return err
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	for name, content := range instance.Spec.Skills.Content {
		key := skillKeyPrefix + name
		if err := unstructured.SetNestedField(cmObj.Object, content, "data", key); err != nil {
			return fmt.Errorf("failed to set skill %q in ConfigMap: %w", name, err)
		}
	}
	return nil
}

// injectSkillConfigMapFiles reads each referenced ConfigMap from the cluster and
// writes its keys as _skill_ entries into the gateway ConfigMap.
func (r *ClawResourceReconciler) injectSkillConfigMapFiles(
	ctx context.Context,
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if instance.Spec.Skills == nil || len(instance.Spec.Skills.ConfigMaps) == 0 {
		return nil
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	// Pre-populate claimed names from other skill sources to detect collisions.
	claimed := make(map[string]string)
	for name := range instance.Spec.Skills.Content {
		claimed[name] = "spec.skills.content"
	}
	for _, si := range instance.Spec.Skills.Images {
		claimed[si.Name] = "spec.skills.images"
	}

	for _, ref := range instance.Spec.Skills.ConfigMaps {
		var sourceCM corev1.ConfigMap
		if err := r.UserSecretReader.Get(ctx, client.ObjectKey{
			Name:      ref.Name,
			Namespace: instance.Namespace,
		}, &sourceCM); err != nil {
			return fmt.Errorf("failed to get skill ConfigMap %q: %w", ref.Name, err)
		}
		for name, content := range sourceCM.Data {
			if err := validateSkillName(name); err != nil {
				return fmt.Errorf("skill ConfigMap %q key %q: %w", ref.Name, name, err)
			}
			if source, exists := claimed[name]; exists {
				return fmt.Errorf("skill ConfigMap %q key %q conflicts with %s entry", ref.Name, name, source)
			}
			claimed[name] = fmt.Sprintf("configMap %q", ref.Name)
			key := skillKeyPrefix + name
			if err := unstructured.SetNestedField(cmObj.Object, content, "data", key); err != nil {
				return fmt.Errorf("failed to set skill %q from ConfigMap %q: %w", name, ref.Name, err)
			}
		}
	}
	return nil
}

// validateSkillName checks a single skill name for validity.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("skill name %q is invalid: must not be %q", name, name)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("skill name %q is invalid: must not contain \"/\"", name)
	}
	if strings.Contains(name, pathSeparatorCode) {
		return fmt.Errorf(
			"skill name %q is invalid: must not contain %q (reserved as path encoding delimiter)",
			name, pathSeparatorCode,
		)
	}
	if builtinSkillNames[name] {
		return fmt.Errorf("skill name %q conflicts with builtin operator skill", name)
	}
	return nil
}

// injectSkipBootstrap sets agents.defaults.skipBootstrap in operator.json
// when spec.workspace.skipBootstrap is true.
func injectSkipBootstrap(config map[string]any, instance *clawv1alpha1.Claw) {
	if instance.Spec.Workspace != nil && instance.Spec.Workspace.SkipBootstrap {
		setNestedValue(config, true, "agents", "defaults", "skipBootstrap")
	}
}

// injectBootstrapHook configures the bootstrap-extra-files hook to load
// BOOTSTRAP.md from .operator/ instead of the workspace root. This avoids
// OpenClaw 6.1+'s reconciliation that deletes root BOOTSTRAP.md when it
// detects completion evidence (custom SOUL.md or skills).
func injectBootstrapHook(config map[string]any) {
	if instance, ok := config["hooks"]; ok {
		if hooks, ok := instance.(map[string]any); ok {
			if internal, ok := hooks["internal"]; ok {
				if internalMap, ok := internal.(map[string]any); ok {
					if entries, ok := internalMap["entries"]; ok {
						if entriesMap, ok := entries.(map[string]any); ok {
							if _, exists := entriesMap["bootstrap-extra-files"]; exists {
								return
							}
						}
					}
				}
			}
		}
	}
	setNestedValue(config, true, "hooks", "internal", "enabled")
	setNestedValue(config, map[string]any{
		"enabled": true,
		"paths":   []any{".operator/BOOTSTRAP.md"},
	}, "hooks", "internal", "entries", "bootstrap-extra-files")
}
