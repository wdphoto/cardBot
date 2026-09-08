package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
	"github.com/wdphoto/cardBot/fsutil"
	"github.com/wdphoto/cardBot/term"
)

const dryRunPreviewLimit = 200

// copyOutcome reports the result of a copy worker to the event loop. The event
// loop (App.Run) is the sole owner of copy lifecycle state: phase transitions,
// copiedModes, copyCancel, and the next-card flow. id identifies the copy
// session; outcomes whose id does not match the active copy are stale and are
// ignored entirely.
type copyOutcome struct {
	id            uint64
	cardPath      string
	mode          string
	destBase      string
	result        *cardcopy.Result
	err           error
	isDryRun      bool
	previewHidden int
}

// copyFiltered runs one copy worker. It reports completion on a.copyDone and
// returns; App.Run owns all lifecycle state changes. cancel is invoked on every
// completion path so the copy context's parent resources are released.
func (a *App) copyFiltered(ctx context.Context, cancel context.CancelFunc, card *detect.Card, mode, destBase string, analyzeResult *analyze.Result, copyID uint64) {
	defer cancel()
	isDryRun := a.dryRun

	// If the copy was already cancelled before the worker ran (e.g. the card
	// was removed immediately after the command), report the cancellation
	// without touching the card: no permission probe, no source access.
	if ctx.Err() != nil {
		a.copyDone <- copyOutcome{
			id:       copyID,
			cardPath: card.Path,
			mode:     mode,
			err:      ctx.Err(),
		}
		return
	}

	// Human-readable mode label for output.
	var modeLabel string
	switch mode {
	case "all":
		modeLabel = "all files"
	case "selects":
		modeLabel = "starred files"
	case "photos":
		modeLabel = "photos"
	case "videos":
		modeLabel = "videos"
	case "today":
		modeLabel = "today's photos"
	case "yesterday":
		modeLabel = "yesterday's photos"
	default:
		modeLabel = mode + " files"
	}
	if isDryRun {
		fmt.Printf("\n%s Dry-run: would copy %s to %s\n", a.TsPrefix(), modeLabel, a.cfg.Destination.Path)
	} else {
		fmt.Printf("\n%s Copying %s to %s\n", a.TsPrefix(), modeLabel, a.cfg.Destination.Path)
		fmt.Printf("%s Press [\\] to cancel\n", a.TsPrefix())
	}
	a.logf("Copy %s starting: %s → %s", mode, card.Path, destBase)

	var filter func(relPath, ext string) bool
	switch mode {
	case "photos":
		filter = func(relPath, ext string) bool { return analyze.IsPhoto(ext) }
	case "videos":
		filter = func(relPath, ext string) bool { return analyze.IsVideo(ext) }
	case "selects":
		filter = func(relPath, ext string) bool {
			return analyzeResult != nil && analyzeResult.FileRatings != nil && analyzeResult.FileRatings[relPath] > 0
		}
	case "today":
		todayStr := time.Now().Format("2006-01-02")
		filter = func(relPath, ext string) bool {
			if !analyze.IsPhoto(ext) {
				return false
			}
			return analyzeResult != nil && analyzeResult.FileDates != nil && analyzeResult.FileDates[relPath] == todayStr
		}
	case "yesterday":
		yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		filter = func(relPath, ext string) bool {
			if !analyze.IsPhoto(ext) {
				return false
			}
			return analyzeResult != nil && analyzeResult.FileDates != nil && analyzeResult.FileDates[relPath] == yesterdayStr
		}
	}
	var fileDates map[string]string
	var fileDateTimes map[string]time.Time
	if analyzeResult != nil {
		fileDates = analyzeResult.FileDates
		fileDateTimes = analyzeResult.FileDateTimes
	}
	opts := cardcopy.Options{
		CardPath:      card.Path,
		DestBase:      destBase,
		BufferKB:      a.cfg.Advanced.BufferSizeKB,
		DryRun:        isDryRun,
		FileDates:     fileDates,
		FileDateTimes: fileDateTimes,
		Filter:        filter,
		NamingMode:    a.cfg.Naming.Mode,
		VerifyMode:    a.cfg.Advanced.VerifyMode,
	}

	lastUpdate := time.Now()
	previewPrinted := 0
	previewHidden := 0

	result, copyErr := a.runCopy(ctx, opts, func(p cardcopy.Progress) {
		if isDryRun {
			if p.SourceFile == "" {
				return
			}
			a.printMu.Lock()
			if previewPrinted < dryRunPreviewLimit {
				if p.SourceFile != p.CurrentFile {
					fmt.Printf("  %s → %s\n", p.SourceFile, p.CurrentFile)
				} else {
					fmt.Printf("  %s (unchanged)\n", p.SourceFile)
				}
				previewPrinted++
			} else {
				previewHidden++
			}
			a.printMu.Unlock()
			return
		}
		now := time.Now()
		if now.Sub(lastUpdate) < 2*time.Second && p.FilesDone < p.FilesTotal {
			return
		}
		lastUpdate = now
		a.printMu.Lock()
		fmt.Printf("\r%s %s    ", term.DimTS(term.Ts()), cardcopy.FormatProgressLine(p))
		a.printMu.Unlock()
	})

	if result != nil {
		for _, w := range result.Warnings {
			a.logf("Copy warning: %s", w)
		}
	}

	a.copyDone <- copyOutcome{
		id:            copyID,
		cardPath:      card.Path,
		mode:          mode,
		destBase:      destBase,
		result:        result,
		err:           copyErr,
		isDryRun:      isDryRun,
		previewHidden: previewHidden,
	}
}

