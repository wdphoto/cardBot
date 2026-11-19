// Package dotfile reads and writes .cardbot, the ingest history kept on the
// card itself. Each mode has a receipt, not a manifest of backed-up files;
// displaying a receipt does not inspect the destination.
package dotfile

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wdphoto/cardBot/fsutil"
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
	source, err := fsutil.IdentifySource(cardPath)
	if err != nil {
		return Status{}
	}
	return ReadSource(source)
}

// ReadSource reads history only from the card that was acquired.
func ReadSource(source *fsutil.Source) Status {
	root, err := source.Open()
	if err != nil {
		return Status{}
	}
	defer root.Close()
	data, err := root.ReadFile(fileName)
	if err != nil {
		return Status{}
	}
	return parseStatus(data)
}

// parseStatus migrates history in memory only; parsing never writes to the card.
func parseStatus(data []byte) Status {
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

// WriteOptions configures what gets written to the dotfile.
type WriteOptions struct {
	Source             *fsutil.Source // Optional identity of the card that was copied.
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
	source := opts.Source
	if source == nil {
		var err error
		source, err = fsutil.IdentifySource(opts.CardPath)
		if err != nil {
			return err
		}
	}
	root, err := source.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	old, err := root.ReadFile(fileName)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var probe struct {
		Schema string `json:"$schema"`
	}
	if json.Unmarshal(old, &probe) == nil && probe.Schema != "" && probe.Schema != schemaV1 && probe.Schema != schemaV2 {
		return fmt.Errorf("unknown .cardbot schema %q: refusing to overwrite", probe.Schema)
	}
	status := parseStatus(old)

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

	// An exclusive, unique temporary must never follow or truncate an existing
	// .cardbot.tmp file/symlink. Clean up only the file created by this call.
	tmpName := ".cardbot-" + rand.Text() + ".tmp"
	tmp, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = root.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0644); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := source.Check(); err != nil {
		return err
	}
	if err := root.Rename(tmpName, fileName); err != nil {
		return err
	}
	committed = true
	return nil
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
