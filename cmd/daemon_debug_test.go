package cmd

import "testing"

func TestParseDaemonDebugMode_DefaultStatus(t *testing.T) {
	t.Parallel()

	mode, err := parseDaemonDebugMode(nil)
	if err != nil {
		t.Fatalf("parseDaemonDebugMode error: %v", err)
	}
	if mode != daemonDebugStatus {
		t.Fatalf("mode = %q, want %q", mode, daemonDebugStatus)
	}
}

func TestParseDaemonDebugMode_OnOffStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want daemonDebugMode
	}{
		{name: "status", args: []string{"status"}, want: daemonDebugStatus},
		{name: "on", args: []string{"on"}, want: daemonDebugOn},
		{name: "enabled", args: []string{"enabled"}, want: daemonDebugOn},
		{name: "true", args: []string{"true"}, want: daemonDebugOn},
		{name: "off", args: []string{"off"}, want: daemonDebugOff},
		{name: "disabled", args: []string{"disabled"}, want: daemonDebugOff},
		{name: "false", args: []string{"false"}, want: daemonDebugOff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := parseDaemonDebugMode(tt.args)
			if err != nil {
				t.Fatalf("parseDaemonDebugMode(%v) error: %v", tt.args, err)
			}
			if mode != tt.want {
				t.Fatalf("mode = %q, want %q", mode, tt.want)
			}
		})
	}
}

func TestParseDaemonDebugMode_Invalid(t *testing.T) {
	t.Parallel()

	if _, err := parseDaemonDebugMode([]string{"wat"}); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestParseDaemonDebugMode_TooManyArgs(t *testing.T) {
	t.Parallel()

	if _, err := parseDaemonDebugMode([]string{"on", "extra"}); err == nil {
		t.Fatal("expected error for extra args")
	}
}
