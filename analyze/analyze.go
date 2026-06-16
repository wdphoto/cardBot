// Package analyze handles card content analysis.
// Walks the DCIM tree, groups files by date, collects per-date stats,
// extracts camera model and star ratings via EXIF/XMP.
package analyze

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
	"sync"
	"sync/atomic"
	"time"

	"github.com/evanoberholster/imagemeta"
	"github.com/wdphoto/cardBot/fsutil"
)

// supportedExif lists extensions we attempt EXIF extraction on (uppercase, no dot).
var supportedExif = map[string]bool{
	"JPG":  true,
	"JPEG": true,
	"NEF":  true,
	"NRW":  true,
	"CR2":  true,
	"CR3":  true,
	"CRW":  true,
	"ARW":  true,
	"SRF":  true,
	"SR2":  true,
	"RAF":  true,
	"ORF":  true,
	"RW2":  true,
	"DNG":  true,
	"PEF":  true,
	"TIFF": true,
	"TIF":  true,
	"HEIC": true,
	"HEIF": true,
}

// IsPhoto returns true if the extension belongs to a known photo format.
// Ext should be uppercase without the dot (e.g., "NEF").
func IsPhoto(ext string) bool {
	return photoExts[ext]
}

// IsVideo returns true if the extension belongs to a known video format.
// Ext should be uppercase without the dot (e.g., "MOV").
func IsVideo(ext string) bool {
	return videoExts[ext]
}

// DateGroup holds stats for all files on a single date.
type DateGroup struct {
	Date       string   // YYYY-MM-DD
	Size       int64    // Total bytes
	FileCount  int      // Number of files
	Extensions []string // Unique extensions, sorted alphabetically, uppercase
}

// Result contains analysis data for a memory card.
type Result struct {
	Groups        []DateGroup          // Newest first
	FileDates     map[string]string    // Per-file date map: relative path from DCIM → "YYYY-MM-DD"
	FileDateTimes map[string]time.Time // Per-file capture time: relative path from DCIM → timestamp
	FileRatings   map[string]int       // Per-file rating: relative path from DCIM → star rating (1-5)
	TotalSize     int64                // Sum of all file sizes
	FileCount     int                  // Total number of files
	PhotoSize     int64                // Total bytes of photo files
	PhotoCount    int                  // Number of photo files
	VideoSize     int64                // Total bytes of video files
	VideoCount    int                  // Number of video files
	Bodies        []string             // Deduplicated camera bodies from EXIF (e.g. ["NIKON Z 9"])
	Lenses        []string             // Deduplicated lens names from EXIF (e.g. ["NIKKOR Z 24-70mm f/2.8 S"])
	Starred       int                  // Count of files with star rating > 0
	Warnings      []string             // Non-fatal errors encountered during scan (permission, I/O)
}

// videoExts lists extensions classified as video.
var videoExts = map[string]bool{
	"MOV":  true,
	"MP4":  true,
	"AVI":  true,
	"MXF":  true,
	"MTS":  true,
	"M2TS": true,
	"R3D":  true,
	"BRAW": true,
}

// photoExts lists extensions classified as photos (RAW + compressed images).
var photoExts = map[string]bool{
	"NEF":  true,
	"NRW":  true,
	"CR2":  true,
	"CR3":  true,
	"CRW":  true,
	"ARW":  true,
	"SRF":  true,
	"SR2":  true,
	"RAF":  true,
	"ORF":  true,
	"RW2":  true,
	"DNG":  true,
	"PEF":  true,
	"3FR":  true,
	"IIQ":  true,
	"JPG":  true,
	"JPEG": true,
	"TIF":  true,
	"TIFF": true,
	"HEIC": true,
	"HEIF": true,
	"PNG":  true,
}

// ProgressFunc is called periodically during analysis with the current file count.
type ProgressFunc func(count int)

// Analyzer scans a card's DCIM directory.
type Analyzer struct {
	cardPath   string
	workers    int
	onProgress ProgressFunc
}

// New creates a new analyzer for the given card path.
// Default is 1 worker (sequential). Use SetWorkers to enable parallel EXIF.
func New(cardPath string) *Analyzer {
	return &Analyzer{cardPath: cardPath, workers: 1}
}

// SetWorkers sets the number of parallel EXIF worker goroutines.
func (a *Analyzer) SetWorkers(n int) {
	if n < 1 {
		n = 1
	}
	a.workers = n
}

