package app

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/term"
)

const (
	minScanVisualDuration = 350 * time.Millisecond
	scanLinePaceDelay     = 120 * time.Millisecond

	targetNoDCIMRetryDelay       = 350 * time.Millisecond
	targetNoDCIMRetryMaxAttempts = 8
)

func (a *App) handleCardEvent(card *detect.Card) {
	if card == nil {
		return
	}
	card.Path = normalizeCardPath(card.Path)
	if card.Path == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Ignore if already being processed or queued.
	if a.isTracked(card.Path) {
		return
	}

	a.stopScanningLocked()

	if a.currentCard == nil {
		a.currentCard = card
		a.setPhaseLocked(phaseAnalyzing)
		fmt.Printf("%s Scanning ✓\n", a.TsPrefix())
		hw := card.HW()
		diskID := ""
		if hw != nil {
			diskID = hw.DiskID()
		}
		fmt.Printf("%s %s\n", a.TsPrefix(), formatDetectedVolume(card.Path, diskID))
		a.logf("Card detected: %s", card.Path)
		scanTS := term.DimTS(term.Ts())
		ctx, cancel := context.WithCancel(a.ctx)
		a.scanCancel = cancel
		go a.displayCard(ctx, card.Path, scanTS)
	} else {
		a.cardQueue = append(a.cardQueue, card)
		a.printQueueNotice(card)
	}
}

func (a *App) isTracked(path string) bool {
	path = normalizeCardPath(path)
	if path == "" {
		return false
	}
	if a.currentCard != nil && sameCardPath(a.currentCard.Path, path) {
		return true
	}
	for _, c := range a.cardQueue {
		if sameCardPath(c.Path, path) {
			return true
		}
	}
	return false
}

func formatDetectedVolume(path, diskID string) string {
	path = strings.TrimSpace(path)
	diskID = strings.TrimSpace(diskID)
	if diskID == "" {
		return fmt.Sprintf("\"%s\" detected", path)
	}
	return fmt.Sprintf("\"%s\" (%s) detected", path, diskID)
}

func (a *App) printQueueNotice(card *detect.Card) {
	plural := ""
	if len(a.cardQueue) > 1 {
		plural = "s"
	}
	fmt.Printf("\n%s %s detected (%d card%s in queue)\n",
		a.TsPrefix(),
		card.Brand,
		len(a.cardQueue),
		plural)
	a.logf("%s detected (%d card%s in queue)", card.Brand, len(a.cardQueue), plural)
}

func (a *App) shouldRetryMissingDCIM(path string, attempt int) bool {
	if attempt >= targetNoDCIMRetryMaxAttempts {
		return false
	}
	if a.targetPath == "" {
		return false
	}
	return sameCardPath(a.targetPath, path)
}

func (a *App) analyzeCard(ctx context.Context, path, scanTS string) (*analyze.Result, error) {
	for attempt := 1; ; attempt++ {
		analyzer := a.newAnalyzer(path)
		analyzer.SetWorkers(a.cfg.Advanced.ExifWorkers)
		analyzer.OnProgress(func(count int) {
			if count%100 == 0 {
				fmt.Printf("\r%s Scanning %d files", scanTS, count)
			}
		})

		result, err := analyzer.Analyze(ctx)
		if err == nil {
			return result, nil
		}
		if !os.IsNotExist(err) || !a.shouldRetryMissingDCIM(path, attempt) {
			return nil, err
		}

		a.logf("Target path not ready yet (missing DCIM), retrying %d/%d: %s", attempt, targetNoDCIMRetryMaxAttempts, path)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(targetNoDCIMRetryDelay):
		}
	}
}

