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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileExecRoleBinding(t *testing.T) {
	// Test 1: RoleBinding is created in the Claw namespace
	t.Run("creates exec RoleBinding in Claw namespace after reconcile", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := &ClawResourceReconciler{
			Client:              k8sClient,
			Scheme:              scheme.Scheme,
			UserSecretReader:    k8sClient,
			OperatorNamespace:   "test-operator-ns",
			OperatorSAName:      "test-operator-sa",
			ExecClusterRoleName: DefaultExecClusterRoleName,
		}
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		rbName := testInstanceName + "-exec"
		rb := &rbacv1.RoleBinding{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{Name: rbName, Namespace: namespace}, rb) == nil
		}, "exec RoleBinding should be created")

		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, DefaultExecClusterRoleName, rb.RoleRef.Name)
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, rbacv1.ServiceAccountKind, rb.Subjects[0].Kind)
		assert.Equal(t, "test-operator-sa", rb.Subjects[0].Name)
		assert.Equal(t, "test-operator-ns", rb.Subjects[0].Namespace)
	})

	// Test 2: RoleBinding has owner reference to Claw CR
	t.Run("exec RoleBinding has owner reference to Claw CR", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := &ClawResourceReconciler{
			Client:              k8sClient,
			Scheme:              scheme.Scheme,
			UserSecretReader:    k8sClient,
			OperatorNamespace:   "test-operator-ns",
			OperatorSAName:      "test-operator-sa",
			ExecClusterRoleName: DefaultExecClusterRoleName,
		}
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		rbName := testInstanceName + "-exec"
		rb := &rbacv1.RoleBinding{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{Name: rbName, Namespace: namespace}, rb) == nil
		}, "exec RoleBinding should be created")

		require.NotEmpty(t, rb.OwnerReferences, "RoleBinding should have owner reference")
		assert.Equal(t, testInstanceName, rb.OwnerReferences[0].Name)
		assert.Equal(t, "Claw", rb.OwnerReferences[0].Kind)
	})
}
