package term

import (
	"errors"
	"testing"
)

func TestFormatCount(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		n    int
		want string
	}{
		{0, "0"}, {1, "1"}, {999, "999"}, {1000, "1,000"},
		{11653, "11,653"}, {1000000, "1,000,000"},
		{-1, "-1"}, {-1000, "-1,000"}, {-2147483648, "-2,147,483,648"},
	} {
		if got := FormatCount(tt.n); got != tt.want {
			t.Errorf("FormatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestTs_Format(t *testing.T) {
	t.Parallel()
	ts := Ts()
	if len(ts) != 19 { // "2006-01-02T15:04:05"
		t.Errorf("Ts() = %q, want 19-char timestamp", ts)
	}
}

func TestDimTS(t *testing.T) {
	t.Parallel()
	got := DimTS("2025-01-01T00:00:00")
	want := "\033[2m[2025-01-01T00:00:00]\033[0m"
	if got != want {
		t.Errorf("DimTS() = %q, want %q", got, want)
	}
}

func TestFriendlyErr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no space", errors.New("no space left on device"), "destination disk is full"},
		{"permission", errors.New("permission denied"), "permission denied — check folder permissions"},
		{"read-only", errors.New("read-only file system"), "destination is read-only"},
		{"io error", errors.New("input/output error"), "I/O error — card may be damaged"},
		{"other", errors.New("something else"), "something else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FriendlyErr(tt.err)
			if got != tt.want {
				t.Errorf("FriendlyErr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrandColor_KnownBrands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		brand string
		want  string
	}{
		{"Nikon", "\033[33m"},
		{"Canon", "\033[31m"},
		{"Sony", "\033[37m"},
		{"Fujifilm", "\033[32m"},
		{"Panasonic", "\033[34m"},
		{"Olympus", "\033[36m"},
		{"OM System", "\033[36m"},
	}
	for _, tt := range tests {
		got := BrandColor(tt.brand)
		if got != tt.want {
			t.Errorf("BrandColor(%q) = %q, want %q", tt.brand, got, tt.want)
		}
	}
}

func TestBrandColor_Unknown(t *testing.T) {
	t.Parallel()
	for _, brand := range []string{"Unknown", "Leica", "Hasselblad", ""} {
		got := BrandColor(brand)
		if got != "\033[37m" {
			t.Errorf("BrandColor(%q) = %q, want white (\\033[37m)", brand, got)
		}
	}
}

func TestReset(t *testing.T) {
	t.Parallel()
	if Reset != "\033[0m" {
		t.Errorf("Reset = %q, want \\033[0m", Reset)
	}
}