func (a *App) displayCard(ctx context.Context, path, scanTS string) {
	path = normalizeCardPath(path)

	// Card may have been removed or replaced before this goroutine runs.
	if ctx.Err() != nil {
		return
	}
	a.setPhase(phaseAnalyzing)

	a.logf("Reading %s", path)
	scanStart := time.Now()
	result, err := a.analyzeCard(ctx, path, scanTS)

	// Cancelled — card was removed or replaced during analysis.
	if ctx.Err() != nil {
		return
	}

	if err != nil {
		if os.IsNotExist(err) {
			a.mu.Lock()
			if a.currentCard == nil || a.currentCard.Path != path {
				a.mu.Unlock()
				return
			}
			a.cardInvalid = true
			a.setPhaseLocked(phaseReady)
			card := a.currentCard
			a.mu.Unlock()
			fmt.Printf("\r%s Card is invalid (no DCIM found)\n", a.TsPrefix())
			a.logf("Card invalid: no DCIM at %s", path)
			a.printInvalidCardInfo(card)
		} else {
			fmt.Printf("\r%s Error scanning card: %s\n", a.TsPrefix(), term.FriendlyErr(err))
			a.logf("Error analyzing card %s: %v", path, err)
			a.finishCard()
		}
		return
	}

	// Log any warnings from the scan.
	if result != nil {
		for _, w := range result.Warnings {
			a.logf("Scan warning: %s: %s", path, w)
		}
	}

	elapsed := time.Since(scanStart)
	if wait := minScanVisualDuration - elapsed; wait > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		elapsed = time.Since(scanStart)
	}

	total := 0
	if result != nil {
		total = result.FileCount
	}
	fmt.Printf("\r%s Scanning %d files ✓\n", a.TsPrefix(), total)
	time.Sleep(scanLinePaceDelay)
	durStr := formatElapsed(elapsed)
	fmt.Printf("%s Scan completed in %s\n", a.TsPrefix(), durStr)
	a.logf("Scan completed: %s — %d files in %s", path, total, durStr)
	fmt.Println()

	a.mu.Lock()
	if a.currentCard == nil || a.currentCard.Path != path {
		a.mu.Unlock()
		return
	}
	a.lastResult = result
	a.setPhaseLocked(phaseReady)
	card := a.currentCard
	a.mu.Unlock()
	a.printCardInfo(card, result)
}

func (a *App) finishCard() {
	a.mu.Lock()
	if a.scanCancel != nil {
		a.scanCancel()
		a.scanCancel = nil
	}
	a.currentCard = nil
	a.lastResult = nil
	a.copiedModes = make(map[string]bool)
	a.cardInvalid = false
	a.setPhaseLocked(phaseAfterFinish(len(a.cardQueue)))

	if len(a.cardQueue) > 0 {
		nextCard := a.cardQueue[0]
		a.cardQueue = a.cardQueue[1:]
		a.currentCard = nextCard
		ctx, cancel := context.WithCancel(a.ctx)
		a.scanCancel = cancel
		a.mu.Unlock()
		go a.displayCard(ctx, nextCard.Path, term.DimTS(term.Ts()))
		return
	}
	a.mu.Unlock()

	a.StartScanning()
}

// resumeScanningIfIdle starts the scanning spinner only if
// no current card is active and no queued cards are waiting.
func (a *App) resumeScanningIfIdle() {
	a.mu.Lock()
	shouldStart := shouldResumeScanning(a.currentCard == nil, len(a.cardQueue))
	a.mu.Unlock()
	if !shouldStart {
		return
	}
	a.StartScanning()
}

func (a *App) handleRemoval(path string) {
	path = normalizeCardPath(path)
	if path == "" {
		return
	}

	a.mu.Lock()
	wasCurrent := a.currentCard != nil && sameCardPath(a.currentCard.Path, path)

	if wasCurrent {
		if a.phase == phaseCopying && a.copyCancel != nil {
			a.copyRemoved = true
			a.copyCancel()
		}
		if a.scanCancel != nil {
			a.scanCancel()
			a.scanCancel = nil
		}
		a.currentCard = nil
		a.lastResult = nil
		a.copiedModes = make(map[string]bool)
		a.cardInvalid = false
		hasQueue := len(a.cardQueue) > 0
		a.setPhaseLocked(phaseAfterFinish(len(a.cardQueue)))
		var nextCard *detect.Card
		var nextCtx context.Context
		if hasQueue {
			nextCard = a.cardQueue[0]
			a.cardQueue = a.cardQueue[1:]
			a.currentCard = nextCard
			a.setPhaseLocked(phaseAnalyzing)
			var cancel context.CancelFunc
			nextCtx, cancel = context.WithCancel(a.ctx)
			a.scanCancel = cancel
		}
		a.mu.Unlock()

		fmt.Printf("\n%s Card removed: %s\n", a.TsPrefix(), path)
		a.logf("Card removed: %s", path)
		if hasQueue {
			go a.displayCard(nextCtx, nextCard.Path, term.DimTS(term.Ts()))
		} else {
			go func() {
				time.Sleep(removalDelay)
				a.resumeScanningIfIdle()
			}()
		}
		return
	}

	// Check queue
	for i, card := range a.cardQueue {
		if sameCardPath(card.Path, path) {
			a.cardQueue = append(a.cardQueue[:i], a.cardQueue[i+1:]...)
			a.mu.Unlock()
			fmt.Printf("\n%s Queued card removed: %s\n", a.TsPrefix(), path)
			a.logf("Queued card removed: %s", path)
			return
		}
	}
	a.mu.Unlock()
}

