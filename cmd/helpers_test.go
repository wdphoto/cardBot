package cmd

import "testing"

func TestStartupMessage(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.0.10", "v0.0.10", "v0.0.10-7-gfa94280"} {
		want := "Starting cardBot v0.0.10"
		if version == "v0.0.10-7-gfa94280" {
			want += "-7-gfa94280"
		}
		if got := startupMessage(version); got != want {
			t.Errorf("startupMessage(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestBoolEnabled(t *testing.T) {
	t.Parallel()

	if got := boolEnabled(true); got != "enabled" {
		t.Fatalf("boolEnabled(true) = %q, want %q", got, "enabled")
	}
	if got := boolEnabled(false); got != "disabled" {
		t.Fatalf("boolEnabled(false) = %q, want %q", got, "disabled")
	}
}

func TestBoolYesNo(t *testing.T) {
	t.Parallel()

	if got := boolYesNo(true); got != "yes" {
		t.Fatalf("boolYesNo(true) = %q, want %q", got, "yes")
	}
	if got := boolYesNo(false); got != "no" {
		t.Fatalf("boolYesNo(false) = %q, want %q", got, "no")
	}
}
