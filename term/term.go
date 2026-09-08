// Package term provides shared terminal formatting helpers used across
// the cardBot CLI: timestamps, ANSI colors, and user-facing error messages.
package term

import (
	"strconv"
	"strings"
	"time"
)

// FormatCount groups decimal digits with commas for human-readable counts.
func FormatCount(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	if strings.HasPrefix(s, "-") {
		b.WriteByte('-')
		s = s[1:]
	}
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Ts returns the current timestamp formatted for log output.
func Ts() string {
	return time.Now().Format("2006-01-02T15:04:05")
}

// DimTS returns a dimmed ANSI-bracketed timestamp string.
func DimTS(ts string) string {
	return "\033[2m[" + ts + "]\033[0m"
}

// FriendlyErr returns a short, user-facing message for common OS-level errors.
func FriendlyErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no space left"):
		return "destination disk is full"
	case strings.Contains(s, "permission denied"):
		return "permission denied — check folder permissions"
	case strings.Contains(s, "read-only file system"):
		return "destination is read-only"
	case strings.Contains(s, "input/output error"):
		return "I/O error — card may be damaged"
	default:
		return s
	}
}

// BrandColor returns an ANSI color code for the given camera brand.
// Falls back to white for unknown brands.
func BrandColor(brand string) string {
	switch brand {
	case "Nikon":
		return "\033[33m" // Yellow
	case "Canon":
		return "\033[31m" // Red
	case "Sony":
		return "\033[37m" // White
	case "Fujifilm":
		return "\033[32m" // Green
	case "Panasonic":
		return "\033[34m" // Blue
	case "Olympus", "OM System":
		return "\033[36m" // Cyan
	default:
		return "\033[37m" // White
	}
}

// Reset is the ANSI reset code.
const Reset = "\033[0m"
