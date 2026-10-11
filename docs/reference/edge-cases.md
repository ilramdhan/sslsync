# Edge cases

## Certificate files

Checked by `sslsync validate`, and before every other command.

| Situation | Behavior |
|---|---|
| expired / not yet valid | ✗ refused |
| expires within 14 days | ⚠ warning |
| key does not match the certificate | ✗ refused |
| server certificate not first in the bundle | ✗ refused ("must come first"); otherwise servers present the wrong cert |
| no intermediates in the bundle | ⚠ warning (some clients fail without them) |
| chain does not verify against trusted roots | ⚠ warning |
| SAN does not cover a `VERIFY` host | ✗ refused, naming the host |
| key is encrypted | ✗ refused, with the `openssl` command to decrypt |
| DER / `.pfx` instead of PEM | ✗ refused ("not PEM") |
| certificate in the key file | ✗ refused ("is it the right file?") |
| CRLF line endings, blank lines, trailing spaces | normalized; MinIO crashes on these |
| cert and key in one file | supported: leave `_KEY_FILE` empty |

## Configuration

Checked by `sslsync list`, and by every other command.

| Situation | Behavior |
|---|---|
| missing `_HOST`, `_USER`, `_TARGETS`, path | ✗ one message listing **all** problems |
| unknown target type | ✗ lists the valid types |
| relative destination path | ✗ |
| same destination in two targets | ✗ |
| same `user@host:port` twice | ✗ ("merge their TARGETS") |
| bad port / boolean / duration | ✗ names the variable and the expected form |
| misspelled or unused variable | ⚠ warning naming the variable |
| `.env` readable by other users | ✗ refused, with the `chmod 600` command |
| unknown name in `ONLY=`/`EXCLUDE=`/`GROUP=` | ✗ (no silent "0 servers") |

## Connection

| Situation | Behavior |
|---|---|
| timeout, refused, unreachable, reset | retried 3× with backoff, then ✗; server untouched |
| wrong password / user / key | ✗ immediately (no retry, avoids lockouts) |
| DNS name not found | ✗ immediately |
| host key unknown | ✗ → `sslsync trust` |
| **host key changed** | ✗, flagged as possible MITM / reinstall; never accepted automatically |
| wrong sudo password, user not in sudoers | ✗ with the reason |
| sudo without password configured | uses `sudo -n` (works with NOPASSWD) |

## On the server

| Situation | Behavior |
|---|---|
| destination missing | ✗ "check the path"; a typo never creates a file nobody reads (unless `_CREATE=true`) |
| destination is a directory / not writable | ✗ before anything changes |
| folder not writable (cannot back up) | ✗ before anything changes |
| existing owner and mode (e.g. `minio-user:minio-user 600`) | preserved: content is written into the existing file |
| new file | mode `0644` (cert) / `0600` (key, pem), owner from `_OWNER` |
| existing private key readable by other users (e.g. `0644`) | `check` reports it; `deploy` tightens it (`0640`, or `_KEY_MODE`); restored on rollback |
| upload corrupted | sha256 mismatch → ✗, nothing replaced |
| write corrupted | sha256 verified after write → rollback |
| file already identical | skipped, no backup, no reload for that target |
| service unhealthy **before** | ✗ refused: never deploy onto a broken service |
| `nginx -t` fails after the new files | reload not done → rollback |
| service does not come back after restart | health (after) fails → rollback |
| port still serves the old cert (no reload, other process, LB in front) | verify fails → rollback, with what is served |
| files new but port old (previous reload never happened) | detected; every target is reloaded |
| run interrupted (Ctrl-C, laptop sleep) after replace | backups remain; next run sees new files, finishes reload/verify. K8s: `state` sees outdated secrets |
| two runs in the same second | different stamps (`…-1`): log folders and backups never collide |
| a command hangs | killed after `_TIMEOUT` (reload) / 2 min (health) / 60 s (others) |

## Kubernetes

| Situation | Behavior |
|---|---|
| secret exists in several namespaces | all updated, all backed up |
| secret name not found anywhere | ✗ before changing any secret |
| rollout does not finish (CrashLoopBackOff) | secrets restored from the backup, restarted again |
| restore needs a fresh `resourceVersion` | stripped from the backup before applying |
| no secret changed yet when it failed | rollback reports "nothing to restore" |
