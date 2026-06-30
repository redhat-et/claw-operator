## ADDED Requirements

### Requirement: ClawSpec has restrictions field
The Claw CRD SHALL include an optional `spec.restrictions` field of type `RestrictionsSpec` that configures runtime restrictions.

#### Scenario: Field is omitted
- **WHEN** a Claw is created without `spec.restrictions`
- **THEN** no restrictions SHALL be applied
- **THEN** existing behavior SHALL be unchanged

### Requirement: pluginInstallation controls init container
The `restrictions.pluginInstallation` field (`*bool`) SHALL control whether the plugins init container is injected into the gateway Deployment.

#### Scenario: Field omitted (default: allowed)
- **WHEN** `restrictions.pluginInstallation` is omitted
- **THEN** the plugins init container SHALL be injected as normal
- **THEN** plugins listed in `spec.plugins` SHALL be installed

#### Scenario: Explicitly true
- **WHEN** `restrictions.pluginInstallation` is set to `true`
- **THEN** behavior SHALL be identical to omitting the field

#### Scenario: Set to false
- **WHEN** `restrictions.pluginInstallation` is set to `false`
- **THEN** the plugins init container SHALL NOT be injected into the Deployment
- **THEN** plugins listed in `spec.plugins` SHALL NOT be installed
- **THEN** the `spec.plugins` field SHALL be silently ignored

#### Scenario: Set to false with no plugins listed
- **WHEN** `restrictions.pluginInstallation` is `false` and `spec.plugins` is empty
- **THEN** the plugins init container SHALL NOT be injected
- **THEN** no error or warning SHALL be generated

### Requirement: RestrictionsEnforced status condition
The operator SHALL set a `RestrictionsEnforced` status condition when any restriction is active.

#### Scenario: Plugin installation blocked
- **WHEN** `restrictions.pluginInstallation` is `false`
- **THEN** the `RestrictionsEnforced` condition SHALL be set to `True`

#### Scenario: No restrictions active
- **WHEN** `spec.restrictions` is omitted or all restriction fields are at their defaults
- **THEN** the `RestrictionsEnforced` condition SHALL NOT be set (or set to `False`)
