## Context

The operator renders a Deployment from a Kustomize base manifest where the OpenClaw image tag is set at build time. All instances in a cluster share the same image tag. There is no per-instance override mechanism.

The reconciler already mutates the rendered Deployment (adding containers, volumes, env vars). Adding an image tag override fits naturally into this pipeline.

## Goals / Non-Goals

**Goals:**
- Allow per-instance OpenClaw version pinning via the CRD
- Keep the override optional — omitting `spec.version` preserves current behavior
- Override all OpenClaw containers atomically to prevent version skew within a pod

**Non-Goals:**
- Overriding the image name/registry (only the tag is overridable)
- Supporting digest-based image references (pattern validation allows tags only)
- Version compatibility checking between operator and OpenClaw versions

## Decisions

### Override all three containers atomically

The operator overrides the image tag on init-volume, init-config, and gateway containers in a single pass. If any of the three containers is missing from the base manifest, the override fails without mutating any container.

**Why:** Partial overrides (e.g., gateway at v2 but init-config at v1) would cause silent data corruption — the init container writes config in one format and the gateway expects another.

**Alternative considered:** Override only the gateway container. Rejected because init containers and gateway must agree on config format and workspace layout.

### Run image override before plugins init container setup

The `configureClawImage` function runs before `configurePluginsInitContainer` in the reconciliation pipeline. This way, if the plugins init container is cloned from the gateway container, it automatically inherits the overridden image.

**Why:** Ordering dependency — the plugins container must use the same OpenClaw version as the gateway.

### Pattern validation on the field

The CRD uses `+kubebuilder:validation:Pattern=^[a-z0-9][a-z0-9._-]*$` to reject clearly invalid tags (empty strings, leading dots/dashes, uppercase, special characters) at admission time.

**Why:** Catches typos early. The pattern is intentionally permissive — it validates format, not whether the tag exists in the registry.

## Risks / Trade-offs

- **[No version compatibility matrix]** The operator does not check whether a given OpenClaw version is compatible with the operator version. A user could set `spec.version: "1.0.0"` against an operator that expects a newer config format. Mitigated by documentation; a compatibility webhook could be added later.
- **[Registry fixed]** Only the tag is overridable. Users who need a different registry (e.g., air-gapped mirrors) must rebuild the operator's base manifest. This is a deliberate scope limit for this change.
