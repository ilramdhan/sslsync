# Development

## Setup

```bash
git clone https://github.com/ilramdhan/sslsync.git
cd sslsync
make build          # bin/sslsync
make test           # go vet + go test -race
make lint           # golangci-lint (config: .golangci.yml)
```

Requirements: Go (version in `go.mod`), [golangci-lint](https://golangci-lint.run) v2, Docker for `make docker`, and Python 3 for the docs.

Docs preview with live reload:

```bash
python3 -m venv .venv && .venv/bin/pip install -r docs/requirements.txt
.venv/bin/mkdocs serve          # http://127.0.0.1:8000
```

## Layout

```
cmd/sslsync/main.go          entry point — only calls app.Main
internal/
  app/                       commands, flags, .env loading, output; embeds env.example + find-ssl.sh
  inventory/                 .env → servers and targets; target-type catalogue
    scripts/                 embedded POSIX sh for Kubernetes (reload / rollback / state / health)
  certs/                     load + validate the certificate pair; render fullchain/leaf/chain/key/pem
  remote/                    SSH client: auth, sudo -S, timeouts, host-key algorithms, readable errors
  tlsprobe/                  read the certificate a host:port serves
  runner/                    per-server phases, permission fixes, rollback
  report/                    logs/<run>/run.log, <server>.log, summary.json
scripts/install.sh           curl | sh installer (published with the docs)
docs/                        this website (MkDocs Material)
.github/workflows/           CI, release, docs, CodeQL, Scorecard
```

Dependencies only point downwards: `app → runner → {remote, tlsprobe, report} → inventory, certs`. See [Architecture](architecture.md).

## Rules of thumb

- **Errors are for operators**: say what is wrong *and* which variable or command fixes it.
- **Shell sent to servers is POSIX `sh`**. Quote every interpolated value with `remote.Shq`. It must work on Ubuntu 20.04+.
- **Anything that changes a server** goes through runner phases, so that it is checked first, verified after, and undone on failure.
- **Tests generate certificates**; never commit real ones. CI rejects `.pem/.key/.crt/.pfx/.env` files and scans every commit with gitleaks.
- `.env.example` is embedded in the binary for `sslsync init`. `make build` / `make test` copy it to `internal/app/env.example`, and CI fails if they differ.

## Testing against a real SSH server, safely

Never test against production. A disposable container is enough for nginx, MinIO and file targets:

```bash
docker run -d --name sslsync-lab -p 2222:22 -p 8443:443 ubuntu:24.04 sleep infinity
docker exec sslsync-lab sh -c 'apt-get update -qq && apt-get install -y -qq openssh-server nginx sudo >/dev/null &&
  useradd -m -s /bin/bash deploy && echo deploy:secret | chpasswd && usermod -aG sudo deploy &&
  mkdir -p /run/sshd /etc/nginx/ssl && sed -i "s/#PasswordAuthentication.*/PasswordAuthentication yes/" /etc/ssh/sshd_config &&
  /usr/sbin/sshd && nginx'
```

Put a self-signed "old" certificate in `/etc/nginx/ssl/`, point an `.env` at `127.0.0.1:2222`, and exercise `check`, `deploy`, a failing reload (`_NGINX_RELOAD=false`, which should roll back), and a rerun (which should report `up-to-date`).

For Kubernetes, use [kind](https://kind.sigs.k8s.io) or [k3d](https://k3d.io), or a stand-in `kubectl` script that stores secrets as files, to test reload, rollback and the `state` check without a cluster.

## Adding a target type

1. Add an entry to `types` in `internal/inventory/targets.go`: its files, health checks, reload, and optional rollback/state/vars/keyMode.
2. Longer shell goes in `internal/inventory/scripts/` and is pulled in with `//go:embed`.
3. Add cases to `inventory_test.go` (valid and invalid configuration).
4. Document it in [Target types](../reference/targets.md) and `.env.example`.
