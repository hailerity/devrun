package cli

// These are set at build time via -ldflags by GoReleaser.
// They remain "dev" / "none" / "unknown" for local builds.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
