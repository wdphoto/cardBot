package detect

import (
	"path/filepath"
	"strings"
)

// unescapeMountField decodes the standard octal escapes the kernel uses in
// /proc/mounts mount paths: \040 (space), \011 (tab), \012 (newline), and
// \134 (backslash). It is a single left-to-right pass; unknown or malformed
// escapes are left unchanged. A literal backslash is itself escaped (\134),
// so a literal "\040" appears as "\134040" and decodes back to "\040".
func unescapeMountField(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	return strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	).Replace(s)
}

// parseMountLine splits one /proc/mounts line into its device and (unescaped)
// mount path. It returns ok=false for lines without at least two fields.
func parseMountLine(line string) (device, path string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], unescapeMountField(fields[1]), true
}

// volumeUUIDMatches reports whether a /dev/disk/by-uuid symlink target refers
// to the requested device by its conventional basename (e.g. "../../sda1").
// It is not a general source canonicalization.
func volumeUUIDMatches(target, device string) bool {
	return filepath.Base(target) == device
}
