// Package dotfile reads and writes .cardbot files on memory cards.
package dotfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	fileName = ".cardbot"
	schemaV1 = "cardbot-dotfile-v1"
	schemaV2 = "cardbot-dotfile-v2"
)

// CopyEntry tracks a single copy mode operation.
type CopyEntry struct {
	Mode               string    `json:"mode"`
	Timestamp          time.Time `json:"-"` // Parsed from json string
	RawTimestamp       string    `json:"timestamp"`
	Destination        string    `json:"destination"`
	FilesCopied        int       `json:"files_copied,omitempty"`
	BytesCopied        int64     `json:"bytes_copied,omitempty"`
	Verified           bool      `json:"verified,omitempty"`
	VerificationMethod string    `json:"verification_method,omitempty"`
	CardbotVersion     string    `json:"cardbot_version,omitempty"`
}

// Status represents the parsed copy state of a card.
type Status struct {
	Copied  bool
	Entries []CopyEntry
}

// dotfileSchemaV1 is the legacy v1 single-entry JSON structure.
type dotfileSchemaV1 struct {
	Schema         string `json:"$schema"`
	LastCopied     string `json:"last_copied"`
	Mode           string `json:"mode"`
	Destination    string `json:"destination"`
	FilesCopied    int    `json:"files_copied,omitempty"`
	BytesCopied    int64  `json:"bytes_copied,omitempty"`
	Verified       bool   `json:"verified,omitempty"`
	CardbotVersion string `json:"cardbot_version,omitempty"`
}

// dotfileSchemaV2 is the v2 array-based JSON structure.
type dotfileSchemaV2 struct {
	Schema string      `json:"$schema"`
	Copies []CopyEntry `json:"copies"`
}

// Read checks for a .cardbot file on the card and returns its status.
// Returns no recorded history (Copied=false) if the file doesn't exist or can't be parsed.
// Automatically migrates v1 schemas to a single-entry v2 representation in memory.
func Read(cardPath string) Status {
	data, err := os.ReadFile(filepath.Join(cardPath, fileName))
	if err != nil {
		return Status{}
	}

	// Try reading schema type first
	var probe struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Status{}
	}

	status := Status{}

	if probe.Schema == schemaV1 {
		var v1 dotfileSchemaV1
		if err := json.Unmarshal(data, &v1); err != nil {
			return Status{}
		}
		t, err := time.Parse(time.RFC3339, v1.LastCopied)
		if err != nil {
			return Status{}
		}
		status.Copied = true
		status.Entries = []CopyEntry{{
			Mode:           v1.Mode,
			Timestamp:      t,
			RawTimestamp:   v1.LastCopied,
			Destination:    v1.Destination,
			FilesCopied:    v1.FilesCopied,
			BytesCopied:    v1.BytesCopied,
			Verified:       v1.Verified,
			CardbotVersion: v1.CardbotVersion,
		}}
		return status
	}

	if probe.Schema == schemaV2 {
		var v2 dotfileSchemaV2
		if err := json.Unmarshal(data, &v2); err != nil {
			return Status{}
		}
		for i := range v2.Copies {
			t, err := time.Parse(time.RFC3339, v2.Copies[i].RawTimestamp)
			if err != nil {
				continue // skip invalid timestamps but try to parse others
			}
			v2.Copies[i].Timestamp = t
			status.Copied = true
		}
		status.Entries = v2.Copies
		return status
	}

	// Unknown schema version
	return Status{}
}

// readExistingSchema returns the schema string for an existing .cardbot file.
// Missing or malformed files are treated as empty schema.
func readExistingSchema(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var probe struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		// Malformed JSON is treated as "new" status and may be replaced.
		return "", nil
	}
	return probe.Schema, nil
}

// WriteOptions configures what gets written to the dotfile.
type WriteOptions struct {
	CardPath           string
	Destination        string
	Mode               string // "all", "photos", "videos", "selects"
	FilesCopied        int
	BytesCopied        int64
	Verified           bool
	VerificationMethod string // "size" or "full"
	CardbotVersion     string
}

// Write creates or overwrites the .cardbot dotfile on the card.
// Uses atomic write (temp file + rename) to avoid corruption.
// Preserves existing v2 entries for other modes.
func Write(opts WriteOptions) error {
	target := filepath.Join(opts.CardPath, fileName)
	schema, err := readExistingSchema(target)
	if err != nil {
		return err
	}
	if schema != "" && schema != schemaV1 && schema != schemaV2 {
		return fmt.Errorf("unknown .cardbot schema %q: refusing to overwrite", schema)
	}

	status := Read(opts.CardPath)

	now := time.Now()
	newEntry := CopyEntry{
		Mode:               opts.Mode,
		Timestamp:          now,
		RawTimestamp:       now.Format(time.RFC3339),
		Destination:        opts.Destination,
		FilesCopied:        opts.FilesCopied,
		BytesCopied:        opts.BytesCopied,
		Verified:           opts.Verified,
		VerificationMethod: opts.VerificationMethod,
		CardbotVersion:     opts.CardbotVersion,
	}

	var copies []CopyEntry
	replaced := false
	for _, e := range status.Entries {
		if e.Mode == opts.Mode {
			copies = append(copies, newEntry) // upsert
			replaced = true
		} else {
			copies = append(copies, e)
		}
	}
	if !replaced {
		copies = append(copies, newEntry) // append
	}

	df := dotfileSchemaV2{
		Schema: schemaV2,
		Copies: copies,
	}

	data, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := target + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmp, target)
}

// FormatStatus describes the most recent valid ingest record, not whether the
// card's current contents are backed up. Modes from different ingests must not
// be combined under one timestamp.
func FormatStatus(s Status) string {
	var latest CopyEntry
	if s.Copied {
		for _, e := range s.Entries {
			if e.Timestamp.After(latest.Timestamp) {
				latest = e
			}
		}
	}
	if latest.Timestamp.IsZero() {
		return "No recorded ingest"
	}
	label := formatModeLabel(latest.Mode)
	if label == "" {
		label = "Unspecified selection"
	}
	return "Last recorded ingest: " + latest.Timestamp.Format(time.RFC3339) + " — " + label
}

// formatModeLabel normalizes a copy mode key for user-facing display.
func formatModeLabel(mode string) string {
	switch mode {
	case "all":
		return "All files"
	case "selects":
		return "Starred files"
	case "today":
		return "Photos from ingest day"
	case "yesterday":
		return "Photos from day before ingest"
	}
	if mode == "" {
		return ""
	}
	r := []rune(mode)
	if len(r) == 0 {
		return ""
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
