package instance

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestHasOtherInteractiveProcess_NoMatches(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		return nil, &exec.ExitError{}
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got {
		t.Fatal("got true, want false")
	}
}

func TestHasOtherInteractiveProcess_OnlySelf(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		return []byte("1234\n"), nil
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got {
		t.Fatal("got true, want false")
	}
}

func TestHasOtherInteractiveProcess_HasAnotherInteractive(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("1234\n9999\n"), nil
		}
		// ps -p 9999 -o args= → interactive (no --daemon)
		return []byte("/usr/local/bin/cardbot /Volumes/CARD\n"), nil
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Fatal("got false, want true — another interactive process exists")
	}
}

func TestHasOtherInteractiveProcess_SkipsDaemonProcess(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("1234\n9999\n"), nil
		}
		// ps -p 9999 -o args= → daemon process
		return []byte("/usr/local/bin/cardbot --daemon\n"), nil
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got {
		t.Fatal("got true, want false — daemon should be skipped")
	}
}

func TestHasOtherInteractiveProcess_DaemonAndInteractive(t *testing.T) {
	callCount := 0
	run := func(name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("1234\n8888\n9999\n"), nil
		}
		callCount++
		// First ps call (pid 8888) → daemon
		if callCount == 1 {
			return []byte("/usr/local/bin/cardbot --daemon\n"), nil
		}
		// Second ps call (pid 9999) → interactive
		return []byte("/usr/local/bin/cardbot /Volumes/CARD\n"), nil
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Fatal("got false, want true — interactive process exists alongside daemon")
	}
}

func TestHasOtherInteractiveProcess_IgnoresInvalidLines(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		return []byte("1234\nnot-a-pid\n"), nil
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got {
		t.Fatal("got true, want false")
	}
}

func TestHasOtherInteractiveProcess_CommandError(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	_, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHasOtherInteractiveProcess_RequiresProcessName(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	_, err := hasOtherInteractiveProcessWithRunner("", 1234, run)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHasOtherInteractiveProcess_PsFailsTreatsAsInteractive(t *testing.T) {
	run := func(name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("9999\n"), nil
		}
		return nil, fmt.Errorf("ps failed")
	}

	got, err := hasOtherInteractiveProcessWithRunner("cardbot", 1234, run)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Fatal("got false, want true — should treat as interactive when ps fails")
	}
}

func TestProcessMatches(t *testing.T) {
	t.Parallel()
	run := func(name string, args ...string) ([]byte, error) {
		if name != "ps" {
			t.Fatalf("command = %q, want ps", name)
		}
		return []byte("/usr/local/bin/cardbot\n"), nil
	}
	got, err := processMatchesWithRunner(42, "cardbot", run)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected process to match")
	}
	got, err = processMatchesWithRunner(42, "other", run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("unexpected process match")
	}
}
