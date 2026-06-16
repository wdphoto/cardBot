package app

import (
	"context"
	"net/http"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
	"github.com/wdphoto/cardBot/update"
)

// cardDetector is the app-facing detector contract.
// Defined at the consumer side to keep integration points testable.
type cardDetector interface {
	Start() error
	Stop()
	Events() <-chan *detect.Card
	Removals() <-chan string
	Eject(path string) error
	Remove(path string)
}

// cardAnalyzer is the app-facing analyzer contract.
type cardAnalyzer interface {
	SetWorkers(n int)
	OnProgress(fn analyze.ProgressFunc)
	Analyze(ctx context.Context) (*analyze.Result, error)
}

type detectorFactory func() cardDetector
type analyzerFactory func(cardPath string) cardAnalyzer
type copyRunner func(ctx context.Context, opts cardcopy.Options, onProgress cardcopy.ProgressFunc) (*cardcopy.Result, error)
type dotfileWriter func(opts dotfile.WriteOptions) error
type updateChecker func(ctx context.Context, client *http.Client, apiBase, repo, current string) (update.CheckResult, error)

var (
	defaultDetectorFactory detectorFactory = func() cardDetector { return detect.NewDetector() }
	defaultAnalyzerFactory analyzerFactory = func(cardPath string) cardAnalyzer { return analyze.New(cardPath) }
	defaultCopyRunner      copyRunner      = cardcopy.Run
	defaultDotfileWriter   dotfileWriter   = dotfile.Write
	defaultUpdateChecker   updateChecker   = update.CheckLatest
)
