package launch

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Options controls how a terminal is launched for a detected card.
type Options struct {
	TerminalApp      string
	WorkingDirectory string
	LaunchArgs       []string
	CardBotBinary    string
	MountPath        string
	Debugf           func(format string, args ...any)
	Logf             func(format string, args ...any)
}

// Open opens the configured terminal and runs cardbot for the given mount path.
func Open(opts Options) error {
	return openWith(opts, runCommand)
}

func openWith(opts Options, run commandRunner) error {
	binary := stripMatchingQuotes(opts.CardBotBinary)
	mountPath := stripMatchingQuotes(opts.MountPath)
	if strings.TrimSpace(binary) == "" {
		return fmt.Errorf("cardbot binary path is required")
	}
	if strings.TrimSpace(mountPath) == "" {
		return fmt.Errorf("mount path is required")
	}

	debugf := opts.Debugf
	if debugf == nil {
		debugf = func(string, ...any) {}
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	runLogged := func(name string, args ...string) error {
		formatted := formatCommandArgs(args)
		debugf("exec: %s %s", name, formatted)
		logf("Launcher exec: %s %s", name, formatted)
		return run(name, args...)
	}

	app := normalizeTerminalApp(opts.TerminalApp)
	debugf("launcher config: app=%q binary=%q mount=%q working_dir=%q custom_args=%d", app, binary, mountPath, opts.WorkingDirectory, len(opts.LaunchArgs))

	if len(opts.LaunchArgs) > 0 {
		resolved := resolveLaunchArgs(opts.LaunchArgs, binary, mountPath)
		if isGhosttyApp(app) {
			resolved = normalizeGhosttyLaunchArgs(resolved, binary, mountPath)
		}
		debugf("launcher branch: custom launch args")
		if isSystemDefaultTerminal(app) {
			return runLogged("open", resolved...)
		}
		openArgs := []string{"-a", app, "--args"}
		if isGhosttyApp(app) {
			if wd := ghosttyWorkingDirectory(opts.WorkingDirectory); wd != "" {
				openArgs = append(openArgs, "--working-directory="+wd)
			}
		}
		openArgs = append(openArgs, resolved...)
		return runLogged("open", openArgs...)
	}

	if isSystemDefaultTerminal(app) {
		debugf("launcher branch: system default terminal")
		scriptPath, err := writeDefaultTerminalCommandScript(binary, mountPath)
		if err != nil {
			return err
		}
		debugf("generated command script: %s", scriptPath)
		return runLogged("open", scriptPath)
	}

	if isTerminalApp(app) {
		debugf("launcher branch: Terminal AppleScript")
		cmd := fmt.Sprintf("%s %s", shQuote(binary), shQuote(mountPath))
		// Activate first so a cold-launched Terminal creates one window,
		// then run the command in that window instead of spawning a second.
		script := fmt.Sprintf(`
tell application "Terminal"
	activate
	delay 0.5
	do script %q in front window
end tell`, cmd)
		return runLogged("osascript", "-e", script)
	}

	if isGhosttyApp(app) {
		debugf("launcher branch: Ghostty")
		args := []string{"-a", app, "--args"}
		if wd := ghosttyWorkingDirectory(opts.WorkingDirectory); wd != "" {
			// Daemon processes launched by launchd often inherit cwd="/".
			// Set Ghostty's working directory explicitly so new windows don't land at root.
			args = append(args, "--working-directory="+wd)
		}
		// Pass mount path as a base64-encoded option value to avoid Ghostty treating
		// a raw path-like argv token as a UI path/open target.
		args = append(args, "-e", binary, encodeTargetPathArg(mountPath))
		return runLogged("open", args...)
	}

	debugf("launcher branch: generic app")
	return runLogged("open", "-a", app, "--args", binary, mountPath)
}

func normalizeTerminalApp(app string) string {
	app = strings.TrimSpace(app)
	if app == "" {
		return "Default"
	}
	if strings.EqualFold(app, "terminal.app") {
		return "Terminal"
	}
	if strings.EqualFold(app, "default") || strings.EqualFold(app, "system default") || strings.EqualFold(app, "macos default") {
		return "Default"
	}
	if strings.EqualFold(app, "ghostty") {
		return "Ghostty"
	}
	return app
}

func resolveLaunchArgs(args []string, binary, mountPath string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		replaced := strings.ReplaceAll(arg, "{{mount_path}}", mountPath)
		replaced = strings.ReplaceAll(replaced, "{{cardbot_binary}}", binary)
		out = append(out, replaced)
	}
	return out
}

