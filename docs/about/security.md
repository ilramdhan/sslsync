# Security

How sslsync protects your secrets and servers.


- **Secrets only in `.env`**: git-ignored, refused unless `chmod 600`. The sudo password goes over stdin (`sudo -S`) and never appears in a process list. The private key is never logged or printed.
- **Host keys are always verified.** A changed key is a hard stop. `_INSECURE_HOST_KEY` exists for lab machines only.
- **Uploads** go to a `umask 077` temp dir that is removed afterwards, and are verified by sha256.
- **Key permissions are never left loose.** New key files are created `0600`. An existing key that other users can read is tightened during deploy (`check` reports it first), and the old mode is restored on rollback. Kubernetes staging copies are always `0600`. Set `_<TARGET>_KEY_MODE=0640` when a service group must read the key.
- **Least change**: identical files are not touched, and services whose files did not change are not reloaded.
- **`certs/`, `logs/`, `reports/`** are git-ignored. Keep the CA account password and `.pfx` files in a password manager, not in this folder.
- Prefer **SSH keys** (`_SSH_KEY`) over passwords where the server allows it.

Found a problem? Report it privately: see the [security policy](security-policy.md).

## What is never stored or sent anywhere

- Passwords are read from `.env`, held in memory, and sent only to the server they belong to (SSH, then `sudo -S` on stdin).
- The private key goes only to the servers that list it, over SSH, into a `umask 077` temp directory.
- sslsync makes no network connections other than to your servers. There is no telemetry and no update check.

## The repository itself

- `.gitignore` excludes `.env`, `certs/`, `logs/` and `reports/`. `.dockerignore` keeps them out of images.
- CI fails if a `.pem`, `.key`, `.crt`, `.pfx` or `.env` file is committed, and scans every commit with gitleaks.
- Releases are signed (cosign) and carry build provenance, so you can verify a binary came from this repository's workflow. See [Releases & CI/CD](releases.md#verifying-a-release).

## Known advisories

GO-2026-5932 is reported by scanners against `golang.org/x/crypto`, because its `openpgp` package is unmaintained and has no fix. sslsync does not import the `openpgp` package, so it is not affected. It is listed in `osv-scanner.toml` with that reason.

`govulncheck` runs in CI on every pull request and fails on any reachable vulnerability.
