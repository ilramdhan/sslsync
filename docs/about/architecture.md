# Architecture

## Goals

1. **One place to configure**: `.env`. Operators should never need to read or edit code or YAML.
2. **Never leave a server half-updated**: either the new certificate is live and verified, or the old one is restored.
3. **Prove it worked**: success means the port serves the new certificate, not just "the command exited 0".
4. **Work on any server**: the differences between nginx, MinIO, Kubernetes, and so on are data (target types), not code paths.

## Packages

```
cmd/sslsync ──► internal/app ──► internal/runner ──► internal/remote    (SSH)
                     │                 │          └─► internal/tlsprobe  (TLS)
                     │                 └────────────► internal/report    (logs)
                     ├──► internal/inventory  (.env → []Server)
                     └──► internal/certs      (files → Bundle)
```

| Package | Responsibility | Knows about |
|---|---|---|
| `certs` | parse, validate and render a cert/key pair | nothing else |
| `inventory` | turn variables into `[]Server` with `[]Target`; target type catalogue | `certs.Kind` |
| `remote` | run a command over SSH, optionally with sudo | `inventory.Server` (credentials) |
| `tlsprobe` | fetch the certificate a port serves | nothing else |
| `report` | screen + file logs, `summary.json` | nothing else |
| `runner` | phases and rollback for one server | all of the above |
| `app` | flags, env files, commands, output | all of the above |

`internal/` keeps everything private to this module.

## Key decisions

### Configuration in environment variables, not YAML

Operators fill in `.env` files already. A second format meant two places to change for a new server. Env names are flat, so structure comes from a naming convention (`<SERVER>_<TARGET>_<SETTING>`) with `DEFAULT_` fallbacks. The cost is that typos are easy. The mitigation is that `inventory` records every variable it reads and warns about every `.env` variable that nothing read.

### Targets instead of "server type"

A single `TYPE=normal|kubernetes` per server cannot describe a VM with nginx *and* MinIO, or a node with nginx in front of k8s. A server therefore has a list of targets. Each target type is a value in a table (`inventory/targets.go`): its files, health checks, reload, rollback, and state check. Adding a type is data, plus a test.

### Overwrite in place

`cat new > existing` keeps the inode, owner, group, mode, ACLs and SELinux label, so services that run as their own user (`minio-user`) keep access. An atomic `mv` would be safer against a crash mid-write, but it would reset ownership. The sha256 check after the write and the `.bak` restore cover that gap.

### Verify by bytes, not by exit code

`systemctl reload` returns 0 even when the service keeps the old certificate (wrong path, another process on the port, a proxy in front). `tlsprobe` compares the served leaf certificate byte for byte with the one being deployed.

### Rollback

`runner` records exactly what it changed: files replaced (with backup), files created, and targets reloaded. It undoes only those. For Kubernetes the "files" are secrets, so the reload script exports them first, and the rollback script re-applies that export. Both scripts are keyed by the run stamp, which is passed to scripts as `SSLSYNC_STAMP`.

### Idempotency

The check phase computes what is already current: file hashes on the server, secret hashes in Kubernetes, and the certificates served on the ports. Deploy only acts on what is not. Re-running is how interrupted runs are completed.

## Adding a target type

1. Add an entry to `types` in `internal/inventory/targets.go`: files (setting name, content kind, required, create by default), health, reload, optional rollback/state, and extra variables.
2. If it needs more than a few lines of shell, put the script in `internal/inventory/scripts/` and `//go:embed` it.
3. Add a case to `TestLoad` / `TestLoadErrors`.
4. Document it in the README (section 4) and `.env.example`.
