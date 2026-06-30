## 1. CRD Changes

- [ ] 1.1 Add `AgentFilesSpec`, `AgentFilesConfigMapRef`, `AgentFilesGitSource`, `GitSecretRef` types to `api/v1alpha1/claw_types.go`
- [ ] 1.2 Add `AgentFilesApplyPolicy` enum type with `IfMissing` (default) and `Always` values
- [ ] 1.3 Add `AgentFiles` field to `ClawSpec`
- [ ] 1.4 Add CEL validation rules: configMapRef/git mutual exclusivity, at-least-one-source requirement
- [ ] 1.5 Add `git.url` pattern validation (`^https://`)
- [ ] 1.6 Regenerate CRD manifests and deep copy

## 2. ConfigMap Source

- [ ] 2.1 Add init-config container logic to extract a gzipped tar archive from a ConfigMap key into the workspace directory
- [ ] 2.2 Support custom key name (default: `agentfiles.tgz`)
- [ ] 2.3 Respect `applyPolicy` — skip extraction for existing files when `IfMissing`

## 3. Git Source

- [ ] 3.1 Add init-config container logic to clone a Git repository into the workspace
- [ ] 3.2 Support `git.ref` (branch, tag, commit) checkout after clone
- [ ] 3.3 Support `git.path` — copy only a subdirectory's contents into the workspace
- [ ] 3.4 Respect `applyPolicy` — skip clone for existing files when `IfMissing`

## 4. Private Git Authentication

- [ ] 4.1 Mount `git.secretRef` Secret at `/etc/git-credentials` on the init-config container only
- [ ] 4.2 Implement GIT_ASKPASS helper script in `merge.js` — read password from mounted file, never expose in args or env
- [ ] 4.3 Clean up askpass script in a `finally` block after clone
- [ ] 4.4 Validate Secret existence and required keys (`username`, `password`) during reconciliation
- [ ] 4.5 Stamp Secret ResourceVersion as pod annotation to trigger rollout on credential rotation

## 5. Read-Only Mounts

- [ ] 5.1 Parse `readOnly` list to distinguish individual files from directories (trailing `/` or `/**`)
- [ ] 5.2 Generate subPath volume mounts for individual read-only files
- [ ] 5.3 Generate whole-directory read-only volume mounts for directory entries
- [ ] 5.4 Ensure read-only mounts do not mask writable files in the same directory

## 6. Mode Independence

- [ ] 6.1 Extract agentFiles configuration as a mode-agnostic function callable from both operator-managed and user-managed code paths
- [ ] 6.2 Verify agentFiles init-config logic is independent of `CLAW_CONFIG_MANAGEMENT` env var

## 7. Tests

- [ ] 7.1 Unit tests for ConfigMap source: extraction, default key, custom key, applyPolicy
- [ ] 7.2 Unit tests for Git source: clone, ref checkout, subdirectory path, applyPolicy
- [ ] 7.3 Unit tests for private Git: credential mount, Secret validation, rollout trigger
- [ ] 7.4 Unit tests for readOnly: individual file subPath mount, directory mount, empty list
- [ ] 7.5 Unit tests for mode independence: agentFiles with operator-managed, agentFiles with user-managed
- [ ] 7.6 Integration test: end-to-end reconciliation with agentFiles.git and readOnly

## 8. Documentation

- [ ] 8.1 Add agentFiles section to user guide with examples for Git source, ConfigMap source, and readOnly
