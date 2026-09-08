package main

import (
	_ "embed"
	"os"
	"strings"

	"github.com/wdphoto/cardBot/cmd"
)

// VERSION is the default development version; release builds override it with
// -ldflags. Keep tags and commit descriptions out of the default version.
//
//go:embed VERSION
var version string

// Set at build time via -ldflags.
var (
	commit = "none"
	date   = "unknown"
)

//go:embed CHANGELOG.md
var changelogRaw string

func main() {
	os.Exit(cmd.Execute(cmd.BuildInfo{
		Version:   strings.TrimSpace(version),
		Commit:    commit,
		Date:      date,
		Changelog: changelogRaw,
	}))
}
