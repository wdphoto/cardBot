// Package cardcopy handles file copying from memory cards to the destination.
package cardcopy

import (
	"bytes"
	"context"
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
	Skip          bool
}

// fileEntry holds a file to be copied.
type fileEntry struct {
	srcPath     string // absolute source path on card
	relPath     string // relative path from DCIM (e.g. "100NIKON/DSC_0001.NEF")
	size        int64
	date        string    // YYYY-MM-DD for folder grouping
	captureTime time.Time // EXIF capture time (fallback: mtime)
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

		// Apply selective copy filter if provided.
		if opts.Filter != nil {
			// Extract extension (uppercase, no dot)
			ext := filepath.Ext(d.Name())
			if len(ext) > 0 {
				ext = strings.ToUpper(ext[1:])
			}
			if !opts.Filter(rel, ext) {
				return nil
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
		})
		totalBytes += info.Size()
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("walking DCIM: %w", err)
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
	if len(files) == 0 {
		return plan, nil
	}

	// Compute rename mappings for progress reporting and dry-run preview.
	namingMode := isTimestampMode(opts.NamingMode)
	if namingMode && len(files) > sequenceMax {
		return nil, fmt.Errorf("timestamp naming supports at most %d files per copy; got %d", sequenceMax, len(files))
	}
	seq := 1

	// Pre-compute all destination paths for dry-run and progress reporting.
	seenDest := make(map[string]string, len(files))
	for i := range files {
		f := &files[i]
		destRelPath := f.relPath
		if namingMode {
			destRelPath = renamedRelativePath(f.relPath, f.captureTime, seq, SequenceDigits)
			seq++
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
		}
		if !opts.DryRun {
			planned.Skip = shouldSkipExisting(planned, fullVerify, opts.BufferKB)
			if !planned.Skip {
				plan.BytesRequired += f.size
			}
		}
		plan.Files = append(plan.Files, planned)
	}

	return plan, nil
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
		if f.Skip {
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

func shouldSkipExisting(f PlannedFile, fullVerify bool, bufferKB int) bool {
	info, err := os.Stat(f.DestPath)
	if err != nil || info.Size() != f.Size {
		return false
	}
	if !fullVerify {
		return true
	}
	buf := make([]byte, bufferKB*1024)
	return verifyBytes(f.SourcePath, f.DestPath, buf) == nil
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
	df, err := os.Create(partPath)
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

	if err := os.Rename(partPath, dst); err != nil {
		os.Remove(partPath)
		return fmt.Errorf("rename: %w", err)
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
