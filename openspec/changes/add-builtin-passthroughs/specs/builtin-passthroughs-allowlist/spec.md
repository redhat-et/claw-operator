## ADDED Requirements

### Requirement: NetworkSpec has builtinPassthroughs field
The `NetworkSpec` type SHALL include an optional `builtinPassthroughs` field of type `*[]string` that controls which builtin proxy passthrough domains are reachable.

#### Scenario: Field is omitted (nil)
- **WHEN** a Claw is created without `spec.network.builtinPassthroughs`
- **THEN** all six builtin passthrough domains SHALL be active
- **THEN** the proxy SHALL allow unauthenticated access to clawhub.ai, openrouter.ai, github.com, codeload.github.com, raw.githubusercontent.com, and registry.npmjs.org

#### Scenario: Field is set to a subset
- **WHEN** `builtinPassthroughs` is set to `["github.com", "registry.npmjs.org"]`
- **THEN** the proxy SHALL allow unauthenticated access to github.com and registry.npmjs.org
- **THEN** the proxy SHALL block unauthenticated access to clawhub.ai, openrouter.ai, codeload.github.com, and raw.githubusercontent.com

#### Scenario: Field is set to empty list
- **WHEN** `builtinPassthroughs` is set to `[]`
- **THEN** the proxy SHALL block unauthenticated access to all six builtin domains

#### Scenario: Field includes all builtins
- **WHEN** `builtinPassthroughs` lists all six builtin domains
- **THEN** behavior SHALL be identical to omitting the field

### Requirement: Credential routes override passthrough blocks
When a builtin domain is blocked by `builtinPassthroughs` but has a credential entry in `spec.credentials`, the credential-injected route SHALL still be generated in the proxy config.

#### Scenario: Blocked builtin with credential
- **WHEN** `builtinPassthroughs` does not include `"github.com"`
- **AND** `spec.credentials` contains a credential with `domain: "github.com"`
- **THEN** the proxy SHALL route github.com requests through the credential injector
- **THEN** the proxy SHALL NOT allow unauthenticated passthrough to github.com

#### Scenario: Blocked builtin without credential
- **WHEN** `builtinPassthroughs` does not include `"registry.npmjs.org"`
- **AND** no credential references registry.npmjs.org
- **THEN** the proxy SHALL return 403 Forbidden for requests to registry.npmjs.org

### Requirement: Unrecognized domains produce warnings
The operator SHALL log a warning for domain names in `builtinPassthroughs` that do not match any known builtin domain.

#### Scenario: Typo in domain name
- **WHEN** `builtinPassthroughs` contains `"clawhb.ai"` (missing a letter)
- **THEN** the operator SHALL log a warning identifying `"clawhb.ai"` as unrecognized
- **THEN** the operator SHALL NOT reject the manifest
- **THEN** the typo'd entry SHALL have no effect on proxy routing

#### Scenario: All entries valid
- **WHEN** all entries in `builtinPassthroughs` are recognized builtin domains
- **THEN** the operator SHALL NOT log any warnings

### Requirement: Nil vs empty distinction preserved
The field type SHALL be a pointer to a slice (`*[]string`) to distinguish "not set" (nil, all builtins active) from "set to empty" (all builtins blocked).

#### Scenario: Omitted field marshals as absent
- **WHEN** `builtinPassthroughs` is not set in the Claw manifest
- **THEN** the field SHALL be nil in the Go struct
- **THEN** the proxy config SHALL include all builtin passthrough routes

#### Scenario: Empty list marshals as empty array
- **WHEN** `builtinPassthroughs` is set to `[]` in the Claw manifest
- **THEN** the field SHALL be a non-nil pointer to an empty slice
- **THEN** the proxy config SHALL include zero builtin passthrough routes
