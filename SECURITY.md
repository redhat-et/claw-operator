# Security Policy

This document describes how to report vulnerabilities, how they are triaged and disclosed,
and what security practices claw-operator follows. For the full system threat model, see
[THREAT_MODEL.md](THREAT_MODEL.md).

---

## Supported Versions

Security updates are provided for the **most recent major release only**. Older versions
do not receive backported patches.

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |
| < Latest | No       |

Because claw-operator is pre-1.0 (API group `claw.sandbox.redhat.com`, maturity `alpha`),
no stable release series exists yet. All security fixes target the `main` branch and are
tagged as new releases.

---

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**  
Public issues expose details before a fix is available and give attackers a head start.

### Preferred Channels (in priority order)

1. **GitHub Private Vulnerability Reporting** (recommended)  
   Use the [Security Advisories](https://github.com/redhat-et/claw-operator/security/advisories/new)
   tab to open a private draft advisory. This creates an audit trail and allows coordinated
   disclosure without public exposure.

2. **Red Hat Security Response Team**  
   Email: **secalert@redhat.com**  
   For reports involving multiple Red Hat components or requiring coordinated multi-vendor
   disclosure, Red Hat Security manages the process via their
   [Coordinated Vulnerability Disclosure policy](https://access.redhat.com/articles/red-hat-coordinated-vulnerability-disclosure).

### What to Include

A useful report provides:

- Affected version(s) and how to identify them (`kubectl get csv` in the operator namespace)
- A description of the vulnerability and its potential impact
- The attack scenario: who is the actor (unauthenticated user, authenticated tenant,
  cluster admin), what is the entry point, and what asset is compromised
- Steps to reproduce or a proof-of-concept (even partial)
- Whether you believe the issue is already publicly known or being actively exploited

Partial reports are welcome — "I found something but couldn't fully reproduce it" is still
valuable.

### Response Commitments

| Stage | Target |
|---|---|
| Acknowledgment | 2 business days |
| Initial triage (is this a vulnerability?) | 5 business days |
| Severity rating communicated to reporter | 10 business days |
| Fix development target | See severity table below |
| Public disclosure | After fix is available, or 90 days max |

We follow Red Hat's preference for embargoes under 45 days where possible. For issues
already publicly known or actively exploited, we skip the embargo and release immediately.

---

## Severity Classification

claw-operator uses Red Hat's four-tier severity scale:

| Severity | Definition | Fix Target | Release Strategy |
|---|---|---|---|
| **Critical** | Easily exploited remotely without authentication; arbitrary code execution or full cluster compromise; worm-exploitable | 7 days | Emergency out-of-band release |
| **Important** | Privilege escalation, unauthorized credential access, remote code execution by authenticated users, or sustained denial of service | 30 days | Expedited out-of-band release |
| **Moderate** | Exploitable only under non-default configuration or with significant attacker prerequisites | 90 days | Bundled into next scheduled release |
| **Low** | Requires highly unlikely conditions; minimal impact if exploited | Next release | Bundled into next scheduled release |

For reference, the MITM proxy and its credential-handling subsystem are classified as
**critical-impact** surfaces — see [THREAT_MODEL.md §4](THREAT_MODEL.md) for the full
threat table and current mitigation status.

---

## Disclosure Policy

claw-operator follows [Red Hat's Coordinated Vulnerability Disclosure policy](https://access.redhat.com/articles/red-hat-coordinated-vulnerability-disclosure).

- **Critical/Important vulnerabilities** receive an emergency release before public disclosure.
- **Moderate/Low vulnerabilities** are disclosed in the release notes of the next scheduled release.
- **Public announcement** is made via GitHub Security Advisories and the repo's release notes.
  Announcements are scheduled for Tuesday–Thursday to maximize global maintainer availability.
- **Reporter credit** is offered by default. Reporters may opt out of attribution.
- **CVE IDs** are requested from Red Hat's CVE numbering authority for confirmed vulnerabilities
  with measurable impact.

---

## Agentic AI Security Considerations

claw-operator manages OpenClaw instances — AI agent gateways that route LLM requests,
execute tools (shell commands, browser, code), and hold credentials for external services.
This creates a broader attack surface than a typical Kubernetes operator.

Relevant threat categories specific to AI operators:

- **Prompt injection via operator-managed config**: A malicious `spec.config.raw` value
  injected into the gateway ConfigMap could alter agent behavior. The operator does not
  sanitize freeform config — it is the cluster admin's responsibility to validate CR inputs.
- **Credential exfiltration via agent tool use**: A compromised or manipulated agent could
  use its `exec` or browser tools to exfiltrate secrets from the pod environment. The
  credential isolation boundary is the MITM proxy, not the agent itself.
- **Plugin supply-chain risk**: Auto-installed plugins (from `spec.credentials` with
  `PluginMinVersion`) are fetched from the OpenClaw plugin registry at pod start. A
  compromised plugin could execute arbitrary code inside the gateway container.
- **MCP server trust**: Remote MCP servers declared in `spec.mcpServers` are given tool
  invocation access by the agent. A malicious MCP server could issue harmful tool calls.

For guidance on securing agentic AI deployments, see the CISA joint advisory:
[Careful Adoption of Agentic AI Services](https://www.cisa.gov/resources-tools/resources/careful-adoption-agentic-ai-services).

Reports involving agentic AI misuse (prompt injection, tool abuse, plugin hijacking) are
treated as security vulnerabilities and should be reported through the channels above.

---

## Security Practices

### Operator

- The controller runs as non-root (UID 65532) under OpenShift's `restricted-v2` SCC
- `automountServiceAccountToken: false` on the operator pod
- All capabilities dropped; `seccompProfile: RuntimeDefault` applied
- Leader election scoped to the operator namespace
- Metrics endpoint served over HTTPS on `:8443`

### Gateway and Proxy Workloads

- All containers run non-root with `allowPrivilegeEscalation: false` and `capabilities: drop: ALL`
- The MITM proxy intercepts credentials so the gateway never holds raw API keys in environment variables
- `HTTP_PROXY`/`HTTPS_PROXY` force all gateway egress through the proxy; direct internet access is blocked by NetworkPolicy
- The gateway `readOnlyRootFilesystem` is intentionally `false` (Node.js requires writable paths); all other containers use `readOnlyRootFilesystem: true`

### RBAC

- The operator ClusterRole grants `pods/exec` cluster-wide — this is a known over-privilege tracked in [issue #34](https://github.com/redhat-et/claw-project/issues/34) and is being replaced with a scoped alternative
- Credentials (API keys, tokens) are stored in Kubernetes Secrets referenced by `spec.credentials[].secretRef`
  and never logged or emitted as Kubernetes Events

### Image Supply Chain

- Container images are currently published to `quay.io/rcook` under mutable tags
- Digest pinning and Red Hat Container Certification are tracked in [issue #39](https://github.com/redhat-et/claw-project/issues/39)

---

## Security Requirements for Contributors

When submitting code to this repository:

- Do not introduce wildcard RBAC verbs or resources without a documented justification
- Do not log or emit Kubernetes Events that contain credential values, token strings, or API keys
- Do not add new `pods/exec` or `pods/log` permissions without maintainer approval
- New credential types in `internal/proxy/` must include a test demonstrating that the
  credential is not readable from the gateway container's environment
- Dependencies must be pinned to a specific version in `go.mod`; do not use `@latest`
  in replace directives
- Any change to the MITM proxy trust boundary (new `injector_*.go`) requires a
  corresponding update to [THREAT_MODEL.md §3](THREAT_MODEL.md)

---

## Acknowledgments

This security policy is informed by:

- [OpenSSF Vulnerability Disclosure Working Group](https://openssf.org/blog/2021/09/27/announcing-the-openssf-vulnerability-disclosure-wg-guide-to-disclosure-for-oss-projects/)
- [Red Hat Coordinated Vulnerability Disclosure Policy](https://access.redhat.com/articles/red-hat-coordinated-vulnerability-disclosure)
- [Kubernetes Security Release Process](https://github.com/kubernetes/committee-security-response/blob/main/security-release-process.md)
- [CISA — Careful Adoption of Agentic AI Services](https://www.cisa.gov/resources-tools/resources/careful-adoption-agentic-ai-services)
- [IBM Open Source AI Security Baseline Framework](https://github.com/IBM/ai-security-baseline)
- [THREAT_MODEL.md standard](THREAT_MODEL.md) — versioned, machine-readable threat model checked into the repository alongside the code it describes
