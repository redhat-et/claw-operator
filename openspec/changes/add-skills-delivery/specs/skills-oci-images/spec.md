## ADDED Requirements

### Requirement: SkillsSpec supports OCI image delivery
The `SkillsSpec` type SHALL include an optional `images` field — a list of `SkillImageSpec` entries, each mounting an OCI image as a read-only skill directory.

#### Scenario: No images configured
- **WHEN** `spec.skills.images` is omitted or empty
- **THEN** no ImageVolume mounts SHALL be added to the gateway pod

#### Scenario: Single image configured
- **WHEN** `spec.skills.images` contains `{name: "my-skill", image: "registry.example.com/skills/my-skill:v1"}`
- **THEN** the gateway pod SHALL have an ImageVolume mount at `/home/node/.openclaw/workspace/skills/my-skill`
- **THEN** the mount SHALL be read-only

#### Scenario: Multiple images configured
- **WHEN** `spec.skills.images` contains multiple entries
- **THEN** each entry SHALL have its own ImageVolume mount at the corresponding skill path

### Requirement: Skill image name validation
The `SkillImageSpec.name` field SHALL be validated with the pattern `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`.

#### Scenario: Valid skill name
- **WHEN** `name` is `"my-skill"` or `"data_analysis.v2"`
- **THEN** the API server SHALL accept the resource

#### Scenario: Invalid skill name with path separator
- **WHEN** `name` contains `"/"`
- **THEN** the API server SHALL reject the resource

### Requirement: PullPolicy defaults
The `pullPolicy` field SHALL default based on the image tag: `Always` if the tag is `:latest`, `IfNotPresent` otherwise.

#### Scenario: Latest tag
- **WHEN** `image` ends with `:latest` and `pullPolicy` is omitted
- **THEN** the ImageVolume SHALL use `Always` pull policy

#### Scenario: Specific tag
- **WHEN** `image` has a specific tag (e.g., `:v1.2.3`) and `pullPolicy` is omitted
- **THEN** the ImageVolume SHALL use `IfNotPresent` pull policy

### Requirement: imagePullSecrets for private registries
Each `SkillImageSpec` SHALL have an optional `imagePullSecrets` list. The operator SHALL collect, deduplicate, and merge pull secrets from all skill images into the gateway pod's `imagePullSecrets`.

#### Scenario: Private registry with pull secret
- **WHEN** a skill image has `imagePullSecrets: [{name: "my-registry-cred"}]`
- **THEN** the gateway pod spec SHALL include `my-registry-cred` in its `imagePullSecrets`

#### Scenario: Multiple images with overlapping secrets
- **WHEN** two skill images both reference `imagePullSecrets: [{name: "shared-cred"}]`
- **THEN** the gateway pod SHALL include `shared-cred` only once (deduplicated)

#### Scenario: No pull secrets
- **WHEN** no skill images have `imagePullSecrets`
- **THEN** the gateway pod's existing `imagePullSecrets` SHALL NOT be modified

### Requirement: OCI skill directories are read-only
Skill directories mounted from OCI images SHALL be read-only. The agent SHALL NOT be able to modify files in OCI-delivered skill directories.

#### Scenario: Agent attempts to write to OCI skill
- **WHEN** the agent attempts to modify a file in an OCI-mounted skill directory
- **THEN** the write SHALL fail with a read-only filesystem error
