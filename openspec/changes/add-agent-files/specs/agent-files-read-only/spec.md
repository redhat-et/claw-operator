## ADDED Requirements

### Requirement: AgentFiles supports read-only file protection
The `spec.agentFiles` field SHALL include an optional `readOnly` list of workspace-relative paths that are mounted read-only on the gateway container.

#### Scenario: ReadOnly is omitted
- **WHEN** `spec.agentFiles.readOnly` is omitted or empty
- **THEN** all seeded files SHALL be writable by the agent

#### Scenario: Single file protected
- **WHEN** `readOnly` contains `"SOUL.md"`
- **THEN** SOUL.md SHALL be mounted read-only on the gateway container
- **THEN** the agent SHALL receive an `EROFS` (read-only file system) error when attempting to modify SOUL.md
- **THEN** other files in the same directory SHALL remain writable

#### Scenario: Multiple files protected
- **WHEN** `readOnly` contains `["SOUL.md", "AGENTS.md", "TOOLS.md"]`
- **THEN** all three files SHALL be mounted read-only
- **THEN** other workspace files SHALL remain writable

### Requirement: Individual files use subPath mounts
When a `readOnly` entry names a single file (no trailing `/` or `/**`), the operator SHALL use a subPath volume mount so that only that file is read-only, not the entire directory.

#### Scenario: SubPath mount for individual file
- **WHEN** `readOnly` contains `"SOUL.md"`
- **THEN** the operator SHALL create a subPath mount for `SOUL.md` from a read-only volume
- **THEN** other files in the workspace root SHALL NOT be affected by the mount

#### Scenario: File in subdirectory
- **WHEN** `readOnly` contains `"config/settings.json"`
- **THEN** the operator SHALL create a subPath mount for `config/settings.json`
- **THEN** other files in `config/` SHALL remain writable

### Requirement: Directories use whole-directory mounts
When a `readOnly` entry ends with `/` or `/**`, the operator SHALL mount the entire directory read-only.

#### Scenario: Directory mount
- **WHEN** `readOnly` contains `"policies/"`
- **THEN** the operator SHALL mount the `policies/` directory read-only
- **THEN** all files within `policies/` SHALL be read-only
- **THEN** the agent SHALL NOT be able to create new files in `policies/`

### Requirement: Read-only protection is kernel-enforced
The read-only protection SHALL be enforced by the Linux kernel via read-only volume mounts, not by application-level checks.

#### Scenario: Agent attempts to bypass via shell
- **WHEN** the agent runs a shell command to write to a read-only file (e.g., `echo "new" > SOUL.md`)
- **THEN** the command SHALL fail with a read-only filesystem error

#### Scenario: Agent attempts to bypass via file API
- **WHEN** the agent uses a file write API to modify a read-only file
- **THEN** the API call SHALL fail with a read-only filesystem error

### Requirement: ReadOnly works with both apply policies
The read-only protection SHALL be enforced regardless of `applyPolicy`.

#### Scenario: IfMissing policy with readOnly
- **WHEN** `applyPolicy` is `IfMissing` and `readOnly` includes `"SOUL.md"`
- **THEN** SOUL.md SHALL be seeded if missing and mounted read-only

#### Scenario: Always policy with readOnly
- **WHEN** `applyPolicy` is `Always` and `readOnly` includes `"SOUL.md"`
- **THEN** SOUL.md SHALL be overwritten from source on every restart and mounted read-only
