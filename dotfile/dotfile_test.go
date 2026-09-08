package dotfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteRead_RoundTrip(t *testing.T) {
	t.Parallel()
	card := t.TempDir()

	err := Write(WriteOptions{
		CardPath:       card,
		Destination:    "/Users/test/Pictures/cardBot",
		Mode:           "all",
		FilesCopied:    150,
		BytesCopied:    5000000,
		Verified:       true,
		CardbotVersion: "0.1.9",
	})
	if err != nil {
		t.Fatal(err)
	}

	status := Read(card)
	if !status.Copied {
		t.Error("expected Copied=true")
	}
	if len(status.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(status.Entries))
	}
	entry := status.Entries[0]
	if entry.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
	if entry.Destination != "/Users/test/Pictures/cardBot" {
		t.Errorf("Destination = %q, want /Users/test/Pictures/cardBot", entry.Destination)
	}
	if entry.Mode != "all" {
		t.Errorf("Mode = %q, want all", entry.Mode)
	}
}

func TestRead_MissingFile(t *testing.T) {
	t.Parallel()
	status := Read(t.TempDir())
	if status.Copied {
		t.Error("expected Copied=false for missing dotfile")
	}
}

func TestRead_MalformedJSON(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	if err := os.WriteFile(filepath.Join(card, ".cardbot"), []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}

	status := Read(card)
	if status.Copied {
		t.Error("expected Copied=false for malformed JSON")
	}
}

func TestRead_V1Migration(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	v1JSON := `{
		"$schema": "cardbot-dotfile-v1",
		"last_copied": "2026-03-12T12:00:00Z",
		"mode": "all",
		"destination": "/dest"
	}`
	if err := os.WriteFile(filepath.Join(card, ".cardbot"), []byte(v1JSON), 0644); err != nil {
		t.Fatal(err)
	}

	status := Read(card)
	if !status.Copied {
		t.Fatal("expected Copied=true for v1 schema")
	}
	if len(status.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(status.Entries))
	}
	e := status.Entries[0]
	if e.Mode != "all" {
		t.Errorf("expected mode 'all', got %q", e.Mode)
	}
	if e.Destination != "/dest" {
		t.Errorf("expected destination '/dest', got %q", e.Destination)
	}
	if e.Timestamp.Format(time.RFC3339) != "2026-03-12T12:00:00Z" {
		t.Errorf("expected parsed timestamp, got %v", e.Timestamp)
	}
}

func TestWrite_Upsert(t *testing.T) {
	t.Parallel()
	card := t.TempDir()

	// First write: photos
	if err := Write(WriteOptions{CardPath: card, Destination: "/dest1", Mode: "photos", FilesCopied: 10}); err != nil {
		t.Fatal(err)
	}

	// Second write: videos (should append)
	if err := Write(WriteOptions{CardPath: card, Destination: "/dest2", Mode: "videos", FilesCopied: 5}); err != nil {
		t.Fatal(err)
	}

	status := Read(card)
	if len(status.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(status.Entries))
	}

	// Third write: photos again (should replace existing photos entry)
	if err := Write(WriteOptions{CardPath: card, Destination: "/dest3", Mode: "photos", FilesCopied: 20}); err != nil {
		t.Fatal(err)
	}

	status = Read(card)
	if len(status.Entries) != 2 {
		t.Fatalf("expected 2 entries after upsert, got %d", len(status.Entries))
	}

	for _, e := range status.Entries {
		if e.Mode == "photos" && e.FilesCopied != 20 {
			t.Errorf("photos entry not updated, got %d files", e.FilesCopied)
		}
		if e.Mode == "photos" && e.Destination != "/dest3" {
			t.Errorf("photos destination not updated, got %s", e.Destination)
		}
		if e.Mode == "videos" && e.FilesCopied != 5 {
			t.Errorf("videos entry mutated unexpectedly")
		}
	}
}

