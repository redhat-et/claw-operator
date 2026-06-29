## ADDED Requirements

### Requirement: SkillsSpec supports ConfigMap references
The `SkillsSpec` type SHALL include an optional `configMaps` field — a list of `SkillConfigMapRef` entries. Each ConfigMap key becomes a skill name and its value becomes SKILL.md content.

#### Scenario: No configMaps configured
- **WHEN** `spec.skills.configMaps` is omitted or empty
- **THEN** no ConfigMap-sourced skills SHALL be injected

#### Scenario: Single ConfigMap referenced
- **WHEN** `spec.skills.configMaps` contains `{name: "team-skills"}`
- **AND** the ConfigMap `team-skills` has keys `code-review` and `test-gen`
- **THEN** skills `workspace/skills/code-review/SKILL.md` and `workspace/skills/test-gen/SKILL.md` SHALL be created with the corresponding values

#### Scenario: Multiple ConfigMaps referenced
- **WHEN** `spec.skills.configMaps` lists multiple ConfigMaps
- **THEN** keys from all ConfigMaps SHALL be injected as skills

### Requirement: ConfigMap is read via direct API call
The operator SHALL read referenced ConfigMaps via a direct API call (UserSecretReader), not through the label-filtered informer cache.

#### Scenario: ConfigMap without operator labels
- **WHEN** a skill ConfigMap exists without operator-specific labels
- **THEN** the operator SHALL still read its contents successfully

### Requirement: Missing ConfigMap causes reconcile error
The operator SHALL return a reconcile error if a referenced skill ConfigMap does not exist.

#### Scenario: ConfigMap does not exist
- **WHEN** `spec.skills.configMaps` references a non-existent ConfigMap
- **THEN** the reconciler SHALL return an error
- **THEN** the Ready condition SHALL reflect the failure

### Requirement: ConfigMap skills are operator-managed
ConfigMap-sourced skills SHALL be overwritten on every pod restart, matching the behavior of inline content skills.

#### Scenario: Skill content updated in ConfigMap
- **WHEN** a skill ConfigMap's value is changed
- **THEN** the skill SHALL be updated on the next pod restart
