//go:build darwin && cgo

package detect

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework DiskArbitration -framework CoreFoundation -framework IOKit

#include <DiskArbitration/DiskArbitration.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <stdlib.h>

extern void diskAppearedCallback(DADiskRef disk, void *context);
extern void diskDisappearedCallback(DADiskRef disk, void *context);

static char* getDiskPath(DADiskRef disk) {
    CFDictionaryRef desc = DADiskCopyDescription(disk);
    if (desc == NULL) return NULL;

    CFURLRef url = CFDictionaryGetValue(desc, kDADiskDescriptionVolumePathKey);
    if (url == NULL) {
        CFRelease(desc);
        return NULL;
    }

    UInt8 path[PATH_MAX];
    if (!CFURLGetFileSystemRepresentation(url, true, path, sizeof(path))) {
        CFRelease(desc);
        return NULL;
    }

    CFRelease(desc);
    return strdup((char*)path);
}

static char* getVolumeName(DADiskRef disk) {
    CFDictionaryRef desc = DADiskCopyDescription(disk);
    if (desc == NULL) return NULL;

    CFStringRef name = CFDictionaryGetValue(desc, kDADiskDescriptionVolumeNameKey);
    char* result = NULL;
    if (name != NULL) {
        CFIndex len = CFStringGetLength(name) + 1;
        result = malloc(len);
        if (result != NULL) {
            CFStringGetCString(name, result, len, kCFStringEncodingUTF8);
        }
    }
    CFRelease(desc);
    return result;
}

static void registerCallbacks(DASessionRef session) {
    DARegisterDiskAppearedCallback(session, NULL, diskAppearedCallback, NULL);
    DARegisterDiskDisappearedCallback(session, NULL, diskDisappearedCallback, NULL);
}

static int ejectDisk(DASessionRef session, const char* path) {
    CFURLRef url = CFURLCreateFromFileSystemRepresentation(kCFAllocatorDefault, (UInt8*)path, strlen(path), true);
    if (url == NULL) return -1;

    DADiskRef disk = DADiskCreateFromVolumePath(kCFAllocatorDefault, session, url);
    CFRelease(url);
    if (disk == NULL) return -1;

    DADiskEject(disk, kDADiskEjectOptionDefault, NULL, NULL);
    CFRelease(disk);
    return 0;
}

static void scheduleOnRunLoop(DASessionRef session, CFRunLoopRef runLoop) {
    DASessionScheduleWithRunLoop(session, runLoop, kCFRunLoopDefaultMode);
}
*/
import "C"
import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

var (
	globalDetector *Detector
	detectorMu     sync.Mutex
)

// Detector monitors for memory card insertion/removal using native macOS APIs.
type Detector struct {
	session  C.DASessionRef
	runLoop  C.CFRunLoopRef
	cards    map[string]*Card
	events   chan *Card
	removals chan string
	mu       sync.RWMutex
	started  bool
}

// NewDetector creates a new card detector.
func NewDetector() *Detector {
	return &Detector{
		cards:    make(map[string]*Card),
		events:   make(chan *Card, 10),
		removals: make(chan string, 10),
	}
}

// Start begins monitoring for card insertion/removal.
func (d *Detector) Start() error {
	d.mu.RLock()
	started := d.started
	d.mu.RUnlock()
	if started {
		return fmt.Errorf("detector already started")
	}

	detectorMu.Lock()
	globalDetector = d
	detectorMu.Unlock()

	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		session := C.DASessionCreate(C.kCFAllocatorDefault)
		if session == 0 {
			detectorMu.Lock()
			if globalDetector == d {
				globalDetector = nil
			}
			detectorMu.Unlock()
			ready <- fmt.Errorf("creating disk arbitration session")
			return
		}

		runLoop := C.CFRunLoopGetCurrent()
		C.registerCallbacks(session)
		C.scheduleOnRunLoop(session, runLoop)

		d.mu.Lock()
		d.session = session
		d.runLoop = runLoop
		d.started = true
		d.mu.Unlock()

		ready <- nil

		// Scan volumes already mounted at startup; native callbacks handle future events.
		go d.scanExistingVolumes()
		C.CFRunLoopRun()
	}()

	return <-ready
}