// handleCopyDone processes a copy worker's completion report. It runs in the
// event loop, which is the sole owner of copy lifecycle state. Outcomes whose
// id does not match the active copy are stale and are ignored entirely.
func (a *App) handleCopyDone(out copyOutcome) {
	a.mu.Lock()
	if out.id == 0 || out.id != a.copyID {
		// Stale outcome from a previous copy session — ignore entirely.
		a.mu.Unlock()
		return
	}
	sameCard := a.currentCard != nil && a.currentCard.Path == out.cardPath
	// The removal marker is owned by the event loop: it is authoritative even
	// if the removal was processed after the worker enqueued its completion.
	removed := a.copyRemoved
	var card *detect.Card
	if sameCard {
		card = a.currentCard
	}
	a.copyID = 0
	a.copyCancel = nil
	a.copyRemoved = false
	a.mu.Unlock()

	a.finishCopyPhase(out.cardPath)

	copied := 0
	if out.result != nil {
		copied = out.result.FilesCopied
	}

	switch {
	case removed:
		if errors.Is(out.err, context.Canceled) {
			a.printMu.Lock()
			fmt.Printf("\n%s Copy stopped — card removed. %s files copied.\n", term.DimTS(term.Ts()), term.FormatCount(copied))
			a.printMu.Unlock()
			a.logf("Copy stopped: card removed. %d files copied.", copied)
		} else if out.err != nil {
			a.logf("Copy failed before card removal: %v", out.err)
		}
	case errors.Is(out.err, context.Canceled):
		if sameCard {
			a.printMu.Lock()
			fmt.Printf("\n%s Copy cancelled — %s files copied.\n", term.DimTS(term.Ts()), term.FormatCount(copied))
			a.printMu.Unlock()
			a.logf("Copy cancelled. %d files copied.", copied)
			a.drainInput()
			a.printPrompt()
		}
	case out.err != nil:
		a.printMu.Lock()
		fmt.Printf("\n%s Copy failed: %s\n", term.DimTS(term.Ts()), term.FriendlyErr(out.err))
		if out.result != nil && out.result.FilesCopied > 0 {
			fmt.Printf("%s %s files copied before failure.\n", term.DimTS(term.Ts()), term.FormatCount(out.result.FilesCopied))
		}
		a.printMu.Unlock()
		a.logf("Copy failed: %v", out.err)
		a.drainInput()
		a.printPrompt()
	default:
		if sameCard {
			a.handleCopySuccess(card, out.mode, out.destBase, out.result, out.isDryRun, out.previewHidden)
			fmt.Println()
			a.drainInput()
			a.printPrompt()
		}
	}

	// If the card was removed during the copy, the removal handler deferred the
	// next-card flow until the worker finished. Advance now that it is done.
	a.mu.Lock()
	advance := removed && a.currentCard == nil && a.phase != phaseShuttingDown
	a.mu.Unlock()
	if advance {
		a.finishCard()
	}
}

