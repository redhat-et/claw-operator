## ADDED Requirements

### Requirement: ClawSpec has agentFiles field
The Claw CRD SHALL include an optional `spec.agentFiles` field of type AgentFilesSpec that configures workspace file seeding from external sources.

#### Scenario: Field is omitted
- **WHEN** a Claw is created without `spec.agentFiles`
- **THEN** no workspace file seeding SHALL occur
- **THEN** existing behavior (spec.workspace.files) SHALL be unaffected

#### Scenario: Field is present with configMapRef
- **WHEN** a Claw is created with `spec.agentFiles.configMapRef` set
- **THEN** the operator SHALL extract the referenced ConfigMap archive into the workspace at pod start

#### Scenario: Field is present with git
- **WHEN** a Claw is created with `spec.agentFiles.git` set
- **THEN** the operator SHALL clone the Git repository into the workspace at pod start

### Requirement: ConfigMapRef and git are mutually exclusive
The CRD SHALL reject manifests that set both `spec.agentFiles.configMapRef` and `spec.agentFiles.git`.

#### Scenario: Both sources set
- **WHEN** a Claw is created with both `configMapRef` and `git` set
- **THEN** the API server SHALL reject the resource with a validation error

#### Scenario: Neither source set
- **WHEN** a Claw is created with `spec.agentFiles` but neither `configMapRef` nor `git`
- **THEN** the API server SHALL reject the resource with a validation error

### Requirement: ConfigMap source extracts gzipped tar archive
When `spec.agentFiles.configMapRef` is set, the init container SHALL extract the referenced ConfigMap key as a gzipped tar archive into the workspace directory.

#### Scenario: Default key name
- **WHEN** `configMapRef.name` is set and `configMapRef.key` is omitted
- **THEN** the init container SHALL use the key `agentfiles.tgz` from the ConfigMap

#### Scenario: Custom key name
- **WHEN** `configMapRef.key` is set to `"my-archive.tgz"`
- **THEN** the init container SHALL use that key from the ConfigMap

### Requirement: Git source clones repository
When `spec.agentFiles.git` is set, the init container SHALL clone the specified Git repository into the workspace directory.

#### Scenario: Clone with default branch
- **WHEN** `git.url` is set and `git.ref` is omitted
- **THEN** the init container SHALL clone the repository's default branch

#### Scenario: Clone with specific ref
- **WHEN** `git.ref` is set to `"v1.0"`
- **THEN** the init container SHALL check out that ref after cloning

#### Scenario: Clone with subdirectory
- **WHEN** `git.path` is set to `"enterprise-profiles/hr-specialist"`
- **THEN** the init container SHALL copy only that subdirectory's contents into the workspace

#### Scenario: Git URL must use HTTPS
- **WHEN** `git.url` is set to a value not starting with `https://`
- **THEN** the API server SHALL reject the resource with a validation error

### Requirement: ApplyPolicy controls overwrite behavior
The `spec.agentFiles.applyPolicy` field SHALL control whether seeded files overwrite existing PVC content.

#### Scenario: IfMissing policy (default)
- **WHEN** `applyPolicy` is omitted or set to `IfMissing`
- **THEN** the init container SHALL seed files only if they do not already exist on the PVC
- **THEN** user and agent edits to seeded files SHALL survive pod restarts

#### Scenario: Always policy
- **WHEN** `applyPolicy` is set to `Always`
- **THEN** the init container SHALL overwrite existing files with the source version on every pod start
- **THEN** runtime edits to seeded files SHALL NOT survive pod restarts

### Requirement: AgentFiles works in both configuration modes
The agentFiles feature SHALL work identically regardless of `spec.config.management` value (operator or user).

#### Scenario: Operator-managed mode with agentFiles
- **WHEN** a Claw has `config.management: operator` (or omitted) and `agentFiles.git` configured
- **THEN** agent files SHALL be seeded into the workspace at pod start

#### Scenario: User-managed mode with agentFiles
- **WHEN** a Claw has `config.management: user` and `agentFiles.git` configured
- **THEN** agent files SHALL be seeded into the workspace at pod start
- **THEN** user-managed config behavior SHALL be unaffected
