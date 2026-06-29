## Context

The upstream operator seeds individual workspace files via `spec.workspace.files` — a map of path-to-content strings. This works for small config snippets but not for enterprise agent profiles that include multiple files (SOUL.md, AGENTS.md, TOOLS.md, etc.) organized in a directory tree. Enterprise teams maintain these profiles in Git repositories alongside their Claw manifests.

The existing init-config container runs `merge.js` to configure `openclaw.json`. The agentFiles feature extends this container with Git clone and ConfigMap extraction capabilities, reusing the existing container image and volume mounts.

## Goals / Non-Goals

**Goals:**
- Seed entire directory trees from Git repos or ConfigMap archives
- Support private Git repos without exposing credentials to the agent
- Protect critical files from agent modification via read-only mounts
- Work identically in operator-managed and user-managed modes
- Preserve user edits across pod restarts (IfMissing policy)

**Non-Goals:**
- Replacing `spec.workspace.files` (both mechanisms coexist)
- Syncing files at runtime (seeding happens at pod start only)
- Supporting non-Git VCS (SVN, Mercurial, etc.)
- Encrypting files at rest on the PVC

## Decisions

### Two mutually exclusive sources: ConfigMap or Git

`spec.agentFiles` requires exactly one of `configMapRef` or `git`. This is enforced by CEL validation rules on the CRD. ConfigMap is simpler (no external dependencies) but limited to 1 MiB and requires a separate packaging step. Git is more natural for teams already using GitOps.

**Why mutually exclusive:** Merging files from two sources creates precedence ambiguity. One source keeps the mental model simple — "these files came from this repo."

**Alternative considered:** Supporting both simultaneously with a precedence order. Rejected because debugging "which source provided this file" becomes non-trivial, and there's no clear use case.

### ApplyPolicy: IfMissing (default) vs Always

`IfMissing` (default) seeds files only if they don't exist on the PVC. Users and the agent can modify seeded files at runtime, and those edits survive pod restarts. `Always` overwrites on every restart, enforcing the Git/ConfigMap version as the source of truth.

**Why IfMissing as default:** Power users expect their runtime edits to persist. Changing this default would surprise users who customized their workspace.

**When to use Always:** In locked-down scenarios (Scenario F) where the agent should not be able to permanently modify its own configuration files.

### Git credentials via mounted Secret, not env vars

`spec.agentFiles.git.secretRef` references a Secret with `username` and `password` keys. The Secret is mounted at `/etc/git-credentials` on the init-config container only. `merge.js` reads the credentials, writes a temporary GIT_ASKPASS helper script, and cleans it up in a `finally` block. The password never appears in process arguments or environment variables.

**Why not env vars:** Process arguments and env vars are visible via `/proc/<pid>/cmdline` and `/proc/<pid>/environ` from any process in the same pid namespace. Volume mounts are more contained.

**Why init container only:** The gateway container (where the agent runs) never sees the Git credentials. This preserves the security boundary — the agent can read the cloned files but cannot discover the repository credentials.

### ReadOnly via subPath and directory mounts

`spec.agentFiles.readOnly` accepts a list of workspace-relative paths. Individual files (e.g., `SOUL.md`) get subPath volume mounts from a read-only copy. Directories (trailing `/` or `/**`) get whole-directory read-only mounts.

**Why filesystem-level protection:** The agent runs arbitrary code. Instruction-level guardrails ("don't modify SOUL.md") can be bypassed. A read-only mount returns `EROFS` on write — the kernel enforces the restriction, not the agent.

**Why subPath for files:** A whole-directory mount would mask other files in the same directory. subPath mounts overlay a single file, leaving the rest of the directory writable.

**Alternative considered:** Using a separate ConfigMap (the deprecated `personaRef` approach). Rejected because it requires maintaining files in two places — the agentFiles source and a separate ConfigMap. `readOnly` protects files from the same source, eliminating duplication.

### Mode-agnostic: works with both operator-managed and user-managed

The agentFiles configuration is decoupled from config management mode. Initially, agentFiles was tied to user-managed mode, but we refactored it into a standalone function. The init-config container handles agentFiles regardless of the `CLAW_CONFIG_MANAGEMENT` env var.

**Why:** Enterprise profiles are useful in both modes. An operator-managed instance with a seeded SOUL.md is a common deployment pattern.

## Risks / Trade-offs

- **[Git clone at every pod start]** With `applyPolicy: Always`, the init container clones the repo on every restart. For large repos, this adds latency. Mitigated by supporting `git.path` (sparse checkout of a subdirectory) and `git.ref` (pinning to a branch or tag).
- **[ConfigMap 1 MiB limit]** The ConfigMap source is limited by Kubernetes' 1 MiB ConfigMap size. For large agent file trees, Git is the recommended source.
- **[ReadOnly path conflicts]** If a readOnly path doesn't exist in the seeded files, the mount fails silently (empty file). Documentation should emphasize that readOnly paths must match seeded file names.
