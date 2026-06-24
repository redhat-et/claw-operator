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

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// DefaultExecClusterRoleName is the kustomize-prefixed name of the ClusterRole that grants
// pods/exec. After kustomize applies namePrefix "claw-operator-", the name in the
// exec_clusterrole.yaml ("exec-role") becomes "claw-operator-exec-role" in the cluster.
// This ClusterRole is never bound cluster-wide; the reconciler creates a namespace-scoped
// RoleBinding per Claw CR, so exec access is limited to namespaces with active Claw instances.
const DefaultExecClusterRoleName = "claw-operator-exec-role"

// reconcileExecRoleBinding creates or updates a namespace-scoped RoleBinding in
// instance.Namespace that binds the exec ClusterRole to the operator's ServiceAccount.
// The RoleBinding is owner-referenced to the Claw CR so it is garbage-collected when the CR is deleted.
// Returns an error if OperatorSAName or OperatorNamespace is not configured.
//
// RoleRef is immutable after creation. If the ClusterRole name ever changes, the existing
// RoleBinding is deleted and recreated to avoid a 422 Unprocessable Entity error on Update.
func (r *ClawResourceReconciler) reconcileExecRoleBinding(ctx context.Context, instance *clawv1alpha1.Claw) error {
	if r.OperatorSAName == "" || r.OperatorNamespace == "" {
		return fmt.Errorf("operator SA identity not configured: OperatorSAName=%q OperatorNamespace=%q", r.OperatorSAName, r.OperatorNamespace)
	}

	desired := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-exec",
			Namespace: instance.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     r.ExecClusterRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      r.OperatorSAName,
			Namespace: r.OperatorNamespace,
		}},
	}
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on exec RoleBinding: %w", err)
	}

	existing := &rbacv1.RoleBinding{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("getting exec RoleBinding: %w", err)
	}

	// RoleRef is immutable — delete and recreate if it differs.
	if existing.RoleRef != desired.RoleRef {
		if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting stale exec RoleBinding: %w", err)
		}
		return r.Create(ctx, desired)
	}

	// Update subjects and owner reference if needed.
	existing.Subjects = desired.Subjects
	existing.OwnerReferences = desired.OwnerReferences
	return r.Update(ctx, existing)
}
