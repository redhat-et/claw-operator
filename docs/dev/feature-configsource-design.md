# Enterprise Configuration Design

Status: **Partially implemented — extending with profiles**

## What's shipped

| Feature | Field | Status |
|---------|-------|--------|
| Passthrough control | `spec.network.builtinPassthroughs` | Merged (PR #5) |
| User-managed mode | `spec.config.management: user` | Merged (PR #4) |
| Agent file seeding | `spec.agentFiles` (ConfigMap or Git) | Merged (PR #4) |
| Read-only persona | `spec.restrictions.personaRef` | Merged (PR #7) |
| Plugin control | `spec.restrictions.pluginInstallation` | Merged (PR #7) |
| Shared credentials | Multiple CRs reference same Secret | Built-in |
| Inline skills | `spec.skills` | Built-in |
| Workspace files | `spec.workspace.files` | Built-in |

## What's next: department profiles via `spec.agentFiles`

The merged `spec.agentFiles` already supports seeding a complete
agent configuration from a Git repo directory. The next step is
making this work for the **operator-managed** deployment model
where ITOps controls persona and skills per department while the
operator still manages providers, models, and infrastructure.

### Current limitation

`spec.agentFiles` is documented as "intended for use with
`spec.config.management=user`." In operator-managed mode, the
operator injects its own persona (AGENTS.md, SOUL.md) and
skills, which could conflict with agentFiles content.

### Proposed: decouple `agentFiles` from user mode

Allow `spec.agentFiles` in operator-managed mode with clear
precedence rules:

| Source | Skills | Persona | Config |
|--------|--------|---------|--------|
| OpenClaw bundled | lowest | lowest | defaults |
| agentFiles (Git/ConfigMap) | `copyTreeWithPolicy` | `copyFileWithPolicy` | seed openclaw.json |
| Operator ConfigMap | `_skill_*` keys | AGENTS.md, SOUL.md | operator.json |
| Inline `spec.skills` | via `_skill_*` | via `spec.workspace` | `spec.config.raw` |
| User edits (PVC) | highest | highest | highest |

In operator-managed mode, operator-injected skills and persona
files would override agentFiles content (they use `copyAlways`).
In user-managed mode, agentFiles content persists because the
operator skips injection.

This means a department profile in a Git repo provides the
**base** that the operator enriches with infrastructure config.
The admin can still override specific skills via `spec.skills`
or persona via `spec.workspace.files`.

### Proposed: `secretRef` for private Git repos

Add authentication support to `AgentFilesGitSource`:

```go
type AgentFilesGitSource struct {
    URL       string `json:"url"`
    Ref       string `json:"ref,omitempty"`
    Path      string `json:"path,omitempty"`
    SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}
```

When `secretRef` is set, the operator mounts the Secret into the
init-config container. merge.js embeds only the `username` into
the clone URL (so Git knows which user to authenticate as) and
writes the `password` to a temporary credential file read by a
`GIT_ASKPASS` helper script. The password never appears in the
URL, process args, or logs. The Secret format follows Git
conventions: `username` + `password` keys for HTTPS.

### Git clone routing through the proxy

Sally's PR #4 already solved this: the init-config container
gets `HTTP_PROXY` / `HTTPS_PROXY` env vars pointing to the MITM
proxy, plus `GIT_SSL_CAINFO` for the proxy CA. Git clones route
through the proxy and benefit from builtin passthrough domains
(github.com is allowed by default). No extra NetworkPolicy rules
needed.

### Repo directory structure (unchanged)

```
claw-configs/
├── hr-team/
│   ├── workspace-main/        # mapped to workspace/
│   │   ├── skills/
│   │   │   └── hr-policy/
│   │   │       └── SKILL.md
│   │   ├── AGENTS.md
│   │   └── SOUL.md
│   └── openclaw.json          # config overlay
├── sales-team/
│   ├── workspace-main/
│   │   ├── skills/
│   │   │   └── sales-playbook/
│   │   │       └── SKILL.md
│   │   └── AGENTS.md
│   └── openclaw.json
└── base/
    └── ...
```

Note: uses `workspace-main/` as the workspace directory name,
matching Sally's `seedAgentFiles` implementation which maps
`workspace-main` to the workspace directory.

## Future: read-only skills

`spec.restrictions.personaRef` (PR #7) already handles read-only
persona files (AGENTS.md, SOUL.md) via subPath bind mounts. For
read-only **skills**, a similar mechanism could mount skill
directories from a ConfigMap or use `spec.agentFiles.readOnly` to
make Git-sourced skills immutable. Design TBD — depends on whether
skills need per-file granularity or whole-directory protection.

## Implementation order

1. ~~**Read-only persona**~~ — done (PR #7, `spec.restrictions`)
2. **Add secretRef** — private Git repo authentication ← **next**
3. **Decouple agentFiles from user mode** — allow in
   operator-managed mode with clear precedence
4. **Read-only skills** — future, for autonomous agents
