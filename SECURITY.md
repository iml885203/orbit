# Security policy

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability.

Use GitHub's private vulnerability reporting from the repository's
**Security → Advisories → Report a vulnerability** page:

<https://github.com/iml885203/orbit/security/advisories/new>

Include the affected version or commit, operating system, reproduction steps,
impact, and any suggested mitigation. You should receive an acknowledgement
within seven days. Please allow time for a fix and coordinated disclosure before
publishing details.

## Supported versions

Orbit is currently pre-1.0. Security fixes target the latest published preview
and the `main` branch; older previews may require upgrading. The stable support
policy will be finalized with `v1.0.0`.

## Scope

Orbit manages local processes, containers, project configuration, and
credentials supplied through the user's environment. Reports about unintended
network exposure, command execution, secret disclosure, unsafe file access, or
destructive behavior are in scope.

## Local trust boundary

The dashboard and its HTTP API bind to loopback only. TCP requests must use a
loopback `Host`; state-changing browser requests must also be same-origin.
These checks block DNS rebinding and cross-site requests from driving local
resource controls. The CLI uses the local Unix socket instead of the
browser-facing TCP listener; that socket is readable and writable only by its
owning operating-system user.

Orbit is a single-user local development tool, not a multi-user authorization
server. Other processes running as the same operating-system user remain
inside its trust boundary.
