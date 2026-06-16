//go:build darwin

package detect

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/wdphoto/cardBot/fsutil"
)

// Pre-compiled regexes for diskutil output parsing.
var (
	reDeviceID   = regexp.MustCompile(`Device Identifier:\s*(\S+)`)
	reDiskSize   = regexp.MustCompile(`Disk Size:\s*[\d.]+\s*\w+\s*\((\d+)\s*Bytes\)`)
	reRemovable  = regexp.MustCompile(`Removable Media:\s*(\w+)`)
	reProtocol   = regexp.MustCompile(`Protocol:\s*(.+)`)
	reVolumeUUID = regexp.MustCompile(`Volume UUID:\s*(\S+)`)
	reParentDisk = regexp.MustCompile(`^(disk\d+)s\d+$`)
	reContent    = regexp.MustCompile(`Content \(IOContent\):\s*(.+)`)
	reFS         = regexp.MustCompile(`File System Personality:\s*(.+)`)
	reRO         = regexp.MustCompile(`Volume Read-Only:\s*(\w+)`)

	// system_profiler NVMe fields
	reNVMeModel    = regexp.MustCompile(`Model:\s*(.+)`)
	reNVMeSerial   = regexp.MustCompile(`Serial Number:\s*(.+)`)
	reNVMeFirmware = regexp.MustCompile(`Revision:\s*(.+)`)
	reNVMeSpeed    = regexp.MustCompile(`Link Speed:\s*(.+)`)
	reNVMeWidth    = regexp.MustCompile(`Link Width:\s*(.+)`)
	reNVMeSmart    = regexp.MustCompile(`S\.M\.A\.R\.T\. status:\s*(.+)`)
	reNVMeTrim     = regexp.MustCompile(`TRIM Support:\s*(.+)`)
)

// HardwareInfo contains device-level information about the card/reader.
// On macOS, CID (Card Identification register) is not accessible through
// USB card readers. See hardware_linux.go for CID support.
type HardwareInfo struct {
	// From syscall / diskutil
	DeviceBytes     int64
	FilesystemBytes int64
	BlockSize       int64
	DeviceID        string
	VolumeUUID      string
	IsRemovable     bool
	Protocol        string
	FileSystem      string
	PartitionScheme string
	ReadOnly        bool

	// From system_profiler (CFexpress/XQD/NVMe cards only)
	Model       string
	Serial      string
	Firmware    string
	LinkSpeed   string
	LinkWidth   string
	TrimSupport bool
	SmartStatus string
}

// DiskID returns a short platform-appropriate disk identifier.
func (h *HardwareInfo) DiskID() string {
	return h.DeviceID
}

// QuickHardwareInfo returns a minimal HardwareInfo with just the DeviceID populated.
// Fast enough to call synchronously — runs a single diskutil info command.
func QuickHardwareInfo(mountPath string) *HardwareInfo {
	info := &HardwareInfo{}
	if id, err := getDeviceID(mountPath); err == nil {
		info.DeviceID = id
	}
	return info
}

// GetHardwareInfo attempts to retrieve hardware information for the given mount path.
func GetHardwareInfo(mountPath string) (*HardwareInfo, error) {
	info := &HardwareInfo{}

	// Filesystem stats
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPath, &stat); err != nil {
		return nil, err
	}
	info.FilesystemBytes = int64(stat.Blocks) * int64(stat.Bsize)
	info.BlockSize = int64(stat.Bsize)

	// Device ID from diskutil
	deviceID, err := getDeviceID(mountPath)
	if err != nil {
		return info, nil
	}
	info.DeviceID = deviceID

	// Parent disk (disk4s1 -> disk4)
	parentDisk := deviceID
	if m := reParentDisk.FindStringSubmatch(deviceID); len(m) >= 2 {
		parentDisk = m[1]
	}

	// Enrich from diskutil
	if du, err := getDiskUtilInfo(deviceID, parentDisk); err == nil {
		info.DeviceBytes = du.TotalSize
		info.IsRemovable = du.Removable
		info.Protocol = du.Protocol
		info.VolumeUUID = du.VolumeUUID
		info.FileSystem = du.FileSystem
		info.PartitionScheme = du.PartitionScheme
		info.ReadOnly = du.ReadOnly
	}

	// Enrich from system_profiler (NVMe/PCIe cards — CFexpress, XQD)
	if sp, err := getNVMeInfo(parentDisk); err == nil {
		info.Model = sp.Model
		info.Serial = sp.Serial
		info.Firmware = sp.Firmware
		info.LinkSpeed = sp.LinkSpeed
		info.LinkWidth = sp.LinkWidth
		info.TrimSupport = sp.TrimSupport
		info.SmartStatus = sp.SmartStatus
	}

	return info, nil
}

// diskUtilInfo holds parsed diskutil output.
type diskUtilInfo struct {
	TotalSize       int64
	Removable       bool
	Protocol        string
	VolumeUUID      string
	FileSystem      string
	PartitionScheme string
	ReadOnly        bool
}

