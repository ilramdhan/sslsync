# How it works

Each server is processed independently, in parallel (default 4 at a time), in these phases:

| # | Phase | Changes the server? | On failure |
|---|---|---|---|
| 1 | **connect**: SSH login, host key verified | no | stop; server untouched |
| 2 | **sudo**: sudo works with the configured password | no | stop; server untouched |
| 3 | **health**: each target's health checks (service running, config valid) | no | stop; server untouched |
| 4 | **files**: each destination exists and is writable; shows owner, mode, and whether it is already the new cert | no | stop; server untouched |
| 5 | **state**: Kubernetes secrets already contain the new cert? | no | stop; server untouched |
| 6 | **tls**: what every `VERIFY` port serves now (information only) | no | — |
| | *`sslsync check` ends here* | | |
| 7 | **upload**: files go to a private temp dir (`umask 077`), sha256 verified | `/tmp` only | stop; temp dir removed |
| 8 | **replace**: back up to `<file>.bak-<stamp>`, overwrite **in place** so owner and mode are kept, sha256 verified | **yes** | **rollback** |
| 9 | **reload**: each changed target's reload (e.g. `nginx -t && systemctl reload nginx`, k8s secret update + rollout) | **yes** | **rollback** |
| 10 | **health**: the same checks again | no | **rollback** |
| 11 | **verify**: every `VERIFY` port must serve the **new** cert (retries for about 30 s) | no | **rollback** |

**Rollback** restores every backed-up file and removes files it created. It then runs each reloaded target's rollback, so services load the old certificate again. For Kubernetes, that means re-applying the secrets saved before the update and restarting. A failure *during* rollback is reported as `ROLLBACK-FAILED` with the backup names, so you can finish it by hand.

**Idempotent:** a server whose files, secrets and ports already have the new certificate is reported as `up-to-date` and left alone. Re-running after a partial failure only does what is still missing.

## Statuses

| Status | Meaning |
|---|---|
| `ready` | `check`: healthy and ready for the new certificate |
| `up-to-date` | already has the new certificate everywhere; nothing done |
| `updated` | deploy succeeded and was verified |
| `planned` | `plan` (dry run) |
| `check-failed` | a check failed; **nothing was changed** |
| `rolled-back` | failed after changes; **old certificate restored** |
| `ROLLBACK-FAILED` | failed **and** the rollback was incomplete; **act now** (see [troubleshooting](../guides/troubleshooting.md)) |
