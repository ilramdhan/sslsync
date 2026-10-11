# shellcheck shell=sh
set -eu
K="${KUBECTL:-kubectl}"
$K get --raw=/readyz >/dev/null
for r in $(echo "${RESTART:-}" | tr ',' ' '); do
  $K -n "${r%%/*}" rollout status "${r#*/}" --timeout=30s >/dev/null
done
