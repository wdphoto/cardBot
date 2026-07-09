package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	DefaultRepo    = "wdphoto/cardBot"
	DefaultAPIBase = "https://api.github.com"
	userAgent      = "cardbot-updater"
	maxMetadata    = 1 << 20
	maxChecksums   = 4 << 20
	maxBinaryBytes = 256 << 20
)

var (
	ErrAlreadyUpToDate = errors.New("already up to date")
	ErrAssetNotFound   = errors.New("release asset not found")
	ErrChecksumMissing = errors.New("checksum missing for release asset")
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type CheckResult struct {
	Current string
	Latest  string
	Update  bool
}

var semverRe = regexp.MustCompile(`^[vV]?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// PlatformAssetName returns the release asset name for the current platform.
func PlatformAssetName(goos, goarch string) string {
	return fmt.Sprintf("cardbot-%s-%s", goos, goarch)
}

// CheckLatest checks GitHub Releases and reports whether an update is available.
func CheckLatest(ctx context.Context, client *http.Client, apiBase, repo, current string) (CheckResult, error) {
	rel, err := latestRelease(ctx, client, apiBase, repo)
	if err != nil {
		return CheckResult{}, err
	}
	latest := normalizeVersion(rel.TagName)
	cmp, err := compareVersions(latest, current)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Current: current, Latest: latest, Update: cmp > 0}, nil
}

// SelfUpdate downloads and atomically replaces the current executable.
// Returns the installed version on success.
func SelfUpdate(ctx context.Context, client *http.Client, apiBase, repo, current, executablePath string) (string, error) {
	return SelfUpdateForPlatform(ctx, client, apiBase, repo, current, executablePath, runtime.GOOS, runtime.GOARCH)
}

// SelfUpdateForPlatform is SelfUpdate with explicit GOOS/GOARCH (useful for tests).
func SelfUpdateForPlatform(ctx context.Context, client *http.Client, apiBase, repo, current, executablePath, goos, goarch string) (string, error) {
	rel, err := latestRelease(ctx, client, apiBase, repo)
	if err != nil {
		return "", err
	}

	latest := normalizeVersion(rel.TagName)
	cmp, err := compareVersions(latest, current)
	if err != nil {
		return "", err
	}
	if cmp <= 0 {
		return "", ErrAlreadyUpToDate
	}

	assetName := PlatformAssetName(goos, goarch)
	assetURL, ok := findAssetURL(rel, assetName)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrAssetNotFound, assetName)
	}

	sumsURL, ok := findAssetURL(rel, "checksums.txt")
	if !ok {
		return "", fmt.Errorf("%w: checksums.txt", ErrAssetNotFound)
	}

	sumsData, err := getBytes(ctx, client, sumsURL, maxChecksums)
	if err != nil {
		return "", fmt.Errorf("downloading checksums: %w", err)
	}
	sums, err := parseChecksums(sumsData)
	if err != nil {
		return "", err
	}
	wantHash, ok := sums[assetName]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrChecksumMissing, assetName)
	}

	tmp, err := os.CreateTemp(filepath.Dir(executablePath), ".cardbot-update-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	gotHash, err := downloadToFileSHA256(ctx, client, assetURL, tmp)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(gotHash, wantHash) {
		return "", fmt.Errorf("checksum mismatch for %s", assetName)
	}

	mode := os.FileMode(0755)
	if st, err := os.Stat(executablePath); err == nil {
		mode = st.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		return "", fmt.Errorf("setting executable mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, executablePath); err != nil {
		return "", fmt.Errorf("replacing binary: %w", err)
	}

	return latest, nil
}

func latestRelease(ctx context.Context, client *http.Client, apiBase, repo string) (*Release, error) {
	url := strings.TrimRight(apiBase, "/") + "/repos/" + repo + "/releases/latest"
	data, err := getBytes(ctx, client, url, maxMetadata)
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("parsing release response: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release response missing tag_name")
	}
	return &rel, nil
}

func findAssetURL(rel *Release, name string) (string, bool) {
	for _, a := range rel.Assets {
		if a.Name == name && a.URL != "" {
			return a.URL, true
		}
	}
	return "", false
}

func getBytes(ctx context.Context, client *http.Client, url string, maxBytes int64) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxBytes)
	}
	return data, nil
}

func downloadToFileSHA256(ctx context.Context, client *http.Client, url string, dst *os.File) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading binary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("downloading binary: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > maxBinaryBytes {
		return "", fmt.Errorf("downloading binary: asset exceeds %d-byte limit", maxBinaryBytes)
	}

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(resp.Body, maxBinaryBytes+1))
	if err != nil {
		return "", fmt.Errorf("writing update file: %w", err)
	}
	if n > maxBinaryBytes {
		return "", fmt.Errorf("downloading binary: asset exceeds %d-byte limit", maxBinaryBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseChecksums(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		hash := strings.ToLower(parts[0])
		if len(hash) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid SHA256 checksum %q", parts[0])
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil, fmt.Errorf("invalid SHA256 checksum %q", parts[0])
		}
		name := strings.TrimPrefix(parts[len(parts)-1], "*")
		if existing, ok := out[name]; ok && existing != hash {
			return nil, fmt.Errorf("conflicting checksums for %s", name)
		}
		out[name] = hash
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("checksums file is empty or malformed")
	}
	return out, nil
}

func compareVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if av.core[i] > bv.core[i] {
			return 1, nil
		}
		if av.core[i] < bv.core[i] {
			return -1, nil
		}
	}
	return comparePrerelease(av.prerelease, bv.prerelease), nil
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
}

type parsedVersion struct {
	core       [3]int
	prerelease string
}

func parseVersion(v string) (parsedVersion, error) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(v))
	if len(m) == 0 {
		return parsedVersion{}, fmt.Errorf("invalid version: %q", v)
	}
	var out parsedVersion
	for i := 1; i <= 3; i++ {
		n, err := strconv.Atoi(m[i])
		if err != nil {
			return parsedVersion{}, fmt.Errorf("invalid version: %q", v)
		}
		out.core[i-1] = n
	}
	out.prerelease = m[4]
	for _, identifier := range strings.Split(out.prerelease, ".") {
		if len(identifier) > 1 && identifier[0] == '0' && allDigits(identifier) {
			return parsedVersion{}, fmt.Errorf("invalid version: %q", v)
		}
	}
	return out, nil
}

func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		aNumeric := allDigits(ap[i])
		bNumeric := allDigits(bp[i])
		switch {
		case aNumeric && bNumeric:
			if len(ap[i]) < len(bp[i]) || (len(ap[i]) == len(bp[i]) && ap[i] < bp[i]) {
				return -1
			}
			return 1
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		case ap[i] < bp[i]:
			return -1
		default:
			return 1
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	return 1
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
