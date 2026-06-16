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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Unit tests ---

func TestSelfUpdateEnabled(t *testing.T) {
	tests := []struct {
		name     string
		spec     *clawv1alpha1.SelfUpdateSpec
		expected bool
	}{
		{"nil spec", nil, false},
		{"enabled false", &clawv1alpha1.SelfUpdateSpec{Enabled: false}, false},
		{"enabled true", &clawv1alpha1.SelfUpdateSpec{Enabled: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &clawv1alpha1.Claw{}
			instance.Spec.SelfUpdate = tt.spec
			assert.Equal(t, tt.expected, selfUpdateEnabled(instance))
		})
	}
}

func TestSelfUpdateResourceNames(t *testing.T) {
	assert.Equal(t, "my-claw-gateway", getSelfUpdateServiceAccountName("my-claw"))
	assert.Equal(t, "my-claw-self-update", getSelfUpdateRoleName("my-claw"))
	assert.Equal(t, "my-claw-self-update", getSelfUpdateRoleBindingName("my-claw"))
}

func makeTestDeploymentForSelfUpdate() []*unstructured.Unstructured {
	dep := &unstructured.Unstructured{}
	dep.SetKind(DeploymentKind)
	dep.SetName(getClawDeploymentName(testInstanceName))
	dep.Object["spec"] = map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"automountServiceAccountToken": false,
				"containers": []any{
					map[string]any{
						"name":  ClawGatewayContainerName,
						"image": testGatewayImage,
					},
				},
			},
		},
	}
	return []*unstructured.Unstructured{dep}
}

func TestConfigureClawDeploymentForSelfUpdate(t *testing.T) {
	t.Run("should be no-op when selfUpdate is nil", func(t *testing.T) {
		objects := makeTestDeploymentForSelfUpdate()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}
		require.NoError(t, configureClawDeploymentForSelfUpdate(objects, instance))

		saName, _, _ := unstructured.NestedString(
			objects[0].Object, "spec", "template", "spec", "serviceAccountName")
		assert.Empty(t, saName)

		autoMount, _, _ := unstructured.NestedBool(
			objects[0].Object, "spec", "template", "spec", "automountServiceAccountToken")
		assert.False(t, autoMount)
	})

	t.Run("should set serviceAccountName and automountServiceAccountToken when enabled", func(t *testing.T) {
		objects := makeTestDeploymentForSelfUpdate()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				SelfUpdate: &clawv1alpha1.SelfUpdateSpec{Enabled: true},
			},
		}
		require.NoError(t, configureClawDeploymentForSelfUpdate(objects, instance))

		saName, found, _ := unstructured.NestedString(
			objects[0].Object, "spec", "template", "spec", "serviceAccountName")
		assert.True(t, found)
		assert.Equal(t, getSelfUpdateServiceAccountName(testInstanceName), saName)

		autoMount, found, _ := unstructured.NestedBool(
			objects[0].Object, "spec", "template", "spec", "automountServiceAccountToken")
		assert.True(t, found)
		assert.True(t, autoMount)
	})

	t.Run("should error when deployment not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				SelfUpdate: &clawv1alpha1.SelfUpdateSpec{Enabled: true},
			},
		}
		err := configureClawDeploymentForSelfUpdate(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "claw deployment not found")
	})
}

// --- Integration tests (envtest) ---

