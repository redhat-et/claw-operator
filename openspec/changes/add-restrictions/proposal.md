## Why

In regulated environments, administrators need hard controls that cannot be bypassed by the agent or the user. The agent runs arbitrary code — instruction-level guardrails ("don't install plugins") can be ignored. A hard, declarative restriction in the CR provides a control surface that only the CR author (admin) can change.

The primary use case is blocking plugin installation in locked-down scenarios. Plugins can execute arbitrary code, access the network, and modify agent behavior. In a kiosk deployment (Scenario F), plugin installation must be impossible regardless of what the user or agent requests.

## What Changes

- Add `spec.restrictions` to the Claw CRD with a `RestrictionsSpec` type
- `restrictions.pluginInstallation` (bool pointer) — when set to `false`, the plugins init container is skipped entirely, and the agent cannot install plugins at runtime
- The operator reports a `RestrictionsEnforced` status condition when restrictions are active
- Note: `restrictions.personaRef` was added and subsequently deprecated in favor of `agentFiles.readOnly` (covered in the `add-agent-files` change)

## Capabilities

### New Capabilities

- `plugin-installation-control`: Block plugin installation via `spec.restrictions.pluginInstallation: false`, skipping the plugins init container regardless of `spec.plugins` content

### Modified Capabilities

- `claw-crd`: Add `spec.restrictions` field with RestrictionsSpec type

## Impact

- `api/v1alpha1/claw_types.go` — add RestrictionsSpec type with PluginInstallation field; add Restrictions field to ClawSpec
- `internal/controller/claw_deployment.go` — skip plugins init container injection when pluginInstallation is false
- `internal/controller/claw_resource_controller.go` — set RestrictionsEnforced condition
- CRD manifest regeneration
- No breaking changes
