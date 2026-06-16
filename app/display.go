package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
	"github.com/wdphoto/cardBot/fsutil"
	"github.com/wdphoto/cardBot/term"
)

// printCardHeader renders the shared header lines used by both printCardInfo
// and printInvalidCardInfo: Status, Path, Storage, Gear.
func (a *App) printCardHeader(card *detect.Card, bodies, lenses []string) {
	status := dotfile.Read(card.Path)
	fmt.Printf("  Status:   %s\n", dotfile.FormatStatus(status))
	fmt.Printf("  Path:     %s\n", card.Path)
	var pct int64
	if card.TotalBytes > 0 {
		pct = (card.UsedBytes * 100) / card.TotalBytes
	}
	fmt.Printf("  Storage:  %s / %s (%d%%)\n",
		fsutil.FormatBytes(card.UsedBytes),
		fsutil.FormatBytes(card.TotalBytes),
		pct)

	// Gear: body and lens lines, all in brand color.
	bodyLine := strings.Join(bodies, ", ")
	if bodyLine == "" {
		bodyLine = card.Brand
	}
	color, reset := "", ""
	if a.cfg.Output.Color {
		color = term.BrandColor(card.Brand)
		reset = term.Reset
	}
	fmt.Printf("  Gear:     %s%s%s\n", color, bodyLine, reset)
	for _, lens := range lenses {
		fmt.Printf("            %s%s%s\n", color, lens, reset)
	}
}

func (a *App) printCardInfo(card *detect.Card, result *analyze.Result) {
	var bodies, lenses []string
	if result != nil {
		bodies = result.Bodies
		lenses = result.Lenses
	}
	a.printCardHeader(card, bodies, lenses)

	if result != nil && result.Starred > 0 {
		fmt.Printf("  Starred:  %d\n", result.Starred)
	}

	if result != nil && result.FileCount > 0 {
		// Find max file count width for alignment.
		maxCount := 0
		for _, g := range result.Groups {
			if g.FileCount > maxCount {
				maxCount = g.FileCount
			}
		}
		countWidth := len(fmt.Sprintf("%d", maxCount))

		for i, g := range result.Groups {
			if i == 0 {
				fmt.Printf("  Content:  ")
			} else {
				fmt.Printf("            ")
			}
			fmt.Printf("%s   %10s   %*d   %s\n",
				g.Date,
				fsutil.FormatBytes(g.Size),
				countWidth,
				g.FileCount,
				strings.Join(g.Extensions, ", "))
		}
		fmt.Println()
		fmt.Printf("  Total:    %d photos, %d videos, %s\n", result.PhotoCount, result.VideoCount, fsutil.FormatBytes(result.TotalSize))
	} else {
		fmt.Println("  Content:  (empty)")
	}

	// Print config info (moved from startup, cleaner format)
	fmt.Println()
	fmt.Printf("  Copy to:  %s\n", config.ContractPath(a.cfg.Destination.Path))
	fmt.Printf("  Naming:   %s\n", NamingModeLabel(a.cfg.Naming.Mode))

	if a.dryRun {
		fmt.Println("  Mode:     dry-run (no files will be copied)")
	}

	a.mu.Lock()
	queueLen := len(a.cardQueue)
	a.mu.Unlock()

	if queueLen > 0 {
		plural := ""
		if queueLen > 1 {
			plural = "s"
		}
		fmt.Printf("  Queue:    %d card%s waiting\n", queueLen, plural)
	}

	fmt.Println()
	a.printPrompt()
}

// printInvalidCardInfo shows basic card info when a card has no DCIM directory.
func (a *App) printInvalidCardInfo(card *detect.Card) {
	fmt.Println()
	a.printCardHeader(card, nil, nil)
	fmt.Printf("  Content:  (no DCIM — not a camera card)\n")
	fmt.Println()
	a.printPrompt()
}

func (a *App) printPrompt() {
	a.mu.Lock()
	invalid := a.cardInvalid
	copiedAll := a.copiedModes["all"]
	a.mu.Unlock()

	fmt.Print(promptText(invalid, copiedAll))
}

// showHelp prints all available commands.
func (a *App) showHelp() {
	fmt.Println()
	fmt.Println("  Commands:")
	fmt.Println("  [a]  Copy All        copy all files to destination")
	fmt.Println("  [s]  Copy Selects    copy starred/picked files only")
	fmt.Println("  [p]  Copy Photos     copy photos only")
	fmt.Println("  [v]  Copy Videos     copy videos only")
	fmt.Println("  [t]  Copy Today      copy today's photos")
	fmt.Println("  [y]  Copy Yesterday  copy yesterday's photos")
	fmt.Println("  [e]  Eject           safely eject this card")
	fmt.Println("  [x]  Exit            skip this card, move to next")
	fmt.Println("  [i]  Card Info       show hardware details")
	fmt.Println("  [\\]  Cancel Copy     cancel the copy in progress")
	fmt.Println("  [?]  Help            show this help")
	fmt.Println()
}

// showHardwareInfo displays hardware details for the current card.
func (a *App) showHardwareInfo(card *detect.Card) {
	fmt.Println()
	hw := card.HW()
	if hw == nil {
		fmt.Println("Hardware info unavailable")
		fmt.Println()
		a.printPrompt()
		return
	}
	fmt.Println(detect.FormatHardwareInfo(hw))
	fmt.Println()
	a.printPrompt()
}

// cardIsReadOnly probes the card path for write access.
// Returns true if a temp file cannot be created (write-protected card).
func cardIsReadOnly(path string) bool {
	probe := filepath.Join(path, ".cardbot_rw")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return true
	}
	f.Close()
	os.Remove(probe)
	return false
}
