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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestOpenShellGatewayReconcileCreatesGatewayResources(t *testing.T) {
	testCtx := context.Background()
	testNamespace := createOpenShellGatewayTestNamespace(t, testCtx, "openshell-gateway-test")
	name := "openshell-test"
	instance := &clawv1alpha1.OpenShellGateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: clawv1alpha1.OpenShellGatewaySpec{
			SandboxImage: "quay.io/example/sandbox:latest",
			OpenShift:    &clawv1alpha1.OpenShellGatewayOpenShiftSpec{PrivilegedSandboxSCC: true},
		},
	}
	require.NoError(t, k8sClient.Create(testCtx, instance))
	t.Cleanup(func() {
		_ = k8sClient.Delete(testCtx, instance)
	})

	reconciler := &OpenShellGatewayReconciler{
		Client: k8sClient,
		Scheme: scheme.Scheme,
	}
	_, err := reconciler.Reconcile(testCtx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace},
	})
	require.NoError(t, err)

	sa := &corev1.ServiceAccount{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: name, Namespace: testNamespace}, sa))
	require.NotEmpty(t, sa.OwnerReferences)
	assert.Equal(t, "OpenShellGateway", sa.OwnerReferences[0].Kind)

	sandboxSA := &corev1.ServiceAccount{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellSandboxServiceAccountName(name), Namespace: testNamespace}, sandboxSA))

	cm := &corev1.ConfigMap{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellConfigMapName(name), Namespace: testNamespace}, cm))
	assert.Contains(t, cm.Data["gateway.toml"], `disable_tls = true`)
	assert.Contains(t, cm.Data["gateway.toml"], `default_image = "quay.io/example/sandbox:latest"`)

	secret := &corev1.Secret{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellJWTSecretName(name), Namespace: testNamespace}, secret))
	assert.NotEmpty(t, secret.Data["signing.pem"])
	assert.NotEmpty(t, secret.Data["public.pem"])
	assert.NotEmpty(t, secret.Data["kid"])

	service := &corev1.Service{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: name, Namespace: testNamespace}, service))
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(8080), service.Spec.Ports[0].Port)

	deployment := &appsv1.Deployment{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: name, Namespace: testNamespace}, deployment))
	assert.Equal(t, defaultOpenShellGatewayImage, deployment.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, name, deployment.Spec.Template.Spec.ServiceAccountName)
	assert.NotEmpty(t, deployment.Spec.Template.Annotations["checksum/gateway-config"])

	roleBinding := &rbacv1.RoleBinding{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellPrivilegedSCCRoleBindingName(name), Namespace: testNamespace}, roleBinding))
	assert.Equal(t, openShiftPrivilegedSCCRole, roleBinding.RoleRef.Name)
	require.Len(t, roleBinding.Subjects, 1)
	assert.Equal(t, openShellSandboxServiceAccountName(name), roleBinding.Subjects[0].Name)

	clusterRole := &rbacv1.ClusterRole{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellGatewayConfigFor(instance).ClusterNodeReaderName}, clusterRole))

	updated := &clawv1alpha1.OpenShellGateway{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: name, Namespace: testNamespace}, updated))
	assert.Equal(t, "http://openshell-test.openshell-gateway-test.svc.cluster.local:8080", updated.Status.Endpoint)
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeOpenShellGatewayReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, clawv1alpha1.ConditionReasonOpenShellGatewayProvisioning, ready.Reason)
}

func TestOpenShellGatewayReconcilePreservesJWTSecret(t *testing.T) {
	testCtx := context.Background()
	testNamespace := createOpenShellGatewayTestNamespace(t, testCtx, "openshell-gateway-jwt-test")
	name := "openshell-jwt-stable"
	instance := &clawv1alpha1.OpenShellGateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
	require.NoError(t, k8sClient.Create(testCtx, instance))
	t.Cleanup(func() {
		_ = k8sClient.Delete(testCtx, instance)
	})

	reconciler := &OpenShellGatewayReconciler{
		Client: k8sClient,
		Scheme: scheme.Scheme,
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace}}
	_, err := reconciler.Reconcile(testCtx, req)
	require.NoError(t, err)

	secret := &corev1.Secret{}
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellJWTSecretName(name), Namespace: testNamespace}, secret))
	firstKid := string(secret.Data["kid"])

	_, err = reconciler.Reconcile(testCtx, req)
	require.NoError(t, err)
	require.NoError(t, k8sClient.Get(testCtx, types.NamespacedName{Name: openShellJWTSecretName(name), Namespace: testNamespace}, secret))
	assert.Equal(t, firstKid, string(secret.Data["kid"]))
}

func createOpenShellGatewayTestNamespace(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, ns)
	require.True(t, err == nil || apierrors.IsAlreadyExists(err))
	t.Cleanup(func() {
		_ = k8sClient.Delete(ctx, ns)
	})
	return name
}
