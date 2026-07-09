package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
	"github.com/wdphoto/cardBot/fsutil"
	"github.com/wdphoto/cardBot/term"
)

const dryRunPreviewLimit = 200

// copyFiltered runs one copy worker. App.Run remains the sole owner of detector,
// removal, input, and shutdown events while this worker reports progress.
func (a *App) copyFiltered(card *detect.Card, mode string) {
	defer a.finishCopyPhase(card.Path)
	destBase, err := config.ExpandPath(a.cfg.Destination.Path)
	if err != nil {
		fmt.Printf("\n%s Error: %s\n", a.TsPrefix(), term.FriendlyErr(err))
		a.printPrompt()
		return
	}

	// Validate destination path.
	if destBase == "" {
		fmt.Printf("\n%s Error: no destination configured — run cardbot --setup\n", a.TsPrefix())
		a.printPrompt()
		return
	}

	isDryRun := a.dryRun

	// Warn if the card is write-protected — dotfile won't be written after copy.
	// (Skip warning in dry-run since we're not writing anyway.)
	if !isDryRun && cardIsReadOnly(card.Path) {
		fmt.Printf("\n%s Warning: card appears to be write-protected — copy status will not be saved to card\n", a.TsPrefix())
		a.logf("Card %s appears write-protected", card.Path)
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

	a.mu.Lock()
	analyzeResult := a.lastResult
	if a.currentCard != nil && a.currentCard.Path == card.Path {
		a.setPhaseLocked(phaseCopying)
	}
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	a.mu.Lock()
	a.copyCancel = cancel
	a.copyRemoved = false
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.copyCancel = nil
		a.copyRemoved = false
		a.mu.Unlock()
	}()

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
	if errors.Is(copyErr, context.Canceled) {
		copied := 0
		if result != nil {
			copied = result.FilesCopied
		}
		a.mu.Lock()
		removed := a.copyRemoved
		shuttingDown := a.phase == phaseShuttingDown
		a.mu.Unlock()
		a.printMu.Lock()
		if removed {
			fmt.Printf("\n%s Copy stopped — card removed. %d files copied.\n", term.DimTS(term.Ts()), copied)
		} else if !shuttingDown {
			fmt.Printf("\n%s Copy cancelled — %d files copied.\n", term.DimTS(term.Ts()), copied)
		}
		a.printMu.Unlock()
		if removed {
			a.logf("Copy stopped: card removed. %d files copied.", copied)
		} else if !shuttingDown {
			a.logf("Copy cancelled. %d files copied.", copied)
			a.drainInput()
			a.printPrompt()
		}
		return
	}
	if copyErr != nil {
		a.printMu.Lock()
		fmt.Printf("\n%s Copy failed: %s\n", term.DimTS(term.Ts()), term.FriendlyErr(copyErr))
		if result != nil && result.FilesCopied > 0 {
			fmt.Printf("%s %d files copied before failure.\n", term.DimTS(term.Ts()), result.FilesCopied)
		}
		a.printMu.Unlock()
		a.logf("Copy failed: %v", copyErr)
		a.drainInput()
		a.printPrompt()
		return
	}

	a.handleCopySuccess(card, mode, destBase, result, isDryRun, previewHidden)
	fmt.Println()
	a.drainInput()
	a.printPrompt()
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
		fmt.Printf("%s %d files, %s would be copied\n",
			term.DimTS(term.Ts()),
			result.FilesCopied,
			fsutil.FormatBytes(result.BytesCopied))
		if previewHidden > 0 {
			fmt.Printf("%s ... +%d more files (preview capped at %d)\n", term.DimTS(term.Ts()), previewHidden, dryRunPreviewLimit)
		}
		a.printMu.Unlock()
		a.logf("Dry-run complete: %d files, %s would be copied", result.FilesCopied, fsutil.FormatBytes(result.BytesCopied))
		return
	}

	a.printMu.Lock()
	fmt.Printf("\r%s Copy complete ✓                                          \n", term.DimTS(term.Ts()))
	if result.FilesSkipped > 0 && result.FilesCopied == 0 {
		fmt.Printf("%s All %d files already copied. Nothing to do.\n",
			term.DimTS(term.Ts()),
			result.FilesSkipped)
	} else if result.FilesSkipped > 0 {
		fmt.Printf("%s %d files, %s copied in %s (%.1f MB/s) — %d files skipped\n",
			term.DimTS(term.Ts()),
			result.FilesCopied,
			fsutil.FormatBytes(result.BytesCopied),
			elapsed,
			speed,
			result.FilesSkipped)
	} else {
		fmt.Printf("%s %d files, %s copied in %s (%.1f MB/s)\n",
			term.DimTS(term.Ts()),
			result.FilesCopied,
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
		fmt.Printf("%s Warning: could not write .cardbot to card: %s\n", a.TsPrefix(), term.FriendlyErr(dotErr))
		a.logf("Dotfile write failed: %v", dotErr)
	} else {
		a.logf("Dotfile written to %s", card.Path)
	}

	a.mu.Lock()
	a.copiedModes[mode] = true
	a.mu.Unlock()
}
