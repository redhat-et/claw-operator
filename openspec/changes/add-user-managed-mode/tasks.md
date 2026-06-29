## 1. CRD Changes

- [ ] 1.1 Add `ConfigManagement` enum type with `operator` (default) and `user` values
- [ ] 1.2 Add `Management` field to `ConfigSpec` with `+optional` and `+kubebuilder:default=operator`
- [ ] 1.3 Regenerate CRD manifests and deep copy

## 2. Init-Config Logic

- [ ] 2.1 Set `CLAW_CONFIG_MANAGEMENT=user` env var on init-config container when `config.management` is `user`
- [ ] 2.2 Mount whole PVC home directory on init-config container in user-managed mode
- [ ] 2.3 Update `merge.js` to detect first boot (check `openclaw.json` existence on PVC) and branch behavior:
  - First boot: seed full config
  - Restart: update infrastructure sections only, preserve application sections

## 3. Plugin Support

- [ ] 3.1 Ensure plugins init container runs in both management modes
- [ ] 3.2 Register implicit provider plugins in user-managed mode

## 4. Tests

- [ ] 4.1 Unit test: user-managed mode sets correct env var and volume mounts
- [ ] 4.2 Unit test: operator-managed mode is unchanged
- [ ] 4.3 Integration test: merge.js first boot — full config seeded
- [ ] 4.4 Integration test: merge.js restart — infrastructure updated, application preserved
- [ ] 4.5 Integration test: mode switch from user to operator restores full management