// Stop halts card monitoring.
func (d *Detector) Stop() {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return
	}
	d.started = false
	runLoop := d.runLoop
	session := d.session
	d.mu.Unlock()

	if runLoop != 0 {
		C.CFRunLoopStop(runLoop)
	}
	if session != 0 {
		C.DASessionUnscheduleFromRunLoop(session, runLoop, C.kCFRunLoopDefaultMode)
		C.CFRelease(C.CFTypeRef(session))
	}

	detectorMu.Lock()
	globalDetector = nil
	detectorMu.Unlock()
}

// Events returns a channel for card insertion events.
func (d *Detector) Events() <-chan *Card { return d.events }

// Removals returns a channel for card removal events.
func (d *Detector) Removals() <-chan string { return d.removals }

// Eject fully ejects the volume at the given path (unmount + release hardware).
func (d *Detector) Eject(path string) error {
	d.mu.RLock()
	session := d.session
	d.mu.RUnlock()

	if session == 0 {
		return fmt.Errorf("detector not started")
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	result := C.ejectDisk(session, cPath)
	if result != 0 {
		return fmt.Errorf("failed to eject %s", path)
	}
	return nil
}

// Remove removes a card from tracking (used after programmatic eject).
func (d *Detector) Remove(path string) {
	d.mu.Lock()
	delete(d.cards, path)
	d.mu.Unlock()
}

// scanExistingVolumes performs a one-time scan of already-mounted volumes at startup.
func (d *Detector) scanExistingVolumes() {
	time.Sleep(200 * time.Millisecond)
	d.scanVolumes()
}

// deferredScan waits for a volume to finish mounting, then scans /Volumes.
// Called from a goroutine when DiskArbitration fires a disk-appeared event
// before the volume is mounted (path=nil). Retries a few times since the
// mount may take a moment after the hardware appears.
func (d *Detector) deferredScan() {
	for i := 0; i < 3; i++ {
		time.Sleep(1 * time.Second)
		if d.scanVolumes() {
			return
		}
	}
}

// scanVolumes scans /Volumes for new memory cards. Returns true if any new card was found.
func (d *Detector) scanVolumes() bool {
	found := false
	volumes, err := os.ReadDir("/Volumes")
	if err != nil {
		return false
	}

	for _, vol := range volumes {
		if !vol.IsDir() {
			continue
		}
		path := filepath.Join("/Volumes", vol.Name())

		d.mu.RLock()
		_, alreadyTracked := d.cards[path]
		d.mu.RUnlock()

		if alreadyTracked {
			continue
		}

		if isMemoryCard(path) {
			card := buildCard(path, vol.Name())
			if card != nil {
				d.mu.Lock()
				// Double-check under write lock.
				if _, exists := d.cards[path]; exists {
					d.mu.Unlock()
					continue
				}
				d.cards[path] = card
				d.mu.Unlock()
				d.events <- card
				found = true
			}
		}
	}
	return found
}

//export diskAppearedCallback
func diskAppearedCallback(disk C.DADiskRef, context unsafe.Pointer) {
	detectorMu.Lock()
	d := globalDetector
	detectorMu.Unlock()

	if d == nil {
		return
	}

	cPath := C.getDiskPath(disk)
	if cPath == nil {
		// Disk appeared but volume isn't mounted yet (common after eject + re-insert).
		// Scan /Volumes from a goroutine once the mount completes.
		go d.deferredScan()
		return
	}
	path := C.GoString(cPath)
	C.free(unsafe.Pointer(cPath))

	if !isMemoryCard(path) {
		return
	}

	name := "Untitled"
	cName := C.getVolumeName(disk)
	if cName != nil {
		name = C.GoString(cName)
		C.free(unsafe.Pointer(cName))
	}

	card := buildCard(path, name)
	if card == nil {
		return
	}

	d.mu.Lock()
	if _, exists := d.cards[path]; exists {
		d.mu.Unlock()
		return
	}
	d.cards[path] = card
	d.mu.Unlock()

	select {
	case d.events <- card:
	default:
	}
}

//export diskDisappearedCallback
func diskDisappearedCallback(disk C.DADiskRef, context unsafe.Pointer) {
	detectorMu.Lock()
	d := globalDetector
	detectorMu.Unlock()

	if d == nil {
		return
	}

	cPath := C.getDiskPath(disk)
	if cPath == nil {
		return
	}
	path := C.GoString(cPath)
	C.free(unsafe.Pointer(cPath))

	d.mu.Lock()
	if _, exists := d.cards[path]; exists {
		delete(d.cards, path)
		d.mu.Unlock()
		select {
		case d.removals <- path:
		default:
		}
	} else {
		d.mu.Unlock()
	}
}
