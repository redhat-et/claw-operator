## Why

The operator manages `openclaw.json` on every pod restart — merging or overwriting provider config, model settings, and user preferences. This is the right default for managed deployments, but power users and development teams need to customize their configuration at runtime (change models, tweak settings, add providers) and have those edits survive restarts. Today, the operator overwrites their changes on every restart.

## What Changes

- Add `spec.config.management` enum field to ConfigSpec with values `operator` (default) and `user`
- In `user` mode, the init-config container seeds provider/model configuration on first boot, then preserves runtime edits on subsequent restarts
- Gateway infrastructure config (proxy, auth, networking) is still enforced in both modes — user-managed mode relaxes config ownership, not security controls
- The feature works with `spec.agentFiles` in both modes

## Capabilities

### New Capabilities

- `user-managed-config`: User-managed configuration mode via `spec.config.management: user`, seeding config once and preserving runtime edits while still enforcing infrastructure config

### Modified Capabilities

- `claw-crd`: Add `management` field to ConfigSpec with `operator`/`user` enum

## Impact

- `api/v1alpha1/claw_types.go` — add ConfigManagement enum and Management field to ConfigSpec
- `internal/controller/claw_deployment.go` — add user-managed init-config logic: mount whole PVC home, set `CLAW_CONFIG_MANAGEMENT=user` env var, conditional config seeding
- `internal/controller/claw_resource_controller.go` — branch reconciliation based on management mode
- `internal/assets/manifests/claw/configmap.yaml` — merge.js changes to handle user-managed mode
- No breaking changes — default is `operator` (existing behavior)
