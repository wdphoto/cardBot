package launch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

func TestPlistPath(t *testing.T) {
	home := "/Users/test"
	got := plistPath(home)
	want := "/Users/test/Library/LaunchAgents/com.illwill.cardbot.plist"
	if got != want {
		t.Fatalf("plistPath() = %q, want %q", got, want)
	}
}

func TestRenderPlist_IncludesBinaryAndDaemonArg(t *testing.T) {
	plist := renderPlist("/usr/local/bin/cardbot")

	for _, want := range []string{
		"<string>com.illwill.cardbot</string>",
		"<string>/usr/local/bin/cardbot</string>",
		"<string>--daemon</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q\n%s", want, plist)
		}
	}
}

func TestInstallWith_WritesPlistAndLaunchesAgent(t *testing.T) {
	home := t.TempDir()
	var calls []call
	run := func(name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name: name, args: append([]string{}, args...)})
		return nil, nil
	}

	plist, err := installWith("/usr/local/bin/cardbot", home, 501, run)
	if err != nil {
		t.Fatalf("installWith error: %v", err)
	}

	if _, err := os.Stat(plist); err != nil {
		t.Fatalf("plist not written: %v", err)
	}

	data, err := os.ReadFile(plist)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "<string>/usr/local/bin/cardbot</string>") {
		t.Fatalf("plist missing binary path:\n%s", content)
	}

	if len(calls) != 3 {
		t.Fatalf("launchctl calls = %d, want 3", len(calls))
	}
	if calls[0].name != "launchctl" || calls[0].args[0] != "bootout" {
		t.Fatalf("first call = %#v, want launchctl bootout ...", calls[0])
	}
	if calls[1].name != "launchctl" || calls[1].args[0] != "bootstrap" {
		t.Fatalf("second call = %#v, want launchctl bootstrap ...", calls[1])
	}
	if calls[2].name != "launchctl" || calls[2].args[0] != "kickstart" {
		t.Fatalf("third call = %#v, want launchctl kickstart ...", calls[2])
	}
}

func TestInstallWith_BootstrapError(t *testing.T) {
	home := t.TempDir()
	run := func(name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "bootstrap" {
			return nil, os.ErrPermission
		}
		return nil, nil
	}

	_, err := installWith("/usr/local/bin/cardbot", home, 501, run)
	if err == nil {
		t.Fatal("expected bootstrap error")
	}
}

func TestUninstallWith_RemovesPlist(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []call
	run := func(name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name: name, args: append([]string{}, args...)})
		return nil, nil
	}

	gotPath, err := uninstallWith(home, 501, run)
	if err != nil {
		t.Fatalf("uninstallWith error: %v", err)
	}
	if gotPath != plist {
		t.Fatalf("gotPath = %q, want %q", gotPath, plist)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatalf("plist should be removed, stat err=%v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("launchctl calls = %d, want 1", len(calls))
	}
	if calls[0].args[0] != "bootout" {
		t.Fatalf("call = %#v, want bootout", calls[0])
	}
}

func TestStatusWith_LoadedWithoutPlist(t *testing.T) {
	home := t.TempDir()
	called := false
	run := func(name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}

	st, err := statusWith(home, 501, run)
	if err != nil {
		t.Fatalf("statusWith error: %v", err)
	}
	if st.Installed {
		t.Fatal("Installed = true, want false")
	}
	if !st.Loaded {
		t.Fatal("Loaded = false despite loaded job")
	}
	if !called {
		t.Fatal("missing plist must not bypass launchctl status")
	}
}

func TestStatusWith_InstalledAndLoaded(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	st, err := statusWith(home, 501, run)
	if err != nil {
		t.Fatalf("statusWith error: %v", err)
	}
	if !st.Installed {
		t.Fatal("Installed = false, want true")
	}
	if !st.Loaded {
		t.Fatal("Loaded = false, want true")
	}
}

func TestStatusWith_InstalledNotLoaded(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(name string, args ...string) ([]byte, error) {
		return []byte("Could not find service \"" + label + "\" in domain for user gui: 501"), &exec.ExitError{}
	}

	st, err := statusWith(home, 501, run)
	if err != nil {
		t.Fatalf("statusWith error: %v", err)
	}
	if !st.Installed {
		t.Fatal("Installed = false, want true")
	}
	if st.Loaded {
		t.Fatal("Loaded = true, want false")
	}
}

func TestStatusWith_LaunchctlError(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	_, err := statusWith(home, 501, run)
	if err == nil {
		t.Fatal("expected error")
	}
}
