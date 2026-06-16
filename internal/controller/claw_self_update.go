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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func selfUpdateEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.SelfUpdate != nil && instance.Spec.SelfUpdate.Enabled
}

func getSelfUpdateServiceAccountName(instanceName string) string {
	return instanceName + "-gateway"
}

func getSelfUpdateRoleName(instanceName string) string {
	return instanceName + "-self-update"
}

func getSelfUpdateRoleBindingName(instanceName string) string {
	return instanceName + "-self-update"
}

func (r *ClawResourceReconciler) reconcileSelfUpdate(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	if !selfUpdateEnabled(instance) {
		return r.cleanupSelfUpdateResources(ctx, instance)
	}
	if err := r.applySelfUpdateServiceAccount(ctx, instance); err != nil {
		return fmt.Errorf("failed to apply self-update ServiceAccount: %w", err)
	}
	if err := r.applySelfUpdateRole(ctx, instance); err != nil {
		return fmt.Errorf("failed to apply self-update Role: %w", err)
	}
	if err := r.applySelfUpdateRoleBinding(ctx, instance); err != nil {
		return fmt.Errorf("failed to apply self-update RoleBinding: %w", err)
	}
	return nil
}

func (r *ClawResourceReconciler) applySelfUpdateServiceAccount(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSelfUpdateServiceAccountName(instance.Name),
			Namespace: instance.Namespace,
		},
	}
	sa.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))
	setInstanceLabel(sa, instance.Name)

	if err := controllerutil.SetControllerReference(instance, sa, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on self-update SA: %w", err)
	}
	return r.Patch(ctx, sa, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	})
}

func (r *ClawResourceReconciler) applySelfUpdateRole(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role",
	})
	role.SetName(getSelfUpdateRoleName(instance.Name))
	role.SetNamespace(instance.Namespace)
	role.SetLabels(map[string]string{
		"app.kubernetes.io/name": "claw",
		InstanceLabelKey:         sanitizeLabelValue(instance.Name),
	})

	role.Object["rules"] = []any{
		map[string]any{
			"apiGroups":     []any{"claw.sandbox.redhat.com"},
			"resources":     []any{"claws"},
			"resourceNames": []any{instance.Name},
			"verbs":         []any{"get", "patch", "update"},
		},
	}

	if err := controllerutil.SetControllerReference(instance, role, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on self-update Role: %w", err)
	}
	return r.Patch(ctx, role, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	})
}

func (r *ClawResourceReconciler) applySelfUpdateRoleBinding(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	rb := &unstructured.Unstructured{}
	rb.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding",
	})
	rb.SetName(getSelfUpdateRoleBindingName(instance.Name))
	rb.SetNamespace(instance.Namespace)
	rb.SetLabels(map[string]string{
		"app.kubernetes.io/name": "claw",
		InstanceLabelKey:         sanitizeLabelValue(instance.Name),
	})

	rb.Object["roleRef"] = map[string]any{
		"apiGroup": "rbac.authorization.k8s.io",
		"kind":     "Role",
		"name":     getSelfUpdateRoleName(instance.Name),
	}
	rb.Object["subjects"] = []any{
		map[string]any{
			"kind":      "ServiceAccount",
			"name":      getSelfUpdateServiceAccountName(instance.Name),
			"namespace": instance.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(instance, rb, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on self-update RoleBinding: %w", err)
	}
	return r.Patch(ctx, rb, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	})
}

func (r *ClawResourceReconciler) cleanupSelfUpdateResources(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	logger := log.FromContext(ctx)
	ns := instance.Namespace

	resources := []client.Object{
		newUnstructuredResource("rbac.authorization.k8s.io", "RoleBinding",
			getSelfUpdateRoleBindingName(instance.Name), ns),
		newUnstructuredResource("rbac.authorization.k8s.io", "Role",
			getSelfUpdateRoleName(instance.Name), ns),
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: getSelfUpdateServiceAccountName(instance.Name), Namespace: ns,
		}},
	}

	for _, obj := range resources {
		key := client.ObjectKeyFromObject(obj)
		existing := obj.DeepCopyObject().(client.Object)
		if err := r.Get(ctx, key, existing); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get self-update resource %s/%s: %w",
				obj.GetObjectKind().GroupVersionKind().Kind,
				obj.GetName(), err)
		}
		if !isOwnedBy(existing, instance) {
			logger.Info("Skipping deletion of resource not owned by this instance",
				"kind", obj.GetObjectKind().GroupVersionKind().Kind,
				"name", obj.GetName())
			continue
		}
		if err := r.Delete(ctx, existing); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete self-update resource %s/%s: %w",
				obj.GetObjectKind().GroupVersionKind().Kind,
				obj.GetName(), err)
		}
		logger.Info("Deleted self-update resource",
			"kind", obj.GetObjectKind().GroupVersionKind().Kind,
			"name", obj.GetName())
	}
	return nil
}

// isOwnedBy checks whether obj has an ownerReference pointing to the given owner.
func isOwnedBy(obj client.Object, owner client.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func configureClawDeploymentForSelfUpdate(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if !selfUpdateEnabled(instance) {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}
		saName := getSelfUpdateServiceAccountName(instance.Name)
		if err := unstructured.SetNestedField(
			obj.Object, saName,
			"spec", "template", "spec", "serviceAccountName",
		); err != nil {
			return fmt.Errorf("failed to set serviceAccountName: %w", err)
		}
		if err := unstructured.SetNestedField(
			obj.Object, true,
			"spec", "template", "spec", "automountServiceAccountToken",
		); err != nil {
			return fmt.Errorf("failed to set automountServiceAccountToken: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}
