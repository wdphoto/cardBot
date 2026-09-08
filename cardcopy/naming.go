package cardcopy

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// SequenceDigits is the minimum sequence padding width. Larger sequences grow
// naturally (9999, 10000, ...); existing four-digit names never change width.
const SequenceDigits = 4

func formatSequence(n, digits int) string {
	// Sequence is 1-based. Padding is a minimum, never a limit or rollover.
	if n < 1 {
		n = 1
	}
	if digits < 3 {
		digits = 3
	}
	if digits > 5 {
		digits = 5
	}
	return fmt.Sprintf("%0*d", digits, n)
}

func timestampStem(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.Format("060102T150405")
}

func renamedRelativePath(relPath string, captureTime time.Time, seq, digits int) string {
	dir := filepath.Dir(relPath)
	ext := strings.ToUpper(filepath.Ext(relPath))
	name := timestampStem(captureTime) + "_" + formatSequence(seq, digits) + ext
	if dir == "." || dir == "" {
		return name
	}
	return filepath.Join(dir, name)
}

// isTimestampMode returns whether the naming mode string means timestamp renaming.
func isTimestampMode(mode string) bool {
	return strings.ToLower(strings.TrimSpace(mode)) == "timestamp"
}
