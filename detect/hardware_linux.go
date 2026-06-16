//go:build linux

package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/wdphoto/cardBot/fsutil"
)

// HardwareInfo contains device-level information about the card.
// On Linux with direct SD slot access, CID is available.
// With USB readers, CID may not be available.
type HardwareInfo struct {
	// Device size from sysfs (may differ from filesystem size)
	DeviceBytes int64

	// Filesystem size (what Statfs reports)
	FilesystemBytes int64

	// Block size
	BlockSize int64

	// Device path (e.g., "/dev/mmcblk0p1")
	DevicePath string

	// Parent device (e.g., "mmcblk0")
	ParentDevice string

	// Volume UUID
	VolumeUUID string

	// Whether this is a removable device
	IsRemovable bool

	// Whether we could read CID
	CIDAvailable bool

	// Raw CID hex string (128 bits = 32 hex chars)
	CIDHex string

	// Parsed CID fields
	ManufacturerID    byte   // MID - 8 bits
	OEMID             string // OID - 16 bits, 2 ASCII chars
	ProductName       string // PNM - 40 bits, 5 ASCII chars
	ProductRevision   byte   // PRV - 8 bits
	ProductSerial     uint32 // PSN - 32 bits
	ManufacturingDate string // MDT - 12 bits
}

// DiskID returns a short platform-appropriate disk identifier.
func (h *HardwareInfo) DiskID() string {
	return h.DevicePath
}

// QuickHardwareInfo returns a minimal HardwareInfo with just the DevicePath populated.
// Fast enough to call synchronously — reads from sysfs.
func QuickHardwareInfo(mountPath string) *HardwareInfo {
	info := &HardwareInfo{}
	if device, err := findBlockDevice(mountPath); err == nil {
		info.DevicePath = device
	}
	return info
}

// GetHardwareInfo attempts to retrieve hardware information.
// On Linux with direct SD slot, CID is available.
func GetHardwareInfo(mountPath string) (*HardwareInfo, error) {
	info := &HardwareInfo{}

	// Get filesystem stats
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPath, &stat); err != nil {
		return nil, err
	}
	info.FilesystemBytes = int64(stat.Blocks) * int64(stat.Bsize)
	info.BlockSize = int64(stat.Bsize)

	// Find the block device for this mount
	device, err := findBlockDevice(mountPath)
	if err != nil {
		return info, nil // Return what we have
	}
	info.DevicePath = device

	// Get parent device (strip partition number)
	parent := getParentDevice(device)
	info.ParentDevice = parent

	// Check if removable
	info.IsRemovable = isRemovable(parent)

	// Try to get device size
	info.DeviceBytes = getDeviceSize(parent)

	// Try to read CID
	cidPath := filepath.Join("/sys/block", parent, "device/cid")
	cidData, err := os.ReadFile(cidPath)
	if err == nil {
		info.CIDAvailable = true
		info.CIDHex = strings.TrimSpace(string(cidData))
		parseCID(info)
	}

	// Get UUID
	info.VolumeUUID = getVolumeUUID(device)

	return info, nil
}

func findBlockDevice(mountPath string) (string, error) {
	// Read /proc/mounts to find device
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == mountPath {
			device := fields[0]
			// Convert /dev/mmcblk0p1 to mmcblk0p1 format
			if strings.HasPrefix(device, "/dev/") {
				return device[5:], nil
			}
			return device, nil
		}
	}

	return "", fmt.Errorf("device not found for %s", mountPath)
}

func getParentDevice(device string) string {
	// mmcblk0p1 -> mmcblk0
	// sda1 -> sda
	re := regexp.MustCompile(`^(mmcblk\d+|sd[a-z]+|vd[a-z]+)`)
	matches := re.FindStringSubmatch(device)
	if len(matches) >= 2 {
		return matches[1]
	}
	return device
}

func isRemovable(device string) bool {
	removablePath := filepath.Join("/sys/block", device, "removable")
	data, err := os.ReadFile(removablePath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

func getDeviceSize(device string) int64 {
	sizePath := filepath.Join("/sys/block", device, "size")
	data, err := os.ReadFile(sizePath)
	if err != nil {
		return 0
	}

	sectors, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}

	// Size is in 512-byte sectors
	return sectors * 512
}

func getVolumeUUID(device string) string {
	// Try /dev/disk/by-uuid
	entries, err := os.ReadDir("/dev/disk/by-uuid")
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/dev/disk/by-uuid", entry.Name()))
		if err != nil {
			continue
		}
		// target looks like "../../mmcblk0p1"
		if strings.Contains(target, device) {
			return entry.Name()
		}
	}

	return ""
}

