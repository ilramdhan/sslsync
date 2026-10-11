# Target types

A **target** is something on a server that uses the certificate. One variable, `<SERVER>_TARGETS`, says what is there, so normal servers and Kubernetes nodes are configured the same way:

```bash
WEB_PROD_TARGETS=nginx,minio      # a VM with nginx and MinIO
K8S_PROD_TARGETS=k8s            # a Kubernetes node
DB_PROD_TARGETS=files              # just place the files
```

| Type | Files | Health check | Reload |
|---|---|---|---|
| `nginx` | `_CERT` (full chain), `_KEY` | service active, `nginx -t` | `nginx -t && systemctl reload nginx` |
| `apache` | `_CERT`, `_KEY`, optional `_CHAIN` | service active, `apachectl configtest` | configtest + reload `apache2`/`httpd` |
| `haproxy` | `_PEM` (chain + key in one file) | service active, `haproxy -c` | `haproxy -c` + reload |
| `minio` | `_CERT` (`public.crt`), `_KEY` (`private.key`) | `systemctl is-active $SERVICE` | `systemctl restart $SERVICE` |
| `postgres` | `_CERT`, `_KEY` | service active | `systemctl reload $SERVICE` |
| `k8s` | `_CERT`, `_KEY` (staging copies on the node, created if missing) | API `readyz` + rollout status | update secrets, then rollout restart (below) |
| `files` | any of `_CERT`, `_KEY`, `_CHAIN`, `_PEM` | — | — |

`sslsync targets` prints the same table, with every setting.

## Common target settings

| Setting | Description |
|---|---|
| `_<TARGET>_TYPE` | when the name is not a type: `TARGETS=nginx,api` + `API_TYPE=minio` (two MinIOs, etc.) |
| `_<TARGET>_CREATE` | `true`: create missing files and folders (default only for `k8s`) |
| `_<TARGET>_OWNER` | owner of created files, e.g. `root:ssl-cert` |
| `_<TARGET>_KEY_MODE` | permissions the private key must have, e.g. `0600` (default for `k8s`). Unset: only access for *other* users is removed. Group-readable keys such as `0640 root:ssl-cert` keep working |
| `_<TARGET>_RELOAD` | replace the default reload; `none` = no reload |
| `_<TARGET>_ROLLBACK` | command after the old files are restored (default: reload again) |
| `_<TARGET>_HEALTH` | replace the default health check; `none` = none |
| `_<TARGET>_<FILE>_CONTENT` | what a file gets: `fullchain`, `leaf`, `chain`, `key`, `pem` |
| `_<TARGET>_SERVICE` | systemd unit (`minio`, `postgres`) |

## Kubernetes (`k8s`)

| Setting | Description |
|---|---|
| `_K8S_CERT`, `_K8S_KEY` | where the files are placed on the node (the source for the secrets) |
| `_K8S_TLS_SECRETS` | `kubernetes.io/tls` secret names; updated in **every namespace** that has them |
| `_K8S_OPAQUE_SECRETS` | `name:certkey:keykey`, e.g. `minio-tls:public.crt:private.key` |
| `_K8S_RESTART` | `namespace/kind/name` list, restarted and awaited after the update |
| `_K8S_KUBECTL` | default `kubectl` (e.g. `k3s kubectl`) |

How it works: before the update, every affected secret is exported to `/root/ssl-secret-backup/<stamp>/`. The secrets are then updated, and the workloads restarted and awaited. A failed rollout, health check or `VERIFY` re-applies the saved secrets and restarts again. The `state` phase compares the certificate in each secret with the file, so an interrupted run is completed next time.

Updating a secret in **every** namespace that has it matters: copies of the same TLS secret often exist in more namespaces than a runbook lists (ingress, ArgoCD, dashboards, monitoring …). Updating only some of them leaves the rest on the old certificate.

## Choosing `VERIFY`

List **every port a client connects to**: web, admin panels, the MinIO API and console, NodePorts. Verification compares the exact certificate bytes, so it catches:
- a service that was not reloaded
- a port served by another process you did not know about
- a load balancer or ingress in front that still has the old cert

A server without `VERIFY` is never checked end-to-end. Add it unless there is truly no TLS port (`files` targets).
