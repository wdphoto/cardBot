package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

type faultTransport func(*http.Request) (*http.Response, error)

func (f faultTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSelfUpdate_FailuresPreserveExecutable(t *testing.T) {
	for _, mode := range []string{"checksum mismatch", "partial download", "malformed manifest"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			executable := filepath.Join(dir, "cardbot")
			unrelated := filepath.Join(dir, ".cardbot-update-owned")
			for _, path := range []string{executable, unrelated} {
				if err := os.WriteFile(path, []byte("preserve me"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			readErr := errors.New("synthetic interrupted download")
			const base = "https://update.invalid"
			client := &http.Client{Transport: faultTransport(func(r *http.Request) (*http.Response, error) {
				// Every HTTP request is synthetic; even redirects cannot reach a network.
				var body io.Reader
				switch r.URL.String() {
				case base + "/repos/owner/repo/releases/latest":
					metadata, err := json.Marshal(Release{TagName: "v0.10.0", Assets: []Asset{
						{Name: "cardbot-darwin-arm64", URL: base + "/binary"},
						{Name: "checksums.txt", URL: base + "/sums"},
					}})
					if err != nil {
						return nil, err
					}
					body = strings.NewReader(string(metadata))
				case base + "/sums":
					sums := fmt.Sprintf("%x  cardbot-darwin-arm64\n", sha256.Sum256([]byte("new-binary")))
					if mode == "malformed manifest" {
						sums = "invalid manifest"
					}
					body = strings.NewReader(sums)
				case base + "/binary":
					body = strings.NewReader("bad-binary")
					if mode == "partial download" {
						body = io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(readErr))
					}
				default:
					return nil, fmt.Errorf("unexpected request: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Header: make(http.Header)}, nil
			})}
			installed, err := SelfUpdateForPlatform(context.Background(), client, base, "owner/repo", "0.9.0", executable, "darwin", "arm64")
			if err == nil || installed != "" {
				t.Fatalf("failed update reported success: %q, %v", installed, err)
			}
			if mode == "partial download" && !errors.Is(err, readErr) {
				t.Fatalf("lost download error: %v", err)
			}
			if mode == "checksum mismatch" && !strings.Contains(err.Error(), "checksum mismatch") {
				t.Fatalf("wrong error: %v", err)
			}
			for _, path := range []string{executable, unrelated} {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != "preserve me" {
					t.Fatalf("preexisting file changed: %q, %v", got, err)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("update temporary left behind: %v, %v", entries, err)
			}
		})
	}
}
