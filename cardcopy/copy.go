// Package cardcopy handles file copying from memory cards to the destination.
package cardcopy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wdphoto/cardBot/fsutil"
)

// ErrDestinationConflict is returned when a planned destination already exists
// but cannot be safely treated as the same file.
var ErrDestinationConflict = errors.New("destination conflict")

// ProgressFunc is called periodically during the copy with current stats.
type ProgressFunc func(stats Progress)

// Progress holds real-time copy stats for the callback.
type Progress struct {
	FilesDone   int
	FilesTotal  int
	BytesDone   int64
	BytesTotal  int64
	CurrentFile string  // relative destination path being copied
	SourceFile  string  // relative source path (for dry-run rename preview)
	SmoothedBPS float64 // EMA-smoothed bytes per second (-1 if not yet available)
	ETASeconds  float64 // estimated seconds remaining (-1 if not yet available)
}

// Result holds the final outcome of a copy operation.
// On a cancelled or failed copy, FilesCopied/BytesCopied reflect files that
// completed successfully before the interruption.
type Result struct {
	FilesCopied  int
	FilesSkipped int
	BytesCopied  int64
	BytesSkipped int64
	Elapsed      time.Duration
	DestPath     string
	Warnings     []string // Non-fatal errors encountered during walk (permission, I/O)
	VerifyMethod string   // Verification method used: "size" or "full"
}

// Options configures the copy operation.
type Options struct {
	CardPath      string                                // Source card mount point
	DestBase      string                                // Base destination directory (e.g. ~/Pictures/cardBot)
	BufferKB      int                                   // Copy buffer size in KB (default 256)
	DryRun        bool                                  // If true, walk and report but don't copy
	FileDates     map[string]string                     // EXIF dates: relative path from DCIM → "YYYY-MM-DD"
	FileDateTimes map[string]time.Time                  // EXIF capture times: relative path from DCIM → timestamp
	Filter        func(relPath string, ext string) bool // If provided, skip files where func returns false
	NamingMode    string                                // "original" (default) or "timestamp"
	VerifyMode    string                                // "size" (default) or "full" (byte-level read-back)
}

// Plan describes the copy work to be performed after walking the card and
// resolving destination paths. It is safe to inspect without writing files.
type Plan struct {
	Options       Options
	Files         []PlannedFile
	TotalBytes    int64
	BytesRequired int64
	Warnings      []string
	VerifyMethod  string
}

// PlannedFile is one source file and its resolved destination.
type PlannedFile struct {
	SourcePath    string
	SourceRelPath string
	DestRelPath   string
	DestPath      string
	Size          int64
	Date          string
	CaptureTime   time.Time
	Action        PlannedAction
}

// PlannedAction describes how Execute should handle a planned file.
type PlannedAction string

const (
	ActionCopy          PlannedAction = "copy"
	ActionSkipSizeMatch PlannedAction = "skip-size-match"
	ActionSkipIdentical PlannedAction = "skip-identical"
)

// fileEntry holds a file to be copied.
type fileEntry struct {
	srcPath     string // absolute source path on card
	relPath     string // relative path from DCIM (e.g. "100NIKON/DSC_0001.NEF")
	size        int64
	date        string    // YYYY-MM-DD for folder grouping
	captureTime time.Time // EXIF capture time (fallback: mtime)
	selected    bool      // selected by the requested copy filter
	ext         string    // uppercase extension without the leading dot
	assetKey    string    // normalized directory + basename without extension
}

var sidecarExts = map[string]bool{
	"XMP": true, "WAV": true, "THM": true, "LRV": true, "AAE": true,
}

// Run executes the copy operation.
// ctx may be cancelled to abort mid-copy; a partial *Result is always returned
// alongside any error so the caller knows how many files completed.
func Run(ctx context.Context, opts Options, onProgress ProgressFunc) (*Result, error) {
	plan, err := PlanCopy(ctx, opts)
	if err != nil {
		return &Result{DestPath: strings.TrimSpace(opts.DestBase)}, err
	}
	return Execute(ctx, plan, onProgress)
}

