# What is an SSL certificate?

New to certificates? This page explains, in plain words, everything you need to use sslsync. If you already manage certificates, skip to the [Quick start](quickstart.md).

## The short version

When you open `https://example.com`, the padlock in the browser means two things:

1. The connection is **encrypted**, so nobody in between can read it.
2. The server **proved it really is** `example.com`.

The server proves this with a **certificate**: a small file, signed by a company everyone trusts (a *Certificate Authority*, or CA, such as Sectigo, DigiCert or Let's Encrypt), that says "this public key belongs to `example.com`, valid until 12 February 2027".

## The files you get

| File | Typical name | What it is | Secret? |
|---|---|---|---|
| **Certificate** | `example.com.crt`, `STAR_example_com.crt` | Your server's certificate | No, every visitor receives it |
| **CA bundle / intermediates** | `.ca-bundle`, `chain.crt` | Certificates of the CA that signed yours | No |
| **Full chain** | `fullchain.crt`, `ssl-bundle.crt` | Your certificate **followed by** the CA bundle, in one file | No |
| **Private key** | `example.com.key` | Proves the server owns the certificate | **Yes, never share it** |
| CSR | `example.com.csr` | The request you sent to the CA | No, not needed anymore |
| PFX / P12 | `.pfx` | Certificate + key in one password-protected file (Windows) | **Yes** |

sslsync needs **two** of them: the **full chain** and the **private key**. You make the full chain yourself with one command:

```bash
cat STAR_example_com.crt STAR_example_com.ca-bundle > fullchain.crt
```

The order matters: your certificate first, then the CA bundle. `sslsync validate` checks this for you.

## Why certificates need renewing

Certificates expire, usually after one year or less. After that, browsers show a red warning and apps refuse to connect. Renewing means getting a new certificate from the CA and putting it on **every** server and service that uses it, then making each service load it.

Doing that by hand on many servers is slow and easy to get wrong:

- a server is forgotten
- a file goes to the wrong path
- a service is not reloaded and keeps serving the old certificate until it expires
- a typo takes a site down

sslsync does every step the same way on every server, checks each one, and undoes its own mistakes.

## Words used in these docs

| Word | Meaning |
|---|---|
| **Server** | A machine sslsync logs into with SSH |
| **Target** | Something on that server that uses the certificate: nginx, MinIO, Kubernetes… |
| **Reload** | Telling a service to read its certificate files again, without stopping it |
| **Verify** | Connecting to a port and checking it now serves the **new** certificate |
| **Rollback** | Putting the old certificate back when something failed |
| **SAN** | The list of domain names a certificate is valid for, e.g. `*.example.com` |
| **Wildcard** | A certificate for `*.example.com`, valid for `a.example.com` and `b.example.com` but **not** `a.b.example.com` |
| **known_hosts** | Your computer's list of SSH servers it trusts, to detect impostors |

Next: [Install sslsync](installation.md)
