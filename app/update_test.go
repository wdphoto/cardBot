package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wdphoto/cardBot/update"
)

func TestMaybeCheckForUpdate_UpdateAvailable(t *testing.T) {
	t.Parallel()

	called := 0
	fake := updateChecker(func(_ context.Context, _ *http.Client, _, _, _ string) (update.CheckResult, error) {
		called++
		return update.CheckResult{Current: "0.4.1", Latest: "0.4.2", Update: true}, nil
	})

	latest, err := MaybeCheckForUpdate(nil, "0.4.1", fake)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if latest != "0.4.2" {
		t.Fatalf("latest = %q, want 0.4.2", latest)
	}
	if called != 1 {
		t.Fatalf("checkLatest called %d times, want 1", called)
	}
}

func TestMaybeCheckForUpdate_UpToDate(t *testing.T) {
	t.Parallel()

	fake := updateChecker(func(_ context.Context, _ *http.Client, _, _, _ string) (update.CheckResult, error) {
		return update.CheckResult{Current: "0.4.1", Latest: "0.4.1", Update: false}, nil
	})

	latest, err := MaybeCheckForUpdate(nil, "0.4.1", fake)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if latest != "" {
		t.Fatalf("latest = %q, want empty", latest)
	}
}

func TestMaybeCheckForUpdate_Error(t *testing.T) {
	t.Parallel()

	fake := updateChecker(func(_ context.Context, _ *http.Client, _, _, _ string) (update.CheckResult, error) {
		return update.CheckResult{}, errors.New("boom")
	})

	latest, err := MaybeCheckForUpdate(nil, "0.4.1", fake)
	if err == nil {
		t.Fatal("expected error")
	}
	if latest != "" {
		t.Fatalf("latest = %q, want empty", latest)
	}
}
