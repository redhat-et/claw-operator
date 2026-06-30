## Context

The operator sets `Ready`, `CredentialsResolved`, `ProxyConfigured`, `McpServersConfigured`, and `WebSearchConfigured` conditions. When a deployment is not ready, the Ready condition says "Provisioning" — regardless of whether the pod is pending scheduling, pulling images, or crash-looping with an actionable error.

The trigger for this work was a real production incident: `spec.version: "2026.6.5"` deployed against a PVC that had been running `2026.6.8`. The `@openclaw/anthropic-vertex-provider` plugin's init container crashed because it required a newer OpenClaw version. The only signal was "Waiting for deployments to become ready." The user spent 20 minutes in pod logs before finding the cause.

## Goals / Non-Goals

**Goals:**
- Surface init container crash messages in the Ready condition
- Warn about plugin/version incompatibility before the crash happens
- Warn about version downgrades that may cause PVC issues
- Track the highest successfully deployed version as a high-water mark

**Non-Goals:**
- Blocking version downgrades (users may have valid reasons)
- Blocking incompatible plugin/version combinations (plugins may be cached on PVC)
- Automated rollback on failure

## Decisions

### Warning-only, not blocking

All three conditions are advisory. `PluginCompatibility` and `VersionDowngrade` are set as warnings but do not prevent the deployment from proceeding. The operator doesn't know whether the combination will actually fail — plugins may be cached on PVC from a previous compatible version.

**Why:** False positives are worse than warnings in enterprise environments. An operator that blocks a valid deployment erodes trust.

### lastDeployedVersion as a high-water mark

`status.lastDeployedVersion` records the highest `spec.version` that was successfully deployed (Ready=True). It never decreases — even if the user deploys an older version successfully, the high-water mark stays at the previous maximum.

**Why high-water mark:** The purpose is detecting *downgrades* relative to what the PVC has seen. If version A writes PVC data in format 2, and version B (older) reads format 1, the PVC is incompatible. The high-water mark captures the PVC's maximum capability, not the current deployment.

### Init container failure inspection

When the Ready condition would be set to "Provisioning" (deployments not ready), the reconciler lists pods for the deployment and inspects init container statuses. If any init container has a non-zero exit code or is in CrashLoopBackOff, the Ready condition reason is changed to `InitContainerFailure` and the message includes the container's error output.

**Why inspect pods:** Init container errors are not surfaced in the Deployment's status conditions. The only way to get the error message is to read the pod's container statuses.

**Alternative considered:** Watch pod events. Rejected because events are ephemeral and may be garbage-collected before the reconciler runs.

### Plugin minimum version from knownProviders

Plugin compatibility is checked against a `PluginMinVersion` field in the operator's `knownProviders` table. If `spec.version` is older than a plugin's declared minimum, the `PluginCompatibility` condition is set.

**Why operator-side, not plugin-side:** The operator knows the version before the pod starts. Checking at reconcile time gives an early warning, not a crash.

## Risks / Trade-offs

- **[knownProviders must be kept current]** Plugin minimum versions are hardcoded in the operator. If a new plugin is released with a version requirement, the operator needs an update. Mitigated by keeping the table minimal (only plugins that have known version dependencies).
- **[Pod inspection adds API calls]** Listing pods for failure inspection adds one API call per reconcile when deployments are not ready. This is acceptable because the condition (not-ready deployments) is transient.
