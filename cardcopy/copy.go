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

	"github.com/illwill/cardbot/fsutil"
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
		return nil, fmt.Errorf("walking DCIM: %w", err)
	}

	if len(files) == 0 {
		return &Result{DestPath: opts.DestBase}, nil
	}

	// Sort files by capture time for chronological sequence numbering.
	// When shooting bursts, this ensures sequence numbers reflect actual shot order
	// even if files are scattered across multiple DCIM subfolders.
	sortFilesByCaptureTime(files)

	// Compute rename mappings for progress reporting and dry-run preview.
	namingMode := isTimestampMode(opts.NamingMode)
	seq := 1

	// Pre-compute all destination paths for dry-run and progress reporting.
	destPaths := make([]string, len(files))
	for i := range files {
		f := &files[i]
		destRelPath := f.relPath
		if namingMode {
			destRelPath = renamedRelativePath(f.relPath, f.captureTime, seq, SequenceDigits)
			seq++
			if seq > sequenceMax {
				seq = 1 // Loop back to 0001 after 9999
			}
		}
		destPaths[i] = destRelPath
	}

	if opts.DryRun {
		// Report all mappings via progress callback for dry-run preview.
		if onProgress != nil {
			for i, f := range files {
				onProgress(Progress{
					FilesDone:   i,
					FilesTotal:  len(files),
					BytesDone:   0,
					BytesTotal:  totalBytes,
					CurrentFile: destPaths[i],
					SourceFile:  f.relPath, // For dry-run rename preview
				})
			}
		}
		return &Result{
			FilesCopied: len(files),
			BytesCopied: totalBytes,
			DestPath:    opts.DestBase,
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
	bytesRequired, err := bytesRequiredForCopy(files, destPaths, opts.DestBase)
	if err != nil {
		return &Result{DestPath: opts.DestBase}, err
	}
	if free, ok := diskFreeBytes(opts.DestBase); ok && free < bytesRequired {
		return nil, fmt.Errorf("not enough space on destination: need %s, only %s free",
			fsutil.FormatBytes(bytesRequired), fsutil.FormatBytes(free))
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

	for i := range files {
		// Check for cancellation before each file.
		select {
		case <-ctx.Done():
			return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase), ctx.Err()
		default:
		}

		f := &files[i]
		destPath, err := safeDestPath(opts.DestBase, f.date, destPaths[i])
		if err != nil {
			return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase), err
		}

		// Check if destination already exists with correct size (skip).
		if info, statErr := os.Stat(destPath); statErr == nil && info.Size() == f.size {
			filesSkipped++
			bytesSkipped += f.size
			bytesDone += f.size
			filesDone++
			continue
		}

		// Reset intra-file counter for this file.
		fileByteCounter.Store(0)

		if onProgress != nil {
			now := time.Now()
			bps := tracker.sample(now, bytesDone)
			remaining := totalBytes - bytesDone
			onProgress(Progress{
				FilesDone:   filesDone,
				FilesTotal:  len(files),
				BytesDone:   bytesDone,
				BytesTotal:  totalBytes,
				CurrentFile: destPaths[i],
				SmoothedBPS: bps,
				ETASeconds:  tracker.eta(remaining),
			})
		}

		if err := copyFileCtx(ctx, destPath, f.srcPath, f.size, buf, madeDir, &fileByteCounter); err != nil {
			return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase),
				fmt.Errorf("copying %s: %w", f.relPath, err)
		}

		// Full verification: read back and compare bytes against source.
		if fullVerify {
			if err := verifyBytes(f.srcPath, destPath, buf); err != nil {
				return partialResult(filesDone, filesSkipped, bytesDone, bytesSkipped, start, opts.DestBase),
					fmt.Errorf("verification failed for %s: %w", f.relPath, err)
			}
		}

		bytesDone += f.size
		filesDone++
	}

	// Final progress
	if onProgress != nil {
		now := time.Now()
		bps := tracker.sample(now, bytesDone)
		onProgress(Progress{
			FilesDone:   filesDone,
			FilesTotal:  len(files),
			BytesDone:   bytesDone,
			BytesTotal:  totalBytes,
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
		Warnings:     walkWarnings,
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

func bytesRequiredForCopy(files []fileEntry, destPaths []string, destBase string) (int64, error) {
	var required int64
	for i := range files {
		destPath, err := safeDestPath(destBase, files[i].date, destPaths[i])
		if err != nil {
			return 0, err
		}
		if info, err := os.Stat(destPath); err == nil && info.Size() == files[i].size {
			continue
		}
		required += files[i].size
	}
	return required, nil
}

// partialResult builds a Result from in-progress counters.
func partialResult(files, skipped int, bytes, bytesSkipped int64, start time.Time, dest string) *Result {
	return &Result{
		FilesCopied:  files - skipped,
		FilesSkipped: skipped,
		BytesCopied:  bytes - bytesSkipped,
		BytesSkipped: bytesSkipped,
		Elapsed:      time.Since(start),
		DestPath:     dest,
	}
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
