## Context

The operator's init-config container runs `merge.js` on every pod start to generate `openclaw.json` from the operator's template and the user's CR. This ensures consistency but prevents runtime customization. Power users who change models, add custom providers, or tweak agent settings via the OpenClaw UI lose those changes on every restart.

The split is: **infrastructure config** (proxy, auth, gateway, plugins) must always be operator-controlled for security. **Application config** (providers, models, user preferences) can be user-owned.

## Goals / Non-Goals

**Goals:**
- Seed application config (providers, models) on first boot only
- Preserve user runtime edits to application config across restarts
- Continue enforcing infrastructure config (proxy, auth) in all modes
- Support agentFiles seeding in both modes

**Non-Goals:**
- Allowing users to override gateway auth or proxy settings
- Partial user-managed mode (e.g., user-managed models but operator-managed providers)
- Config drift detection or reconciliation in user-managed mode

## Decisions

### Two-mode enum, not a boolean

`spec.config.management` is an enum (`operator` | `user`), not a boolean. This leaves room for future modes (e.g., `hybrid`) without a boolean-to-enum migration.

**Alternative considered:** `spec.config.userManaged: true`. Rejected because boolean fields can't extend to a third state without deprecation.

### Init container detects first boot via filesystem

In user-managed mode, `merge.js` checks whether `openclaw.json` already exists on the PVC. If missing (first boot), it seeds the full config. If present (restart), it only updates infrastructure sections (proxy, auth, gateway) and leaves application sections untouched.

**Why filesystem check, not a status field:** The PVC is the ground truth. A status field could drift if the operator is reinstalled or the status is cleared. The filesystem check is idempotent and requires no external state.

### Whole-home PVC mount in user-managed mode

The init-config container mounts the entire PVC home directory (not just the workspace subdirectory) so it can read and update `openclaw.json` and other config files that live outside the workspace.

**Why:** In operator-managed mode, only the workspace subdirectory is mounted on the init container. User-managed mode needs broader access because the user may have changed configs outside the workspace.

### CLAW_CONFIG_MANAGEMENT env var signals mode to merge.js

The reconciler sets `CLAW_CONFIG_MANAGEMENT=user` on the init-config container. `merge.js` reads this env var to decide between full-merge and infrastructure-only-merge behavior.

**Why env var, not a config file:** The env var is set before `merge.js` runs. A config file approach would create a chicken-and-egg problem (the config file is what merge.js is generating).

## Risks / Trade-offs

- **[Config drift is invisible]** In user-managed mode, the operator has no visibility into what the user changed at runtime. If something breaks, the admin can't tell whether the operator config or the user edit caused it. Mitigated by the ability to switch back to `operator` mode (which overwrites the user config).
- **[Infrastructure boundary is implicit]** Which config sections are "infrastructure" vs "application" is defined in `merge.js`, not in the CRD schema. Changes to merge.js could unintentionally reclassify a section.
