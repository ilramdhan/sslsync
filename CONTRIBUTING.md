# Contributing

Thank you for improving sslsync. This tool changes production servers, so the bar is **correct, tested and boring**.

## Before you start

- Using an AI assistant (Claude Code or similar)? Point it at [`CLAUDE.md`](https://github.com/ilramdhan/sslsync/blob/main/CLAUDE.md) first: it has the commands, architecture and rules in one place.
- Open an issue first for anything bigger than a small fix, so the approach can be agreed before you write it.
- Never commit `.env`, certificates, keys, `logs/` or `reports/`. They are git-ignored. Check `git status` before every commit anyway.
- Never paste passwords, private keys or full `.env` files into issues or PRs. Replace real host names and IPs with `example.com` / `10.0.0.x`.

## Setup

```bash
git clone https://github.com/ilramdhan/sslsync.git
cd sslsync
make setup              # .env from .env.example + build
make test lint
```

Go version: see `go.mod`. Linter: [golangci-lint](https://golangci-lint.run) v2.

## Workflow

1. Branch from `main`: `feat/<topic>`, `fix/<topic>`, `docs/<topic>`, `chore/<topic>`.
2. Make the change with tests.
3. `make test lint` must pass with zero issues.
4. If the change affects operators, update **README.md** and **.env.example**.
5. Commit using [Conventional Commits](https://www.conventionalcommits.org): `feat(inventory): add traefik target type`, `fix(runner): restore created files on rollback`.
6. Open a PR using the template. CI must be green and the maintainer (@ilramdhan, see `CODEOWNERS`) must approve.

## Code guidelines

- **Idiomatic Go**: `gofmt`/`goimports`. Return errors and wrap them with `%w`; do not log and continue. Keep interfaces small and only where there are two implementations.
- **Package boundaries**: see [architecture](https://ilramdhan.github.io/sslsync/about/architecture/). `certs`, `tlsprobe` and `report` must not import other internal packages.
- **No new dependencies** without discussion. The tool has two (`golang.org/x/crypto`, `github.com/joho/godotenv`).
- **Errors are for operators**: say what is wrong *and* which variable or command fixes it (`check WEB_PROD_PORT`, `run make trust-hosts`).
- **Shell sent to servers**: POSIX `sh`, not bash. Quote every interpolated value with `remote.Shq`. Must work on Ubuntu 20.04+.
- **Safety first**: a change that could modify a server must go through `runner` phases so that it is checked, verified and rolled back. No shortcuts.

## Tests

- Unit tests go next to the code (`*_test.go`) and are table-driven.
- New config settings need a test in `inventory_test.go`, both valid and invalid.
- New certificate handling needs a test in `certs_test.go`. Generate certificates in the test; never commit real ones.
- Changes to `runner/` or the k8s scripts: describe in the PR how you tested them against a disposable server or container (see [Development](https://ilramdhan.github.io/sslsync/about/development/)). Never test against production.

## Releasing

Releases are automated (see [Releases & CI/CD](https://ilramdhan.github.io/sslsync/about/releases/)): merging the release PR that release-please keeps open publishes the release. Contributors don't need to do anything beyond writing [Conventional Commit](https://www.conventionalcommits.org) messages.

## Security issues

Do **not** open an issue. Follow the [security policy](https://github.com/ilramdhan/sslsync/blob/main/SECURITY.md).
