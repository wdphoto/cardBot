package launch

import (
	"os"
	"strings"
	"testing"
)

type recordedCommand struct {
	name string
	args []string
}

func TestOpenWith_SystemDefault_UsesOpenWithCommandScript(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Default",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/NIKON Z 9",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if len(got.args) != 1 {
		t.Fatalf("args = %v, want exactly one script path", got.args)
	}
	scriptPath := got.args[0]
	t.Cleanup(func() { _ = os.Remove(scriptPath) })
	if !strings.HasSuffix(scriptPath, ".command") {
		t.Fatalf("script path = %q, want .command suffix", scriptPath)
	}
	scriptBytes, readErr := os.ReadFile(scriptPath)
	if readErr != nil {
		t.Fatalf("read script %q: %v", scriptPath, readErr)
	}
	script := string(scriptBytes)
	if !strings.Contains(script, "/usr/local/bin/cardbot") {
		t.Fatalf("script missing cardbot path: %s", script)
	}
	if !strings.Contains(script, "/Volumes/NIKON Z 9") {
		t.Fatalf("script missing mount path: %s", script)
	}
}

func TestOpenWith_TerminalApp_UsesAppleScript(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Terminal",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/NIKON Z 9",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "osascript" {
		t.Fatalf("command name = %q, want %q", got.name, "osascript")
	}
	joined := strings.Join(got.args, " ")
	if !strings.Contains(joined, `tell application "Terminal"`) {
		t.Fatalf("args missing Terminal tell block: %v", got.args)
	}
	if !strings.Contains(joined, "do script") {
		t.Fatalf("args missing do script command: %v", got.args)
	}
	if !strings.Contains(joined, "in front window") {
		t.Fatalf("args missing 'in front window': %v", got.args)
	}
}

func TestOpenWith_GhosttyDefault_UsesOpenWithE(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/CARD",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if len(got.args) != 7 {
		t.Fatalf("args = %v, want 7 args", got.args)
	}
	if got.args[0] != "-a" || got.args[1] != "Ghostty" {
		t.Fatalf("args = %v, want '-a Ghostty ...'", got.args)
	}
	if got.args[2] != "--args" || !strings.HasPrefix(got.args[3], "--working-directory=") || got.args[4] != "-e" {
		t.Fatalf("args = %v, want '--args --working-directory=<home> -e ...'", got.args)
	}
	if got.args[5] != "/usr/local/bin/cardbot" || got.args[6] != encodeTargetPathArg("/Volumes/CARD") {
		t.Fatalf("args = %v, want binary + encoded target-path arg", got.args)
	}
}

func TestOpenWith_GhosttyDefault_PreservesTrailingSpacesInMountPath(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	mount := "/Volumes/NIKON Z 9  "
	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     mount,
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.args[6] != encodeTargetPathArg(mount) {
		t.Fatalf("target arg = %q, want %q", got.args[6], encodeTargetPathArg(mount))
	}
}

func TestOpenWith_GhosttyDefault_UsesConfiguredWorkingDirectory(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:      "Ghostty",
		WorkingDirectory: "/Users/illwill/Code",
		CardBotBinary:    "/usr/local/bin/cardbot",
		MountPath:        "/Volumes/CARD",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.args[3] != "--working-directory=/Users/illwill/Code" {
		t.Fatalf("args = %v, expected configured working directory arg", got.args)
	}
}

func TestOpenWith_GenericApp_PreservesBinaryEdgeSpace(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "MyCustomTerm.app",
		CardBotBinary: "/pfx/cardbot ",
		MountPath:     "/Volumes/NIKON",
	}, run)
	if err != nil {
		t.Fatalf("openWith error: %v", err)
	}
	// generic branch: run("open", "-a", app, "--args", binary, mountPath);
	// recordedCommand.args excludes the command name, so binary is at index 3.
	if got.args[3] != "/pfx/cardbot " {
		t.Fatalf("binary arg = %q, want preserved %q", got.args[3], "/pfx/cardbot ")
	}
	if got.args[4] != "/Volumes/NIKON" {
		t.Fatalf("mount arg = %q, want %q", got.args[4], "/Volumes/NIKON")
	}
}

func TestOpenWith_GenericApp_PreservesQuotedBinaryInsideEdgeSpace(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	// Fully quoted binary with a significant inside trailing space.
	err := openWith(Options{
		TerminalApp:   "MyCustomTerm.app",
		CardBotBinary: `"/pfx/cardbot "`,
		MountPath:     "/Volumes/NIKON",
	}, run)
	if err != nil {
		t.Fatalf("openWith error: %v", err)
	}
	if got.args[3] != "/pfx/cardbot " {
		t.Fatalf("binary arg = %q, want preserved %q", got.args[3], "/pfx/cardbot ")
	}
}

