# Contributing to Orbit

Thanks for helping make local development easier.

Orbit is still being hardened for 1.0. Before starting a large change, open an
issue describing the user problem and proposed behavior so the design can be
discussed before implementation. Small fixes and documentation improvements can
go directly to a pull request.

## Development setup

The required tools and build commands are documented in
[Installation and development](docs/development.md#contributing-to-orbit).

```bash
pnpm --dir ui install --frozen-lockfile
make build
make test
```

During implementation, run the narrowest relevant sociable test. After a
coherent user-visible change is complete, run `make test`. Run
`make preflight` once before committing that complete change; it is the same
source and contract gate used by CI. `make test-journeys` exercises the built
binary through real Git, process, and Docker boundaries. Use `make lint` for
the stricter Go lint pass.

## Pull requests

- Keep each pull request focused on one user-visible problem.
- Add tests that prove the behavior, including failure and recovery paths.
- Prefer end-to-end journeys and sociable domain tests over tests coupled to
  private helpers. See [Testing strategy](docs/testing.md).
- Update English and Traditional Chinese documentation together when both
  versions exist.
- Preserve the `orbit.cli.v1` JSON contract and keep destructive operations
  explicit.
- Do not include credentials, private project names, customer data, or generated
  local state.

Project conventions are in [Code conventions](docs/CODE_CONVENTIONS.md) and
architecture context is in [Architecture](docs/architecture.md).

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
Security reports must follow [SECURITY.md](SECURITY.md), not a public issue.