// PlanCopy walks the card, applies filters and naming rules, and resolves every
// destination path before any file is written.
func PlanCopy(ctx context.Context, opts Options) (*Plan, error) {
	if opts.BufferKB <= 0 {
		opts.BufferKB = 256
	}

	cardPath, destBase, err := normalizeCopyRoots(opts.CardPath, opts.DestBase)
	if err != nil {
		return nil, err
	}
	opts.CardPath = cardPath
	opts.DestBase = destBase

	dcim := filepath.Join(opts.CardPath, "DCIM")
	if _, err := os.Stat(dcim); err != nil {
		return nil, fmt.Errorf("no DCIM folder found on card")
	}

	// Build EXIF lookups from analyze result if available.
	var exifDates map[string]string
	var exifDateTimes map[string]time.Time
	exifDates = opts.FileDates
	exifDateTimes = opts.FileDateTimes

	// --- Phase 1: Collect files ---
	var files []fileEntry
	var totalBytes int64
	var selectedFiles int
	var walkWarnings []string

	err = filepath.WalkDir(dcim, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			// Log permission/IO errors but keep walking.
			// Broken symlinks and not-exist are silently skipped.
			if !os.IsNotExist(err) {
				rel, _ := filepath.Rel(dcim, path)
				walkWarnings = append(walkWarnings, fmt.Sprintf("%s: %v", rel, err))
			}
			return nil
		}
		if d.IsDir() {
			if fsutil.IsHidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if fsutil.IsHidden(d.Name()) {
			return nil
		}
		// Skip symlinks — only copy real files from the card.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(dcim, path)

		// Record selective-copy eligibility, but retain every file in the list.
		// Timestamp sequence assignment is based on the complete card so a file
		// receives the same name in all/photos/videos/selects modes.
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		selected := true
		if opts.Filter != nil {
			if !opts.Filter(rel, ext) {
				selected = false
			}
		}

		// Use EXIF date/time if available, fall back to mtime.
		captureTime := info.ModTime()
		date := captureTime.Format("2006-01-02")
		if exifDate, ok := exifDates[rel]; ok {
			date = exifDate
		}
		if exifDateTime, ok := exifDateTimes[rel]; ok && !exifDateTime.IsZero() {
			captureTime = exifDateTime
		}

		files = append(files, fileEntry{
			srcPath:     path,
			relPath:     rel,
			size:        info.Size(),
			date:        date,
			captureTime: captureTime,
			selected:    selected,
			ext:         ext,
			assetKey:    assetKey(rel),
		})
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("walking DCIM: %w", err)
	}

	// Promote sidecars into the selection and metadata of their primary media.
	// RAW+JPEG pairs and their companions intentionally share one asset key.
	type assetState struct {
		selected    bool
		captureTime time.Time
		date        string
		hasPrimary  bool
	}
	assets := make(map[string]*assetState)
	for i := range files {
		f := &files[i]
		if sidecarExts[f.ext] {
			continue
		}
		state := assets[f.assetKey]
		if state == nil {
			state = &assetState{}
			assets[f.assetKey] = state
		}
		state.selected = state.selected || f.selected
		if !state.hasPrimary || f.captureTime.Before(state.captureTime) {
			state.captureTime = f.captureTime
			state.date = f.date
		}
		state.hasPrimary = true
	}
	for i := range files {
		f := &files[i]
		if sidecarExts[f.ext] {
			if state := assets[f.assetKey]; state != nil && state.hasPrimary {
				f.selected = state.selected
				f.captureTime = state.captureTime
				f.date = state.date
			}
		}
		if f.selected {
			selectedFiles++
			totalBytes += f.size
		}
	}

	// Sort files by capture time for chronological sequence numbering.
	// When shooting bursts, this ensures sequence numbers reflect actual shot order
	// even if files are scattered across multiple DCIM subfolders.
	sortFilesByCaptureTime(files)

	fullVerify := opts.VerifyMode == "full"
	verifyMethod := "size"
	if fullVerify {
		verifyMethod = "full"
	}

	plan := &Plan{
		Options:      opts,
		Files:        make([]PlannedFile, 0, len(files)),
		TotalBytes:   totalBytes,
		Warnings:     walkWarnings,
		VerifyMethod: verifyMethod,
	}
	if selectedFiles == 0 {
		return plan, nil
	}

	// Compute rename mappings for progress reporting and dry-run preview.
	namingMode := isTimestampMode(opts.NamingMode)
	assetSequence := make(map[string]int, len(files))
	nextSequence := 1
	for i := range files {
		key := files[i].assetKey
		if _, ok := assetSequence[key]; !ok {
			assetSequence[key] = nextSequence
			nextSequence++
		}
	}
	assetCount := nextSequence - 1
	if namingMode && assetCount > sequenceMax {
		return nil, fmt.Errorf("timestamp naming supports at most %d assets per copy; got %d", sequenceMax, assetCount)
	}
	// Pre-compute all destination paths for dry-run and progress reporting.
	seenDest := make(map[string]string, len(files))
	for i := range files {
		f := &files[i]
		seq := assetSequence[f.assetKey]
		if !f.selected {
			continue
		}
		destRelPath := f.relPath
		if namingMode {
			destRelPath = renamedRelativePath(f.relPath, f.captureTime, seq, SequenceDigits)
		}
		destPath, err := safeDestPath(opts.DestBase, f.date, destRelPath)
		if err != nil {
			return nil, err
		}
		if other, ok := seenDest[destPath]; ok {
			return nil, fmt.Errorf("duplicate destination path %s for %s and %s", destPath, other, f.relPath)
		}
		seenDest[destPath] = f.relPath

		planned := PlannedFile{
			SourcePath:    f.srcPath,
			SourceRelPath: f.relPath,
			DestRelPath:   destRelPath,
			DestPath:      destPath,
			Size:          f.size,
			Date:          f.date,
			CaptureTime:   f.captureTime,
			Action:        ActionCopy,
		}
		if !opts.DryRun {
			action, actionErr := classifyDestination(planned, fullVerify, opts.BufferKB)
			if actionErr != nil {
				return nil, actionErr
			}
			planned.Action = action
			if planned.Action == ActionCopy {
				plan.BytesRequired += f.size
			}
		}
		plan.Files = append(plan.Files, planned)
	}

	return plan, nil
}