func TestSelfUpdateRBACCreation(t *testing.T) {
	t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
	ctx := context.Background()

	secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret))

	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Credentials = testCredentials()
	instance.Spec.SelfUpdate = &clawv1alpha1.SelfUpdateSpec{Enabled: true}
	require.NoError(t, k8sClient.Create(ctx, instance))

	reconciler := createClawReconciler()
	reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

	t.Run("should create ServiceAccount with owner ref and instance label", func(t *testing.T) {
		sa := &corev1.ServiceAccount{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: getSelfUpdateServiceAccountName(testInstanceName), Namespace: namespace,
		}, sa))

		assert.Equal(t, sanitizeLabelValue(testInstanceName), sa.Labels[InstanceLabelKey])

		require.Len(t, sa.OwnerReferences, 1)
		assert.Equal(t, testInstanceName, sa.OwnerReferences[0].Name)
	})

	t.Run("should create Role with correct rules", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: getSelfUpdateRoleName(testInstanceName), Namespace: namespace,
		}, role))

		require.Len(t, role.Rules, 1)
		rule := role.Rules[0]
		assert.Equal(t, []string{"claw.sandbox.redhat.com"}, rule.APIGroups)
		assert.Equal(t, []string{"claws"}, rule.Resources)
		assert.Equal(t, []string{testInstanceName}, rule.ResourceNames)
		assert.ElementsMatch(t, []string{"get", "patch", "update"}, rule.Verbs)
	})

	t.Run("should create RoleBinding binding Role to SA", func(t *testing.T) {
		rb := &rbacv1.RoleBinding{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: getSelfUpdateRoleBindingName(testInstanceName), Namespace: namespace,
		}, rb))

		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, getSelfUpdateRoleName(testInstanceName), rb.RoleRef.Name)

		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, getSelfUpdateServiceAccountName(testInstanceName), rb.Subjects[0].Name)
	})

	t.Run("should set serviceAccountName on gateway deployment", func(t *testing.T) {
		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, deployment))

		assert.Equal(t, getSelfUpdateServiceAccountName(testInstanceName),
			deployment.Spec.Template.Spec.ServiceAccountName)
		require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
		assert.True(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	})
}

func TestSelfUpdateRBACCleanup(t *testing.T) {
	t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
	ctx := context.Background()

	secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret))

	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Credentials = testCredentials()
	instance.Spec.SelfUpdate = &clawv1alpha1.SelfUpdateSpec{Enabled: true}
	require.NoError(t, k8sClient.Create(ctx, instance))

	reconciler := createClawReconciler()
	reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

	// Verify resources exist
	sa := &corev1.ServiceAccount{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
		Name: getSelfUpdateServiceAccountName(testInstanceName), Namespace: namespace,
	}, sa))

	// Disable self-update
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
		Name: testInstanceName, Namespace: namespace,
	}, instance))
	instance.Spec.SelfUpdate = nil
	require.NoError(t, k8sClient.Update(ctx, instance))

	reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

	// SA should be gone
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name: getSelfUpdateServiceAccountName(testInstanceName), Namespace: namespace,
	}, &corev1.ServiceAccount{})
	assert.True(t, apierrors.IsNotFound(err), "ServiceAccount should be deleted")

	// Role should be gone
	role := &rbacv1.Role{}
	err = k8sClient.Get(ctx, client.ObjectKey{
		Name: getSelfUpdateRoleName(testInstanceName), Namespace: namespace,
	}, role)
	assert.True(t, apierrors.IsNotFound(err), "Role should be deleted")

	// RoleBinding should be gone
	rb := &rbacv1.RoleBinding{}
	err = k8sClient.Get(ctx, client.ObjectKey{
		Name: getSelfUpdateRoleBindingName(testInstanceName), Namespace: namespace,
	}, rb)
	assert.True(t, apierrors.IsNotFound(err), "RoleBinding should be deleted")

	// Deployment should revert to default SA
	deployment := &appsv1.Deployment{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
		Name: testInstanceName, Namespace: namespace,
	}, deployment))
	assert.NotEqual(t, getSelfUpdateServiceAccountName(testInstanceName),
		deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestSelfUpdateDisabledByDefault(t *testing.T) {
	t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
	ctx := context.Background()

	secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret))

	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Credentials = testCredentials()
	require.NoError(t, k8sClient.Create(ctx, instance))

	reconciler := createClawReconciler()
	reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

	// SA should not exist
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name: getSelfUpdateServiceAccountName(testInstanceName), Namespace: namespace,
	}, &corev1.ServiceAccount{})
	assert.True(t, apierrors.IsNotFound(err), "ServiceAccount should not exist by default")

	// Deployment should have automountServiceAccountToken false
	deployment := &appsv1.Deployment{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
		Name: testInstanceName, Namespace: namespace,
	}, deployment))
	require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
}

