## Why

The upstream operator has `spec.workspace.files` for seeding individual files into the agent workspace, but enterprise deployments need to seed entire directory trees — agent configuration files (SOUL.md, AGENTS.md, TOOLS.md), project scaffolds, and shared templates. These trees live in Git repositories or ConfigMap archives and need to be injected at pod start time with control over overwrite behavior and file protection.

Without `spec.agentFiles`, administrators must either embed large file trees inline in the CR (impractical), build custom init containers (unsupported), or manually copy files into PVCs (doesn't scale). The existing `spec.workspace.files` is designed for small, individual files — not for the "seed an entire agent profile from a Git repo" use case that enterprise onboarding requires.

## What Changes

- Add `spec.agentFiles` to the Claw CRD with two mutually exclusive sources: a ConfigMap archive (`configMapRef`) or a Git repository (`git`)
- Add `applyPolicy` to control whether seeded files overwrite existing PVC content on restart (`IfMissing` vs `Always`)
- Add `git.secretRef` for cloning from private Git repositories without exposing credentials to the agent
- Add `readOnly` to mount specific workspace files or directories read-only on the gateway container, preventing the agent from modifying its own guardrails
- The feature works with both operator-managed and user-managed configuration modes

## Capabilities

### New Capabilities

- `agent-files-seeding`: Seed workspace files from a ConfigMap archive or Git repository via `spec.agentFiles`, with configurable overwrite policy
- `agent-files-private-git`: Clone agent files from private Git repositories using `spec.agentFiles.git.secretRef`, with credentials mounted securely on the init container only
- `agent-files-read-only`: Mount specific workspace files or directories read-only via `spec.agentFiles.readOnly`, providing filesystem-enforced protection against agent or user modification

### Modified Capabilities

- `claw-crd`: Add `spec.agentFiles` field with AgentFilesSpec type to ClawSpec

## Impact

- `api/v1alpha1/claw_types.go` — add AgentFilesSpec, AgentFilesConfigMapRef, AgentFilesGitSource, GitSecretRef types; add AgentFiles field to ClawSpec
- `internal/controller/claw_deployment.go` — add functions to configure init-config container with Git clone or ConfigMap extraction; add read-only volume mount generation
- `internal/controller/claw_resource_controller.go` — wire agentFiles configuration into reconciliation pipeline
- CRD manifest regeneration with CEL validation rules (configMapRef/git mutual exclusivity, at-least-one-source requirement)
- No breaking changes — agentFiles is fully optional and existing workspace.files behavior is unchanged
