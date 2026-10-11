#!/usr/bin/env bash
# Finds where SSL certificates live on a server, which ports serve TLS
# (including custom ports and MinIO), and which file each port uses.
# Read-only: it changes nothing.
#
# Run by `sslsync find user@host [domain]`, which streams this script over
# SSH (nothing is copied to the server).

DOMAIN="${1:-}"
section() { printf '\n===== %s =====\n' "$1"; }
fp() { openssl x509 -noout -fingerprint -sha256 2>/dev/null | cut -d= -f2 | tr -d : | cut -c1-16; }

section "TLS on every listening port (custom ports included)"
printf '%-24s %-18s %-28s %-12s %s\n' "ADDRESS" "PROCESS" "SUBJECT CN" "EXPIRES" "SHA256 (first 16)"
ss -tlnpH 2>/dev/null | awk '{print $4, $6}' | sort -u -t' ' -k1,1 | while read -r addr proc; do
  port=${addr##*:}
  host=${addr%:*}
  case "$host" in 0.0.0.0|\*|\[::\]|"") host=127.0.0.1 ;; esac
  [ "$port" = 22 ] && continue
  sni=()
  [ -n "$DOMAIN" ] && sni=(-servername "$DOMAIN")
  pem=$(echo | timeout 4 openssl s_client -connect "$host:$port" "${sni[@]}" 2>/dev/null | openssl x509 2>/dev/null)
  [ -z "$pem" ] && continue
  cn=$(echo "$pem" | openssl x509 -noout -subject -nameopt multiline | awk -F' = ' '/commonName/{print $2; exit}')
  end=$(echo "$pem" | openssl x509 -noout -enddate | cut -d= -f2)
  end=$(date -d "$end" +%F 2>/dev/null || echo "$end")
  pname=$(echo "$proc" | grep -oE '"[^"]+"' | head -1 | tr -d '"')
  printf '%-24s %-18s %-28s %-12s %s\n' "$addr" "${pname:-?}" "${cn:-?}" "$end" "$(echo "$pem" | fp)"
done
echo "(match SHA256 with the file list below to see which file each port uses)"

section "Nginx: listen ports + certificate paths (active config)"
if command -v nginx >/dev/null; then
  nginx -T 2>/dev/null | grep -v '^\s*#' | grep -oE '(listen|server_name|ssl_certificate(_key)?)\s+[^;]+' | sed 's/^/  /'
else
  echo "(nginx not installed)"
fi

section "Apache: certificate paths"
grep -rhsoE '^\s*(Listen|ServerName|SSLCertificate(Key|Chain)?File)\s+\S+' /etc/apache2/ports.conf /etc/apache2/sites-enabled /etc/httpd 2>/dev/null | sed 's/^\s*//' | awk '!seen[$0]++' || echo "(none)"

section "MinIO"
found=""
if systemctl list-unit-files 2>/dev/null | grep -qi minio; then
  found=1
  for u in $(systemctl list-unit-files --no-legend 2>/dev/null | awk 'tolower($1) ~ /minio/ {print $1}'); do
    echo "systemd unit: $u ($(systemctl is-active "$u" 2>/dev/null))"
    systemctl cat "$u" 2>/dev/null | grep -E '^(User|Group|ExecStart|EnvironmentFile|WorkingDirectory)=' | sed 's/^/  /'
  done
fi
for f in /etc/default/minio /etc/minio/minio.conf; do
  [ -f "$f" ] && { found=1; echo "$f (secrets hidden):"; grep -vE 'PASSWORD|SECRET|KEY=' "$f" | grep -v '^\s*#' | grep . | sed 's/^/  /'; }
done
ps -eo user=,args= 2>/dev/null | grep -E '[m]inio server' | sed 's/^/process: /' && found=1
if command -v docker >/dev/null; then
  docker ps --format '{{.ID}} {{.Names}} {{.Image}} {{.Ports}}' 2>/dev/null | grep -i minio | while read -r id rest; do
    echo "docker: $rest"
    docker inspect "$id" --format '{{range .Mounts}}  mount {{.Source}} -> {{.Destination}}{{"\n"}}{{end}}{{range .Config.Cmd}}  cmd {{.}}{{"\n"}}{{end}}' | grep .
  done && found=1
