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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func makeGatewayDeployment() []*unstructured.Unstructured {
	dep := &unstructured.Unstructured{}
	dep.SetKind(DeploymentKind)
	dep.SetName(getClawDeploymentName(testInstanceName))
	dep.Object["spec"] = map[string]any{
		"template": map[string]any{
			"metadata": map[string]any{},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":         ClawGatewayContainerName,
						"volumeMounts": []any{},
					},
				},
				"volumes": []any{},
			},
		},
	}
	return []*unstructured.Unstructured{dep}
}

func TestConfigurePersonaGuard(t *testing.T) {
	t.Run("should mount persona files read-only into workspace", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PersonaRef: &clawv1alpha1.PersonaRef{
						Name: "finance-guardrails",
					},
				},
			},
		}

		keys := []string{"AGENTS.md", "SOUL.md"}
		require.NoError(t, configurePersonaGuard(objects, instance, keys))

		// Verify volume
		volumes, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "volumes",
		)
		require.Len(t, volumes, 1)
		vol := volumes[0].(map[string]any)
		assert.Equal(t, personaGuardVolumeName, vol["name"])
		cmRef := vol["configMap"].(map[string]any)
		assert.Equal(t, "finance-guardrails", cmRef["name"])

		// Verify volumeMounts
		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		container := containers[0].(map[string]any)
		mounts := container["volumeMounts"].([]any)
		require.Len(t, mounts, 2)

		agents := mounts[0].(map[string]any)
		assert.Equal(t, personaGuardVolumeName, agents["name"])
		assert.Equal(t, workspaceDir+"/AGENTS.md", agents["mountPath"])
		assert.Equal(t, "AGENTS.md", agents["subPath"])
		assert.Equal(t, true, agents["readOnly"])

		soul := mounts[1].(map[string]any)
		assert.Equal(t, personaGuardVolumeName, soul["name"])
		assert.Equal(t, workspaceDir+"/SOUL.md", soul["mountPath"])
		assert.Equal(t, "SOUL.md", soul["subPath"])
		assert.Equal(t, true, soul["readOnly"])
	})

	t.Run("should be no-op when restrictions are nil", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		}

		require.NoError(t, configurePersonaGuard(objects, instance, nil))

		volumes, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "volumes",
		)
		assert.Empty(t, volumes)
	})

	t.Run("should be no-op when personaRef is nil", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{},
			},
		}

		require.NoError(t, configurePersonaGuard(objects, instance, nil))

		volumes, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "volumes",
		)
		assert.Empty(t, volumes)
	})

	t.Run("should handle single key", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PersonaRef: &clawv1alpha1.PersonaRef{Name: "minimal"},
				},
			},
		}

		keys := []string{"SOUL.md"}
		require.NoError(t, configurePersonaGuard(objects, instance, keys))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		container := containers[0].(map[string]any)
		mounts := container["volumeMounts"].([]any)
		require.Len(t, mounts, 1)

		soul := mounts[0].(map[string]any)
		assert.Equal(t, workspaceDir+"/SOUL.md", soul["mountPath"])
		assert.Equal(t, true, soul["readOnly"])
	})
}

func TestValidatePersonaKeys(t *testing.T) {
	t.Run("valid keys", func(t *testing.T) {
		assert.NoError(t, validatePersonaKeys([]string{"AGENTS.md", "SOUL.md"}))
		assert.NoError(t, validatePersonaKeys([]string{"my-config.yaml"}))
	})

	t.Run("rejects path separators", func(t *testing.T) {
		assert.Error(t, validatePersonaKeys([]string{"subdir/AGENTS.md"}))
		assert.Error(t, validatePersonaKeys([]string{"sub\\dir"}))
	})

	t.Run("rejects dot-dot", func(t *testing.T) {
		assert.Error(t, validatePersonaKeys([]string{".."}))
	})

	t.Run("rejects dot", func(t *testing.T) {
		assert.Error(t, validatePersonaKeys([]string{"."}))
	})

	t.Run("rejects empty key", func(t *testing.T) {
		assert.Error(t, validatePersonaKeys([]string{""}))
	})
}

