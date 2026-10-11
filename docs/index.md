---
title: sslsync
hide:
  - navigation
  - toc
---

<div class="hero" markdown>

# Renew SSL certificates on every server, safely

**sslsync** puts a new certificate on all your servers from one laptop. It checks every server first, replaces the files, reloads the services, and **proves** that each port serves the new certificate. If anything goes wrong, it puts the old certificate back automatically.

```bash
curl -fsSL https://ilramdhan.github.io/sslsync/install.sh | sh
```

[Get started :material-arrow-right:](getting-started/quickstart.md){ .md-button .md-button--primary }
[What is an SSL certificate?](getting-started/concepts.md){ .md-button }

</div>

## Why sslsync

<div class="grid cards" markdown>

-   :material-file-cog-outline: **One file to configure**

    ---

    Every server, port, path and password lives in one `.env` file. No YAML, no code. `sslsync list` tells you about typos.

-   :material-shield-check-outline: **Nothing changes until it is safe**

    ---

    Login, sudo, service health, file paths and permissions are all checked before a single byte is written.

-   :material-check-decagram-outline: **Proof, not hope**

    ---

    After a reload, sslsync connects to every port and compares the certificate byte for byte. "Reload succeeded" is not enough.

-   :material-undo-variant: **Automatic rollback**

    ---

    A failed reload, health check or verification restores the old files, Kubernetes secrets and permissions, then reloads again.

-   :material-server-network: **Works with what you have**

    ---

    Nginx, Apache, HAProxy, MinIO, PostgreSQL, Kubernetes secrets, or plain files. Mix several on one server.

-   :material-text-box-search-outline: **Full audit trail**

    ---

    Every run writes a log per server plus a machine-readable `summary.json`. Backups on the server share the run's ID.

</div>

## How it looks

```text
$ sslsync deploy --group staging
[web-staging] ✓ connect  web-staging | Ubuntu 24.04 LTS
[web-staging] ✓ sudo     ok
[web-staging] ✓ health   3 check(s) passed
[web-staging]   ~ /etc/ssl/certs/site.crt        will be replaced (root:root 644, fullchain)
[web-staging]   ~ /etc/ssl/private/site.key      will be replaced (root:root 600, key)
[web-staging] ✓ upload   2 file(s) staged, sha256 verified
[web-staging] ✓ replace  2 replaced, 0 created
[web-staging] ✓ reload   NGINX
[web-staging]   ✓ web.example.com:443           serves the NEW certificate (expires 2027-02-12)
[web-staging] ● updated
```

## Where to go next

| I want to… | Read |
|---|---|
| understand certificates first | [What is an SSL certificate?](getting-started/concepts.md) |
| install it | [Installation](getting-started/installation.md) |
| use it for the first time | [Quick start](getting-started/quickstart.md) |
| renew a certificate | [Tutorial: renew a certificate](guides/renewal.md) |
| add a server | [Tutorial: add a server](guides/add-server.md) |
| look up a setting | [Configuration reference](reference/configuration.md) |
| fix an error | [Troubleshooting](guides/troubleshooting.md) |
