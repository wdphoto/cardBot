package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// label is intentionally retained for LaunchAgent upgrade/uninstall stability.
// Changing it would orphan existing agents unless a migration removed both IDs.
const label = "com.illwill.cardbot"

type commandRunner func(name string, args ...string) error
type outputCommandRunner func(name string, args ...string) ([]byte, error)

// Status describes the current LaunchAgent state for cardBot.
type Status struct {
	PlistPath string
	Installed bool
	Loaded    bool
}

// Install creates/updates the cardBot LaunchAgent plist and loads it with launchctl.
// Returns the plist path.
func Install(binaryPath string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("launch agents are only supported on macOS")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return installWith(binaryPath, home, os.Getuid(), runCommand)
}

// Uninstall unloads and removes the cardBot LaunchAgent plist.
// Returns the plist path.
func Uninstall() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("launch agents are only supported on macOS")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return uninstallWith(home, os.Getuid(), runCommand)
}

// CurrentStatus reports whether cardBot's LaunchAgent plist is installed
// and currently loaded in launchd.
func CurrentStatus() (Status, error) {
	if runtime.GOOS != "darwin" {
		return Status{}, fmt.Errorf("launch agents are only supported on macOS")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Status{}, fmt.Errorf("resolving home directory: %w", err)
	}
	return statusWith(home, os.Getuid(), runCommandOutput)
}

func installWith(binaryPath, home string, uid int, run commandRunner) (string, error) {
	if strings.TrimSpace(binaryPath) == "" {
		return "", fmt.Errorf("binary path is required")
	}
	absBinary, err := filepath.Abs(binaryPath)
	if err != nil {
		return "", fmt.Errorf("resolving binary path: %w", err)
	}

	plist := plistPath(home)
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return "", fmt.Errorf("creating LaunchAgents directory: %w", err)
	}
	if err := os.WriteFile(plist, []byte(renderPlist(absBinary)), 0o644); err != nil {
		return "", fmt.Errorf("writing plist: %w", err)
	}

	domain := fmt.Sprintf("gui/%d", uid)
	_ = run("launchctl", "bootout", domain, plist) // ignore if not loaded
	if err := run("launchctl", "bootstrap", domain, plist); err != nil {
		return "", fmt.Errorf("loading launch agent: %w", err)
	}
	if err := run("launchctl", "kickstart", "-k", fmt.Sprintf("%s/%s", domain, label)); err != nil {
		return "", fmt.Errorf("starting launch agent: %w", err)
	}

	return plist, nil
}

func uninstallWith(home string, uid int, run commandRunner) (string, error) {
	plist := plistPath(home)
	domain := fmt.Sprintf("gui/%d", uid)
	_ = run("launchctl", "bootout", domain, plist) // ignore if not loaded

	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("removing plist: %w", err)
	}
	return plist, nil
}

func statusWith(home string, uid int, run outputCommandRunner) (Status, error) {
	st := Status{PlistPath: plistPath(home)}

	if _, err := os.Stat(st.PlistPath); err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, fmt.Errorf("checking plist: %w", err)
	}
	st.Installed = true

	if run == nil {
		return st, nil
	}

	service := fmt.Sprintf("gui/%d/%s", uid, label)
	_, err := run("launchctl", "print", service)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// launchctl returns non-zero when service is not loaded.
			return st, nil
		}
		return st, fmt.Errorf("checking launchctl service: %w", err)
	}
	st.Loaded = true
	return st, nil
}

func plistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func renderPlist(binaryPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, label, binaryPath)
}

func runCommand(name string, args ...string) error {
	_, err := runCommandOutput(name, args...)
	return err
}

func runCommandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}
	return out, nil
}
