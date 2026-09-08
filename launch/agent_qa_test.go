package launch

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestAgent_IsolatedLoginLifecycle(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	binary := filepath.Join(home, `A&B <QA> "quoted" 'photo'`, "cardbot  ")
	plistPath := plistPath(home)
	loaded := false
	var calls []call
	// No launchctl process is executed: only plist I/O in this test's home.
	run := func(name string, args ...string) error {
		calls = append(calls, call{name, slices.Clone(args)})
		if name != "launchctl" {
			t.Fatalf("unexpected command %q", name)
		}
		switch args[0] {
		case "bootout":
			loaded = false
		case "bootstrap":
			loaded = true
		case "kickstart":
		default:
			t.Fatalf("unexpected args %q", args)
		}
		return nil
	}
	path, err := installWith(binary, home, 501, run)
	if err != nil {
		t.Fatal(err)
	}
	if path != plistPath {
		t.Fatalf("plist path = %q, want %q", path, plistPath)
	}
	wantCalls := [][]string{
		{"bootout", "gui/501", plistPath},
		{"bootstrap", "gui/501", plistPath},
		{"kickstart", "-k", "gui/501/" + label},
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("calls = %v", calls)
	}
	for i, want := range wantCalls {
		if !slices.Equal(calls[i].args, want) {
			t.Fatalf("call %d = %q, want %q", i, calls[i].args, want)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plist struct {
		Dict struct {
			Label string     `xml:"string"`
			Args  []string   `xml:"array>string"`
			Keys  []string   `xml:"key"`
			True  []struct{} `xml:"true"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &plist); err != nil {
		t.Fatalf("invalid LaunchAgent XML: %v", err)
	}
	if plist.Dict.Label != label || !slices.Equal(plist.Dict.Args, []string{binary, "--daemon"}) {
		t.Fatalf("plist changed label or executable arguments: %+v", plist.Dict)
	}
	if !slices.Contains(plist.Dict.Keys, "RunAtLoad") || !slices.Contains(plist.Dict.Keys, "KeepAlive") || len(plist.Dict.True) != 2 {
		t.Fatalf("missing login/restart settings: %+v", plist.Dict)
	}
	statusRun := func(name string, args ...string) ([]byte, error) {
		if name != "launchctl" || !slices.Equal(args, []string{"print", "gui/501/" + label}) {
			t.Fatalf("unexpected status command: %s %q", name, args)
		}
		if !loaded {
			return nil, &exec.ExitError{}
		}
		return []byte("running"), nil
	}
	st, err := statusWith(home, 501, statusRun)
	if err != nil || !st.Installed || !st.Loaded {
		t.Fatalf("installed status = %+v, %v", st, err)
	}
	if _, err := uninstallWith(home, 501, run); err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("agent was not unloaded")
	}
	st, err = statusWith(home, 501, statusRun)
	if err != nil || st.Installed || st.Loaded {
		t.Fatalf("uninstalled status = %+v, %v", st, err)
	}
	// Repeated uninstall is harmless in the isolated home.
	if _, err := uninstallWith(home, 501, run); err != nil {
		t.Fatal(err)
	}
}

func TestInstallWith_KickstartError(t *testing.T) {
	t.Parallel()
	_, err := installWith("/synthetic/cardbot", t.TempDir(), 501, func(_ string, args ...string) error {
		if args[0] == "kickstart" {
			return os.ErrPermission
		}
		return nil
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission error", err)
	}
}

func TestInstallWith_UnavailableDirectoryDoesNotLaunch(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Library"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := installWith("/synthetic/cardbot", home, 501, func(string, ...string) error {
		t.Fatal("must not launch when plist cannot be written")
		return nil
	})
	if err == nil {
		t.Fatal("expected directory error")
	}
}