func assetKey(relPath string) string {
	ext := filepath.Ext(relPath)
	stem := strings.TrimSuffix(filepath.Clean(relPath), ext)
	return strings.ToLower(stem)
}

// Execute performs a previously planned copy.
func Execute(ctx context.Context, plan *Plan, onProgress ProgressFunc) (*Result, error) {
	if plan == nil {
		return nil, fmt.Errorf("copy plan is required")
	}
	opts := plan.Options
	if len(plan.Files) == 0 {
		return &Result{DestPath: opts.DestBase, Warnings: plan.Warnings, VerifyMethod: plan.VerifyMethod}, nil
	}

	if opts.DryRun {
		// Report all mappings via progress callback for dry-run preview.
		if onProgress != nil {
			for i, f := range plan.Files {
				onProgress(Progress{
					FilesDone:   i,
					FilesTotal:  len(plan.Files),
					BytesDone:   0,
					BytesTotal:  plan.TotalBytes,
					CurrentFile: f.DestRelPath,
					SourceFile:  f.SourceRelPath,
				})
			}
		}
		return &Result{
			FilesCopied:  len(plan.Files),
			BytesCopied:  plan.TotalBytes,
			DestPath:     opts.DestBase,
			Warnings:     plan.Warnings,
			VerifyMethod: plan.VerifyMethod,
		}, nil
	}

	// Verify destination is writable.
	// Skip the probe if the directory already exists (we've written here before).
	if _, err := os.Stat(opts.DestBase); os.IsNotExist(err) {
		if err := os.MkdirAll(opts.DestBase, 0755); err != nil {
			return nil, fmt.Errorf("cannot create destination %s: %w", opts.DestBase, err)
		}
		probe := filepath.Join(opts.DestBase, ".cardbot_probe")
		if f, err := os.Create(probe); err != nil {
			return nil, fmt.Errorf("destination %s is not writable: %w", opts.DestBase, err)
		} else {
			f.Close()
			os.Remove(probe)
		}
	}

	// --- Disk space check ---
	// If we can query free space and it's clearly insufficient, fail fast.
	// Only count files that will actually be written; existing same-size files
	// are skipped during copy and do not require additional destination space.
	if free, ok := diskFreeBytes(opts.DestBase); ok && free < plan.BytesRequired {
		return nil, fmt.Errorf("not enough space on destination: need %s, only %s free",
			fsutil.FormatBytes(plan.BytesRequired), fsutil.FormatBytes(free))
	}

	// --- Phase 2: Copy ---
	fullVerify := opts.VerifyMode == "full"
	verifyMethod := "size"
	if fullVerify {
		verifyMethod = "full"
	}
	buf := make([]byte, opts.BufferKB*1024)
	var bytesDone int64
	var filesDone int
	var filesSkipped int
	var bytesSkipped int64
	start := time.Now()
	madeDir := make(map[string]bool, 32)

	// Intra-file byte counter for live progress on large files.
	var fileByteCounter atomic.Int64

	// Throughput tracker for smoothed MB/s and ETA.
	tracker := newThroughputTracker()
	tracker.start(start, 0)

	for i := range plan.Files {
		// Check for cancellation before each file.
		select {
		case <-ctx.Done():
			return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase, verifyMethod), ctx.Err()
		default:
		}

		f := &plan.Files[i]
		if f.Action != ActionCopy {
			filesSkipped++
			bytesSkipped += f.Size
			bytesDone += f.Size
			filesDone++
			continue
		}

		// Reset intra-file counter for this file.
		fileByteCounter.Store(0)

		if onProgress != nil {
			now := time.Now()
			bps := tracker.sample(now, bytesDone)
			remaining := plan.TotalBytes - bytesDone
			onProgress(Progress{
				FilesDone:   filesDone,
				FilesTotal:  len(plan.Files),
				BytesDone:   bytesDone,
				BytesTotal:  plan.TotalBytes,
				CurrentFile: f.DestRelPath,
				SmoothedBPS: bps,
				ETASeconds:  tracker.eta(remaining),
			})
		}

		if err := copyFileCtx(ctx, f.DestPath, f.SourcePath, f.Size, buf, madeDir, &fileByteCounter); err != nil {
			return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase, verifyMethod),
				fmt.Errorf("copying %s: %w", f.SourceRelPath, err)
		}

		// Full verification: read back and compare bytes against source.
		if fullVerify {
			if err := verifyBytes(f.SourcePath, f.DestPath, buf); err != nil {
				return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase, verifyMethod),
					fmt.Errorf("verification failed for %s: %w", f.SourceRelPath, err)
			}
		}

		bytesDone += f.Size
		filesDone++
	}

	// Final progress
	if onProgress != nil {
		now := time.Now()
		bps := tracker.sample(now, bytesDone)
		onProgress(Progress{
			FilesDone:   filesDone,
			FilesTotal:  len(plan.Files),
			BytesDone:   bytesDone,
			BytesTotal:  plan.TotalBytes,
			SmoothedBPS: bps,
			ETASeconds:  -1, // copy is done
		})
	}

	return &Result{
		FilesCopied:  filesDone - filesSkipped,
		FilesSkipped: filesSkipped,
		BytesCopied:  bytesDone - bytesSkipped,
		BytesSkipped: bytesSkipped,
		Elapsed:      time.Since(start),
		DestPath:     opts.DestBase,
		Warnings:     plan.Warnings,
		VerifyMethod: verifyMethod,
	}, nil
}

