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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// ExecClusterRoleName is the kustomize-prefixed name of the ClusterRole that grants
// pods/exec. After kustomize applies namePrefix "claw-operator-", the name in the
// exec_clusterrole.yaml ("exec-role") becomes "claw-operator-exec-role" in the cluster.
// This ClusterRole is never bound cluster-wide; the reconciler creates a namespace-scoped
// RoleBinding per Claw CR, so exec access is limited to namespaces with active Claw instances.
const ExecClusterRoleName = "claw-operator-exec-role"

// reconcileExecRoleBinding creates or updates a namespace-scoped RoleBinding in
// instance.Namespace that binds ExecClusterRoleName to the operator's ServiceAccount.
// The RoleBinding is owner-referenced to the Claw CR so it is garbage-collected when the CR is deleted.
// If OperatorSAName or OperatorNamespace is not configured, the function is a no-op.
func (r *ClawResourceReconciler) reconcileExecRoleBinding(ctx context.Context, instance *clawv1alpha1.Claw) error {
	if r.OperatorSAName == "" || r.OperatorNamespace == "" {
		log.FromContext(ctx).Info("skipping exec RoleBinding: operator SA identity not configured")
		return nil
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-exec",
			Namespace: instance.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(instance, rb, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on exec RoleBinding: %w", err)
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     ExecClusterRoleName,
		}
		rb.Subjects = []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      r.OperatorSAName,
			Namespace: r.OperatorNamespace,
		}}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconciling exec RoleBinding: %w", err)
	}
	return nil
}
