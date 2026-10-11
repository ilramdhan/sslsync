# Quick start

This walks you through your **first** run, one step at a time. It takes about 15 minutes. Nothing on your servers changes until step 7, and each step tells you what it does.

You need:

- sslsync [installed](installation.md)
- your new certificate files (see [What is an SSL certificate?](concepts.md))
- for each server: its address, an SSH user that can use `sudo`, and that user's password or SSH key

## 1. Make a folder for your setup

```bash
mkdir ~/ssl-renewal && cd ~/ssl-renewal
sslsync init
```

```text
  + .env
  + certs/
  + .gitignore

Next steps:
  1. Put your certificate in certs/   (full chain + private key)
  ...
```

`init` creates `.env`, your settings file, already filled with explained examples and readable only by you. It never overwrites an existing `.env`.

## 2. Put the certificate in `certs/`

```bash
mkdir certs/example.com
cat STAR_example_com.crt STAR_example_com.ca-bundle > certs/example.com/fullchain.crt
cp example.com.key certs/example.com/
```

## 3. Describe your servers in `.env`

Open `.env` in any editor. It has three steps inside; here is a complete, minimal example for **one** server running Nginx:

```bash
# STEP 1 — server names
SSLSYNC_SERVERS=WEB_PROD

# STEP 2 — the certificate on your laptop
DEFAULT_CERT_FILE=certs/example.com/fullchain.crt
DEFAULT_KEY_FILE=certs/example.com/example.com.key

# STEP 3 — the server
WEB_PROD_HOST=web.example.com          # where to SSH to
WEB_PROD_USER=deploy                   # SSH user
WEB_PROD_PASSWORD='your password'      # quotes if it has special characters
WEB_PROD_TARGETS=nginx                 # what uses the certificate there
WEB_PROD_NGINX_CERT=/etc/ssl/certs/example.com.crt     # where nginx reads it
WEB_PROD_NGINX_KEY=/etc/ssl/private/example.com.key
WEB_PROD_VERIFY=:443                   # port that must show the new certificate
```

!!! question "How do I know the paths and ports?"
    Ask sslsync to look for you. This is read-only:
    ```bash
    sslsync find user@web.example.com
    ```
    Or see [Tutorial: add a server](../guides/add-server.md).

**The naming rule:** every line for a server starts with the name you chose (`WEB_PROD_`). To add a second server, copy the block and change the name, e.g. `WEB_STAGING_`, and add it to `SSLSYNC_SERVERS`. All settings: [configuration reference](../reference/configuration.md).

## 4. Check how sslsync understood it

```bash
sslsync list
```

```text
web-prod           group=         deploy@web.example.com:22  (password, sudo=true)
    NGINX      nginx
        fullchain → /etc/ssl/certs/example.com.crt
        key       → /etc/ssl/private/example.com.key
    verify     web.example.com:443
```

A typo shows up as a warning: `⚠ WEB_PROD_NGINX_CRT is not a known setting …`.

## 5. Check the certificate

```bash
sslsync validate
```

It refuses an expired certificate, a key that does not match, the wrong order, or a certificate that does not cover your domains, **before** anything is sent anywhere.

## 6. Check the servers (read-only)

First let your computer recognise the server, once:

```bash
ssh deploy@web.example.com exit     # answer "yes" if the fingerprint is right
```

Then:

```bash
sslsync check
```

This logs in and checks sudo, service health, file paths and permissions, and what each port serves now. **It does not change anything.** Fix anything marked ✗ and run it again until every server says `ready`.

## 7. Deploy

Start with one server:

```bash
sslsync deploy --only web-prod
```

You will be asked to type `yes`. Then, for each server, sslsync backs up and replaces the files, reloads the service, and checks every `VERIFY` port. If any step fails, the old certificate is restored automatically.

## 8. Confirm

```bash
sslsync status
```

```text
SERVER             ADDRESS                    STATE  SERVED CERTIFICATE
web-prod           web.example.com:443        NEW    *.example.com, expires 2027-02-12 (124d)
```

Done. Everything was logged to `logs/`. Next year, replace the files in `certs/` and repeat steps 5–8, or follow [Tutorial: renew a certificate](../guides/renewal.md).
