package dotfile

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func FuzzIngestHistory(f *testing.F) {
	for _, seed := range []string{
		``, `null`, `[]`, `{"$schema":"unknown"}`,
		`{"$schema":"cardbot-dotfile-v1","last_copied":"2026-09-08T01:00:00-07:00","mode":"all"}`,
		`{"$schema":"cardbot-dotfile-v2","copies":[{"timestamp":"invalid"},{"timestamp":"2026-09-08T01:00:00Z","mode":"today"}]}`,
		`{"$schema":"cardbot-dotfile-v2","copies":[{"timestamp":"0001-01-01T00:00:00Z"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16384 {
			t.Skip()
		}
		original := bytes.Clone(data)
		status := parseStatus(data)
		if !bytes.Equal(original, data) {
			t.Fatal("parser mutated input")
		}
		if again := parseStatus(data); !reflect.DeepEqual(status, again) {
			t.Fatal("unstable history parsing")
		}
		valid := false
		for _, entry := range status.Entries {
			parsed, err := time.Parse(time.RFC3339, entry.RawTimestamp)
			if err == nil {
				valid = true
				if !parsed.Equal(entry.Timestamp) {
					t.Fatal("timestamp was not preserved")
				}
			} else if !entry.Timestamp.IsZero() {
				t.Fatal("invalid timestamp acquired a date")
			}
		}
		if status.Copied != valid {
			t.Fatal("history validity disagrees with timestamps")
		}
		if got := FormatStatus(status); got == "" || got != FormatStatus(status) {
			t.Fatal("empty or unstable history display")
		}
	})
}
