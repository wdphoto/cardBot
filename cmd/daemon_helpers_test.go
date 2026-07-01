package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDaemonTerminalAppLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"default", "Default (macOS)"},
		{"Default", "Default (macOS)"},
		{"  default  ", "Default (macOS)"},
		{"Terminal", "Terminal"},
		{"Ghostty", "Ghostty"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := daemonTerminalAppLabel(tt.in); got != tt.want {
			t.Errorf("daemonTerminalAppLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveDaemonWorkingDirectory(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to home", "", home},
		{"whitespace defaults to home", "   ", home},
		{"tilde expands to home", "~", home},
		{"absolute path passes through", "/tmp/test", "/tmp/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDaemonWorkingDirectory(tt.in)
			if got != tt.want {
				t.Errorf("resolveDaemonWorkingDirectory(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDaemonWorkingDirectoryLabel(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		name        string
		configValue string
		resolved    string
		want        string
	}{
		{"empty config shows tilde", "", home, "~"},
		{"tilde contracts cleanly", "~", home, "~"},
		{"absolute path differs", "~/Photos", home + "/Photos", "~/Photos"},
		{"non-contractable path", "/opt/data", "/opt/data", "/opt/data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := daemonWorkingDirectoryLabel(tt.configValue, tt.resolved)
			if got != tt.want {
				t.Errorf("daemonWorkingDirectoryLabel(%q, %q) = %q, want %q",
					tt.configValue, tt.resolved, got, tt.want)
			}
		})
	}
}

func TestDaemonLaunchHint_Automation(t *testing.T) {
	t.Parallel()

	tests := []string{
		"not authorized to send Apple events to Terminal",
		"not authorised to send apple events",
		"execution error: Not authorized to send Apple events to Terminal. (-1743)",
		"Error Domain=NSOSStatusErrorDomain Code=-1743",
		"automation permission denied",
		"errAEEventNotPermitted",
	}

	for _, msg := range tests {
		t.Run(fmt.Sprintf("msg_%q", msg), func(t *testing.T) {
			hint := daemonLaunchHint(errors.New(msg))
			if !strings.Contains(strings.ToLower(hint), "automation") {
				t.Fatalf("hint = %q, expected automation guidance for msg=%q", hint, msg)
			}
		})
	}
}

func TestDaemonLaunchHint_FullDiskAccess(t *testing.T) {
	t.Parallel()

	tests := []string{
		"operation not permitted",
		"permission denied",
		"open: EPERM",
	}

	for _, msg := range tests {
		t.Run(fmt.Sprintf("msg_%q", msg), func(t *testing.T) {
			hint := daemonLaunchHint(errors.New(msg))
			if !strings.Contains(strings.ToLower(hint), "full disk access") {
				t.Fatalf("hint = %q, expected full disk access guidance for msg=%q", hint, msg)
			}
		})
	}
}

func TestDaemonLaunchHint_Unknown(t *testing.T) {
	t.Parallel()

	hint := daemonLaunchHint(errors.New("random error"))
	if hint != "" {
		t.Fatalf("hint = %q, want empty", hint)
	}
}

func TestNormalizeDaemonTerminalAppForLaunch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", "Terminal"},
		{"   ", "Terminal"},
		{"default", "Default"},
		{"macos default", "Default"},
		{"system default", "Default"},
		{"terminal.app", "Terminal"},
		{"terminal", "Terminal"},
		{"ghostty", "Ghostty"},
		{"WezTerm", "WezTerm"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("in_%q", tt.in), func(t *testing.T) {
			got := normalizeDaemonTerminalAppForLaunch(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeDaemonTerminalAppForLaunch(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
