## 1. CRD Changes

- [ ] 1.1 Add `PluginCompatibility`, `VersionDowngrade` condition type constants
- [ ] 1.2 Add `Incompatible`, `VersionDowngrade`, `InitContainerFailure` condition reason constants
- [ ] 1.3 Add `LastDeployedVersion string` field to `ClawStatus`
- [ ] 1.4 Regenerate CRD manifests and deep copy

## 2. Init Container Failure Detection

- [ ] 2.1 When deployments are not ready, list pods and inspect init container statuses for non-zero exit codes or CrashLoopBackOff
- [ ] 2.2 Set Ready condition to `False` with reason `InitContainerFailure` and include the error message
- [ ] 2.3 Fall back to reason `Provisioning` when no init container failures are detected

## 3. Plugin Compatibility Check

- [ ] 3.1 Add `PluginMinVersion` field to `knownProviders` table for plugins with known version dependencies
- [ ] 3.2 Compare `spec.version` against plugin minimum versions during reconciliation
- [ ] 3.3 Set `PluginCompatibility` condition to `True` with reason `Incompatible` when version is too old

## 4. Version Downgrade Warning

- [ ] 4.1 Compare `spec.version` against `status.lastDeployedVersion` during reconciliation
- [ ] 4.2 Set `VersionDowngrade` condition to `True` with reason `VersionDowngrade` when downgrade detected
- [ ] 4.3 Update `status.lastDeployedVersion` as a high-water mark when Ready=True and spec.version exceeds current value

## 5. Tests

- [ ] 5.1 Unit test: init container CrashLoopBackOff — Ready condition shows InitContainerFailure with error message
- [ ] 5.2 Unit test: init container non-zero exit — Ready condition shows InitContainerFailure with exit code
- [ ] 5.3 Unit test: deployment pending (no init failure) — Ready condition shows Provisioning
- [ ] 5.4 Unit test: version older than plugin minimum — PluginCompatibility condition set
- [ ] 5.5 Unit test: version meets plugin minimum — no PluginCompatibility condition
- [ ] 5.6 Unit test: version downgrade — VersionDowngrade condition set
- [ ] 5.7 Unit test: version upgrade — no VersionDowngrade condition
- [ ] 5.8 Unit test: lastDeployedVersion high-water mark — stays at maximum after downgrade