func normalizeCopyRoots(cardPath, destBase string) (string, string, error) {
	cardPath = strings.TrimSpace(cardPath)
	if cardPath == "" {
		return "", "", fmt.Errorf("card path is required")
	}
	destBase = strings.TrimSpace(destBase)
	if destBase == "" {
		return "", "", fmt.Errorf("destination path is required")
	}

	cardRoot, err := pathForComparison(cardPath)
	if err != nil {
		return "", "", fmt.Errorf("resolving card path: %w", err)
	}
	destRoot, err := pathForComparison(destBase)
	if err != nil {
		return "", "", fmt.Errorf("resolving destination path: %w", err)
	}

	if isSameOrWithin(cardRoot, destRoot) {
		return "", "", fmt.Errorf("destination %s is inside source card %s", destRoot, cardRoot)
	}
	return cardRoot, destRoot, nil
}

func pathForComparison(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return resolveSymlinksBestEffort(filepath.Clean(abs)), nil
}

func resolveSymlinksBestEffort(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}

	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return filepath.Clean(resolved)
			}
			return filepath.Clean(filepath.Join(resolved, rel))
		}
		next := filepath.Dir(dir)
		if next == dir {
			return filepath.Clean(path)
		}
	}
}

func isSameOrWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func safeDestPath(destBase, date, relPath string) (string, error) {
	destPath := filepath.Clean(filepath.Join(destBase, date, relPath))
	if !isSameOrWithin(destBase, destPath) {
		return "", fmt.Errorf("refusing to write outside destination: %s", destPath)
	}
	return destPath, nil
}

// partialResult builds a Result from in-progress counters.
func partialResult(files, skipped int, bytes, bytesSkipped int64, start time.Time, dest, verifyMethod string) *Result {
	return &Result{
		FilesCopied:  files - skipped,
		FilesSkipped: skipped,
		BytesCopied:  bytes - bytesSkipped,
		BytesSkipped: bytesSkipped,
		Elapsed:      time.Since(start),
		DestPath:     dest,
		VerifyMethod: verifyMethod,
	}
}

