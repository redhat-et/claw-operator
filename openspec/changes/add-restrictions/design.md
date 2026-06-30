## Context

The operator injects a plugins init container that runs `openclaw plugins install` for each entry in `spec.plugins`. Additionally, OpenClaw allows runtime plugin installation via the agent UI. In locked-down deployments, both paths must be blocked.

## Goals / Non-Goals

**Goals:**
- Provide a hard block on plugin installation via the CRD
- Skip the plugins init container when installation is blocked
- Report restriction status via a status condition

**Non-Goals:**
- Fine-grained plugin allowlisting (e.g., "allow plugin A but block plugin B")
- Runtime enforcement inside the OpenClaw process (that's OpenClaw's responsibility; we block the init container and trust the network policy to prevent runtime downloads)

## Decisions

### Bool pointer for pluginInstallation

`pluginInstallation` is `*bool` — nil (omitted) defaults to true (plugins allowed), `false` blocks installation. This follows the Kubernetes convention for optional boolean fields where the default behavior is the permissive case.

**Why pointer, not plain bool:** A plain `false` is indistinguishable from "user didn't set the field" in Go. The pointer ensures explicit `false` is intentional.

### Skip the init container entirely

When `pluginInstallation` is `false`, the plugins init container is not injected into the Deployment at all — not started, not created, not present. This is stronger than passing a "don't install" flag to the container.

**Why:** Removing the container eliminates any attack surface from the plugin installation code path. It also avoids the confusing state of having an init container that does nothing.

### RestrictionsEnforced status condition

The operator sets a `RestrictionsEnforced` condition when any restriction is active. This gives admins visibility into which instances have restrictions applied.

**Why a condition, not a log:** Conditions are queryable via `kubectl` and programmatic tooling. A log message requires log access and grep.

## Risks / Trade-offs

- **[spec.plugins is silently ignored]** If `pluginInstallation` is `false` but `spec.plugins` lists plugins, the plugins are silently not installed. An alternative would be to reject the manifest, but this was judged too strict — an admin might want to toggle installation on/off without editing the plugins list.