func TestValidateReadOnlyPaths(t *testing.T) {
	t.Run("valid file paths", func(t *testing.T) {
		assert.NoError(t, validateReadOnlyPaths([]string{"SOUL.md", "TOOLS.md"}))
	})

	t.Run("valid directory paths", func(t *testing.T) {
		assert.NoError(t, validateReadOnlyPaths([]string{"skills/managed/", "skills/managed/**"}))
	})

	t.Run("valid nested file", func(t *testing.T) {
		assert.NoError(t, validateReadOnlyPaths([]string{"skills/hr-policy/SKILL.md"}))
	})

	t.Run("rejects empty path", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{""}))
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{"/etc/passwd"}))
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{"../etc/passwd"}))
		assert.Error(t, validateReadOnlyPaths([]string{"skills/../../etc"}))
	})

	t.Run("rejects unsupported globs", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{"*.md"}))
		assert.Error(t, validateReadOnlyPaths([]string{"skills/*/SKILL.md"}))
		assert.Error(t, validateReadOnlyPaths([]string{"config[0].json"}))
	})

	t.Run("rejects bare slash", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{"/"}))
	})

	t.Run("rejects directory marker on file-like path", func(t *testing.T) {
		assert.Error(t, validateReadOnlyPaths([]string{"SOUL.md/"}))
		assert.Error(t, validateReadOnlyPaths([]string{"skills/hr-policy/SKILL.md/**"}))
	})
}

func TestSortedPersonaKeys(t *testing.T) {
	data := map[string]string{
		"SOUL.md":   "soul content",
		"AGENTS.md": "agents content",
		"CONFIG.md": "config content",
	}
	keys := sortedPersonaKeys(data)
	assert.Equal(t, []string{"AGENTS.md", "CONFIG.md", "SOUL.md"}, keys)
}

func TestPluginInstallationDisabled(t *testing.T) {
	t.Run("returns false when restrictions are nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		assert.False(t, pluginInstallationDisabled(instance))
	})

	t.Run("returns false when pluginInstallation is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{},
			},
		}
		assert.False(t, pluginInstallationDisabled(instance))
	})

	t.Run("returns false when pluginInstallation is true", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PluginInstallation: ptr.To(true),
				},
			},
		}
		assert.False(t, pluginInstallationDisabled(instance))
	})

	t.Run("returns true when pluginInstallation is false", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PluginInstallation: ptr.To(false),
				},
			},
		}
		assert.True(t, pluginInstallationDisabled(instance))
	})
}

func TestStampPersonaConfigHash(t *testing.T) {
	t.Run("should stamp hash annotation on gateway deployment", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		}
		data := map[string]string{
			"AGENTS.md": "You are a finance assistant.",
			"SOUL.md":   "Never modify your config.",
		}

		require.NoError(t, stampPersonaConfigHash(objects, instance, data))

		annotations, _, _ := unstructured.NestedStringMap(
			objects[0].Object, "spec", "template", "metadata", "annotations",
		)
		hash, ok := annotations[clawv1alpha1.AnnotationKeyPersonaConfigHash]
		assert.True(t, ok, "persona config hash annotation should be set")
		assert.Len(t, hash, 64, "should be a SHA-256 hex string")
	})

	t.Run("should be no-op when persona data is empty", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		}

		require.NoError(t, stampPersonaConfigHash(objects, instance, nil))

		annotations, _, _ := unstructured.NestedStringMap(
			objects[0].Object, "spec", "template", "metadata", "annotations",
		)
		_, ok := annotations[clawv1alpha1.AnnotationKeyPersonaConfigHash]
		assert.False(t, ok, "no annotation should be set")
	})

	t.Run("should produce different hashes for different content", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		}

		objects1 := makeGatewayDeployment()
		require.NoError(t, stampPersonaConfigHash(objects1, instance, map[string]string{
			"SOUL.md": "version 1",
		}))

		objects2 := makeGatewayDeployment()
		require.NoError(t, stampPersonaConfigHash(objects2, instance, map[string]string{
			"SOUL.md": "version 2",
		}))

		ann1, _, _ := unstructured.NestedStringMap(
			objects1[0].Object, "spec", "template", "metadata", "annotations",
		)
		ann2, _, _ := unstructured.NestedStringMap(
			objects2[0].Object, "spec", "template", "metadata", "annotations",
		)
		assert.NotEqual(t, ann1[clawv1alpha1.AnnotationKeyPersonaConfigHash],
			ann2[clawv1alpha1.AnnotationKeyPersonaConfigHash])
	})
}

