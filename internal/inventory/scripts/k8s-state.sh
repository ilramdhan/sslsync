# shellcheck shell=sh
# Read-only. Prints "OUTDATED <namespace>/<secret>" for every secret whose
# certificate differs from $CERT, so a run that was interrupted after the
# files were copied still updates the secrets next time.
set -eu
K="${KUBECTL:-kubectl}"
[ -f "$CERT" ] || exit 0
want=$(sha256sum < "$CERT" | cut -d' ' -f1)
check() { # name datakey
  esc=$(printf '%s' "$2" | sed 's/\./\\./g')
  for ns in $($K get secret -A --field-selector "metadata.name=$1" -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{end}'); do
    got=$($K -n "$ns" get secret "$1" -o jsonpath="{.data.$esc}" | base64 -d | sha256sum | cut -d' ' -f1)
    [ "$got" = "$want" ] || echo "OUTDATED $ns/$1"
  done
}
for name in $(echo "${TLS_SECRETS:-}" | tr ',' ' '); do check "$name" tls.crt; done
for spec in $(echo "${OPAQUE_SECRETS:-}" | tr ',' ' '); do
  rest=${spec#*:}; check "${spec%%:*}" "${rest%%:*}"
done
