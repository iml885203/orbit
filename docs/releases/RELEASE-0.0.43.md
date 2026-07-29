# Orbit v0.0.43 — make setup problems feel smaller

Orbit now turns setup diagnostics into a short recovery path instead of a
catalog of internal checks. Daily status also stays focused on the environment
and its resources rather than the repository machinery behind it.

## What changed

- Human `orbit doctor` leads with failed and warning checks plus their exact
  remedies. Passing checks are summarized rather than printed as an equally
  prominent list.
- The dashboard Doctor view uses the same problem-first order, shows the
  remedy that was previously hidden, and provides a copy button for executable
  terminal commands. Passing and informational checks remain available behind
  one disclosure.
- Services sharing a Node workspace or Python requirements file are reported
  together. A monorepo that needs one package installation now produces one
  setup action, not the same command repeated for every affected service.
- Human `orbit status` no longer shows the managed environment repository URL,
  tag, and commit during normal daily use. Full provenance remains available
  through `orbit env list` and the JSON contract.
- The installed-user journey now rejects repository provenance leaking back
  into daily status, while retaining the version-matched demo and source
  evidence used by release automation.

## Why it matters

A developer should only have to answer two questions: what is blocking the
environment, and what should they do next? They should not need to scan healthy
checks, execute the same package command several times, or understand the Git
repository that delivered a shared environment just to start local work.

This release keeps the detailed evidence available for maintainers and agents,
but removes it from the ordinary human path until the user explicitly asks for
environment-management details.