// OnProgress sets a callback invoked during the walk with the running file count.
func (a *Analyzer) OnProgress(fn ProgressFunc) {
	a.onProgress = fn
}

// fileEntry holds metadata collected during the fast directory walk.
type fileEntry struct {
	path    string
	relPath string // relative to DCIM directory
	size    int64
	ext     string // uppercase, no dot
	mtime   time.Time
}

// exifResult holds the output from a single EXIF worker.
type exifResult struct {
	date   time.Time
	body   string
	lens   string
	rating int
	ok     bool
}

// Analyze walks the DCIM tree and returns content stats grouped by date.
//
// Phase 1: Fast directory walk — collects paths, sizes, extensions, mtimes.
// Phase 2: Parallel EXIF extraction — N workers decode date, camera, rating.
// Phase 3: Merge — combines walk data with EXIF results into grouped stats.
//
// The context enables clean cancellation if the card is removed mid-scan.
// Returns an empty Result (not nil) if the card has no files.
// Returns an error only if DCIM cannot be read at all.
func (a *Analyzer) Analyze(ctx context.Context) (*Result, error) {
	dcim := filepath.Join(a.cardPath, "DCIM")
	if _, err := os.Stat(dcim); err != nil {
		return nil, err
	}

	// --- Phase 1: Fast directory walk ---
	var files []fileEntry
	var warnings []string
	err := filepath.WalkDir(dcim, func(path string, d os.DirEntry, err error) error {
		// Check for cancellation.
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
				warnings = append(warnings, fmt.Sprintf("%s: %v", rel, err))
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
		// Match copy behavior: do not follow symlinks on untrusted card media.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		ext := normalizeExt(filepath.Ext(d.Name()))
		if ext == "" || (!photoExts[ext] && !videoExts[ext]) {
			return nil
		}

		rel, _ := filepath.Rel(dcim, path)
		files = append(files, fileEntry{
			path:    path,
			relPath: rel,
			size:    info.Size(),
			ext:     ext,
			mtime:   info.ModTime(),
		})
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	// --- Phase 2: Parallel EXIF extraction ---
	// Build lookup of EXIF-eligible files and fan out to workers.
	exifFiles := make(chan int, 256)
	exifResults := make([]exifResult, len(files))

	var processed atomic.Int64
	onProgress := a.onProgress
	var progressMu sync.Mutex
	reportProgress := func(count int) {
		if onProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		onProgress(count)
	}

	var wg sync.WaitGroup
	for w := 0; w < a.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			xmpBuf := make([]byte, xmpBufSize)
			for idx := range exifFiles {
				// Skip expensive EXIF reads if cancelled.
				select {
				case <-ctx.Done():
					continue
				default:
				}
				f := &files[idx]
				date, body, lens, rating, ok := readExif(f.path, xmpBuf)
				exifResults[idx] = exifResult{
					date:   date,
					body:   body,
					lens:   lens,
					rating: rating,
					ok:     ok,
				}
				n := processed.Add(1)
				if n%100 == 0 {
					reportProgress(int(n))
				}
			}
		}()
	}

	// Send EXIF-eligible files to workers; non-EXIF files need no processing.
loop:
	for i := range files {
		if supportedExif[files[i].ext] {
			select {
			case <-ctx.Done():
				break loop
			case exifFiles <- i:
			}
		}
	}
	close(exifFiles)
	wg.Wait()

	// Check for cancellation after EXIF phase.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Fire final progress with total count.
	totalFiles := len(files)
	reportProgress(totalFiles)

	// --- Phase 3: Merge ---
	groups := make(map[string]*dateAccumulator)
	fileDates := make(map[string]string, len(files))
	fileDateTimes := make(map[string]time.Time, len(files))
	fileRatings := make(map[string]int)
	bodySet := make(map[string]bool)
	lensSet := make(map[string]bool)
	var totalSize, photoSize, videoSize int64
	var photoCount, videoCount, starred int

	for i := range files {
		f := &files[i]

		captureTime := f.mtime
		date := captureTime.Format("2006-01-02")

		if supportedExif[f.ext] {
			r := &exifResults[i]
			if r.ok {
				if !r.date.IsZero() {
					captureTime = r.date
					date = r.date.Format("2006-01-02")
				}
				if r.body != "" {
					bodySet[r.body] = true
				}
				if r.lens != "" {
					lensSet[r.lens] = true
				}
				if r.rating > 0 {
					starred++
					fileRatings[f.relPath] = r.rating
				}
			}
		}

		fileDates[f.relPath] = date
		fileDateTimes[f.relPath] = captureTime

		acc, ok := groups[date]
		if !ok {
			acc = &dateAccumulator{exts: make(map[string]bool)}
			groups[date] = acc
		}
		acc.size += f.size
		acc.count++
		acc.exts[f.ext] = true

		totalSize += f.size
		if videoExts[f.ext] {
			videoSize += f.size
			videoCount++
		} else if photoExts[f.ext] {
			photoSize += f.size
			photoCount++
		}
	}

	return &Result{
		Groups:        buildGroups(groups),
		FileDates:     fileDates,
		FileDateTimes: fileDateTimes,
		FileRatings:   fileRatings,
		TotalSize:     totalSize,
		FileCount:     totalFiles,
		PhotoSize:     photoSize,
		PhotoCount:    photoCount,
		VideoSize:     videoSize,
		VideoCount:    videoCount,
		Bodies:        sortedKeys(bodySet),
		Lenses:        sortedKeys(lensSet),
		Starred:       starred,
		Warnings:      warnings,
	}, nil
}