func getDeviceID(mountPath string) (string, error) {
	out, err := exec.Command("diskutil", "info", mountPath).Output()
	if err != nil {
		return "", err
	}
	if m := reDeviceID.FindStringSubmatch(string(out)); len(m) >= 2 {
		return m[1], nil
	}
	return "", fmt.Errorf("device identifier not found")
}

func getDiskUtilInfo(deviceID, parentDisk string) (*diskUtilInfo, error) {
	info := &diskUtilInfo{}

	// Parent disk for physical properties
	out, err := exec.Command("diskutil", "info", parentDisk).Output()
	if err != nil {
		return nil, err
	}
	output := string(out)

	if m := reDiskSize.FindStringSubmatch(output); len(m) >= 2 {
		info.TotalSize, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := reRemovable.FindStringSubmatch(output); len(m) >= 2 {
		info.Removable = m[1] == "Yes" || m[1] == "Removable"
	}
	if m := reProtocol.FindStringSubmatch(output); len(m) >= 2 {
		info.Protocol = strings.TrimSpace(m[1])
	}

	if m := reContent.FindStringSubmatch(output); len(m) >= 2 {
		info.PartitionScheme = strings.TrimSpace(m[1])
	}

	// Partition for volume properties
	out, err = exec.Command("diskutil", "info", deviceID).Output()
	if err != nil {
		return info, nil
	}
	output = string(out)

	if m := reVolumeUUID.FindStringSubmatch(output); len(m) >= 2 {
		info.VolumeUUID = m[1]
	}

	if m := reFS.FindStringSubmatch(output); len(m) >= 2 {
		info.FileSystem = strings.TrimSpace(m[1])
	}

	if m := reRO.FindStringSubmatch(output); len(m) >= 2 {
		info.ReadOnly = m[1] == "Yes"
	}

	return info, nil
}

// nvmeInfo holds parsed system_profiler NVMe output.
type nvmeInfo struct {
	Model       string
	Serial      string
	Firmware    string
	LinkSpeed   string
	LinkWidth   string
	TrimSupport bool
	SmartStatus string
}

// getNVMeInfo queries system_profiler SPNVMeDataType and finds the entry
// matching the given BSD name (e.g. "disk4").
func getNVMeInfo(bsdName string) (*nvmeInfo, error) {
	out, err := exec.Command("system_profiler", "SPNVMeDataType").Output()
	if err != nil {
		return nil, err
	}

	// Split into per-device blocks on BSD Name lines.
	// Each block starts with "BSD Name: diskN".
	blocks := strings.Split(string(out), "\n\n")
	var target string
	for _, block := range blocks {
		if strings.Contains(block, "BSD Name: "+bsdName) {
			target = block
			break
		}
	}
	if target == "" {
		return nil, fmt.Errorf("BSD name %s not found in NVMe data", bsdName)
	}

	info := &nvmeInfo{}

	re := func(pattern *regexp.Regexp) string {
		m := pattern.FindStringSubmatch(target)
		if len(m) >= 2 {
			return strings.TrimSpace(m[1])
		}
		return ""
	}

	info.Model = re(reNVMeModel)
	info.Serial = re(reNVMeSerial)
	info.Firmware = re(reNVMeFirmware)
	info.LinkSpeed = re(reNVMeSpeed)
	info.LinkWidth = re(reNVMeWidth)
	info.SmartStatus = re(reNVMeSmart)

	trim := re(reNVMeTrim)
	info.TrimSupport = strings.EqualFold(trim, "yes")

	return info, nil
}

// FormatHardwareInfo returns a formatted string with hardware details.
func FormatHardwareInfo(info *HardwareInfo) string {
	if info == nil {
		return "Hardware info unavailable"
	}

	var lines []string
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf("  %-18s%s", label, value))
		}
	}

	add("Device:", info.DeviceID)
	add("Model:", info.Model)
	add("Serial:", info.Serial)
	add("Firmware:", info.Firmware)
	add("Protocol:", info.Protocol)
	add("Link Speed:", info.LinkSpeed)
	add("Link Width:", info.LinkWidth)
	add("File System:", info.FileSystem)
	add("Partition Map:", info.PartitionScheme)

	if info.DeviceBytes > 0 {
		add("Raw Size:", fsutil.FormatBytes(info.DeviceBytes))
	}
	if info.FilesystemBytes > 0 {
		add("Volume Size:", fsutil.FormatBytes(info.FilesystemBytes))
	}

	if info.TrimSupport {
		add("TRIM:", "Supported")
	}
	if info.SmartStatus != "" {
		add("S.M.A.R.T.:", info.SmartStatus)
	}
	if info.ReadOnly {
		add("Read-Only:", "Yes")
	}
	add("Volume UUID:", info.VolumeUUID)

	return strings.Join(lines, "\n")
}
