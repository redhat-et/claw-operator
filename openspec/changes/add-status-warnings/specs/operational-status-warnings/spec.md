## ADDED Requirements

### Requirement: InitContainerFailure enriches Ready condition
When a deployment is not ready due to init container failures, the Ready condition SHALL use reason `InitContainerFailure` and include the container's error message.

#### Scenario: Init container crash-loops
- **WHEN** the gateway Deployment is not ready
- **AND** a pod has an init container in CrashLoopBackOff
- **THEN** the Ready condition SHALL be set to `False` with reason `InitContainerFailure`
- **THEN** the condition message SHALL include the failing container's name and error output

#### Scenario: Init container exits with non-zero code
- **WHEN** the gateway Deployment is not ready
- **AND** a pod has an init container that exited with a non-zero exit code
- **THEN** the Ready condition SHALL be set to `False` with reason `InitContainerFailure`
- **THEN** the condition message SHALL include the exit code and termination message

#### Scenario: Deployment not ready for other reasons
- **WHEN** the gateway Deployment is not ready
- **AND** no init container failures are detected (e.g., pending scheduling)
- **THEN** the Ready condition SHALL use reason `Provisioning` (existing behavior)

### Requirement: PluginCompatibility condition warns on version mismatch
The operator SHALL set a `PluginCompatibility` condition when `spec.version` is older than a plugin's declared minimum required version.

#### Scenario: Version older than plugin minimum
- **WHEN** `spec.version` is `"2026.6.5"` and a plugin requires minimum `"2026.6.8"`
- **THEN** `PluginCompatibility` condition SHALL be set to `True` with reason `Incompatible`
- **THEN** the condition message SHALL identify the plugin and its minimum version
- **THEN** the deployment SHALL NOT be blocked

#### Scenario: Version meets plugin minimum
- **WHEN** `spec.version` meets or exceeds all plugin minimum versions
- **THEN** `PluginCompatibility` condition SHALL NOT be set (or set to `False`)

#### Scenario: No version override
- **WHEN** `spec.version` is omitted (using operator default)
- **THEN** `PluginCompatibility` condition SHALL NOT be set

### Requirement: VersionDowngrade condition warns on downgrade
The operator SHALL set a `VersionDowngrade` condition when `spec.version` is older than `status.lastDeployedVersion`.

#### Scenario: Version downgrade
- **WHEN** `status.lastDeployedVersion` is `"2026.6.8"` and `spec.version` is changed to `"2026.6.5"`
- **THEN** `VersionDowngrade` condition SHALL be set to `True` with reason `VersionDowngrade`
- **THEN** the condition message SHALL warn about potential PVC data incompatibility
- **THEN** the deployment SHALL NOT be blocked

#### Scenario: Version upgrade
- **WHEN** `spec.version` is newer than `status.lastDeployedVersion`
- **THEN** `VersionDowngrade` condition SHALL NOT be set

#### Scenario: First deployment
- **WHEN** `status.lastDeployedVersion` is empty
- **THEN** `VersionDowngrade` condition SHALL NOT be set

### Requirement: lastDeployedVersion tracks high-water mark
`status.lastDeployedVersion` SHALL record the highest `spec.version` that was successfully deployed (Ready condition became True).

#### Scenario: Successful deployment updates high-water mark
- **WHEN** a Claw with `spec.version: "2026.6.8"` reaches Ready=True
- **THEN** `status.lastDeployedVersion` SHALL be set to `"2026.6.8"`

#### Scenario: Downgrade does not lower high-water mark
- **WHEN** `status.lastDeployedVersion` is `"2026.6.8"` and a deployment with `spec.version: "2026.6.5"` reaches Ready=True
- **THEN** `status.lastDeployedVersion` SHALL remain `"2026.6.8"`

#### Scenario: No version override
- **WHEN** `spec.version` is omitted
- **THEN** `status.lastDeployedVersion` SHALL NOT be updated