func (a *App) handleCopySuccess(card *detect.Card, mode, destBase string, result *cardcopy.Result, isDryRun bool, previewHidden int) {
	if result == nil {
		result = &cardcopy.Result{}
	}

	elapsed := result.Elapsed.Round(time.Second)
	speed := float64(0)
	if result.Elapsed.Seconds() > 0 {
		speed = float64(result.BytesCopied) / result.Elapsed.Seconds() / (1024 * 1024)
	}

	if isDryRun {
		a.printMu.Lock()
		fmt.Printf("%s Dry-run complete ✓\n", term.DimTS(term.Ts()))
		fmt.Printf("%s %s files, %s would be copied\n",
			term.DimTS(term.Ts()),
			term.FormatCount(result.FilesCopied),
			fsutil.FormatBytes(result.BytesCopied))
		if previewHidden > 0 {
			fmt.Printf("%s ... +%s more files (preview capped at %s)\n", term.DimTS(term.Ts()), term.FormatCount(previewHidden), term.FormatCount(dryRunPreviewLimit))
		}
		a.printMu.Unlock()
		a.logf("Dry-run complete: %d files, %s would be copied", result.FilesCopied, fsutil.FormatBytes(result.BytesCopied))
		return
	}

	a.printMu.Lock()
	fmt.Printf("\r%s Copy complete ✓                                          \n", term.DimTS(term.Ts()))
	if result.FilesSkipped > 0 && result.FilesCopied == 0 {
		fmt.Printf("%s All %s files already copied. Nothing to do.\n",
			term.DimTS(term.Ts()),
			term.FormatCount(result.FilesSkipped))
	} else if result.FilesSkipped > 0 {
		fmt.Printf("%s %s files, %s copied in %s (%.1f MB/s) — %s files skipped\n",
			term.DimTS(term.Ts()),
			term.FormatCount(result.FilesCopied),
			fsutil.FormatBytes(result.BytesCopied),
			elapsed,
			speed,
			term.FormatCount(result.FilesSkipped))
	} else {
		fmt.Printf("%s %s files, %s copied in %s (%.1f MB/s)\n",
			term.DimTS(term.Ts()),
			term.FormatCount(result.FilesCopied),
			fsutil.FormatBytes(result.BytesCopied),
			elapsed,
			speed)
	}
	a.printMu.Unlock()
	a.logf("Copy complete: %d files, %s in %s (%.1f MB/s), %d skipped",
		result.FilesCopied,
		fsutil.FormatBytes(result.BytesCopied),
		elapsed,
		speed,
		result.FilesSkipped)

	dotErr := a.writeDotfile(dotfile.WriteOptions{
		CardPath:           card.Path,
		Destination:        destBase,
		Mode:               mode,
		FilesCopied:        result.FilesCopied + result.FilesSkipped,
		BytesCopied:        result.BytesCopied + result.BytesSkipped,
		Verified:           true,
		VerificationMethod: result.VerifyMethod,
		CardbotVersion:     a.version,
	})
	if dotErr != nil {
		fmt.Printf("%s Warning: ingest completed, but could not save .cardbot on card: %v\n", a.TsPrefix(), dotErr)
		a.logf("Dotfile write failed: %v", dotErr)
	} else {
		a.logf("Dotfile written to %s", card.Path)
	}

	a.mu.Lock()
	a.copiedModes[mode] = true
	a.mu.Unlock()
}