fi
# MinIO reads <certs-dir>/public.crt + private.key (default ~/.minio/certs)
find / -xdev \( -path /proc -o -path /sys -o -path /usr -o -path /var/lib/docker/overlay2 \) -prune -o \
  -type f \( -name public.crt -o -name private.key \) -print 2>/dev/null | sed 's/^/certs file: /'
[ -z "$found" ] && echo "(no MinIO service/process found)"

section "Kubernetes (K3s): TLS secrets + NodePorts (NodePorts don't show in ss)"
if command -v kubectl >/dev/null; then
  [ -f /etc/rancher/k3s/k3s.yaml ] && export KUBECONFIG=${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}
  [ -f /root/.kube/config ] && export KUBECONFIG=/root/.kube/config
  echo "-- secrets of type tls / named *tls*:"
  kubectl get secret -A --no-headers 2>/dev/null | awk '$3=="kubernetes.io/tls" || $2 ~ /tls/ {print "  "$1"/"$2" ("$3")"}' | grep -v -E 'cert-manager|webhook' | head -30
  echo "-- NodePort / LoadBalancer services:"
  kubectl get svc -A --no-headers 2>/dev/null | awk '$3=="NodePort" || $3=="LoadBalancer" {print "  "$1"/"$2"  "$3"  "$6}'
else
  echo "(kubectl not installed)"
fi

section "Docker: container mounts that look like certs"
if command -v docker >/dev/null; then
  for c in $(docker ps -q 2>/dev/null); do
    docker inspect "$c" --format '{{.Name}}{{range .Mounts}} | {{.Source}} -> {{.Destination}}{{end}}' |
      grep -iE 'ssl|cert|tls|\.pem|\.crt|\.key' || true
  done
else
  echo "(docker not installed)"
fi

section "Certificate files on disk (excluding OS CA store)"
printf '%-60s %-28s %-12s %s\n' "PATH" "SUBJECT CN" "EXPIRES" "SHA256 (first 16)"
find / -xdev \( -path /proc -o -path /sys -o -path /usr -o -path /snap -o -path /etc/ssl/certs/ca-certificates.crt \
  -o -path /var/lib/docker/overlay2 -o -path /var/lib/containerd -o -path /var/lib/kubelet -o -path /var/lib/rancher -o -path /run/k3s -o -path '*/ModSecurity/*' -o -path '*/node_modules' \) -prune \
  -o -type f -size -64k \( -name '*.crt' -o -name '*.pem' -o -name '*.cer' -o -name '*bundle*' \) -print 2>/dev/null |
while read -r f; do
  [ -L "$f" ] && continue   # Ubuntu's public root CAs in /etc/ssl/certs are symlinks
  grep -q 'BEGIN CERTIFICATE' "$f" 2>/dev/null || continue
  cn=$(openssl x509 -in "$f" -noout -subject -nameopt multiline 2>/dev/null | awk -F' = ' '/commonName/{print $2; exit}')
  end=$(openssl x509 -in "$f" -noout -enddate 2>/dev/null | cut -d= -f2)
  end=$(date -d "$end" +%F 2>/dev/null || echo "$end")
  printf '%-60s %-28s %-12s %s\n' "$f" "${cn:-?}" "$end" "$(fp < "$f")"
done

section "Private key files on disk"
find / -xdev \( -path /proc -o -path /sys -o -path /usr -o -path /snap \
  -o -path /var/lib/docker/overlay2 -o -path /var/lib/containerd -o -path /var/lib/kubelet -o -path /var/lib/rancher -o -path /run/k3s -o -path '*/ModSecurity/*' -o -path '*/node_modules' \) -prune \
  -o -type f -size -16k \( -name '*.key' -o -name '*.pem' -o -name '*bundle*' \) -print 2>/dev/null |
while read -r f; do
  grep -qE 'BEGIN (RSA |EC |ENCRYPTED )?PRIVATE KEY' "$f" 2>/dev/null && ls -l "$f"
done
