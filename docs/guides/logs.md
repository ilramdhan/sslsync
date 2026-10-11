# Logs and audit trail

Every `check` and `deploy` creates `logs/<stamp>_<command>/`:

| File | Content |
|---|---|
| `run.log` | everything printed on screen |
| `<server>.log` | one server, timestamped |
| `summary.json` | per server: status, error, changed files, each phase with duration; plus the certificate deployed |

```bash
sslsync logs          # history with ok/failed counts
sslsync last-log      # newest run.log
jq '.servers[] | {server, status, error}' logs/<run>/summary.json
```

On the servers:
- `<file>.bak-<stamp>`: the previous file, kept unless `_BACKUP=false`
- `/root/ssl-secret-backup/<stamp>/`: Kubernetes secrets before the update

`<stamp>` is the same in the log folder name and in every backup of that run.

Logs never contain passwords or private keys. They do contain host names and paths, so they are git-ignored.
