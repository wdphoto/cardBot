//go:build !darwin && !linux

package detect

// HardwareInfo is an empty placeholder on unsupported platforms so the CLI
// remains buildable even though detection and eject are unavailable.
type HardwareInfo struct{}

func (h *HardwareInfo) DiskID() string { return "" }

func QuickHardwareInfo(string) *HardwareInfo { return &HardwareInfo{} }

func GetHardwareInfo(string) (*HardwareInfo, error) { return &HardwareInfo{}, nil }

func FormatHardwareInfo(*HardwareInfo) string { return "Hardware info unavailable" }
