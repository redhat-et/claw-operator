## ADDED Requirements

### Requirement: ConfigSpec has management field
The `ConfigSpec` type SHALL include an optional `management` field of type `ConfigManagement` enum with values `operator` (default) and `user`.

#### Scenario: Field omitted
- **WHEN** a Claw is created without `spec.config.management`
- **THEN** the operator SHALL use `operator` mode (existing behavior)
- **THEN** operator config SHALL be merged on every pod restart

#### Scenario: Operator mode
- **WHEN** `spec.config.management` is set to `"operator"`
- **THEN** behavior SHALL be identical to omitting the field

#### Scenario: User mode
- **WHEN** `spec.config.management` is set to `"user"`
- **THEN** application config SHALL be seeded on first boot only
- **THEN** user runtime edits to application config SHALL survive pod restarts

### Requirement: First boot seeds full config in user mode
In user-managed mode, the init-config container SHALL seed the complete configuration (providers, models, preferences) on first boot when `openclaw.json` does not exist on the PVC.

#### Scenario: First boot with user mode
- **WHEN** `config.management` is `user` and the PVC has no existing `openclaw.json`
- **THEN** the init container SHALL write a complete `openclaw.json` with all operator-configured providers and models

#### Scenario: Restart with user mode
- **WHEN** `config.management` is `user` and `openclaw.json` exists on the PVC
- **THEN** the init container SHALL update only infrastructure sections (proxy, auth, gateway)
- **THEN** application sections (providers, models, user preferences) SHALL be preserved from the PVC

### Requirement: Infrastructure config is always enforced
Regardless of management mode, the operator SHALL enforce infrastructure configuration: proxy settings, authentication, gateway config, and plugin installation.

#### Scenario: User-managed mode with auth change
- **WHEN** `config.management` is `user` and the admin changes `spec.auth`
- **THEN** the auth change SHALL be applied on the next pod restart
- **THEN** user-modified application config SHALL be preserved

#### Scenario: User-managed mode with credential change
- **WHEN** `config.management` is `user` and `spec.credentials` is modified
- **THEN** proxy routes SHALL be updated on the next pod restart
- **THEN** user-modified model and provider preferences SHALL be preserved

### Requirement: Plugins install in both modes
Plugin installation via `spec.plugins` SHALL run in both operator-managed and user-managed modes.

#### Scenario: Plugins in user-managed mode
- **WHEN** `config.management` is `user` and `spec.plugins` lists plugins
- **THEN** the plugins init container SHALL install the listed plugins
- **THEN** implicit provider plugins SHALL also be installed

### Requirement: Mode switch from user to operator restores operator control
When `spec.config.management` is changed from `user` to `operator`, the operator SHALL resume full config management on the next pod restart.

#### Scenario: Switch to operator mode
- **WHEN** `config.management` is changed from `user` to `operator`
- **THEN** the next pod restart SHALL merge the full operator config
- **THEN** user runtime edits that conflict with operator config SHALL be overwritten
