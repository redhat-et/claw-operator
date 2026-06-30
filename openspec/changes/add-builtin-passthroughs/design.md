## Context

The proxy generates its route table from two sources: credential-derived routes (from `spec.credentials`) and builtin passthrough routes (hardcoded in the operator). The six builtins allow the agent to reach common services (npm registry, GitHub raw content, ClawHub, OpenRouter) without requiring explicit credential entries.

In enterprise environments, these defaults are a liability. Security policies may require blocking public registries, restricting model providers to approved ones, or routing all traffic through internal mirrors. The proxy's MITM architecture already blocks unknown domains — the problem is that builtins bypass this block unconditionally.

## Goals / Non-Goals

**Goals:**
- Allow admins to selectively disable individual builtin passthrough domains
- Preserve full backward compatibility when the field is omitted
- Warn on typos in domain names (unrecognized entries)
- Ensure credential-injected routes for builtin domains still work when the passthrough is blocked

**Non-Goals:**
- Adding new builtin domains (that's a separate change)
- Allowing custom passthrough domains without credentials (use `type: none` credentials for that)
- Changing the proxy's MITM behavior for non-builtin domains

## Decisions

### Pointer to slice (`*[]string`) for nil-vs-empty distinction

The field is `*[]string`, not `[]string`. This creates a three-state semantic:
- **nil** (field omitted): all builtins active — backward compatible
- **non-nil, non-empty**: only listed builtins active
- **non-nil, empty** (`[]`): all builtins blocked

**Why:** A plain `[]string` cannot distinguish "user didn't set the field" from "user set it to empty list." Without the pointer, omitting the field would block all builtins — a breaking change for every existing manifest.

### Credential routes override passthrough blocks

If a builtin domain is blocked by `builtinPassthroughs` but also has a credential entry in `spec.credentials`, the credential route is still generated. This ensures that blocking `github.com` as a passthrough doesn't prevent a credential-injected GitHub API integration.

**Why:** Passthroughs and credentials serve different purposes. A passthrough allows unauthenticated access; a credential injects authentication. Blocking unauthenticated access to GitHub while allowing authenticated access is a valid enterprise configuration.

### Unrecognized domains logged as warnings

The filter function returns a list of domain names not found in the builtin set. The reconciler logs these as warnings. This catches typos (`"clawhb.ai"` instead of `"clawhub.ai"`) without rejecting the manifest.

**Why warning, not error:** A domain name might be a future builtin that the current operator version doesn't know about. Hard rejection would break forward compatibility. A warning is sufficient for catching typos.

**Alternative considered:** CEL enum validation on the CRD. Rejected because the builtin list is defined in Go code, not in the CRD schema, and adding a new builtin would require a CRD schema update.

### Filter once, pass the result

`filterBuiltinPassthroughs` is called once in the reconciler. The filtered list is passed to `generateProxyConfig` as a pre-filtered slice, not as the raw `*[]string`. This avoids filtering twice per reconcile and keeps the proxy config generator pure.

## Risks / Trade-offs

- **[No CRD-level validation of domain names]** Typos are caught at runtime (warning log), not at admission time. The alternative (CEL enum) would couple the CRD schema to the Go-level builtin list.
- **[Blocking all builtins may surprise users]** Setting `builtinPassthroughs: []` blocks npm, GitHub raw content, and ClawHub. Agents relying on these services will fail. Documentation should list which services each builtin domain supports.
