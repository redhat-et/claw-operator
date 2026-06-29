## Why

Upstream `spec.skills` is a `map[string]string` — each key is a skill name, each value is inline SKILL.md content. This works for small, operator-authored skills but breaks down in enterprise environments:

- **Air-gapped clusters** cannot download skills from the internet at runtime. Skills must be pre-packaged and shipped as container images alongside the application.
- **Shared skill libraries** maintained by platform teams need a distribution mechanism that doesn't require embedding content in every CR.
- **Large skills** with multiple files (skill + examples + data) cannot be represented as a single string value.

The restructured `spec.skills` supports three independent delivery channels: inline content (preserving upstream behavior), OCI images (Kubernetes ImageVolume), and ConfigMap references.

## What Changes

- **BREAKING:** Change `spec.skills` from `map[string]string` to a structured `SkillsSpec` type
- `skills.content` — inline map (same semantics as the old `spec.skills`)
- `skills.images` — list of OCI images mounted as read-only skill directories via Kubernetes ImageVolume (requires K8s 1.31+)
- `skills.configMaps` — list of ConfigMap references whose keys become skill names
- Add `imagePullSecrets` per skill image for private OCI registries
- Cross-source collision detection: a skill name appearing in multiple sources causes a reconcile error

## Capabilities

### New Capabilities

- `skills-oci-images`: Mount OCI images as read-only skill directories via Kubernetes ImageVolume, with per-image imagePullSecrets for private registries
- `skills-configmap-refs`: Reference ConfigMaps whose keys become skill names and values become SKILL.md content
- `skills-collision-detection`: Detect and reject skill name collisions across delivery channels

### Modified Capabilities

- `claw-crd`: Restructure `spec.skills` from `map[string]string` to `SkillsSpec` struct — **BREAKING** type change

## Impact

- `api/v1alpha1/claw_types.go` — add SkillsSpec, SkillImageSpec, SkillConfigMapRef types; change Skills field type
- `internal/controller/claw_deployment.go` — add ImageVolume generation for OCI skills, ConfigMap key injection, collision detection
- `internal/controller/claw_resource_controller.go` — wire skills delivery into reconciliation; collect imagePullSecrets and merge into pod spec
- `internal/assets/manifests/claw/configmap.yaml` — merge.js changes to write skills from `_skill_` ConfigMap keys
- CRD manifest regeneration
- **Breaking:** existing manifests using the old `map[string]string` format will need migration to `skills.content`
