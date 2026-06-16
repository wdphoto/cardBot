package app

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/wdphoto/cardBot/analyze"
)

// inputAction is the parsed intent of a user command.
type inputAction int

const (
	actionNone inputAction = iota
	actionHelp
	actionCopyAll
	actionCopySelects
	actionCopyPhotos
	actionCopyVideos
	actionCopyToday
	actionCopyYesterday
	actionEject
	actionExitCard
	actionHardwareInfo
	actionCancelCopy
	actionNoCardMessage
	actionUnknown
)

// parseInputAction normalizes stdin input into a high-level action.
func parseInputAction(input string, hasCard bool) inputAction {
	cmd := strings.ToLower(strings.TrimSpace(input))

	if cmd == "?" {
		return actionHelp
	}

	if !hasCard {
		if cmd == "" {
			return actionNone
		}
		return actionNoCardMessage
	}

	switch cmd {
	case "":
		return actionNone
	case "a":
		return actionCopyAll
	case "s":
		return actionCopySelects
	case "p":
		return actionCopyPhotos
	case "v":
		return actionCopyVideos
	case "t":
		return actionCopyToday
	case "y":
		return actionCopyYesterday
	case "e":
		return actionEject
	case "x":
		return actionExitCard
	case "i":
		return actionHardwareInfo
	case "\\":
		return actionCancelCopy
	default:
		return actionUnknown
	}
}

// modeDisplayName returns a user-facing mode label.
func modeDisplayName(mode string) string {
	switch mode {
	case "all":
		return "All"
	case "selects":
		return "Selects"
	case "photos":
		return "Photos"
	case "videos":
		return "Videos"
	case "today":
		return "Today's photos"
	case "yesterday":
		return "Yesterday's photos"
	default:
		if mode == "" {
			return "Copy"
		}
		r := []rune(mode)
		if len(r) == 0 {
			return "Copy"
		}
		return strings.ToUpper(string(r[0])) + string(r[1:])
	}
}

// copyBlockReason returns a user-facing reason that a copy command should be blocked.
// Empty string means the copy is allowed.
func copyBlockReason(mode string, invalid, copiedAll, copiedMode bool, result *analyze.Result) string {
	if invalid {
		return "No media found on this card."
	}

	if copiedAll {
		if mode == "all" {
			return "Already copied."
		}
		return modeDisplayName(mode) + " already copied."
	}

	if copiedMode {
		return modeDisplayName(mode) + " already copied."
	}

	if result == nil {
		return ""
	}

	switch mode {
	case "selects":
		if result.Starred == 0 {
			return "No starred files found on this card."
		}
	case "photos":
		if result.PhotoCount == 0 {
			return "No photo files found on this card."
		}
	case "videos":
		if result.VideoCount == 0 {
			return "No video files found on this card."
		}
	case "today":
		if countPhotosForDate(result, time.Now().Format("2006-01-02")) == 0 {
			return "No photos from today found on this card."
		}
	case "yesterday":
		if countPhotosForDate(result, time.Now().AddDate(0, 0, -1).Format("2006-01-02")) == 0 {
			return "No photos from yesterday found on this card."
		}
	}

	return ""
}

// copyReadinessReason returns a user-facing message when the app phase is not
// ready to accept copy commands.
func copyReadinessReason(phase appPhase) string {
	switch phase {
	case phaseAnalyzing:
		return "Still scanning card. Please wait."
	case phaseCopying:
		return "Copy already in progress."
	case phaseShuttingDown:
		return "Shutting down."
	default:
		return "Card is not ready for copy."
	}
}

// canCopy determines whether a copy command should run.
func canCopy(mode string, phase appPhase, invalid, copiedAll, copiedMode bool, result *analyze.Result) (bool, string) {
	if phase != phaseReady {
		return false, copyReadinessReason(phase)
	}
	if reason := copyBlockReason(mode, invalid, copiedAll, copiedMode, result); reason != "" {
		return false, reason
	}
	return true, ""
}

// promptText returns the command prompt for the current card state.
func promptText(invalid, copiedAll bool) string {
	switch {
	case invalid:
		return "[e] Eject  [x] Exit  [?] Help  > "
	case copiedAll:
		return "[e] Eject  [x] Done  [?] Help  > "
	default:
		return "[a] Copy All  [e] Eject  [x] Exit  [?] Help  > "
	}
}

// shouldResumeScanning reports whether the scanner should resume waiting.
func shouldResumeScanning(noCurrentCard bool, queueLen int) bool {
	return noCurrentCard && queueLen == 0
}

// countPhotosForDate counts photo files in the analyze result matching a specific date.
func countPhotosForDate(result *analyze.Result, date string) int {
	if result == nil || result.FileDates == nil {
		return 0
	}
	count := 0
	for relPath, d := range result.FileDates {
		if d != date {
			continue
		}
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(relPath), "."))
		if analyze.IsPhoto(ext) {
			count++
		}
	}
	return count
}