// xmpBufSize is the size of the reusable buffer for XMP rating scans.
// XMP is typically embedded in the first 256KB of RAW files (NEF, CR2, ARW, etc.).
const xmpBufSize = 256 * 1024

// readExif opens a file and extracts date, camera body, lens, and star rating.
// The caller-provided xmpBuf is reused across calls to avoid per-file allocations.
// The file is read once: first xmpBufSize bytes are read into xmpBuf for XMP scanning,
// then the file is seeked back to 0 for EXIF decoding.
// Returns ok=false if EXIF cannot be read (not an error — file is still counted).
func readExif(path string, xmpBuf []byte) (date time.Time, body string, lens string, rating int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, "", "", 0, false
	}
	defer f.Close()

	// Read the head of the file for XMP scanning before EXIF decode.
	// This avoids a second read/seek after imagemeta.Decode consumes the reader.
	n, _ := f.Read(xmpBuf)
	xmpData := xmpBuf[:n]

	// Seek back for EXIF decode.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return time.Time{}, "", "", 0, false
	}

	exif, err := imagemeta.Decode(f)
	if err != nil {
		return time.Time{}, "", "", 0, false
	}

	// Camera body: use raw EXIF Model string as-is.
	body = strings.TrimSpace(exif.Model)

	// Lens: use raw EXIF LensModel string as-is.
	lens = strings.TrimSpace(exif.LensModel)

	// Star rating: check EXIF tag first, then scan XMP from the already-read buffer.
	rating = int(exif.Rating)
	if rating == 0 {
		rating = scanXMPRating(xmpData)
	}

	dto := exif.DateTimeOriginal()
	return dto, body, lens, rating, true
}

// xmpRatingPrefix is the byte sequence before the rating digit in XMP.
var xmpRatingPrefix = []byte("<xmp:Rating>")

// scanXMPRating searches a byte slice for an embedded XMP rating.
// The Nikon Z9 (and others) store star ratings in XMP embedded in the file,
// not in the EXIF Rating tag. Format: <xmp:Rating>N</xmp:Rating>
// Returns 0 if no rating found or rating is 0.
func scanXMPRating(buf []byte) int {
	idx := bytes.Index(buf, xmpRatingPrefix)
	if idx < 0 {
		return 0
	}
	pos := idx + len(xmpRatingPrefix)
	if pos >= len(buf) {
		return 0
	}
	ch := buf[pos]
	if ch >= '1' && ch <= '5' {
		return int(ch - '0')
	}
	return 0
}

// dateAccumulator collects stats while walking.
type dateAccumulator struct {
	size  int64
	count int
	exts  map[string]bool
}

// buildGroups converts the accumulator map into a sorted slice (newest first).
func buildGroups(m map[string]*dateAccumulator) []DateGroup {
	groups := make([]DateGroup, 0, len(m))
	for date, acc := range m {
		exts := make([]string, 0, len(acc.exts))
		for ext := range acc.exts {
			exts = append(exts, ext)
		}
		sort.Strings(exts)

		groups = append(groups, DateGroup{
			Date:       date,
			Size:       acc.size,
			FileCount:  acc.count,
			Extensions: exts,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Date > groups[j].Date
	})
	return groups
}

// sortedKeys returns the keys of a bool map sorted alphabetically.
func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// normalizeExt returns the uppercase extension without the leading dot.
func normalizeExt(ext string) string {
	if ext == "" {
		return ""
	}
	return strings.ToUpper(ext[1:])
}
