# Commands

Run `sslsync help` to see these commands in your terminal.

## Overview

| Command | Connects to | Changes anything? | What it does |
|---|---|---|---|
| `sslsync init` | — | creates `.env`, `certs/`, `.gitignore` here | first-time setup; never overwrites |
| `sslsync list` | — | — | how `.env` was read, plus warnings for typos |
| `sslsync targets` | — | — | the target types and their settings |
| `sslsync validate` | — | — | checks the certificate files ([what is checked](edge-cases.md#certificate-files)) |
| `sslsync status` | ports (TLS only) | — | `NEW` / `OLD` for every `VERIFY` port; no SSH or passwords needed |
| `sslsync plan` | — | — | exactly what `deploy` would run, per server |
| `sslsync find USER@HOST [DOMAIN]` | one server (SSH) | — | finds certificate paths, TLS on every port, MinIO, Kubernetes |
| `sslsync trust` | servers (SSH key scan) | your `~/.ssh/known_hosts` | shows unknown host keys and saves the ones you confirm |
| `sslsync check` | servers (SSH) | — | phases 1–6 of [how it works](../getting-started/how-it-works.md); logged |
| `sslsync deploy` | servers (SSH) | **yes** | phases 1–11; asks you to type `yes`; logged |
| `sslsync version` | — | — | prints the version |

## Choosing servers

Every server command accepts:

| Flag | Example | Meaning |
|---|---|---|
| `--only` | `--only web-prod,db-prod` | only these servers |
| `--exclude` | `--exclude db-prod` | all except these |
| `--group` | `--group staging` | servers whose `<NAME>_GROUP` is `staging` |

Names can be written as in `.env` (`WEB_PROD`) or in lowercase with dashes (`web-prod`). An unknown name is an error, so a typo never silently selects zero servers.

```bash
sslsync check --group staging
sslsync deploy --only web-staging
sslsync deploy --group prod --exclude db-prod
```

## Other flags

| Flag | Default | Meaning |
|---|---|---|
| `--env FILE` | `.env` | settings file; repeat to combine several (later files win) |
| `--parallel N` | `4` | servers processed at the same time |
| `--retries N` | `3` | SSH connection attempts on network errors |
| `--yes` | off | `deploy` without the confirmation prompt (for scripts) |
| `--log-dir DIR` | `logs` | where run logs are written; empty = none |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | every selected server succeeded (or, for `status`, every port is `NEW`) |
| `1` | at least one server failed, or the configuration or certificate is invalid |
| `2` | wrong command-line usage |

Use them in scripts, for example `sslsync status || notify "certificate not rolled out everywhere"`.

## Makefile shortcuts (when working from a clone)

If you cloned the repository instead of installing the binary, `make` builds it and wraps the same commands. `make` with no arguments lists them all.

| make | same as |
|---|---|
| `make setup` | build + `sslsync init` |
| `make check ONLY=a,b GROUP=x EXCLUDE=y` | `sslsync check --only a,b --group x --exclude y` |
| `make deploy YES=1` | `sslsync deploy --yes` |
| `make staging` / `make prod` | `sslsync deploy --group staging` / `--group prod` |
| `make renew` | validate → check all → deploy staging → status → **pause for confirmation** → deploy prod → status |
| `make find HOST=user@host DOMAIN=…` | `sslsync find user@host …`, also saved to `reports/` |
| `make trust-hosts` | `sslsync trust` |
| `make logs` / `make last-log` | list runs / show the newest log |
| `make test` / `make lint` / `make docker` | development |
