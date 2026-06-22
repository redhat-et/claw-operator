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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestConfigureSkillImages(t *testing.T) {
	t.Run("should be no-op when skills is nil", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := configureSkillImages(objects, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		assert.Empty(t, volumes)
	})

	t.Run("should be no-op when images is empty", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{},
				},
			},
		}
		err := configureSkillImages(objects, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		assert.Empty(t, volumes)
	})

	t.Run("should add volume and mount for a single skill image", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{
						{Name: "openshift-review", Image: "quay.io/corp/openshift-review:1.0.0"},
					},
				},
			},
		}
		err := configureSkillImages(objects, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 1)
		vol := volumes[0].(map[string]any)
		assert.Equal(t, "skill-image-openshift-review", vol["name"])
		imgMap := vol["image"].(map[string]any)
		assert.Equal(t, "quay.io/corp/openshift-review:1.0.0", imgMap["reference"])
		_, hasPullPolicy := imgMap["pullPolicy"]
		assert.False(t, hasPullPolicy, "pullPolicy should be omitted when not set")

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		gwContainer := containers[0].(map[string]any)
		mounts, _, _ := unstructured.NestedSlice(gwContainer, "volumeMounts")
		require.Len(t, mounts, 1)
		mount := mounts[0].(map[string]any)
		assert.Equal(t, "skill-image-openshift-review", mount["name"])
		assert.Equal(t, "/home/node/.openclaw/workspace/skills/openshift-review", mount["mountPath"])
		assert.Equal(t, true, mount["readOnly"])
	})

	t.Run("should add volumes and mounts for multiple skill images", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{
						{Name: "skill-a", Image: "quay.io/corp/skill-a:1.0.0"},
						{Name: "skill-b", Image: "quay.io/corp/skill-b:2.0.0"},
					},
				},
			},
		}
		err := configureSkillImages(objects, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 2)

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		gwContainer := containers[0].(map[string]any)
		mounts, _, _ := unstructured.NestedSlice(gwContainer, "volumeMounts")
		require.Len(t, mounts, 2)
	})

	t.Run("should set pullPolicy when specified", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{
						{
							Name:       "my-skill",
							Image:      "quay.io/corp/my-skill:latest",
							PullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		}
		err := configureSkillImages(objects, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 1)
		vol := volumes[0].(map[string]any)
		imgMap := vol["image"].(map[string]any)
		assert.Equal(t, "Always", imgMap["pullPolicy"])
	})

	t.Run("should skip non-deployment objects", func(t *testing.T) {
		svc := &unstructured.Unstructured{}
		svc.SetKind(ServiceKind)
		svc.SetName("test-svc")
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{
						{Name: "my-skill", Image: "quay.io/corp/my-skill:1.0.0"},
					},
				},
			},
		}
		err := configureSkillImages([]*unstructured.Unstructured{svc}, instance)
		require.NoError(t, err)
	})

	t.Run("should return error when gateway container not found", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "not-gateway"},
					},
					"volumes": []any{},
				},
			},
		}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: &clawv1alpha1.SkillsSpec{
					Images: []clawv1alpha1.SkillImageSpec{
						{Name: "my-skill", Image: "quay.io/corp/my-skill:1.0.0"},
					},
				},
			},
		}
		err := configureSkillImages([]*unstructured.Unstructured{dep}, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), ClawGatewayContainerName)
	})
}
