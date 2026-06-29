## 1. CRD Changes

- [ ] 1.1 Add `BuiltinPassthroughs *[]string` field to `NetworkSpec` in `api/v1alpha1/claw_types.go` with `+optional`
- [ ] 1.2 Regenerate CRD manifests and deep copy

## 2. Proxy Config Changes

- [ ] 2.1 Add `filterBuiltinPassthroughs(allowlist *[]string, builtins []builtinPassthrough)` function that returns the filtered builtin list and a list of unrecognized domain names
- [ ] 2.2 Modify `generateProxyConfig` to accept pre-filtered builtins instead of the raw `*[]string`
- [ ] 2.3 Call `filterBuiltinPassthroughs` once in the reconciler; log unrecognized domains as warnings
- [ ] 2.4 Ensure credential routes for blocked builtin domains are still generated

## 3. Tests

- [ ] 3.1 Unit test: nil `builtinPassthroughs` — all builtins present in proxy config
- [ ] 3.2 Unit test: subset of builtins — only listed domains present
- [ ] 3.3 Unit test: empty list — no builtins in proxy config
- [ ] 3.4 Unit test: unrecognized domain — returned in the unrecognized list
- [ ] 3.5 Unit test: credential on a blocked builtin domain — credential route still present
- [ ] 3.6 Integration test: full reconcile with `builtinPassthroughs` set, verify proxy ConfigMap output
