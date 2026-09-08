package update

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"
)

func FuzzVersions(f *testing.F) {
	for _, pair := range [][2]string{{"v0.0.10", "v0.10.0"}, {"0.9.0", "0.10.0-dev"}, {"1.2.3-alpha.2", "1.2.3-alpha.10"}, {"v1.2.3+build", "V1.2.3"}, {"garbage", "1.2"}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a)+len(b) > 4096 {
			t.Skip()
		}
		ab, err := compareVersions(a, b)
		ba, reverseErr := compareVersions(b, a)
		if (err == nil) != (reverseErr == nil) {
			t.Fatal("asymmetric validity")
		}
		if err != nil {
			return
		}
		if ab != -ba || ab < -1 || ab > 1 {
			t.Fatal("asymmetric version ordering")
		}
		for _, v := range []string{a, b} {
			p, err := parseVersion(v)
			if err != nil {
				t.Fatal(err)
			}
			canonical := fmt.Sprintf("%d.%d.%d", p.core[0], p.core[1], p.core[2])
			if p.prerelease != "" {
				canonical += "-" + p.prerelease
			}
			if cmp, err := compareVersions(v, canonical+"+fuzz"); err != nil || cmp != 0 {
				t.Fatalf("normalization/build metadata changed precedence: %q, %v", v, err)
			}
		}
	})
}

func FuzzChecksums(f *testing.F) {
	hash := strings.Repeat("ab", 32)
	for _, data := range []string{"", "invalid checksum", hash + "  cardbot-darwin-arm64\n", hash + " *cardbot-linux-amd64\n", hash + " *\n"} {
		f.Add([]byte(data))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16384 {
			t.Skip()
		}
		sums, err := parseChecksums(data)
		if err != nil {
			return
		}
		if len(sums) == 0 {
			t.Fatal("accepted empty checksum manifest")
		}
		var canonical strings.Builder
		for name, sum := range sums {
			decoded, err := hex.DecodeString(sum)
			if name == "" || err != nil || len(decoded) != 32 {
				t.Fatalf("invalid checksum entry: %q = %q", name, sum)
			}
			fmt.Fprintf(&canonical, "%s  *%s\n", sum, name)
		}
		again, err := parseChecksums([]byte(canonical.String()))
		if err != nil || !maps.Equal(sums, again) {
			t.Fatalf("checksum round trip failed: %v", err)
		}
	})
}
