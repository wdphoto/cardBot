package app

import (
	"path/filepath"
	"strings"
)

func normalizeCardPath(path string) string {
	if path == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return abs
}

func sameCardPath(a, b string) bool {
	a = normalizeCardPath(a)
	b = normalizeCardPath(b)
	if a == "" || b == "" {
		return false
	}
	return a == b
}
