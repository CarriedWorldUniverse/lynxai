// Package version holds the build-time version string for lynxai.
//
// Default is "dev"; release builds override via -ldflags:
//
//	go build -ldflags "-X github.com/CarriedWorldUniverse/lynxai/internal/version.Version=v0.1.0" ./...
//
// goreleaser handles the injection at release time; local dev builds report
// "dev" unless a Makefile or script supplies the flag.
package version

// Version is the build-time version string. Overridden via -ldflags;
// "dev" when unset.
var Version = "dev"
