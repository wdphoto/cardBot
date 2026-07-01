package main

import (
	_ "embed"
	"os"

	"github.com/wdphoto/cardBot/cmd"
)

// Set at build time via -ldflags.
var (
	version = "0.0.10"
	commit  = "none"
	date    = "unknown"
)

//go:embed CHANGELOG.md
var changelogRaw string

func main() {
	os.Exit(cmd.Execute(cmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		Date:      date,
		Changelog: changelogRaw,
	}))
}
