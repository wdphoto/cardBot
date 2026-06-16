package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/analyze"
)

func TestParseInputAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		hasCard bool
		want    inputAction
	}{
		{"help no card", "?", false, actionHelp},
		{"help with card", "?", true, actionHelp},
		{"empty no card", "", false, actionNone},
		{"empty with card", "", true, actionNone},
		{"copy all", "a", true, actionCopyAll},
		{"copy selects", "s", true, actionCopySelects},
		{"copy photos", "p", true, actionCopyPhotos},
		{"copy videos", "v", true, actionCopyVideos},
		{"copy today", "t", true, actionCopyToday},
		{"copy yesterday", "y", true, actionCopyYesterday},
		{"eject", "e", true, actionEject},
		{"exit", "x", true, actionExitCard},
		{"info", "i", true, actionHardwareInfo},
		{"cancel copy", "\\", true, actionCancelCopy},
		{"uppercase + spaces", "  A  ", true, actionCopyAll},
		{"unknown with card", "z", true, actionUnknown},
		{"input but no card", "z", false, actionNoCardMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseInputAction(tt.input, tt.hasCard)
			if got != tt.want {
				t.Errorf("parseInputAction(%q, %v) = %v, want %v", tt.input, tt.hasCard, got, tt.want)
			}
		})
	}
}

func TestModeDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want string
	}{
		{"all", "All"},
		{"selects", "Selects"},
		{"photos", "Photos"},
		{"videos", "Videos"},
		{"today", "Today's photos"},
		{"yesterday", "Yesterday's photos"},
		{"", "Copy"},
		{"custom", "Custom"},
	}

	for _, tt := range tests {
		got := modeDisplayName(tt.mode)
		if got != tt.want {
			t.Errorf("modeDisplayName(%q) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestCopyBlockReason(t *testing.T) {
	t.Parallel()

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	tests := []struct {
		name       string
		mode       string
		invalid    bool
		copiedAll  bool
		copiedMode bool
		result     *analyze.Result
		wantEmpty  bool
	}{
		{"invalid card", "all", true, false, false, nil, false},
		{"all already copied", "all", false, true, false, nil, false},
		{"mode already copied", "photos", false, false, true, nil, false},
		{"selects no starred", "selects", false, false, false, &analyze.Result{Starred: 0}, false},
		{"selects has starred", "selects", false, false, false, &analyze.Result{Starred: 5}, true},
		{"photos no photos", "photos", false, false, false, &analyze.Result{PhotoCount: 0}, false},
		{"photos has photos", "photos", false, false, false, &analyze.Result{PhotoCount: 10}, true},
		{"videos no videos", "videos", false, false, false, &analyze.Result{VideoCount: 0}, false},
		{"videos has videos", "videos", false, false, false, &analyze.Result{VideoCount: 3}, true},
		{"all allowed", "all", false, false, false, nil, true},
		{
			"today no photos",
			"today", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.NEF": "1999-01-01"}},
			false,
		},
		{
			"today has photos",
			"today", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.NEF": today}},
			true,
		},
		{
			"today video only does not count",
			"today", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.MOV": today}},
			false,
		},
		{
			"yesterday no photos",
			"yesterday", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.NEF": "1999-01-01"}},
			false,
		},
		{
			"yesterday has photos",
			"yesterday", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.NEF": yesterday}},
			true,
		},
		{
			"yesterday video only does not count",
			"yesterday", false, false, false,
			&analyze.Result{FileDates: map[string]string{"100NIKON/DSC_0001.MOV": yesterday}},
			false,
		},
		{
			"today nil result",
			"today", false, false, false, nil, true,
		},
		{
			"yesterday nil result",
			"yesterday", false, false, false, nil, true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := copyBlockReason(tt.mode, tt.invalid, tt.copiedAll, tt.copiedMode, tt.result)
			if (got == "") != tt.wantEmpty {
				t.Errorf("copyBlockReason() = %q, wantEmpty = %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestCopyReadinessReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase appPhase
		want  string
	}{
		{phaseAnalyzing, "Still scanning card. Please wait."},
		{phaseCopying, "Copy already in progress."},
		{phaseShuttingDown, "Shutting down."},
		{phaseScanning, "Card is not ready for copy."},
	}

	for _, tt := range tests {
		if got := copyReadinessReason(tt.phase); got != tt.want {
			t.Fatalf("copyReadinessReason(%v) = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestCanCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       string
		phase      appPhase
		invalid    bool
		copiedAll  bool
		copiedMode bool
		result     *analyze.Result
		wantOK     bool
		wantReason string
	}{
		{
			name:       "not ready analyzing",
			mode:       "all",
			phase:      phaseAnalyzing,
			wantOK:     false,
			wantReason: "Still scanning card. Please wait.",
		},
		{
			name:       "ready blocked by invalid",
			mode:       "all",
			phase:      phaseReady,
			invalid:    true,
			wantOK:     false,
			wantReason: "No media found on this card.",
		},
		{
			name:       "ready allowed",
			mode:       "photos",
			phase:      phaseReady,
			result:     &analyze.Result{PhotoCount: 2},
			wantOK:     true,
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := canCopy(tt.mode, tt.phase, tt.invalid, tt.copiedAll, tt.copiedMode, tt.result)
			if ok != tt.wantOK {
				t.Fatalf("canCopy ok = %v, want %v", ok, tt.wantOK)
			}
			if reason != tt.wantReason {
				t.Fatalf("canCopy reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestPromptText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		invalid   bool
		copiedAll bool
		contains  string
	}{
		{"normal", false, false, "Copy All"},
		{"invalid card", true, false, "Eject"},
		{"copied all", false, true, "Done"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promptText(tt.invalid, tt.copiedAll)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("promptText() = %q, should contain %q", got, tt.contains)
			}
		})
	}
}

func TestShouldResumeScanning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		noCurrentCard bool
		queueLen      int
		want          bool
	}{
		{"no card, empty queue", true, 0, true},
		{"has card", false, 0, false},
		{"no card, has queue", true, 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldResumeScanning(tt.noCurrentCard, tt.queueLen)
			if got != tt.want {
				t.Errorf("shouldResumeScanning(%v, %d) = %v, want %v", tt.noCurrentCard, tt.queueLen, got, tt.want)
			}
		})
	}
}

func TestCountPhotosForDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *analyze.Result
		date   string
		want   int
	}{
		{"nil result", nil, "2026-03-25", 0},
		{"nil FileDates", &analyze.Result{}, "2026-03-25", 0},
		{"no match", &analyze.Result{
			FileDates: map[string]string{"100NIKON/DSC_0001.NEF": "2026-03-24"},
		}, "2026-03-25", 0},
		{"one photo match", &analyze.Result{
			FileDates: map[string]string{"100NIKON/DSC_0001.NEF": "2026-03-25"},
		}, "2026-03-25", 1},
		{"video not counted", &analyze.Result{
			FileDates: map[string]string{"100NIKON/DSC_0001.MOV": "2026-03-25"},
		}, "2026-03-25", 0},
		{"mixed files", &analyze.Result{
			FileDates: map[string]string{
				"100NIKON/DSC_0001.NEF": "2026-03-25",
				"100NIKON/DSC_0002.MOV": "2026-03-25",
				"100NIKON/DSC_0003.CR3": "2026-03-25",
				"100NIKON/DSC_0004.NEF": "2026-03-24",
			},
		}, "2026-03-25", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countPhotosForDate(tt.result, tt.date)
			if got != tt.want {
				t.Errorf("countPhotosForDate() = %d, want %d", got, tt.want)
			}
		})
	}
}
