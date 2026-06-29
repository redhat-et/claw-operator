## 1. CRD Changes

- [ ] 1.1 Add `RestrictionsSpec` type with `PluginInstallation *bool` field
- [ ] 1.2 Add `Restrictions` field to `ClawSpec` with `+optional`
- [ ] 1.3 Add `RestrictionsEnforced` condition type constant
- [ ] 1.4 Regenerate CRD manifests and deep copy

## 2. Reconciler Implementation

- [ ] 2.1 Check `restrictions.pluginInstallation` before injecting the plugins init container — skip injection when `false`
- [ ] 2.2 Set `RestrictionsEnforced` status condition to `True` when any restriction is active

## 3. Tests

- [ ] 3.1 Unit test: pluginInstallation omitted — plugins init container present
- [ ] 3.2 Unit test: pluginInstallation false — plugins init container absent, spec.plugins ignored
- [ ] 3.3 Unit test: pluginInstallation true — same as omitted
- [ ] 3.4 Unit test: RestrictionsEnforced condition set when restriction active