// --- RBAC enforcement test (envtest with impersonated client) ---

func TestSelfUpdateRBACEnforcement(t *testing.T) {
	t.Cleanup(func() {
		deleteAndWaitAllResources(t, namespace, testInstanceName)
		deleteAndWaitAllResources(t, namespace, "other-claw")
	})
	ctx := context.Background()

	secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret))

	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	instance.Spec.Credentials = testCredentials()
	instance.Spec.SelfUpdate = &clawv1alpha1.SelfUpdateSpec{Enabled: true}
	require.NoError(t, k8sClient.Create(ctx, instance))

	reconciler := createClawReconciler()
	reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

	// Create an impersonated client acting as the instance's ServiceAccount
	saUser := "system:serviceaccount:" + namespace + ":" +
		getSelfUpdateServiceAccountName(testInstanceName)
	impersonatedCfg := rest.CopyConfig(cfg)
	impersonatedCfg.Impersonate = rest.ImpersonationConfig{
		UserName: saUser,
	}
	saClient, err := client.New(impersonatedCfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	t.Run("should allow SA to get its own Claw CR", func(t *testing.T) {
		fetched := &clawv1alpha1.Claw{}
		err := saClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, fetched)
		require.NoError(t, err)
		assert.Equal(t, testInstanceName, fetched.Name)
	})

	t.Run("should allow SA to patch its own Claw CR spec", func(t *testing.T) {
		patch := []byte(`{"spec":{"webSearch":{"provider":"duckduckgo"}}}`)
		err := saClient.Patch(ctx, &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}, client.RawPatch(types.MergePatchType, patch))
		require.NoError(t, err)

		// Verify the patch stuck
		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))
		assert.NotNil(t, updated.Spec.WebSearch)
		assert.Equal(t, "duckduckgo", updated.Spec.WebSearch.Provider)
	})

	t.Run("should reconcile successfully after self-update patch", func(t *testing.T) {
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))
		assert.NotNil(t, updated.Spec.WebSearch)
		assert.Equal(t, "duckduckgo", updated.Spec.WebSearch.Provider)
	})

	t.Run("should deny SA from listing Claw CRs", func(t *testing.T) {
		clawList := &clawv1alpha1.ClawList{}
		err := saClient.List(ctx, clawList, client.InNamespace(namespace))
		require.Error(t, err)
		assert.True(t, apierrors.IsForbidden(err),
			"list should be forbidden, got: %v", err)
	})

	t.Run("should deny SA from accessing another instance", func(t *testing.T) {
		otherInstance := &clawv1alpha1.Claw{}
		otherInstance.Name = "other-claw"
		otherInstance.Namespace = namespace
		otherInstance.Spec.Credentials = testCredentials()
		require.NoError(t, k8sClient.Create(ctx, otherInstance))

		// get should be forbidden
		fetched := &clawv1alpha1.Claw{}
		err := saClient.Get(ctx, client.ObjectKey{
			Name: "other-claw", Namespace: namespace,
		}, fetched)
		require.Error(t, err)
		assert.True(t, apierrors.IsForbidden(err),
			"get on other instance should be forbidden, got: %v", err)

		// patch should be forbidden
		patch, _ := json.Marshal(map[string]any{
			"spec": map[string]any{"idle": true},
		})
		err = saClient.Patch(ctx, &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "other-claw", Namespace: namespace},
		}, client.RawPatch(types.MergePatchType, patch))
		require.Error(t, err)
		assert.True(t, apierrors.IsForbidden(err),
			"patch on other instance should be forbidden, got: %v", err)
	})
}
