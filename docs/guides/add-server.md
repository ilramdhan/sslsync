# Tutorial: add a server

**1. Find what it uses.** This is read-only and asks for the SSH and sudo passwords:

```bash
sslsync find user@new-server.example.com new-server.example.com
```

The report (`reports/…txt`) shows:
- TLS on **every** listening port, with the certificate and process behind it
- the `ssl_certificate` paths Nginx/Apache really use
- MinIO's `--certs-dir`
- Kubernetes secrets

Match the SHA256 fingerprints to see which file feeds which port.

> Kubernetes NodePorts do not show up as listening sockets. Use `ssh -t …` 'sudo kubectl get svc,secret -A' for those.

**2. Add it to `.env`:**

```bash
SSLSYNC_SERVERS=…,NEW_PROD

NEW_PROD_GROUP=prod
NEW_PROD_HOST=new-server.example.com
NEW_PROD_USER=deploy
NEW_PROD_PASSWORD='…'
NEW_PROD_TARGETS=nginx
NEW_PROD_NGINX_CERT=/etc/nginx/ssl/site.crt
NEW_PROD_NGINX_KEY=/etc/nginx/ssl/site.key
NEW_PROD_VERIFY=:443
```

**3. Trust its host key, then check:**

```bash
sslsync list --only new-prod          # no warnings?
sslsync trust --only new-prod   # compare the fingerprint with the server's console
sslsync check --only new-prod
```
