## ADDED Requirements

### Requirement: Skill name collisions across sources are rejected
The operator SHALL detect skill name collisions across all three delivery channels (content, images, configMaps) and return a reconcile error.

#### Scenario: Content and image collision
- **WHEN** `skills.content` has key `"my-skill"` and `skills.images` has an entry with `name: "my-skill"`
- **THEN** the reconciler SHALL return an error identifying the collision
- **THEN** the Ready condition SHALL reflect the failure

#### Scenario: Content and ConfigMap collision
- **WHEN** `skills.content` has key `"my-skill"` and a referenced ConfigMap also has key `"my-skill"`
- **THEN** the reconciler SHALL return an error identifying the collision

#### Scenario: Image and ConfigMap collision
- **WHEN** `skills.images` has an entry with `name: "my-skill"` and a referenced ConfigMap has key `"my-skill"`
- **THEN** the reconciler SHALL return an error identifying the collision

#### Scenario: Cross-ConfigMap collision
- **WHEN** two different ConfigMaps both have a key `"my-skill"`
- **THEN** the reconciler SHALL return an error identifying the collision

#### Scenario: No collision
- **WHEN** all skill names across all sources are unique
- **THEN** the reconciler SHALL proceed without error

### Requirement: skills.content preserves upstream semantics
The `skills.content` field SHALL be `map[string]string` with identical semantics to the upstream `spec.skills` flat map.

#### Scenario: Inline skill delivery
- **WHEN** `skills.content` has key `"my-skill"` with SKILL.md content
- **THEN** `workspace/skills/my-skill/SKILL.md` SHALL be created with that content
- **THEN** the skill SHALL be overwritten on every pod restart