func TestWrite_SchemaV2(t *testing.T) {
	t.Parallel()
	card := t.TempDir()

	if err := Write(WriteOptions{CardPath: card, Destination: "/dest", Mode: "selects"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(card, ".cardbot"))
	if err != nil {
		t.Fatal(err)
	}

	var df dotfileSchemaV2
	if err := json.Unmarshal(data, &df); err != nil {
		t.Fatal(err)
	}

	if df.Schema != "cardbot-dotfile-v2" {
		t.Errorf("Schema = %q, want cardbot-dotfile-v2", df.Schema)
	}
	if len(df.Copies) != 1 {
		t.Fatalf("expected 1 copy entry in JSON, got %d", len(df.Copies))
	}
	if df.Copies[0].Mode != "selects" {
		t.Errorf("Mode = %q, want selects", df.Copies[0].Mode)
	}
}

func TestWrite_UnknownSchemaDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	path := filepath.Join(card, ".cardbot")
	original := []byte(`{"$schema":"cardbot-dotfile-v99","copies":[]}`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	err := Write(WriteOptions{CardPath: card, Destination: "/dest", Mode: "all"})
	if err == nil {
		t.Fatal("expected write to fail for unknown schema")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("dotfile was modified despite unknown schema")
	}
}

func TestWrite_MalformedExistingDotfileIsReplaced(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	path := filepath.Join(card, ".cardbot")
	if err := os.WriteFile(path, []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Write(WriteOptions{CardPath: card, Destination: "/dest", Mode: "photos"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var df dotfileSchemaV2
	if err := json.Unmarshal(data, &df); err != nil {
		t.Fatalf("expected malformed file to be replaced with valid v2 JSON: %v", err)
	}
	if df.Schema != "cardbot-dotfile-v2" {
		t.Fatalf("Schema = %q, want cardbot-dotfile-v2", df.Schema)
	}
}

func TestFormatModeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want string
	}{
		{"photos", "Photos"},
		{"videos", "Videos"},
		{"all", "All files"},
		{"selects", "Starred files"},
		{"today", "Photos from ingest day"},
		{"yesterday", "Photos from day before ingest"},
		{"étoiles", "Étoiles"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := formatModeLabel(tt.mode); got != tt.want {
			t.Errorf("formatModeLabel(%q) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestFormatStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   Status
		expected string
	}{
		{"no history", Status{}, "No recorded ingest"},
		{
			"all only",
			Status{
				Copied:  true,
				Entries: []CopyEntry{{Mode: "all", Timestamp: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)}},
			},
			"Last recorded ingest: 2026-03-12T12:00:00Z — All files",
		},
		{
			"single selective",
			Status{
				Copied:  true,
				Entries: []CopyEntry{{Mode: "photos", Timestamp: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)}},
			},
			"Last recorded ingest: 2026-03-12T12:00:00Z — Photos",
		},
		{
			"multiple selective",
			Status{
				Copied: true,
				Entries: []CopyEntry{
					{Mode: "photos", Timestamp: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)},
					{Mode: "videos", Timestamp: time.Date(2026, 3, 12, 14, 0, 0, 0, time.UTC)},
				},
			},
			"Last recorded ingest: 2026-03-12T14:00:00Z — Videos",
		},
		{
			"empty mode ignored",
			Status{
				Copied:  true,
				Entries: []CopyEntry{{Mode: "", Timestamp: time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC)}},
			},
			"Last recorded ingest: 2026-03-12T15:00:00Z — Unspecified selection",
		},
		{
			"latest all",
			Status{
				Copied: true,
				Entries: []CopyEntry{
					{Mode: "selects", Timestamp: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)},
					{Mode: "all", Timestamp: time.Date(2026, 3, 12, 16, 0, 0, 0, time.UTC)},
				},
			},
			"Last recorded ingest: 2026-03-12T16:00:00Z — All files",
		},
		{
			"old all must not cover newer selective ingest",
			Status{Copied: true, Entries: []CopyEntry{
				{Mode: "photos", Timestamp: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)},
				{Mode: "all", Timestamp: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)},
			}},
			"Last recorded ingest: 2026-09-06T12:00:00Z — Photos",
		},
		{
			"historical today preserves timezone",
			Status{Copied: true, Entries: []CopyEntry{
				{Mode: "today", Timestamp: time.Date(2026, 4, 9, 19, 40, 20, 0, time.FixedZone("PDT", -7*60*60))},
			}},
			"Last recorded ingest: 2026-04-09T19:40:20-07:00 — Photos from ingest day",
		},
		{
			"historical yesterday",
			Status{Copied: true, Entries: []CopyEntry{
				{Mode: "yesterday", Timestamp: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)},
			}},
			"Last recorded ingest: 2026-04-09T12:00:00Z — Photos from day before ingest",
		},
		{
			"invalid timestamp cannot broaden valid record",
			Status{Copied: true, Entries: []CopyEntry{
				{Mode: "photos", Timestamp: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)},
				{Mode: "all"},
			}},
			"Last recorded ingest: 2026-04-09T12:00:00Z — Photos",
		},
		{"no valid entries", Status{Copied: true, Entries: []CopyEntry{{Mode: "all"}}}, "No recorded ingest"},
		{"empty history", Status{Copied: true}, "No recorded ingest"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := FormatStatus(tc.status)
			if s != tc.expected {
				t.Errorf("FormatStatus() = %q, want %q", s, tc.expected)
			}
		})
	}
}
