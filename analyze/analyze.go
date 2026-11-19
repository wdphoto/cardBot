// Package analyze handles card content analysis.
// Walks the DCIM tree, groups files by date, collects per-date stats,
// extracts camera model and star ratings via EXIF/XMP.
package analyze

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

var ratingSidecarExts = map[string]bool{"XMP": true}

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
	Warnings      []string             // Advisory diagnostics only; inventory errors are fatal.
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

// OnProgress sets a callback for EXIF processing. Intermediate counts are
// decoded attempts; the final count includes media that needed no EXIF pass.
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
	err    error
}

// Analyze walks the DCIM tree and returns content stats grouped by date.
//
// Phase 1: Fast directory walk — collects paths, sizes, extensions, mtimes.
// Phase 2: Parallel EXIF extraction — N workers decode date, camera, rating.
// Phase 3: Merge — combines walk data with EXIF results into grouped stats.
//
// The resulting maps are keyed relative to DCIM, not the mount path. The app
// uses them for selection and passes capture dates to the planner for naming.
// They are not the copy inventory: XMP can supply a rating without being a photo.
// An empty DCIM returns a non-nil Result. A failed inventory or metadata read
// returns an error rather than offering selections from an incomplete scan.
func (a *Analyzer) Analyze(ctx context.Context) (*Result, error) {
	dcim := filepath.Join(a.cardPath, "DCIM")
	files, ratingSidecars, err := collectMediaFiles(ctx, dcim)
	if err != nil {
		return nil, err
	}

	exifResults, err := a.extractEXIF(ctx, files)
	if err != nil {
		return nil, err
	}
	totalFiles := len(files)

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

		r := &exifResults[i]
		if r.err != nil {
			return nil, fmt.Errorf("reading metadata for %s: %w", f.relPath, r.err)
		}
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
			fileRatings[f.relPath] = r.rating
		}
		if fileRatings[f.relPath] == 0 {
			if sidecarPath := ratingSidecars[mediaStem(f.relPath)]; sidecarPath != "" {
				rating, err := readXMPSidecarRating(sidecarPath)
				if err != nil {
					return nil, fmt.Errorf("reading rating sidecar %s: %w", sidecarPath, err)
				}
				if rating > 0 {
					fileRatings[f.relPath] = rating
				}
			}
		}
		if fileRatings[f.relPath] > 0 {
			starred++
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
	}, nil
}

func collectMediaFiles(ctx context.Context, dcim string) ([]fileEntry, map[string]string, error) {
	var files []fileEntry
	ratingSidecars := make(map[string]string)
	err := fsutil.WalkDCIM(ctx, dcim, func(path, rel string, info fs.FileInfo) error {
		ext := normalizeExt(filepath.Ext(rel))
		if ratingSidecarExts[ext] {
			ratingSidecars[mediaStem(rel)] = path
			return nil
		}
		if ext == "" || (!photoExts[ext] && !videoExts[ext]) {
			return nil
		}
		files = append(files, fileEntry{path: path, relPath: rel, size: info.Size(), ext: ext, mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return files, ratingSidecars, nil
}

// Workers receive indices rather than appending to a shared slice. Each
// writes its own slot; after Wait, the merge pairs results with the matching
// walk entries regardless of the order in which the files finished decoding.
func (a *Analyzer) extractEXIF(ctx context.Context, files []fileEntry) ([]exifResult, error) {
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
	for range a.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			xmpBuf := make([]byte, xmpBufSize)
			for idx := range exifFiles {
				if ctx.Err() != nil {
					continue
				}
				f := &files[idx]
				exifResults[idx] = readExif(f.path, xmpBuf)
				if n := processed.Add(1); n%100 == 0 {
					reportProgress(int(n))
				}
			}
		}()
	}

sendLoop:
	for i := range files {
		if !supportedExif[files[i].ext] {
			continue
		}
		select {
		case <-ctx.Done():
			break sendLoop
		case exifFiles <- i:
		}
	}
	close(exifFiles)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	reportProgress(len(files))
	return exifResults, nil
}

// xmpBufSize is the size of the reusable buffer for XMP rating scans.
// XMP is typically embedded in the first 256KB of RAW files (NEF, CR2, ARW, etc.).
const xmpBufSize = 256 * 1024

// readExif opens a file and extracts date, camera body, lens, and star rating.
// The caller-provided xmpBuf is reused across calls to avoid per-file allocations.
// The file is read once: first xmpBufSize bytes are read into xmpBuf for XMP scanning,
// then the file is seeked back to 0 for EXIF decoding.
// Missing or unsupported EXIF is normal; filesystem I/O failures are not.
func readExif(path string, xmpBuf []byte) exifResult {
	f, err := os.Open(path)
	if err != nil {
		return exifResult{err: err}
	}
	defer f.Close()

	// Read the head of the file for XMP scanning before EXIF decode.
	// This avoids a second read/seek after imagemeta.Decode consumes the reader.
	n, err := f.Read(xmpBuf)
	if err != nil && err != io.EOF {
		return exifResult{err: err}
	}
	xmpData := xmpBuf[:n]

	// Seek back for EXIF decode.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return exifResult{err: err}
	}

	exif, err := imagemeta.Decode(f)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return exifResult{err: err}
		}
		return exifResult{}
	}

	// Camera body: use raw EXIF Model string as-is.
	body := strings.TrimSpace(exif.Model)

	// Lens: use raw EXIF LensModel string as-is.
	lens := strings.TrimSpace(exif.LensModel)

	// Star rating: check EXIF tag first, then scan XMP from the already-read buffer.
	rating := int(exif.Rating)
	if rating == 0 {
		rating = scanXMPRating(xmpData)
	}

	dto := exif.DateTimeOriginal()
	return exifResult{date: dto, body: body, lens: lens, rating: rating}
}

// xmpRatingPrefix is the byte sequence before the rating digit in XMP.
var xmpRatingPrefix = []byte("<xmp:Rating>")
var xmpRatingAttributePrefixes = [][]byte{[]byte(`xmp:Rating="`), []byte("xmp:Rating='")}

// scanXMPRating searches a byte slice for an embedded XMP rating.
// The Nikon Z9 (and others) store star ratings in XMP embedded in the file,
// not in the EXIF Rating tag. Format: <xmp:Rating>N</xmp:Rating>
// Returns 0 if no rating found or rating is 0.
func scanXMPRating(buf []byte) int {
	idx := bytes.Index(buf, xmpRatingPrefix)
	if idx >= 0 {
		pos := idx + len(xmpRatingPrefix)
		if pos < len(buf) && buf[pos] >= '1' && buf[pos] <= '5' {
			return int(buf[pos] - '0')
		}
	}
	for _, prefix := range xmpRatingAttributePrefixes {
		idx = bytes.Index(buf, prefix)
		if idx < 0 {
			continue
		}
		pos := idx + len(prefix)
		if pos < len(buf) && buf[pos] >= '1' && buf[pos] <= '5' {
			return int(buf[pos] - '0')
		}
	}
	return 0
}

func readXMPSidecarRating(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := make([]byte, xmpBufSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, err
	}
	return scanXMPRating(buf[:n]), nil
}

func mediaStem(relPath string) string {
	return strings.ToLower(strings.TrimSuffix(filepath.Clean(relPath), filepath.Ext(relPath)))
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
