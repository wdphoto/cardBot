package cmd

import "strings"

func boolEnabled(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
