# Security policy

sslsync handles SSH passwords and private keys, so security reports get priority.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private reporting instead:

**[Report a vulnerability](https://github.com/ilramdhan/sslsync/security/advisories/new)**

Please include the version (`sslsync version`), what you did, and what happened. Remove any passwords, keys and host names first.

You can expect:

- acknowledgement within **3 working days**
- an assessment and a plan within **10 working days**
- credit in the release notes, if you want it

## Supported versions

Only the latest release receives fixes. Update with the [install command](https://ilramdhan.github.io/sslsync/getting-started/installation/).

## What is in scope

- leaking secrets: passwords, private keys or `.env` contents in output, logs, temp files or process lists
- weaknesses in SSH host key verification
- commands built from configuration that could be abused for shell injection
- leaving servers with a weaker state than before, e.g. wider permissions
- release integrity: checksums, signatures, the install script

How sslsync protects secrets is described in [Security](https://ilramdhan.github.io/sslsync/about/security/).
