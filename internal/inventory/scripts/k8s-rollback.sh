# shellcheck shell=sh
# Re-applies the secrets saved by k8s-reload.sh for this run and restarts.
set -eu
K="${KUBECTL:-kubectl}"
B="/root/ssl-secret-backup/$SSLSYNC_STAMP"
if ! ls "$B"/*.yaml >/dev/null 2>&1; then
  echo "no secret was changed in this run — nothing to restore"
  exit 0
fi
for f in "$B"/*.yaml; do
  # an exported object cannot be applied with its old resourceVersion/uid
  sed -e '/^  resourceVersion:/d' -e '/^  uid:/d' -e '/^  creationTimestamp:/d' "$f" | $K apply -f - >/dev/null 2>&1
  echo "secret $(basename "$f" .yaml | tr . /) restored"
done
for r in $(echo "${RESTART:-}" | tr ',' ' '); do
  $K -n "${r%%/*}" rollout restart "${r#*/}"
done
for r in $(echo "${RESTART:-}" | tr ',' ' '); do
  $K -n "${r%%/*}" rollout status "${r#*/}" --timeout=180s
done
