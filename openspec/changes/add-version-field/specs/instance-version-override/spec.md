## ADDED Requirements

### Requirement: ClawSpec has version field
The Claw CRD SHALL include an optional `spec.version` string field that overrides the OpenClaw container image tag for this instance.

#### Scenario: Version field is omitted
- **WHEN** a Claw is created without `spec.version`
- **THEN** the operator SHALL use its built-in default image tag for all OpenClaw containers

#### Scenario: Version field is set
- **WHEN** a Claw is created with `spec.version: "2026.6.8"`
- **THEN** the operator SHALL set the image tag to `2026.6.8` on the init-volume, init-config, and gateway containers

#### Scenario: Version field is updated
- **WHEN** a Claw's `spec.version` is changed from `"2026.6.8"` to `"2026.6.15"`
- **THEN** the operator SHALL update the image tag on all three OpenClaw containers to `2026.6.15`
- **THEN** the Deployment rollout SHALL be triggered

#### Scenario: Version field is removed
- **WHEN** a Claw's `spec.version` is removed (set to empty string or omitted)
- **THEN** the operator SHALL revert to the built-in default image tag on all three OpenClaw containers

### Requirement: Version field validates format
The CRD SHALL reject `spec.version` values that do not match the pattern `^[a-z0-9][a-z0-9._-]*$`.

#### Scenario: Valid version string
- **WHEN** a Claw is created with `spec.version: "2026.6.8"`
- **THEN** the API server SHALL accept the resource

#### Scenario: Valid version with dots and dashes
- **WHEN** a Claw is created with `spec.version: "1.0.0-rc.1"`
- **THEN** the API server SHALL accept the resource

#### Scenario: Invalid version with leading dash
- **WHEN** a Claw is created with `spec.version: "-bad"`
- **THEN** the API server SHALL reject the resource with a validation error

#### Scenario: Invalid version with uppercase
- **WHEN** a Claw is created with `spec.version: "Latest"`
- **THEN** the API server SHALL reject the resource with a validation error

### Requirement: All OpenClaw containers are overridden atomically
When `spec.version` is set, the operator SHALL override the image tag on all three OpenClaw containers (init-volume, init-config, gateway) or none.

#### Scenario: All containers present
- **WHEN** the base Deployment manifest contains init-volume, init-config, and gateway containers
- **THEN** the operator SHALL override the image tag on all three containers

#### Scenario: Container missing from manifest
- **WHEN** the base Deployment manifest is missing one of the three required containers
- **THEN** the operator SHALL return an error without overriding any container image

### Requirement: Image override runs before plugins init container
The image override SHALL be applied before the plugins init container is configured, so that any container cloned from gateway inherits the overridden image.

#### Scenario: Plugins init container inherits version
- **WHEN** a Claw has `spec.version: "2026.6.8"` and plugins are configured
- **THEN** the plugins init container SHALL use the `2026.6.8` image tag

### Requirement: Only the tag is overridable
The operator SHALL override only the tag portion of the image reference. The image name (`ghcr.io/openclaw/openclaw`) SHALL remain fixed.

#### Scenario: Image name preserved
- **WHEN** a Claw has `spec.version: "2026.6.8"`
- **THEN** the container image SHALL be `ghcr.io/openclaw/openclaw:2026.6.8`
- **THEN** the image name portion SHALL NOT be changed
