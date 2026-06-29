## Context

The upstream operator injects skills by writing SKILL.md content into the gateway ConfigMap with `_skill_<name>` key prefixes. `merge.js` reads these keys and writes them to `workspace/skills/<name>/SKILL.md` on pod start. This mechanism is retained for inline content; the new channels extend it.

## Goals / Non-Goals

**Goals:**
- Support three independent skill delivery channels
- Preserve backward compatibility for inline skills (via `skills.content`)
- Detect name collisions across channels at reconcile time
- Support private OCI registries via per-image imagePullSecrets
- Keep skill images read-only (agents cannot modify OCI-delivered skills)

**Non-Goals:**
- Runtime skill installation (skills are delivered at pod start only)
- Skill versioning or dependency resolution
- Supporting skill images with multiple SKILL.md files per image (one image = one skill directory)

## Decisions

### Three independent channels, not a union type

`SkillsSpec` has three optional fields (content, images, configMaps), all of which can be used simultaneously. This is simpler than a discriminated union and matches how enterprises actually deploy: some skills are inline (quick customizations), some are in OCI images (versioned, shared), and some are in ConfigMaps (managed by Helm or Kustomize).

**Alternative considered:** A union type where each skill entry declares its source. Rejected because it makes the common case (all inline or all OCI) unnecessarily verbose.

### OCI images via Kubernetes ImageVolume

Skill images are mounted using Kubernetes ImageVolume — a native feature (K8s 1.31+ with feature gate) that mounts an OCI image as a read-only volume without requiring an init container to pull and extract it.

**Why ImageVolume, not init containers:** Init containers that `docker pull` and extract images add complexity, require registry credentials to be available inside the container, and must handle caching. ImageVolume delegates all of this to the kubelet.

**Trade-off:** Requires K8s 1.31+ with the ImageVolume feature gate enabled. Not available on older clusters.

### imagePullSecrets per skill image

Each `SkillImageSpec` has an optional `imagePullSecrets` list. The operator collects and deduplicates pull secrets across all skill images and merges them into the gateway pod's `imagePullSecrets` field.

**Why per-image, not per-spec:** Different skill images may come from different registries with different credentials. A single global list would require all registries to share the same credential.

**Note:** Because `imagePullSecrets` is a pod-level field in Kubernetes, secrets declared for one skill image may be used to pull any image in the pod. This is a Kubernetes limitation, not an operator design choice.

### ConfigMap keys as skill names

`SkillConfigMapRef` references a ConfigMap by name. The operator reads all keys via the UserSecretReader (bypassing the label-filtered cache) and injects them as `_skill_<key>` entries in the gateway ConfigMap. `merge.js` treats them identically to inline content skills.

**Why UserSecretReader:** The operator's informer cache only watches ConfigMaps with specific labels. User-owned skill ConfigMaps won't have these labels, so direct API reads are necessary.

### Cross-source collision detection

If a skill name appears in more than one source (content, images, or any ConfigMap key), the reconciler returns an error. No priority order, no silent overwrite.

**Why error, not priority:** Silent overwrites hide misconfiguration. In a multi-team environment where platform engineers maintain OCI skills and developers add inline skills, a name collision likely means someone is unaware of the other's skill. An error surfaces this immediately.

### skills.content preserves upstream map semantics

`skills.content` is `map[string]string` — identical semantics to the old `spec.skills`. The migration path is: rename `spec.skills` to `spec.skills.content`.

## Risks / Trade-offs

- **[Breaking type change]** Existing manifests using `spec.skills` as a flat map will fail CRD validation after upgrade. Migration is straightforward (move content under `skills.content`) but requires coordination.
- **[ImageVolume feature gate]** OCI skill images require K8s 1.31+ with ImageVolume enabled. On older clusters, only content and configMap channels are available.
- **[ConfigMap 1 MiB limit]** ConfigMap-sourced skills are subject to the 1 MiB Kubernetes limit. Large skills should use OCI images instead.
