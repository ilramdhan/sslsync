# Kubernetes

In Kubernetes, certificates usually live in **secrets**, not in files the services read. sslsync handles this with the `k8s` target: it logs into a node where `kubectl` works, updates the secrets, restarts what uses them, and verifies the ports.

## What you need

- SSH access to a node (or bastion) where `sudo kubectl get secrets -A` works
- the names of the secrets that hold the certificate
- which deployments must restart to pick up the new secret

Find them with:

```bash
sslsync find deploy@k8s.example.com k8s.example.com
# or only the Kubernetes part:
ssh -t deploy@k8s.example.com 'sudo kubectl get secret -A | grep -i tls; sudo kubectl get svc -A | grep -E "NodePort|LoadBalancer"'
```

## Configuration

```bash
K8S_PROD_HOST=k8s.example.com
K8S_PROD_USER=deploy
K8S_PROD_PASSWORD='…'
K8S_PROD_TARGETS=k8s

# where the files are placed on the node (created if missing, key mode 0600)
K8S_PROD_K8S_CERT=/root/ssl/tls.crt
K8S_PROD_K8S_KEY=/root/ssl/tls.key

# kubernetes.io/tls secrets (keys tls.crt / tls.key), updated in EVERY namespace that has them
K8S_PROD_K8S_TLS_SECRETS=example-tls

# other secrets: name:certfield:keyfield (e.g. MinIO expects public.crt / private.key)
K8S_PROD_K8S_OPAQUE_SECRETS=minio-tls:public.crt:private.key

# restarted after the update, then awaited (namespace/kind/name)
K8S_PROD_K8S_RESTART=ingress-nginx/deployment/ingress-nginx-controller,minio/deployment/minio

# every port clients use — ingress, NodePorts …
K8S_PROD_VERIFY=:443,:30090,:30091
```

On k3s without a `kubectl` on the PATH, set `K8S_PROD_K8S_KUBECTL="k3s kubectl"`.

Several clusters with the same layout? Put the shared lines once as `DEFAULT_K8S_…` (for example `DEFAULT_K8S_TLS_SECRETS=example-tls`), and give each cluster only its `HOST`, `USER`, `PASSWORD`, `TARGETS` and `VERIFY`.

## What happens

1. **check**: the API server is ready, the `RESTART` deployments are healthy, and every secret is compared with the certificate. Outdated ones are listed, for example `~ argocd/example-tls outdated`.
2. **deploy**: each affected secret is exported to `/root/ssl-secret-backup/<run-id>/`, then updated, then the deployments are restarted and awaited with `rollout status`.
3. **verify**: every `VERIFY` port must serve the new certificate.
4. **rollback** (if 2 or 3 fails, e.g. a pod in CrashLoopBackOff): the saved secrets are re-applied and the deployments restarted again.

!!! note "Why every namespace?"
    Copies of one TLS secret often exist in more namespaces than anyone remembers: ingress, ArgoCD, dashboards, monitoring. Updating only some leaves the rest serving the old certificate until it expires. sslsync finds and updates them all, and the `check` output lists each one.

## MinIO in Kubernetes

MinIO reads `public.crt` and `private.key` from a secret mounted at its certs directory. MinIO also refuses keys with blank lines at the end, which sslsync removes automatically. Add the secret to `OPAQUE_SECRETS` and the deployment to `RESTART`, as in the example above.