// parseCID parses the 32-character hex CID string
func parseCID(info *HardwareInfo) {
	if len(info.CIDHex) != 32 {
		return
	}

	// CID structure (128 bits):
	// Bits 127-120: Manufacturer ID (MID)
	// Bits 119-104: OEM/Application ID (OID)
	// Bits 103-64:  Product Name (PNM) - 5 chars
	// Bits 63-56:   Product Revision (PRV)
	// Bits 55-24:   Product Serial Number (PSN)
	// Bits 23-20:   Reserved
	// Bits 19-8:    Manufacturing Date (MDT)
	// Bits 7-1:     CRC7 checksum
	// Bit 0:        Always 1

	// Convert hex string to bytes
	cidBytes := make([]byte, 16)
	for i := 0; i < 16; i++ {
		b, _ := strconv.ParseUint(info.CIDHex[i*2:i*2+2], 16, 8)
		cidBytes[i] = byte(b)
	}

	// Manufacturer ID (byte 0)
	info.ManufacturerID = cidBytes[0]

	// OEM ID (bytes 1-2) - 2 ASCII characters
	info.OEMID = string([]byte{cidBytes[1], cidBytes[2]})

	// Product Name (bytes 3-7) - 5 ASCII characters
	info.ProductName = string(cidBytes[3:8])

	// Product Revision (byte 8)
	info.ProductRevision = cidBytes[8]

	// Product Serial Number (bytes 9-12) - big endian
	info.ProductSerial = uint32(cidBytes[9])<<24 |
		uint32(cidBytes[10])<<16 |
		uint32(cidBytes[11])<<8 |
		uint32(cidBytes[12])

	// Manufacturing Date (bytes 13-14, upper 12 bits)
	// MDT = ((byte13 & 0x0F) << 8) | byte14
	// Year = MDT >> 4 + 2000, Month = MDT & 0x0F
	mdt := ((int(cidBytes[13]) & 0x0F) << 8) | int(cidBytes[14])
	year := (mdt >> 4) + 2000
	month := mdt & 0x0F
	if month >= 1 && month <= 12 {
		info.ManufacturingDate = fmt.Sprintf("%04d-%02d", year, month)
	}
}

// FormatHardwareInfo returns a formatted string with hardware details.
func FormatHardwareInfo(info *HardwareInfo) string {
	if info == nil {
		return "Hardware info unavailable"
	}

	var parts []string

	parts = append(parts, fmt.Sprintf("Device: /dev/%s", info.ParentDevice))

	if info.DeviceBytes > 0 {
		parts = append(parts, fmt.Sprintf("Raw Size: %s", fsutil.FormatBytes(info.DeviceBytes)))
	}

	if info.FilesystemBytes > 0 {
		parts = append(parts, fmt.Sprintf("Filesystem: %s", fsutil.FormatBytes(info.FilesystemBytes)))
	}

	if info.IsRemovable {
		parts = append(parts, "Removable: Yes")
	}

	if info.VolumeUUID != "" {
		parts = append(parts, fmt.Sprintf("UUID: %s", info.VolumeUUID))
	}

	if info.CIDAvailable {
		parts = append(parts, "CID: "+info.CIDHex)
		parts = append(parts, fmt.Sprintf("Manufacturer: 0x%02X (%s)", info.ManufacturerID, lookupManufacturer(info.ManufacturerID)))
		parts = append(parts, fmt.Sprintf("OEM: %s", info.OEMID))
		parts = append(parts, fmt.Sprintf("Product: %s", info.ProductName))
		parts = append(parts, fmt.Sprintf("Revision: %d.%d", info.ProductRevision>>4, info.ProductRevision&0x0F))
		parts = append(parts, fmt.Sprintf("Serial: 0x%08X", info.ProductSerial))
		parts = append(parts, fmt.Sprintf("Mfg Date: %s", info.ManufacturingDate))
	} else {
		parts = append(parts, "CID: Not available (USB reader or non-SD device)")
	}

	return strings.Join(parts, "\n  ")
}

// lookupManufacturer returns the manufacturer name for a given MID
func lookupManufacturer(mid byte) string {
	manufacturers := map[byte]string{
		0x01: "Panasonic",
		0x02: "Toshiba",
		0x03: "SanDisk",
		0x06: "Ritek",
		0x08: "Silicon Power",
		0x11: "Toshiba",
		0x13: "Micron",
		0x15: "Samsung",
		0x18: "Infineon",
		0x1B: "Samsung",
		0x1C: "Transcend",
		0x1D: "Adtec",
		0x1E: "Verbatim",
		0x1F: "SK Hynix",
		0x27: "Philips",
		0x28: "Lexar",
		0x30: "SanDisk",
		0x31: "Silicon Power",
		0x33: "STMicroelectronics",
		0x41: "Kingston",
		0x44: "Sony",
		0x58: "Transcend",
		0x6F: "STMicroelectronics",
		0x74: "Transcend",
		0x76: "Patriot",
		0x82: "Gobe",
		0x89: "HP",
		0x9C: "Angelbird",
	}

	if name, ok := manufacturers[mid]; ok {
		return name
	}
	return "Unknown"
}
