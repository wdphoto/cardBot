package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNoArgSubcommand_RejectsExtraArgs(t *testing.T) {
	t.Parallel()

	err := executeTestRoot("install-daemon", "extra")
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Execute() error = %T, want commandError", err)
	}
	if cmdErr.code != 2 {
		t.Fatalf("command error code = %d, want 2", cmdErr.code)
	}
}

func TestLooksLikeCommandToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{name: "unknown command token", arg: "daemon-statuz", want: true},
		{name: "flag", arg: "--setup", want: false},
		{name: "absolute path", arg: "/Volumes/NIKON", want: false},
		{name: "relative path", arg: "./card", want: false},
		{name: "home path", arg: "~/card", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeCommandToken(tt.arg); got != tt.want {
				t.Fatalf("looksLikeCommandToken(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestLooksLikeCommandToken_ExistingRelativePath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	temp := t.TempDir()
	if err := os.Chdir(temp); err != nil {
		t.Fatalf("Chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	name := "card"
	if err := os.Mkdir(filepath.Join(temp, name), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if got := looksLikeCommandToken(name); got {
		t.Fatalf("looksLikeCommandToken(%q) = true, want false for existing path", name)
	}
}

func TestRootCommand_UnknownCommand(t *testing.T) {
	t.Parallel()

	err := executeTestRoot("daemon-statuz")
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Execute() error = %T, want commandError", err)
	}
	if cmdErr.code != 2 {
		t.Fatalf("command error code = %d, want 2", cmdErr.code)
	}
}

func executeTestRoot(args ...string) error {
	cmd := NewRootCommand(BuildInfo{Version: "0.0.10", Commit: "none", Date: "unknown"})
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}
