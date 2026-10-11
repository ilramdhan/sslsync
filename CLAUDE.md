# CLAUDE.md

Guidance for Claude Code and other AI assistants working in this repository.

## What this is

`sslsync` is a Go CLI (module `github.com/ilramdhan/sslsync`, MIT, public repo) that renews SSL/TLS certificates on many servers over SSH. Everything is configured in `.env` files. It checks each server, uploads the new certificate, replaces files in place, reloads services, verifies that every port serves the new certificate, and rolls back on failure. Targets (nginx, MinIO, Kubernetes, files, ...) are data, not code paths.

## Never touch

`.env`, `certs/`, `logs/`, `reports/` and `.claude/` are local-only and may hold real secrets. Do not read, print, edit or commit them. Use `.env.example` as the only template.

## Commands

```bash
make build        # bin/sslsync (also syncs .env.example into internal/app/env.example)
make test         # sync-template + go vet + go test -race ./...
make lint         # golangci-lint v2, config in .golangci.yml; must report 0 issues
make fuzz         # certs.FuzzLoad, inventory.FuzzLoad, remote.FuzzShq; FUZZTIME=30s default
make sync-template  # copy .env.example -> internal/app/env.example
make help         # every target
```

Docs preview (hash-pinned requirements):

```bash
python3 -m venv .venv && .venv/bin/pip install --require-hashes -r docs/requirements.txt
.venv/bin/mkdocs serve            # http://127.0.0.1:8000
.venv/bin/mkdocs build --strict   # CI does this; any warning (broken link) fails
```

Try the CLI safely (never against production):

```bash
bin/sslsync help
d=$(mktemp -d) && cd "$d" && /path/to/bin/sslsync init   # writes a template .env there
sslsync list | plan | validate | targets                  # no network, no server changes
```

`check`, `deploy`, `status`, `trust` and `find` connect to servers or ports. Use a disposable container (see `docs/about/development.md`).

## Architecture

Dependencies point downwards only:
`cmd/sslsync -> app -> runner -> {remote, tlsprobe, report} -> inventory, certs`

| Package | Responsibility | May import |
|---|---|---|
| `internal/app` | flags, env files, commands, output | all internal packages |
| `internal/runner` | per-server phases, permission fixes, rollback | remote, tlsprobe, report, inventory, certs |
| `internal/remote` | SSH client, sudo, timeouts, `Shq` quoting | `inventory.Server` (credentials) |
| `internal/inventory` | env variables -> `[]Server` / `[]Target`; target-type catalogue | `certs.Kind` |
| `internal/certs` | load, validate, render cert/key pair | no internal package |
| `internal/tlsprobe` | fetch the certificate a host:port serves | no internal package |
| `internal/report` | screen + file logs, `summary.json` | no internal package |

Embedded files:

- `internal/app/env.example` must equal `.env.example` (CI fails if they differ). Edit `.env.example`, then `make sync-template`.
- `internal/app/find-ssl.sh` is the discovery script used by `sslsync find`.
- `internal/inventory/scripts/*.sh` are the Kubernetes reload/rollback/state/health scripts.

## Configuration model

- `SSLSYNC_SERVERS` lists server names; each gets variables `<SERVER>_<SETTING>` (host, port, user, ...).
- Targets use `<SERVER>_<TARGET>_<SETTING>`.
- `DEFAULT_<SETTING>` is the fallback when a server does not set one.
- `inventory` records every variable it reads and warns about unread ones (typo detection). Keep that working.

Add a target type:

1. Add an entry to the `types` map in `internal/inventory/targets.go` (files, health, reload, optional rollback/state/vars/keyMode).
2. Longer shell goes in `internal/inventory/scripts/` with `//go:embed`.
3. Add valid and invalid cases to `internal/inventory/inventory_test.go`.
4. Document it in `docs/reference/targets.md` and `.env.example` (then `make sync-template`).

## Runner phases and safety invariants

`check` runs connect, sudo, health, files, state, tls and is read-only. `deploy` runs those, then upload, replace, reload, health, verify. These must never be broken:

- `check` never changes a server.
- Every server change goes through runner phases: checked first, verified after, undone on failure.
- Backups are `<file>.bak-<stamp>`; the stamp is also passed to scripts as `SSLSYNC_STAMP`.
- Files are overwritten in place (`cat new > existing`), never `mv`, to keep owner, mode, ACLs and SELinux label.
- Success means the port serves the new certificate: compare certificate bytes, not exit codes.
- Rollback restores replaced files, removes created files, restores key modes, then runs the target rollback/reload. It undoes only what the run changed.
- Reruns are idempotent: already-current files, secrets and ports report `up-to-date`.

## Coding rules

- Idiomatic Go, `gofmt`/`goimports`. Return errors wrapped with `%w`; do not log and continue.
- Operator-facing errors say what is wrong and which variable or command fixes it (`check WEB_PROD_PORT`, `run make trust-hosts`).
- Remote shell is POSIX `sh` (Ubuntu 20.04+, not bash). Every interpolated value goes through `remote.Shq`.
- No new dependencies without discussion (currently `golang.org/x/crypto`, `github.com/joho/godotenv`).
- Tests are table-driven and generate certificates at test time; never commit real or fixture certs.
- Keep fuzz targets green. A fuzz crash is a bug; keep the generated `testdata/fuzz/` input as a regression seed.
- Keep interfaces small; add one only where there are two implementations.

## Security rules (public repo)

- Never commit secrets, keys, certificates, real hostnames, IPs or company names. Use `example.com`, `10.0.0.x`, `WEB_PROD`.
- CI runs gitleaks and rejects `.pem`, `.key`, `.crt`, `.pfx` and `.env` files.
- `tlsprobe` uses `InsecureSkipVerify` on purpose: it only reads the served certificate and compares bytes. Do not "fix" it.
- Do not paste real `.env` contents in code, tests, issues or PRs.

## Git and PR workflow

- Branch from `main`: `feat/`, `fix/`, `docs/`, `chore/`, `ci/`, `test/` + topic.
- Conventional Commits (`feat(inventory): add traefik target type`). Titles drive release-please versions and the CHANGELOG; `pr-title.yml` checks the PR title.
- Squash merge only. Open the PR using `.github/pull_request_template.md` and fill every section.
- Required CI (`ci.yml`): Test (per OS), Lint, Security, Build (per OS/arch), Fuzz, Release config, Docker image, Docs build; plus CodeQL (`analyze`) and PR title.
- Releases are automated: release-please opens a release PR, GoReleaser publishes. Never tag manually.
- If operator-facing behavior changes, update `docs/`, `.env.example` and the README overview as needed.

## Docs

- `docs/` is the single source (MkDocs Material, `mkdocs build --strict`; broken links fail CI).
- `README.md` is a short overview linking into `docs/`.
- Root `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md` are included into the site via `pymdownx.snippets`.
- `CLAUDE.md` is for tools and is intentionally not part of the site.
