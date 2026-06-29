## 1. CRD Changes

- [ ] 1.1 Add `ServiceAccountName` field to `ClawSpec` in `api/v1alpha1/claw_types.go` with `+optional`
- [ ] 1.2 Regenerate CRD manifests (`make manifests`) and deep copy (`make generate`)

## 2. Reconciler Implementation

- [ ] 2.1 Add `configureClawDeploymentServiceAccount(objects, instance)` function in `internal/controller/claw_deployment.go` that sets `serviceAccountName` and `automountServiceAccountToken: true` on the gateway Deployment's pod template
- [ ] 2.2 Return early (no-op) when `spec.serviceAccountName` is empty
- [ ] 2.3 Return an error if the gateway Deployment is not found in the rendered manifests
- [ ] 2.4 Call the new function in the reconciliation pipeline in `claw_resource_controller.go`

## 3. Tests

- [ ] 3.1 Add unit test: `spec.serviceAccountName` set — verify pod template has the SA and `automountServiceAccountToken: true`
- [ ] 3.2 Add unit test: `spec.serviceAccountName` omitted — verify pod template is unchanged
- [ ] 3.3 Add unit test: gateway Deployment missing — verify error is returned
