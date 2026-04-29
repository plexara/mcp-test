// Package build holds version metadata stamped at link time via -ldflags -X.
package build

// Version is the human-readable build version (git tag / "dev" if unstamped).
var Version = "dev"

// Commit is the short git SHA the binary was built from.
var Commit = "none"

// Date is the UTC build timestamp in RFC 3339.
var Date = "unknown"
