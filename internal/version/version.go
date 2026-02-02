// Package version provides version information for goansible binaries.
// These variables are set at build time via ldflags.
package version

var (
	// Version is the semantic version (e.g., "v1.2.3")
	Version = "dev"
	// Commit is the git commit hash
	Commit = "none"
	// Date is the build date in ISO 8601 format
	Date = "unknown"
)
