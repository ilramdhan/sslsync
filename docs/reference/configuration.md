# Configuration reference (.env)

`.env` is the only file you edit. `.env.example` documents every setting and has a worked example for each kind of server.

## Naming

```
SSLSYNC_SERVERS=WEB_PROD,WEB_STAGING,K8S_PROD      which servers exist (and in what order)

<SERVER>_<SETTING>              WEB_PROD_HOST=web.example.com
<SERVER>_<TARGET>_<SETTING>     WEB_PROD_NGINX_CERT=/etc/ssl/certs/ssl-bundle.crt
DEFAULT_<SETTING>               DEFAULT_PORT=22          fallback for every server
DEFAULT_<TARGET>_<SETTING>      DEFAULT_K8S_TLS_SECRETS=app-tls
```

**Server names** are `UPPER_SNAKE_CASE`. Use `<APP>_<ENV>` so related servers sort together and read well in logs:

| Good | Why |
|---|---|
| `WEB_PROD`, `WEB_STAGING` | app + environment |
| `BILLING_PROD_1`, `BILLING_PROD_2` | several servers of one app |
| `K8S_PROD` | a Kubernetes cluster is one server (the node you SSH to) |

| Avoid | Why |
|---|---|
| `SERVER1` | says nothing in a log at 2 a.m. |
| `192_168_0_40` | the IP belongs in `_HOST`; it can change |
| `WEB__PROD`, `web-prod` | must be `UPPER_SNAKE_CASE` with single underscores |

In commands you may write either `WEB_PROD` or `web-prod`: `sslsync deploy --only web-prod`.

## Server settings

| Variable | Required | Default | Description |
|---|---|---|---|
| `_HOST` | ✔ | | IP or DNS name for SSH |
| `_USER` | ✔ | | SSH user |
| `_PASSWORD` | one of | | password login; also used for sudo |
| `_SSH_KEY` | one of | | path to a private key (`~` allowed) |
| `_SSH_KEY_PASSPHRASE` | | | |
| `_PORT` | | `22` | SSH port |
| `_GROUP` | | | free label, e.g. `staging`/`prod`; selected with `GROUP=` |
| `_SUDO` | | `true` unless user is `root` | |
| `_SUDO_PASSWORD` | | `_PASSWORD` | only if different |
| `_TARGETS` | ✔ | | what on the server uses the certificate; see [target types](../reference/targets.md) |
| `_VERIFY` | recommended | | `host:port` list that must serve the new cert; `:443` = `<HOST>:443` |
| `_CERT_FILE`, `_KEY_FILE` | ✔ (usually via `DEFAULT_`) | | local certificate files on your Mac |
| `_BACKUP` | | `true` | keep `<file>.bak-<stamp>` on the server |
| `_TIMEOUT` | | `10m` | max time for one reload/rollback command |
| `_HEALTH` | | | extra command that must succeed before and after |
| `_POST` / `_ROLLBACK` | | | extra command after all reloads / its undo |
| `_INSECURE_HOST_KEY` | | `false` | skip SSH host key checking (avoid) |

Any of these can be set once as `DEFAULT_<SETTING>`.

## Overriding for one run

Variables already present in your shell win over `.env` for names `.env` uses, so you can test a value without editing the file:

```bash
WEB_PROD_PORT=2222 sslsync check --only web-prod
```

## Several env files

```bash
sslsync check --env .env --env .env.local        # later files win
```

Typical uses: the shared layout in `.env.servers` and personal passwords in `.env.local`.

## Typo detection

`sslsync list` (and every other command) warns about variables that nothing reads. For example:

```
⚠ WEB_PROD_NGINX_CRT is not a known setting of web-prod (typo? or the target is not in WEB_PROD_TARGETS)
⚠ OLD_SERVER_HOST is not used (server missing from SSLSYNC_SERVERS?)
```