// --- Integration test: persona guard with full reconciliation ---

func TestPersonaGuardIntegration(t *testing.T) {
	t.Run("should mount persona files read-only on gateway deployment", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		// Create persona ConfigMap
		personaCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-guardrails",
				Namespace: namespace,
			},
			Data: map[string]string{
				"AGENTS.md": "You are a test assistant.",
				"SOUL.md":   "Never modify config files.",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, personaCM))
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, personaCM)
		})

		// Create API key Secret
		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		// Create Claw instance with restrictions.personaRef
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testInstanceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "anthropic",
						Provider: "anthropic",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
					},
				},
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PersonaRef: &clawv1alpha1.PersonaRef{
						Name: "test-guardrails",
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		// Reconcile
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify the gateway deployment has persona guard mounts
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		gateway := findContainer(deployment, ClawGatewayContainerName)
		require.NotNil(t, gateway, "gateway container should exist")

		// Check for persona guard volume mounts
		var agentsMount, soulMount *corev1.VolumeMount
		for i := range gateway.VolumeMounts {
			vm := &gateway.VolumeMounts[i]
			switch vm.MountPath {
			case workspaceDir + "/AGENTS.md":
				agentsMount = vm
			case workspaceDir + "/SOUL.md":
				soulMount = vm
			}
		}

		require.NotNil(t, agentsMount, "AGENTS.md mount should exist")
		assert.Equal(t, personaGuardVolumeName, agentsMount.Name)
		assert.Equal(t, "AGENTS.md", agentsMount.SubPath)
		assert.True(t, agentsMount.ReadOnly)

		require.NotNil(t, soulMount, "SOUL.md mount should exist")
		assert.Equal(t, personaGuardVolumeName, soulMount.Name)
		assert.Equal(t, "SOUL.md", soulMount.SubPath)
		assert.True(t, soulMount.ReadOnly)

		// Check for persona guard volume
		var personaVol *corev1.Volume
		for i := range deployment.Spec.Template.Spec.Volumes {
			v := &deployment.Spec.Template.Spec.Volumes[i]
			if v.Name == personaGuardVolumeName {
				personaVol = v
				break
			}
		}
		require.NotNil(t, personaVol, "persona-guard volume should exist")
		require.NotNil(t, personaVol.ConfigMap, "volume source should be ConfigMap")
		assert.Equal(t, "test-guardrails", personaVol.ConfigMap.Name)

		// Check persona config hash annotation
		hash, ok := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyPersonaConfigHash]
		assert.True(t, ok, "persona config hash annotation should be set")
		assert.Len(t, hash, 64)

		// Check RestrictionsEnforced condition
		updatedInstance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      testInstanceName,
			Namespace: namespace,
		}, updatedInstance))

		var restrictionCond *metav1.Condition
		for i := range updatedInstance.Status.Conditions {
			if updatedInstance.Status.Conditions[i].Type == clawv1alpha1.ConditionTypeRestrictionsEnforced {
				restrictionCond = &updatedInstance.Status.Conditions[i]
				break
			}
		}
		require.NotNil(t, restrictionCond, "RestrictionsEnforced condition should exist")
		assert.Equal(t, metav1.ConditionTrue, restrictionCond.Status)
		assert.Contains(t, restrictionCond.Message, "AGENTS.md")
		assert.Contains(t, restrictionCond.Message, "SOUL.md")
	})

	t.Run("should block plugins when pluginInstallation is false", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testInstanceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "anthropic",
						Provider: "anthropic",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
					},
				},
				Plugins: []string{"@openclaw/matrix"},
				Restrictions: &clawv1alpha1.RestrictionsSpec{
					PluginInstallation: ptr.To(false),
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		// Verify no plugins init container was created
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			assert.NotEqual(t, "install-plugins", ic.Name,
				"plugins init container should not exist when pluginInstallation is false")
		}
	})
}
