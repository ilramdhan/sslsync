Put certificate files here, one folder per certificate (not per server). Point `.env` at them:

    DEFAULT_CERT_FILE=certs/example.com/ssl-bundle.crt       full chain: server cert first, then intermediates
    DEFAULT_KEY_FILE=certs/example.com/example.com.key     private key (the one generated with the CSR)

A server that uses a different certificate sets `<SERVER>_CERT_FILE` / `<SERVER>_KEY_FILE`.

Building the full chain from Sectigo files:

    cat STAR_example_com.crt STAR_example_com.ca-bundle > ssl-bundle.crt

After every renewal run `make validate`. It checks expiry, the chain order, that the key matches, and that the SAN covers every VERIFY host.
Everything here except this README is git-ignored. Keep CA account passwords and `.pfx` files in the password manager, not here.
