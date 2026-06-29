## Why

The OpenClaw container image tag is baked into the operator binary via Kustomize. Upgrading or pinning the OpenClaw version for a single instance requires rebuilding and redeploying the entire operator. In multi-tenant clusters where different teams need different OpenClaw versions (e.g., one team testing a canary release while others stay on stable), this is a blocker.

## What Changes

- Add an optional `spec.version` field to the Claw CRD that overrides the OpenClaw container image tag for that instance
- When set, the operator replaces the tag on all three OpenClaw containers (init-volume, init-config, gateway) during reconciliation
- When omitted, the operator's built-in default tag is used (backward compatible)
- The image name (`ghcr.io/openclaw/openclaw`) remains fixed — only the tag is overridable

## Capabilities

### New Capabilities

- `instance-version-override`: Per-instance OpenClaw image tag override via `spec.version`, allowing different instances to run different OpenClaw versions without operator rebuild

### Modified Capabilities

- `claw-crd`: Add `spec.version` field with pattern validation to ClawSpec

## Impact

- `api/v1alpha1/claw_types.go` — add Version field to ClawSpec
- `internal/controller/claw_deployment.go` — add image override function that mutates all three containers
- `internal/controller/claw_resource_controller.go` — call image override early in reconciliation (before plugins init container setup)
- CRD manifest regeneration
- No breaking changes, no migration needed
