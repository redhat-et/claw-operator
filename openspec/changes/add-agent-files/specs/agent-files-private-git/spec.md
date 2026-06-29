## ADDED Requirements

### Requirement: Git source supports private repositories
The `spec.agentFiles.git` field SHALL include an optional `secretRef` that references a Kubernetes Secret containing Git credentials for private repository authentication.

#### Scenario: Public repository without secretRef
- **WHEN** `git.url` points to a public repository and `git.secretRef` is omitted
- **THEN** the init container SHALL clone the repository without authentication

#### Scenario: Private repository with secretRef
- **WHEN** `git.secretRef.name` references a Secret with `username` and `password` keys
- **THEN** the init container SHALL use those credentials to authenticate the Git clone

#### Scenario: GitHub PAT authentication
- **WHEN** the Secret has `username: "x-access-token"` and `password: "ghp_..."` (a GitHub PAT)
- **THEN** the clone SHALL succeed for repositories the PAT has access to

#### Scenario: GitLab deploy token authentication
- **WHEN** the Secret has a GitLab deploy token as username/password
- **THEN** the clone SHALL succeed for the token's scoped repositories

### Requirement: Credentials are mounted on init container only
The Git credentials Secret SHALL be mounted only on the init-config container, not on the gateway container where the agent runs.

#### Scenario: Gateway container has no access to credentials
- **WHEN** a Claw has `agentFiles.git.secretRef` configured
- **THEN** the gateway container SHALL NOT have the credentials Secret mounted
- **THEN** the gateway container SHALL NOT have Git credential environment variables

#### Scenario: Init container mounts credentials at known path
- **WHEN** a Claw has `agentFiles.git.secretRef` configured
- **THEN** the init-config container SHALL have the Secret mounted at `/etc/git-credentials`

### Requirement: Credentials never appear in process args or env vars
The init container SHALL read credentials from the mounted file and use a GIT_ASKPASS helper script. Credentials SHALL NOT appear in process arguments, environment variables, or Git remote URLs.

#### Scenario: Password not in clone URL
- **WHEN** the init container clones a private repository
- **THEN** the Git remote URL SHALL contain only the username, not the password
- **THEN** the password SHALL be provided via GIT_ASKPASS

#### Scenario: Askpass script is cleaned up
- **WHEN** the Git clone completes (success or failure)
- **THEN** the temporary GIT_ASKPASS script SHALL be deleted

### Requirement: Credential rotation triggers rollout
The operator SHALL stamp the Secret's ResourceVersion as a pod annotation. When the Secret is rotated, the annotation changes and triggers a Deployment rollout.

#### Scenario: Secret is updated
- **WHEN** the Git credentials Secret is updated with new credentials
- **THEN** the operator SHALL detect the ResourceVersion change
- **THEN** a new pod rollout SHALL be triggered so the init container uses the new credentials

### Requirement: Missing Secret is surfaced as validation error
The operator SHALL validate that the referenced Secret exists and contains the required keys (`username`, `password`) during reconciliation.

#### Scenario: Secret does not exist
- **WHEN** `git.secretRef.name` references a non-existent Secret
- **THEN** the operator SHALL set the CredentialsResolved condition to False with reason ValidationFailed

#### Scenario: Secret missing required keys
- **WHEN** the referenced Secret exists but is missing the `username` or `password` key
- **THEN** the operator SHALL set the CredentialsResolved condition to False with reason ValidationFailed
