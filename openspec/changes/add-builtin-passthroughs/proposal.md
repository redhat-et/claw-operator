## Why

The proxy hardcodes six builtin passthrough domains that are always reachable without credential injection: clawhub.ai, openrouter.ai, github.com, codeload.github.com, raw.githubusercontent.com, and registry.npmjs.org. On locked-down enterprise clusters, ITOps needs to block some or all of these public domains — for example, blocking npmjs.org to force agents to use an internal npm mirror, or blocking openrouter.ai to prevent unauthorized model usage. Currently there is no way to disable individual builtins without modifying the operator source.

## What Changes

- Add an optional `spec.network.builtinPassthroughs` field (pointer to string slice) to the existing `NetworkSpec`
- When nil (omitted): all six builtin domains remain active — full backward compatibility
- When set to a list: only the listed builtin domains are allowed; omitted builtins are blocked by the proxy
- When set to an empty list: all builtin passthroughs are blocked
- Unrecognized domain names in the list are logged as warnings (catches typos like `"clawhb.ai"`)
- Credentials on blocked builtin domains still work — the credential route takes precedence over the passthrough block

## Capabilities

### New Capabilities

- `builtin-passthroughs-allowlist`: Control which builtin proxy passthrough domains are reachable via `spec.network.builtinPassthroughs`, enabling enterprise proxy lockdown without disabling credential-injected routes

### Modified Capabilities

- `network-security`: Add builtinPassthroughs filtering to the proxy config generation pipeline

## Impact

- `api/v1alpha1/claw_types.go` — add BuiltinPassthroughs field to NetworkSpec
- `internal/controller/claw_proxy.go` — add filterBuiltinPassthroughs function; pass filtered list to generateProxyConfig
- `internal/controller/claw_resource_controller.go` — call filter, log unrecognized domains
- CRD manifest regeneration
- No breaking changes — nil (omitted) preserves all existing builtins
