# Tutorial: renew a certificate

**1. Get the files from the CA.** For Sectigo: `STAR_<domain>.crt`, `STAR_<domain>.ca-bundle`, and the private key from when you made the CSR.

**2. Build the full chain** (server certificate first, then intermediates), into `certs/<domain>/`:

```bash
cat STAR_example_com.crt STAR_example_com.ca-bundle > certs/example.com/ssl-bundle.crt
cp example.com.key certs/example.com/
```

Point `DEFAULT_CERT_FILE` / `DEFAULT_KEY_FILE` in `.env` at them. If you keep the same file names every year, nothing in `.env` changes.

**3. Validate:**

```bash
sslsync validate
```
```
  cert  certs/example.com/ssl-bundle.crt
        *.example.com [*.example.com example.com], issuer "Sectigo …", valid 2026-10-05 → 2027-02-12 (124 days)
        + chain: Sectigo Public Server Authentication CA OV R36
✓ certificate files valid for 7 server(s)
```

**4. See the current state.** It should be all `OLD`:

```bash
sslsync status
```

**5. Check every server (read-only):**

```bash
sslsync check
```

Fix anything ✗ before continuing. Nothing has been changed yet.

**6. Deploy one server, then staging, then prod:**

```bash
sslsync deploy --only web-staging
sslsync deploy --group staging
# open the staging apps in a browser
sslsync deploy --group prod
```

**7. Confirm:**

```bash
sslsync status        # every row NEW, exit code 0
```

Working from a clone? `make renew` runs steps 3, 5, 6 and 7 in order and stops for confirmation between staging and prod.
