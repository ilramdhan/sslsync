# Troubleshooting

| Message | Fix |
|---|---|
| `.env is readable by other users` | `chmod 600 .env` |
| `unknown host key` | `sslsync trust --only <server>` |
| `HOST KEY CHANGED` | Confirm with the server owner that it was reinstalled, then `ssh-keygen -R <host>` and `sslsync trust`. **Never skip this.** |
| `authentication failed` | check `_USER`/`_PASSWORD`; try `ssh user@host` by hand |
| `sudo password incorrect` | set `_SUDO_PASSWORD` if it differs from the login password |
| `does not exist — check the path` | compare with `sslsync find user@host`; for a new location set `_<TARGET>_CREATE=true` |
| `still serves the old certificate` | the reload did not make the service pick up the file. Is `VERIFY` pointing at the right service? Is there a load balancer in front? Check the per-port table of `sslsync find` |
| `certificate … does not cover <host>` | the `VERIFY` host is outside the SAN, e.g. `a.b.example.com` vs `*.example.com` |
| `health: … failed` before deploy | the service was already broken; fix it first. Deploying would hide the cause |

## `ROLLBACK-FAILED`

The message lists what could not be restored. On that server:

```bash
ls /path/to/*.bak-<stamp>                    # stamp is in the message and the log folder name
sudo cp -p /path/file.bak-<stamp> /path/file
sudo systemctl reload nginx                  # or the target's reload
# Kubernetes:
ls /root/ssl-secret-backup/<stamp>/
sudo kubectl apply -f <file>                 # after removing resourceVersion/uid lines
```

Then run `sslsync status --only <server>`.