func normalizeGhosttyLaunchArgs(args []string, binary, mountPath string) []string {
	if len(args) == 0 {
		return args
	}

	out := make([]string, 0, len(args)+1)
	for _, arg := range args {
		out = append(out, stripMatchingQuotes(arg))
	}

	for i := 0; i < len(out)-1; i++ {
		if out[i] != "-e" {
			continue
		}
		cmd := strings.TrimSpace(out[i+1])
		if cmd == "" {
			break
		}

		if strings.HasPrefix(cmd, binary+" ") && strings.TrimSpace(strings.TrimPrefix(cmd, binary)) == mountPath {
			replacement := []string{"-e", binary, mountPath}
			prefix := append([]string{}, out[:i]...)
			suffix := append([]string{}, out[i+2:]...)
			return append(append(prefix, replacement...), suffix...)
		}

		if words, ok := parseSimpleShellWords(cmd); ok && len(words) == 2 && words[0] == binary && words[1] == mountPath {
			replacement := []string{"-e", binary, mountPath}
			prefix := append([]string{}, out[:i]...)
			suffix := append([]string{}, out[i+2:]...)
			return append(append(prefix, replacement...), suffix...)
		}

		break
	}

	return out
}

func stripMatchingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}

	if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") {
		inner := s[1 : len(s)-1]
		if !strings.Contains(inner, "'") {
			return inner
		}
		return s
	}
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		inner := s[1 : len(s)-1]
		if !strings.Contains(inner, "\"") {
			return inner
		}
		return s
	}
	return s
}

func parseSimpleShellWords(s string) ([]string, bool) {
	var words []string
	var token strings.Builder
	inSingle := false
	inDouble := false
	escape := false
	hasToken := false

	flush := func() {
		if !hasToken {
			return
		}
		words = append(words, token.String())
		token.Reset()
		hasToken = false
	}

	for _, r := range s {
		switch {
		case escape:
			token.WriteRune(r)
			escape = false
			hasToken = true
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				token.WriteRune(r)
			}
			hasToken = true
		case inDouble:
			if r == '"' {
				inDouble = false
			} else if r == '\\' {
				escape = true
			} else {
				token.WriteRune(r)
			}
			hasToken = true
		default:
			if unicode.IsSpace(r) {
				flush()
				continue
			}
			switch r {
			case '\\':
				escape = true
				hasToken = true
			case '\'':
				inSingle = true
				hasToken = true
			case '"':
				inDouble = true
				hasToken = true
			default:
				token.WriteRune(r)
				hasToken = true
			}
		}
	}

	if escape || inSingle || inDouble {
		return nil, false
	}
	flush()
	return words, true
}

func isTerminalApp(app string) bool {
	a := strings.ToLower(strings.TrimSpace(app))
	return a == "terminal" || a == "terminal.app"
}

func isSystemDefaultTerminal(app string) bool {
	a := strings.ToLower(strings.TrimSpace(app))
	return a == "default" || a == "system default" || a == "macos default"
}

func isGhosttyApp(app string) bool {
	return strings.Contains(strings.ToLower(app), "ghostty")
}

func ghosttyWorkingDirectory(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return home
}

func encodeTargetPathArg(path string) string {
	return "--target-path-b64=" + base64.StdEncoding.EncodeToString([]byte(path))
}

func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func formatCommandArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func writeDefaultTerminalCommandScript(binary, mountPath string) (string, error) {
	f, err := os.CreateTemp("", "cardbot-launch-*.command")
	if err != nil {
		return "", fmt.Errorf("creating command script: %w", err)
	}
	defer f.Close()

	scriptPath := f.Name()
	script := fmt.Sprintf("#!/bin/sh\nrm -- %s\nexec %s %s\n", shQuote(scriptPath), shQuote(binary), shQuote(mountPath))
	if _, err := f.WriteString(script); err != nil {
		_ = os.Remove(scriptPath)
		return "", fmt.Errorf("writing command script: %w", err)
	}
	if err := f.Chmod(0o700); err != nil {
		_ = os.Remove(scriptPath)
		return "", fmt.Errorf("chmod command script: %w", err)
	}
	return filepath.Clean(scriptPath), nil
}
