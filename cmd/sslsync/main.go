// Command sslsync renews SSL certificates on many servers over SSH:
// it validates the certificate locally, checks every server, replaces the
// files in place, reloads the services, verifies every port serves the new
// certificate and rolls back automatically when anything fails.
//
// All configuration is read from .env — see .env.example and README.md.
package main

import (
	"os"

	"github.com/ilramdhan/sslsync/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr))
}
