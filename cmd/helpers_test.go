package cmd

import "testing"

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
