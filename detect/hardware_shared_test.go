package detect

import "testing"

func TestUnescapeMountField(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no escapes", "/mnt/data", "/mnt/data"},
		{"space", `\040`, " "},
		{"tab", `\011`, "\t"},
		{"newline", `\012`, "\n"},
		{"backslash", `\134`, `\`},
		{"literal backslash-space", `\134040`, `\040`},
		{"unknown escape unchanged", `\141`, `\141`},
		{"malformed trailing backslash", `abc\`, `abc\`},
		{"mixed", `/mnt/My\040Card`, "/mnt/My Card"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeMountField(tt.in); got != tt.want {
				t.Fatalf("unescapeMountField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseMountLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		device string
		path   string
		ok     bool
	}{
		{"normal", "/dev/sda1 /mnt ext4 rw 0 0", "/dev/sda1", "/mnt", true},
		{"spaced path", `/dev/sda1 /mnt/My\040Card ext4 rw 0 0`, "/dev/sda1", "/mnt/My Card", true},
		{"too few fields", "/dev/sda1", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, path, ok := parseMountLine(tt.line)
			if ok != tt.ok || device != tt.device || path != tt.path {
				t.Fatalf("parseMountLine(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tt.line, device, path, ok, tt.device, tt.path, tt.ok)
			}
		})
	}
}

func TestVolumeUUIDMatches(t *testing.T) {
	tests := []struct {
		name   string
		target string
		device string
		want   bool
	}{
		{"exact basename", "../../sda1", "sda1", true},
		{"substring not a match", "../../sda10", "sda1", false},
		{"different device", "../../sdb1", "sda1", false},
		{"mmcblk", "../../mmcblk0p1", "mmcblk0p1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := volumeUUIDMatches(tt.target, tt.device); got != tt.want {
				t.Fatalf("volumeUUIDMatches(%q, %q) = %v, want %v", tt.target, tt.device, got, tt.want)
			}
		})
	}
}