func (a *App) handleInput(input string) {
	a.mu.Lock()
	card := a.currentCard
	hasCard := card != nil
	a.mu.Unlock()

	action := parseInputAction(input, hasCard)

	switch action {
	case actionNone:
		return
	case actionHelp:
		a.showHelp()
		if hasCard {
			a.printPrompt()
		}
		return
	case actionNoCardMessage:
		return
	}

	// Re-read current card to avoid acting on a stale pointer if card state changed.
	a.mu.Lock()
	card = a.currentCard
	a.mu.Unlock()
	if card == nil {
		return
	}

	switch action {
	case actionCopyAll:
		a.handleCopyCmd(card, "all")
	case actionCopySelects:
		a.handleCopyCmd(card, "selects")
	case actionCopyPhotos:
		a.handleCopyCmd(card, "photos")
	case actionCopyVideos:
		a.handleCopyCmd(card, "videos")
	case actionCopyToday:
		a.handleCopyCmd(card, "today")
	case actionCopyYesterday:
		a.handleCopyCmd(card, "yesterday")
	case actionEject:
		a.ejectCard(card)
	case actionExitCard:
		a.cancelCard()
	case actionHardwareInfo:
		a.showHardwareInfo(card)
	case actionCancelCopy:
		a.mu.Lock()
		cancel := a.copyCancel
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		} else {
			fmt.Printf("\n%s No copy in progress.\n", term.DimTS(term.Ts()))
			a.printPrompt()
		}
	case actionUnknown:
		fmt.Printf("\nUnknown command %q. Press [?] for help.\n", input)
		a.printPrompt()
	}
}

func (a *App) ejectCard(card *detect.Card) {
	fmt.Printf("\nEjecting %s...\n", card.Name)
	a.logf("Ejecting %s", card.Path)
	if err := a.detector.Eject(card.Path); err != nil {
		fmt.Printf("Error: %s\n", term.FriendlyErr(err))
		a.logf("Eject error: %v", err)
		a.printPrompt()
		return
	}
	a.detector.Remove(card.Path)
	fmt.Printf("\n%s Card ejected: %s\n", a.TsPrefix(), card.Path)
	a.logf("Card ejected: %s", card.Path)
	a.finishCard()
}

func (a *App) cancelCard() {
	fmt.Println("\nCancelled.")
	a.logf("Card cancelled")
	a.finishCard()
}

func (a *App) handleCopyCmd(card *detect.Card, mode string) {
	a.mu.Lock()
	phase := a.phase
	invalid := a.cardInvalid
	copiedAll := a.copiedModes["all"]
	copiedMode := a.copiedModes[mode]
	analyzeResult := a.lastResult
	a.mu.Unlock()

	if ok, reason := canCopy(mode, phase, invalid, copiedAll, copiedMode, analyzeResult); !ok {
		fmt.Printf("\n%s %s\n", a.TsPrefix(), reason)
		a.printPrompt()
		return
	}

	a.mu.Lock()
	if a.currentCard == nil || !sameCardPath(a.currentCard.Path, card.Path) || a.phase != phaseReady {
		a.mu.Unlock()
		return
	}
	a.setPhaseLocked(phaseCopying)
	a.mu.Unlock()
	a.copyWG.Add(1)
	go func() {
		defer a.copyWG.Done()
		a.copyFiltered(card, mode)
	}()
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		tenths := math.Round(d.Seconds()*10) / 10
		return fmt.Sprintf("%.1fs", tenths)
	}
	return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
}

// newStdinReader creates a buffered reader for stdin.
func newStdinReader() *bufio.Reader {
	return bufio.NewReader(os.Stdin)
}

// launchTargetPath synthesizes a card from the given path and begins analysis
// immediately, bypassing the normal detector-driven scan-and-wait flow.
// Used when the user invokes `cardbot /path/to/card`.
func (a *App) launchTargetPath(path string) {
	path = normalizeCardPath(path)
	if path == "" {
		return
	}
	a.stopScanning()

	card := detect.CardFromPath(path)
	if card == nil {
		card = &detect.Card{Path: path, Name: filepath.Base(path), Brand: "Unknown"}
	}

	a.mu.Lock()
	a.currentCard = card
	a.setPhaseLocked(phaseAnalyzing)
	a.mu.Unlock()

	scanTS := term.DimTS(term.Ts())
	fmt.Printf("%s Scanning ✓\n", a.TsPrefix())
	fmt.Printf("%s \"%s\" (target)\n", tsIndent, path)
	a.logf("Target path: %s", path)

	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.scanCancel = cancel
	a.mu.Unlock()

	go a.displayCard(ctx, path, scanTS)
}
