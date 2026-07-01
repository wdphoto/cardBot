package cmd

import (
	"fmt"
	"io"
	"strings"
)

// parseChangelogSection extracts bullet points for a specific version from
// a CHANGELOG.md string. Looks for a "## <version>" header and collects
// lines starting with "- " until the next "##" header or EOF.
func parseChangelogSection(raw, version string) []string {
	lines := strings.Split(raw, "\n")
	header := "## " + version

	var bullets []string
	inSection := false

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break // hit next version section
			}
			if strings.TrimSpace(line) == header {
				inSection = true
			}
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			bullets = append(bullets, strings.TrimPrefix(trimmed, "- "))
		}
	}

	return bullets
}

// fprintChangelog renders changelog bullets in a left-aligned block.
func fprintChangelog(w io.Writer, bullets []string) {
	if len(bullets) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  What's new")
	for _, b := range bullets {
		fmt.Fprintf(w, "  · %s\n", b)
	}
	fmt.Fprintln(w)
}
