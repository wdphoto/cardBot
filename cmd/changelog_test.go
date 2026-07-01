package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseChangelogSection(t *testing.T) {
	t.Parallel()

	raw := `# cardBot Changelog

## 0.9.0

- Feature A
- Feature B

## 0.8.0

- Gear display
- Copy today/yesterday
- Timestamp cleanup

## 0.7.0

- Structural refactor
`

	tests := []struct {
		name    string
		version string
		want    []string
	}{
		{"current version", "0.9.0", []string{"Feature A", "Feature B"}},
		{"middle version", "0.8.0", []string{"Gear display", "Copy today/yesterday", "Timestamp cleanup"}},
		{"oldest version", "0.7.0", []string{"Structural refactor"}},
		{"unknown version", "0.6.0", nil},
		{"empty version", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseChangelogSection(raw, tt.version)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d bullets %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("bullet %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseChangelogSection_EmptyRaw(t *testing.T) {
	t.Parallel()
	got := parseChangelogSection("", "0.8.0")
	if len(got) != 0 {
		t.Fatalf("expected no bullets, got %v", got)
	}
}

func TestParseChangelogSection_SkipsNonBullets(t *testing.T) {
	t.Parallel()
	raw := `## 0.8.0

- Real bullet
Some paragraph text
  indented stuff
- Another bullet
`
	got := parseChangelogSection(raw, "0.8.0")
	want := []string{"Real bullet", "Another bullet"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bullet %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFprintChangelog(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	fprintChangelog(&buf, []string{"Feature A", "Feature B"})

	out := buf.String()
	if !strings.Contains(out, "What's new") {
		t.Fatalf("missing header\n%s", out)
	}
	if !strings.Contains(out, "· Feature A") {
		t.Fatalf("missing bullet A\n%s", out)
	}
	if !strings.Contains(out, "· Feature B") {
		t.Fatalf("missing bullet B\n%s", out)
	}
}

func TestFprintChangelog_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	fprintChangelog(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty bullets, got %q", buf.String())
	}
}
