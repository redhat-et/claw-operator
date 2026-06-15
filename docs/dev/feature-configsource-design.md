# Enterprise Configuration Design

Status: **agentFiles decoupled from user mode (PR #9) — readOnly next**

## What's shipped

| Feature | Field | Status |
|---------|-------|--------|
| Passthrough control | `spec.network.builtinPassthroughs` | Merged (PR #5) |
| User-managed mode | `spec.config.management: user` | Merged (PR #4) |
| Agent file seeding | `spec.agentFiles` (ConfigMap or Git) | Merged (PR #4), decoupled from user mode (PR #9) |
| Read-only persona | `spec.restrictions.personaRef` | Merged (PR #7) |
| Plugin control | `spec.restrictions.pluginInstallation` | Merged (PR #7) |
| Private Git repos | `spec.agentFiles.git.secretRef` | Merged (PR #8) |
| Shared credentials | Multiple CRs reference same Secret | Built-in |
| Inline skills | `spec.skills` | Built-in |
| Workspace files | `spec.workspace.files` | Built-in |

## What's next: unified `agentFiles` model

### Problem with the current design

The current design has three separate mechanisms for injecting
workspace content, each with its own CRD fields and implementation
path:

1. `spec.restrictions.personaRef` — a ConfigMap with AGENTS.md
   and SOUL.md mounted read-only
2. `spec.agentFiles` — seeds workspace from Git/ConfigMap, but
   only works with `management: user`
3. Operator-managed mode — the operator injects its own persona
   and skills, ignoring agentFiles entirely

This creates friction: Scenario B (per-department assistants)
needs ITOps to seed department-specific skills and persona from
Git while keeping infrastructure config operator-managed. The
admin has to choose between control and customization.

### Proposed: single source, per-file control

Replace the three mechanisms with one:

- `spec.agentFiles` provides ALL workspace files from a Git
  repo, independent of management mode
- `spec.agentFiles.readOnly` specifies which files/directories
  are mounted read-only (filesystem enforcement)
- `spec.config.mergeMode` controls config protection
  (merge-on-restart enforcement)

The ITOps admin maintains one Git repo with the complete
workspace definition. The CRD specifies which files to protect.
No separate ConfigMap, no management mode split.

### OpenClaw workspace files and their protection model

Based on the [OpenClaw workspace files][openclaw-files]:

| File | Purpose | Typically provided? | Read-only? |
|------|---------|---------------------|------------|
| SOUL.md | Identity, boundaries, hard constraints | Yes | Yes — this IS the guardrail |
| AGENTS.md | Workflows, procedures, memory rules | Yes | Scenario-dependent |
| IDENTITY.md | Display name, agent ID | Yes | Usually yes |
| TOOLS.md | Tool usage instructions, restrictions | Yes | Yes — controls tool behavior |
| HEARTBEAT.md | Scheduled recurring tasks | Scenario-dependent | Yes for autonomous agents |
| USER.md | Context about the human user | No — user fills in | No |
| MEMORY.md | Agent's accumulated knowledge | No — agent builds | No |
| skills/*.md | Custom skill definitions | Yes | See below |
| openclaw.json | Config overlay (models, agents) | Yes | Via mergeMode (not filesystem) |

[openclaw-files]: https://capodieci.medium.com/ai-agents-003-openclaw-workspace-files-explained-soul-md-agents-md-heartbeat-md-and-more-5bdfbee4827a

### Two enforcement tiers

Workspace files and config need different enforcement because
they work differently:

**Tier 1 — Filesystem enforcement (workspace files):**
Read-only files are mounted via emptyDir overlay with
`readOnly: true`. The agent gets `EROFS: read-only file system`
when trying to edit them. This is hard, real-time enforcement.

**Tier 2 — Merge enforcement (openclaw.json):**
OpenClaw writes to its own config at runtime (memory settings,
channel state, etc.), so it can't be mounted read-only. Instead,
operator-managed keys are re-applied on every pod restart via
`merge.js`. The `mergeMode` field controls how aggressively:

- `merge` — operator keys win, user keys preserved
- `overwrite` — entire config reset from source on restart

### Skills protection model

Read-only protection on persona files (SOUL.md, AGENTS.md) is a
strong defense — the agent can't rewrite its own constraints.

Read-only skills are also a strong defense, **but only when the
user can't add their own skills**. In a locked-down deployment
(Scenario F) where network restrictions block npm, ClawHub, and
GitHub, read-only admin skills are the only skills available —
full control.

When read-only admin skills and user-created skills coexist in
the same workspace, the protection becomes cosmetic. The admin's
`skills/hr-policy/SKILL.md` can't be modified, but nothing
prevents `skills/do-anything/SKILL.md` from being created next
to it. The file is protected; the capability is not.

**Summary: read-only skills are meaningful when paired with
installation restrictions, and an anti-footgun convenience
otherwise.**

| Deployment | Read-only skills | User skills | Security posture |
|------------|-----------------|-------------|------------------|
| Kiosk (F) | Yes | No (network blocked) | Strong |
| Autonomous (E) | Yes | No (network blocked) | Strong |
| Department (B) | Yes | Yes (allowed) | Anti-footgun only |
| Power user (D) | No | Yes | User controls all |

For deployments that allow both, admin-provided skills can live
in a `skills/managed/` subdirectory. OpenClaw discovers skills
recursively, so both admin and user skills are found. The
`readOnly` entry `skills/managed/` prevents accidental edits to
admin skills while leaving the rest of `skills/` writable.

Future: OCI image-based skill distribution (via the skillimage
registry) would use the same read-only mount mechanism — an
OCI-sourced skill directory mounted read-only, just a different
source than Git.

### Replacing `management` mode

With per-file readOnly and per-config mergeMode, the explicit
`management: operator` vs `management: user` distinction becomes
unnecessary. The behavior is derived from the combination:

| Scenario | agentFiles | readOnly | mergeMode | Effect |
|----------|------------|----------|-----------|--------|
| A — shared team | none | — | merge | Bare instance, users customize |
| B — department (flexible) | Git | — | merge | Seeded workspace, users can modify |
| B — department (controlled) | Git | SOUL.md, TOOLS.md | merge | Locked persona, flexible workspace |
| E — autonomous agent | Git | SOUL.md, AGENTS.md, HEARTBEAT.md | overwrite | Locked behavior, no drift |
| F — kiosk | Git | SOUL.md, AGENTS.md, TOOLS.md | overwrite | Full lockdown |

### Replacing `personaRef`

`spec.restrictions.personaRef` (PR #7) becomes a special case of
`spec.agentFiles.readOnly` with SOUL.md and AGENTS.md listed.
The unified model is cleaner — one mechanism instead of two.

`personaRef` should be deprecated in favor of
`agentFiles.readOnly`. If demand emerges for ConfigMap-only
workflows (no Git), it can be revisited.

### What stays in the CRD

Some concerns must remain in the CRD because they are
infrastructure, not behavioral:

- `spec.credentials` — API keys in K8s Secrets, never in Git
- `spec.network` — builtinPassthroughs, additionalEgress
- `spec.mcpServers` — MCP endpoints with auto-egress rules
- `spec.auth` — token/password mode, device pairing
- `spec.agentFiles` — the Git URL, ref, path, readOnly list
- `spec.config.mergeMode` — how config is reconciled on restart
- `spec.config.raw` — inline config overrides (escape hatch)

Everything behavioral (persona, skills, model preferences,
agent workflows, tool instructions) moves to the Git repo.

### CRD shape

```yaml
spec:
  agentFiles:
    git:
      url: https://github.com/corp/configs.git
      ref: v1.0.0
      path: hr
      secretRef:
        name: git-creds
    applyPolicy: IfMissing    # or Always for autonomous agents
    readOnly:                  # files/patterns mounted read-only
      - SOUL.md
      - TOOLS.md
      - IDENTITY.md
      - skills/managed/**
  config:
    mergeMode: merge           # merge or overwrite
    raw: {}                    # inline config overrides
  credentials:
    - name: anthropic
      provider: anthropic
      secretRef:
        - name: shared-anthropic
          key: api-key
```

### Git repo structure

```
corp-claw-configs/
├── hr/
│   ├── workspace-main/          # maps to workspace/
│   │   ├── SOUL.md              # HR persona and constraints
│   │   ├── AGENTS.md            # HR workflows and procedures
│   │   ├── TOOLS.md             # Approved tools and usage notes
│   │   ├── IDENTITY.md          # Display name, agent ID
│   │   └── skills/
│   │       └── managed/         # admin-provided, read-only
│   │           └── hr-policy/
│   │               └── SKILL.md
│   └── openclaw.json            # model preferences, agent defaults
├── sales/
│   ├── workspace-main/
│   │   └── ...
│   └── openclaw.json
└── engineering/
    └── ...
```

### Implementation: read-only via emptyDir overlay

The read-only mechanism uses a Kubernetes emptyDir volume as an
intermediary. The init-config container writes protected files
into the emptyDir; the gateway container mounts them read-only
on top of the writable PVC.

**Init-config container** (writable access to both volumes):

1. Clones Git repo, seeds all files into the PVC
2. Reads `AGENT_FILES_READ_ONLY` env var (pattern list)
3. Resolves patterns against the cloned tree
4. Copies matched files into `/protected-files/` emptyDir,
   preserving directory structure
5. Writes a manifest of resolved paths to
   `/protected-files/.manifest` for the operator to consume
   on subsequent reconciles (see Pattern Resolution below)

**Gateway container** (read-only access to protected files):

For individual files, subPath mounts overlay the PVC:
```yaml
volumeMounts:
  - name: claw-home
    mountPath: /home/node/.openclaw
  - name: protected-files
    mountPath: /home/node/.openclaw/workspace/SOUL.md
    subPath: workspace/SOUL.md
    readOnly: true
```

For directory patterns (e.g., `skills/managed/`), mount the
entire directory:
```yaml
  - name: protected-files
    mountPath: /home/node/.openclaw/workspace/skills/managed
    subPath: workspace/skills/managed
    readOnly: true
```

### Pattern resolution

The operator needs to generate subPath mounts at reconcile time,
but patterns like `skills/managed/**` can only be resolved
against the actual Git tree at clone time (the operator doesn't
clone Git).

**For individual files** (no wildcards): the operator generates
subPath mounts directly from the `readOnly` list. No runtime
resolution needed.

**For directory patterns** (trailing `/` or `**`): the operator
mounts the entire directory read-only. This is simpler and
matches the intent — "protect everything under this path." The
init-config populates the directory in the emptyDir; the gateway
sees it as a single read-only mount.

Wildcard patterns that don't map to a single directory
(e.g., `*.md`) are not supported — use explicit file paths or
directory mounts. This keeps the operator's mount generation
deterministic without runtime Git access.

**Supported patterns:**

| Pattern | Operator action |
|---------|----------------|
| `SOUL.md` | subPath mount for one file |
| `skills/managed/` | directory mount |
| `skills/managed/**` | same as `skills/managed/` |
| `*.md` | not supported — list files explicitly |

### Volumes generated by the operator

```yaml
volumes:
  - name: claw-home
    persistentVolumeClaim:
      claimName: hr-assistant
  - name: protected-files
    emptyDir: {}
  - name: config
    configMap:
      name: hr-assistant

initContainers:
  - name: init-config
    volumeMounts:
      - name: claw-home
        mountPath: /home/node/.openclaw
      - name: protected-files
        mountPath: /protected-files    # writable for init
      - name: config
        mountPath: /config

containers:
  - name: gateway
    volumeMounts:
      - name: claw-home
        mountPath: /home/node/.openclaw
      # Read-only overlays (generated from readOnly list):
      - name: protected-files
        mountPath: /home/node/.openclaw/workspace/SOUL.md
        subPath: workspace/SOUL.md
        readOnly: true
      - name: protected-files
        mountPath: /home/node/.openclaw/workspace/TOOLS.md
        subPath: workspace/TOOLS.md
        readOnly: true
      - name: protected-files
        mountPath: /home/node/.openclaw/workspace/IDENTITY.md
        subPath: workspace/IDENTITY.md
        readOnly: true
      - name: protected-files
        mountPath: /home/node/.openclaw/workspace/skills/managed
        subPath: workspace/skills/managed
        readOnly: true
```

### Interaction with applyPolicy

`applyPolicy` controls whether agentFiles content is re-seeded
on restart:

- `IfMissing` — seed once, preserve user edits to writable files
  (USER.md, MEMORY.md, unprotected AGENTS.md). Read-only files
  are always refreshed from Git (via emptyDir, which is
  ephemeral).
- `Always` — re-seed everything on every restart. User edits to
  writable files are overwritten. Combined with readOnly, this
  creates a fully immutable workspace.

Read-only files are always fresh regardless of applyPolicy
because the emptyDir is ephemeral — it's repopulated from Git
on every pod start. The applyPolicy only affects files written
to the PVC.

## Implementation order

1. ~~**Read-only persona**~~ — done (PR #7, `spec.restrictions`)
2. ~~**Add secretRef**~~ — done (PR #8, `agentFiles.git.secretRef`)
3. ~~**Decouple agentFiles from user mode**~~ — done (PR #9)
4. **Add readOnly to agentFiles** — emptyDir overlay mechanism,
   supports files and directory patterns
5. **Deprecate personaRef** — migrate to `agentFiles.readOnly`,
   keep personaRef working but document as deprecated
6. **Deprecate management mode** — derive behavior from
   agentFiles + readOnly + mergeMode
7. **Documentation** — update enterprise onboarding workflows
   with unified model examples

Steps 5-6 are backward-compatible: existing CRs with
`management: user` and `personaRef` continue to work.
The new fields provide the same capabilities with a cleaner
model.
