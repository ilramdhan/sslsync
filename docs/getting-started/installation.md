# Installation

sslsync is a single program with no dependencies. Pick **one** way to install it.

=== "macOS / Linux (recommended)"

    ```bash
    curl -fsSL https://ilramdhan.github.io/sslsync/install.sh | sh
    ```

    The script detects your system, downloads the right file from the [latest release](https://github.com/ilramdhan/sslsync/releases/latest), **checks its SHA-256 checksum**, and copies `sslsync` to `/usr/local/bin` (or `~/.local/bin`). You can [read it first](https://github.com/ilramdhan/sslsync/blob/main/scripts/install.sh).

    Specific version or folder:

    ```bash
    curl -fsSL https://ilramdhan.github.io/sslsync/install.sh | SSLSYNC_VERSION=v1.2.0 SSLSYNC_INSTALL_DIR=~/bin sh
    ```

=== "Homebrew"

    ```bash
    brew install --cask ilramdhan/tap/sslsync
    ```

    Upgrade later with `brew upgrade --cask sslsync`.

=== "Go"

    ```bash
    go install github.com/ilramdhan/sslsync/cmd/sslsync@latest
    ```

    Requires Go 1.26 or newer. The binary goes to `$(go env GOPATH)/bin`.

=== "Debian / Ubuntu / RHEL"

    Download the `.deb`, `.rpm` or `.apk` from the [latest release](https://github.com/ilramdhan/sslsync/releases/latest), then:

    ```bash
    sudo dpkg -i sslsync_*_linux_amd64.deb      # Debian / Ubuntu
    sudo rpm -i sslsync_*_linux_amd64.rpm       # RHEL / Fedora
    ```

=== "Docker"

    ```bash
    docker run --rm ghcr.io/ilramdhan/sslsync:latest version
    ```

    See [Running in Docker](#running-in-docker) below.

=== "Windows"

    Download `sslsync_*_windows_amd64.zip` from the [latest release](https://github.com/ilramdhan/sslsync/releases/latest), unzip it, and put `sslsync.exe` in a folder on your `PATH`. WSL works too: use the macOS / Linux installer inside WSL.

## Check it works

```bash
sslsync version
```

```text
sslsync v1.2.0
```

!!! tip "`command not found`?"
    The install folder is not on your `PATH`. The installer prints the line to add to `~/.zshrc` or `~/.bashrc`. Open a new terminal afterwards.

## Verify the download (optional)

Every release's `checksums.txt` is signed with [cosign](https://docs.sigstore.dev), without keys, by GitHub Actions:

```bash
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/ilramdhan/sslsync/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

## Running in Docker

The image is ~15 MB: a single static binary on [distroless](https://github.com/GoogleContainerTools/distroless), running as a non-root user. Mount your folder into `/work`:

```bash
docker run --rm -it --user "$(id -u)" \
  -v "$PWD:/work" \
  -v "$HOME/.ssh/known_hosts:/home/nonroot/.ssh/known_hosts:ro" \
  ghcr.io/ilramdhan/sslsync:latest check
```

## Updating

Run the same install command again, or `brew upgrade --cask sslsync`. Your `.env`, certificates and logs are not touched.

## Uninstalling

```bash
rm "$(command -v sslsync)"      # or: brew uninstall --cask sslsync
```

Next: [Quick start](quickstart.md)
