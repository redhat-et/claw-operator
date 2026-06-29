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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

type patchFailClient struct {
	client.Client
	err error
}

func (c patchFailClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.err
}

func TestEmitAuditTrackingEvents(t *testing.T) {
	t.Run("should not emit diff events when annotation patch fails", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)
		reconciler := &ClawResourceReconciler{
			Client: patchFailClient{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
				err:    errors.New("patch failed"),
			},
			Recorder: recorder,
		}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testInstanceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "primary"},
				},
			},
		}

		err := reconciler.emitAuditTrackingEvents(ctx, instance)

		require.Error(t, err)
		require.Empty(t, recorder.Events, "diff events should wait until tracking annotations are persisted")
	})
}
