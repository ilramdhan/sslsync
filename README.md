<div align="center">

# sslsync

**Renew SSL certificates on every server, safely.**

[![CI](https://github.com/ilramdhan/sslsync/actions/workflows/ci.yml/badge.svg)](https://github.com/ilramdhan/sslsync/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ilramdhan/sslsync?sort=semver)](https://github.com/ilramdhan/sslsync/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/ilramdhan/sslsync.svg)](https://pkg.go.dev/github.com/ilramdhan/sslsync)
[![CodeQL](https://github.com/ilramdhan/sslsync/actions/workflows/codeql.yml/badge.svg)](https://github.com/ilramdhan/sslsync/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/ilramdhan/sslsync/badge)](https://scorecard.dev/viewer/?uri=github.com/ilramdhan/sslsync)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

[**Documentation**](https://ilramdhan.github.io/sslsync/) · [Quick start](https://ilramdhan.github.io/sslsync/getting-started/quickstart/) · [Releases](https://github.com/ilramdhan/sslsync/releases)

</div>

---

**sslsync** puts a new SSL/TLS certificate on all your servers from one laptop. It checks every server first, replaces the files, reloads the services, and **proves** each port serves the new certificate. If anything fails, it puts the old certificate back by itself.

- **One file to configure**: hosts, ports, paths and passwords all go in `.env`. Typos are reported.
- **Checked before, verified after**: SSH, sudo, service health, paths and permissions are checked first. Afterwards every port is compared byte for byte with the new certificate.
- **Automatic rollback**: files, Kubernetes secrets and permissions are restored, and services are reloaded.
- **Works with what you run**: Nginx, Apache, HAProxy, MinIO, PostgreSQL, Kubernetes secrets, plain files. Several on one server.
- **Auditable**: a log per run and per server, plus `summary.json`.

## Install

```bash
curl -fsSL https://ilramdhan.github.io/sslsync/install.sh | sh
```

Other ways: `brew install --cask ilramdhan/tap/sslsync`, `go install github.com/ilramdhan/sslsync/cmd/sslsync@latest`, `.deb`/`.rpm`/`.apk`, or Docker `ghcr.io/ilramdhan/sslsync`. See [Installation](docs/getting-started/installation.md).

## Use

```bash
mkdir ~/ssl-renewal && cd ~/ssl-renewal
sslsync init              # creates .env with explained examples, and certs/

# put fullchain.crt + private key in certs/, describe your servers in .env, then:
sslsync list              # how .env was understood (+ typo warnings)
sslsync validate          # is the certificate OK?
sslsync check             # read-only check of every server
sslsync deploy --only web-staging    # one server first
sslsync deploy --group prod
sslsync status            # every port NEW?
```

A server in `.env` looks like this:

```bash
SSLSYNC_SERVERS=WEB_PROD
DEFAULT_CERT_FILE=certs/example.com/fullchain.crt
DEFAULT_KEY_FILE=certs/example.com/example.com.key

WEB_PROD_HOST=web.example.com
WEB_PROD_USER=deploy
WEB_PROD_PASSWORD='…'
WEB_PROD_TARGETS=nginx
WEB_PROD_NGINX_CERT=/etc/ssl/certs/example.com.crt
WEB_PROD_NGINX_KEY=/etc/ssl/private/example.com.key
WEB_PROD_VERIFY=:443
```

New to certificates? Start with [What is an SSL certificate?](docs/getting-started/concepts.md), then the [Quick start](docs/getting-started/quickstart.md).

## Documentation

| | |
|---|---|
| **Getting started** | [Concepts](docs/getting-started/concepts.md) · [Installation](docs/getting-started/installation.md) · [Quick start](docs/getting-started/quickstart.md) · [How it works](docs/getting-started/how-it-works.md) |
| **Guides** | [Renew a certificate](docs/guides/renewal.md) · [Add a server](docs/guides/add-server.md) · [Kubernetes](docs/guides/kubernetes.md) · [Logs](docs/guides/logs.md) · [Troubleshooting](docs/guides/troubleshooting.md) |
| **Reference** | [Configuration](docs/reference/configuration.md) · [Target types](docs/reference/targets.md) · [Commands](docs/reference/commands.md) · [Edge cases](docs/reference/edge-cases.md) |
| **About** | [Security](docs/about/security.md) · [Architecture](docs/about/architecture.md) · [Development](docs/about/development.md) · [Releases & CI/CD](docs/about/releases.md) |

The same pages, searchable, are published at **[ilramdhan.github.io/sslsync](https://ilramdhan.github.io/sslsync/)**.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). **Never** post passwords, private keys or your `.env`. Report security problems privately: see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 Ilham Ramadhan ([@ilramdhan](https://github.com/ilramdhan))
