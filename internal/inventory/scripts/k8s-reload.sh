# shellcheck shell=sh
# Loads $CERT/$KEY into Kubernetes secrets and restarts what mounts them.
#   TLS_SECRETS     names of kubernetes.io/tls secrets, updated in EVERY namespace that has them
#   OPAQUE_SECRETS  name:certkey:keykey, e.g. minio-tls:public.crt:private.key (every namespace)
#   RESTART         namespace/kind/name, e.g. ingress-nginx/deployment/ingress-nginx-controller
# The current secrets are saved to /root/ssl-secret-backup/$SSLSYNC_STAMP for rollback.
set -eu
K="${KUBECTL:-kubectl}"
B="/root/ssl-secret-backup/$SSLSYNC_STAMP"
mkdir -p "$B"
chmod 700 /root/ssl-secret-backup "$B"

namespaces() {
  $K get secret -A --field-selector "metadata.name=$1" -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{end}'
}

for name in $(echo "${TLS_SECRETS:-}" | tr ',' ' '); do
  nss=$(namespaces "$name")
  [ -n "$nss" ] || { echo "TLS secret '$name' not found in any namespace" >&2; exit 1; }
  for ns in $nss; do
    $K -n "$ns" get secret "$name" -o yaml > "$B/$ns.$name.yaml"
    $K -n "$ns" create secret tls "$name" --cert="$CERT" --key="$KEY" --dry-run=client -o yaml | $K apply -f - >/dev/null 2>&1
    echo "secret $ns/$name updated"
  done
done

for spec in $(echo "${OPAQUE_SECRETS:-}" | tr ',' ' '); do
  name=${spec%%:*}; rest=${spec#*:}; ck=${rest%%:*}; kk=${rest#*:}
  [ "$name" != "$spec" ] && [ "$ck" != "$rest" ] || { echo "OPAQUE_SECRETS entry '$spec' must be name:certkey:keykey" >&2; exit 1; }
  nss=$(namespaces "$name")
  [ -n "$nss" ] || { echo "secret '$name' not found in any namespace" >&2; exit 1; }
  for ns in $nss; do
    $K -n "$ns" get secret "$name" -o yaml > "$B/$ns.$name.yaml"
    $K -n "$ns" create secret generic "$name" --from-file="$ck=$CERT" --from-file="$kk=$KEY" --dry-run=client -o yaml | $K apply -f - >/dev/null 2>&1
    echo "secret $ns/$name updated"
  done
done

for r in $(echo "${RESTART:-}" | tr ',' ' '); do
  $K -n "${r%%/*}" rollout restart "${r#*/}"
done
for r in $(echo "${RESTART:-}" | tr ',' ' '); do
  $K -n "${r%%/*}" rollout status "${r#*/}" --timeout=180s
done
