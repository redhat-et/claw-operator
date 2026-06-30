## 1. CRD Changes

- [ ] 1.1 Add `Version` field to `ClawSpec` in `api/v1alpha1/claw_types.go` with `+optional` and pattern validation `^[a-z0-9][a-z0-9._-]*$`
- [ ] 1.2 Regenerate CRD manifests (`make manifests`) and deep copy (`make generate`)

## 2. Reconciler Implementation

- [ ] 2.1 Add `configureClawImage(deployment, version, defaultTag)` function in `internal/controller/claw_deployment.go` that overrides the image tag on init-volume, init-config, and gateway containers
- [ ] 2.2 Validate all three required containers exist before mutating any image — return an error if any is missing
- [ ] 2.3 Call `configureClawImage` in the reconciliation pipeline before `configurePluginsInitContainer`

## 3. Tests

- [ ] 3.1 Add unit tests for `configureClawImage`: version set, version omitted, missing container error
- [ ] 3.2 Add integration test that reconciles a full Claw CR with `spec.version` and verifies all three container images in the rendered Deployment

## 4. Documentation

- [ ] 4.1 Add `spec.version` to the user guide with an example manifest