func classifyDestination(f PlannedFile, fullVerify bool, bufferKB int) (PlannedAction, error) {
	info, err := os.Lstat(f.DestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ActionCopy, nil
		}
		return "", fmt.Errorf("inspecting destination %s: %w", f.DestPath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrDestinationConflict, f.DestPath)
	}
	if info.Size() != f.Size {
		return "", fmt.Errorf("%w: %s exists with size %d; source size is %d", ErrDestinationConflict, f.DestPath, info.Size(), f.Size)
	}
	if !fullVerify {
		return ActionSkipSizeMatch, nil
	}
	buf := make([]byte, bufferKB*1024)
	if err := verifyBytes(f.SourcePath, f.DestPath, buf); err != nil {
		return "", fmt.Errorf("%w: %s differs from source: %v", ErrDestinationConflict, f.DestPath, err)
	}
	return ActionSkipIdentical, nil
}

// copyFileCtx copies a single file with size verification, atomic rename,
// and mid-file cancellation support.
//
// Writes to a temporary .part file, syncs, then renames to the final path.
// The trackingReader wraps the source to update fileBytes atomically during
// the copy (for intra-file progress) and to check ctx for cancellation every
// 4 MB (so large video files can be cancelled promptly).
//
// madeDir caches directories already created to avoid redundant MkdirAll syscalls.
func copyFileCtx(ctx context.Context, dst, src string, srcSize int64, buf []byte, madeDir map[string]bool, fileBytes *atomic.Int64) (err error) {
	dir := filepath.Dir(dst)
	if !madeDir[dir] {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		madeDir[dir] = true
	}

	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	// Write to a temporary .part file to avoid exposing half-written files.
	partPath := dst + ".part"
	df, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			df.Close()
			os.Remove(partPath)
		}
	}()

	// Wrap the source in a tracking reader for byte counting + cancellation.
	tr := &trackingReader{
		r:          sf,
		ctx:        ctx,
		counter:    fileBytes,
		checkEvery: defaultCheckEvery,
	}

	n, err := io.CopyBuffer(df, tr, buf)
	if err != nil {
		return err
	}

	if n != srcSize {
		return fmt.Errorf("size mismatch: wrote %d, expected %d", n, srcSize)
	}

	if err := df.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	if err := df.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	if err := commitNoReplace(partPath, dst); err != nil {
		os.Remove(partPath)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s was created after planning", ErrDestinationConflict, dst)
		}
		return fmt.Errorf("committing destination: %w", err)
	}

	return nil
}

// sortFilesByCaptureTime sorts files chronologically by capture time.
// Falls back to lexicographic path order if capture times are equal.
func sortFilesByCaptureTime(files []fileEntry) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].captureTime.Equal(files[j].captureTime) {
			return files[i].relPath < files[j].relPath
		}
		return files[i].captureTime.Before(files[j].captureTime)
	})
}

// verifyBytes reads source and destination files simultaneously and compares
// them byte-for-byte. Returns nil if identical, an error on any mismatch.
// Uses the provided buffer (split in half) to avoid extra allocations.
// This is faster than hashing for large media files: same I/O cost, zero
// hash overhead, and can short-circuit on first mismatch.
func verifyBytes(src, dst string, buf []byte) error {
	sf, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer sf.Close()

	df, err := os.Open(dst)
	if err != nil {
		return fmt.Errorf("opening destination: %w", err)
	}
	defer df.Close()

	// Split the buffer for simultaneous reads.
	half := len(buf) / 2
	if half < 4096 {
		half = 4096
		buf = make([]byte, half*2)
	}
	srcBuf := buf[:half]
	dstBuf := buf[half : half*2]

	for {
		sn, sErr := io.ReadFull(sf, srcBuf)
		dn, dErr := io.ReadFull(df, dstBuf)

		if sn != dn {
			return fmt.Errorf("byte count mismatch at offset: source read %d, destination read %d", sn, dn)
		}

		// Compare the bytes we read.
		if !bytes.Equal(srcBuf[:sn], dstBuf[:dn]) {
			return fmt.Errorf("content mismatch detected")
		}

		// Both reached EOF — files match.
		if sErr == io.EOF && dErr == io.EOF {
			return nil
		}
		if sErr == io.ErrUnexpectedEOF && dErr == io.ErrUnexpectedEOF {
			// Final partial block matched above; done.
			return nil
		}

		// One EOF'd but the other didn't.
		if sErr != nil && sErr != io.ErrUnexpectedEOF {
			if sErr == io.EOF {
				return fmt.Errorf("source shorter than destination")
			}
			return fmt.Errorf("reading source: %w", sErr)
		}
		if dErr != nil && dErr != io.ErrUnexpectedEOF {
			if dErr == io.EOF {
				return fmt.Errorf("destination shorter than source")
			}
			return fmt.Errorf("reading destination: %w", dErr)
		}
	}
}
