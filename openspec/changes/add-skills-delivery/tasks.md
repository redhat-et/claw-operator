## 1. CRD Changes

- [ ] 1.1 Add `SkillsSpec`, `SkillImageSpec`, `SkillConfigMapRef` types to `api/v1alpha1/claw_types.go`
- [ ] 1.2 Add `imagePullSecrets` field to `SkillImageSpec` with `+listType=map` and `+listMapKey=name`
- [ ] 1.3 **BREAKING:** Change `Skills` field in `ClawSpec` from `map[string]string` to `*SkillsSpec`
- [ ] 1.4 Add `name` pattern validation on `SkillImageSpec` (`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
- [ ] 1.5 Regenerate CRD manifests and deep copy

## 2. Inline Content (migration from upstream)

- [ ] 2.1 Update ConfigMap generation to read skills from `skills.content` instead of the flat `skills` map
- [ ] 2.2 Inject content skills as `_skill_<name>` keys in the gateway ConfigMap (preserving existing merge.js behavior)

## 3. OCI Image Delivery

- [ ] 3.1 Generate ImageVolume entries for each `skills.images` entry in the gateway Deployment pod spec
- [ ] 3.2 Mount each ImageVolume read-only at `workspace/skills/<name>/`
- [ ] 3.3 Handle `pullPolicy` defaulting (Always for `:latest`, IfNotPresent otherwise)
- [ ] 3.4 Collect and deduplicate `imagePullSecrets` across all skill images; merge into pod spec

## 4. ConfigMap References

- [ ] 4.1 Read each referenced ConfigMap via UserSecretReader (direct API call, not cached)
- [ ] 4.2 Inject ConfigMap keys as `_skill_<key>` entries in the gateway ConfigMap
- [ ] 4.3 Return reconcile error if a referenced ConfigMap does not exist

## 5. Collision Detection

- [ ] 5.1 Collect all skill names across content, images, and configMap keys
- [ ] 5.2 Detect duplicates and return a reconcile error identifying the collision source

## 6. Tests

- [ ] 6.1 Unit test: inline content skill injection (migration from flat map)
- [ ] 6.2 Unit test: OCI image — ImageVolume generation, read-only mount, pullPolicy defaulting
- [ ] 6.3 Unit test: imagePullSecrets — collection, deduplication, merge
- [ ] 6.4 Unit test: ConfigMap ref — key injection, missing ConfigMap error
- [ ] 6.5 Unit test: collision detection across all source combinations
- [ ] 6.6 Integration test: end-to-end reconciliation with all three skill sources
- [ ] 6.7 E2E test: skill image validation in kind cluster
