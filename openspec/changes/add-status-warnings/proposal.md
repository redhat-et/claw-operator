## Why

When a Claw deployment fails due to init container crashes, the operator reports only "Waiting for deployments to become ready" — forcing users to dig through pod logs. The most common production failure we observed was a version mismatch: `spec.version` set to an older tag against a PVC configured for a newer one, causing plugin init containers to crash-loop. The operator had no way to surface the root cause.

Additionally, version downgrades (setting `spec.version` to a value older than what was previously deployed) can cause PVC data incompatibility. Users need a warning before they discover the problem at runtime.

## What Changes

- Add three warning-only status conditions: `PluginCompatibility`, `VersionDowngrade`, `InitContainerFailure`
- Add `status.lastDeployedVersion` to track the last successfully deployed `spec.version` as a high-water mark
- Enrich the `Ready` condition with init container failure details when deployments are not ready
- All warnings are non-blocking — they don't prevent deployment, they surface information

## Capabilities

### New Capabilities

- `operational-status-warnings`: Surface init container failures, plugin version compatibility, and version downgrade warnings via status conditions and `status.lastDeployedVersion`

### Modified Capabilities

- `claw-crd`: Add `LastDeployedVersion` field to ClawStatus
- `status-conditions`: Add PluginCompatibility, VersionDowngrade, and InitContainerFailure condition types and reasons

## Impact

- `api/v1alpha1/claw_types.go` — add condition type/reason constants; add LastDeployedVersion to ClawStatus
- `internal/controller/claw_resource_controller.go` — inspect pods for init container failures; compare spec.version against lastDeployedVersion; check plugin minimum versions
- CRD manifest regeneration
- No breaking changes — all conditions are additive, all warnings are advisory