func TestOpenWith_WhitespaceOnlyBinaryRejected(t *testing.T) {
	run := func(name string, args ...string) error { return nil }
	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "   ",
		MountPath:     "/Volumes/NIKON",
	}, run)
	if err == nil {
		t.Fatal("expected error for whitespace-only binary")
	}
}

func TestOpenWith_Ghostty_PreservesWorkingDirEdgeSpace(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:      "Ghostty",
		WorkingDirectory: "/dest/Client ",
		CardBotBinary:    "/usr/local/bin/cardbot",
		MountPath:        "/Volumes/CARD",
	}, run)
	if err != nil {
		t.Fatalf("openWith error: %v", err)
	}
	if got.args[3] != "--working-directory=/dest/Client " {
		t.Fatalf("args = %v, want preserved working-directory arg", got.args)
	}
}

func TestOpenWith_CustomLaunchArgs_TemplatesResolved(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "/opt/cardbot",
		MountPath:     "/Volumes/NIKON Z 9",
		LaunchArgs: []string{
			"-e",
			"{{cardbot_binary}} {{mount_path}}",
		},
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if len(got.args) != 7 {
		t.Fatalf("args = %v, want 7 args", got.args)
	}
	if got.args[0] != "-a" || got.args[1] != "Ghostty" {
		t.Fatalf("args = %v, want '-a Ghostty ...'", got.args)
	}
	if !strings.HasPrefix(got.args[3], "--working-directory=") || got.args[4] != "-e" || got.args[5] != "/opt/cardbot" || got.args[6] != "/Volumes/NIKON Z 9" {
		t.Fatalf("args = %v, want '--working-directory=<home> -e /opt/cardbot /Volumes/NIKON Z 9'", got.args)
	}
}

func TestOpenWith_GhosttyCustomLaunchArgs_StripsQuotedPlaceholders(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/NIKON Z 9",
		LaunchArgs: []string{
			"-e",
			"'{{cardbot_binary}}'",
			"'{{mount_path}}'",
		},
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if got.args[5] != "/usr/local/bin/cardbot" || got.args[6] != "/Volumes/NIKON Z 9" {
		t.Fatalf("args = %v, expected unquoted binary and mount path", got.args)
	}
}

func TestOpenWith_GhosttyCustomLaunchArgs_LegacyCombinedCommandIsSplit(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/NIKON Z 9",
		LaunchArgs: []string{
			"-e",
			"'{{cardbot_binary}}' '{{mount_path}}'",
		},
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if len(got.args) != 7 {
		t.Fatalf("args = %v, want 7 args", got.args)
	}
	if !strings.HasPrefix(got.args[3], "--working-directory=") || got.args[4] != "-e" || got.args[5] != "/usr/local/bin/cardbot" || got.args[6] != "/Volumes/NIKON Z 9" {
		t.Fatalf("args = %v, expected legacy combined command to split into binary + mount", got.args)
	}
}

func TestOpenWith_StripsMatchingQuotesFromBinaryAndMount(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "Ghostty",
		CardBotBinary: "'/usr/local/bin/cardbot'",
		MountPath:     "'/Volumes/NIKON Z 9'",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.args[5] != "/usr/local/bin/cardbot" || got.args[6] != encodeTargetPathArg("/Volumes/NIKON Z 9") {
		t.Fatalf("args = %v, expected normalized binary + encoded mount path", got.args)
	}
}

func TestOpenWith_EmptyTerminalApp_DefaultsToSystemDefault(t *testing.T) {
	var got recordedCommand
	run := func(name string, args ...string) error {
		got = recordedCommand{name: name, args: append([]string{}, args...)}
		return nil
	}

	err := openWith(Options{
		TerminalApp:   "   ",
		CardBotBinary: "/usr/local/bin/cardbot",
		MountPath:     "/Volumes/CARD",
	}, run)
	if err != nil {
		t.Fatalf("launchWith error: %v", err)
	}
	if got.name != "open" {
		t.Fatalf("command name = %q, want %q", got.name, "open")
	}
	if len(got.args) != 1 || !strings.HasSuffix(got.args[0], ".command") {
		t.Fatalf("args = %v, want one .command path", got.args)
	}
	t.Cleanup(func() { _ = os.Remove(got.args[0]) })
}

func TestOpenWith_RequiresBinaryAndMountPath(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{name: "missing binary", opts: Options{MountPath: "/Volumes/CARD"}},
		{name: "missing mount path", opts: Options{CardBotBinary: "/usr/local/bin/cardbot"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := openWith(tt.opts, func(name string, args ...string) error { return nil })
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
