# Reading cardBot: follow one photo

Start with the `p` command, not the package list. Suppose a card contains `DCIM/100CAM/FRAME.NEF`, its `FRAME.XMP` sidecar, and an unrelated movie. Why does **Copy Photos** copy two files when the scan counted one photo? Follow that question through the code:

```text
main → cmd → app.Run
               ├─ card arrival → analyze.Result → card summary
               ├─ p + Enter    → cardcopy.PlanCopy → Execute → copyDone
               └─ copyDone     → report result → dotfile.Write (real success only)
```

In Go, `func (a *App) handleInput(...)` is a method on `App`. Its definition can live in any file in the `app` package. Follow the **method name**, not just the filename.

## 1. Get a card into the foreground session

- [`main.go`](main.go) supplies the embedded version and changelog to `cmd.Execute`. In [`cmd/root.go`](cmd/root.go), `NewRootCommand` declares the CLI. The root command calls `runInteractive`: load JSON settings, apply flag/environment overrides, handle setup, and branch to the daemon or the foreground app. Cobra routes shell commands here; it does **not** handle the `p` key.
- [`app/app.go`](app/app.go), `(*App).Run`, is the place to keep open in your editor. Its `select` waits for an arrival, removal, input line, copy result, or shutdown. On normal macOS builds [`detect/detect_darwin_nocgo.go`](detect/detect_darwin_nocgo.go) polls `/Volumes` for directories containing `DCIM`. Despite the filename, this is the default even with cgo enabled. [`detect/shared.go`](detect/shared.go) builds a `detect.Card`: mount path, space, brand hint and hardware info, **not** a list of photos.
- [`app/handlers.go`](app/handlers.go), `handleCardEvent`, queues the card or starts `displayCard` in a goroutine (`go ...`), so analysis runs while the loop keeps listening. Its context lets removal or shutdown ask the scan to stop. Passing a path on the command line takes the shorter `launchTargetPath` route to `displayCard`; the detector still runs to notice removals.

## 2. Turn the scan into something you can ask questions about

Read `Analyzer.Analyze` in [`analyze/analyze.go`](analyze/analyze.go) in three passes: `collectMediaFiles` gathers known media and indexes XMP sidecars; `extractEXIF` distributes file indices to workers; the merge builds counts and per-file maps. Capture time falls back to modification time. If the primary has no positive rating, a matching XMP can supply one.

Look at `analyze.Result.FileDates`: the key for our photo is `100CAM/FRAME.NEF`, **relative to `DCIM`**. Those same keys are how the menu's date and rating filters find the photo later. `displayCard` stores the result in `a.lastResult` and [`app/display.go`](app/display.go) renders it. The scan didn't count the XMP as another photo, and it hasn't decided what to copy yet.

## 3. Follow `p` to an exact destination filename

1. [`app/input.go`](app/input.go): `parseInputAction` maps the line `p` to `actionCopyPhotos`. [`app/handlers.go`](app/handlers.go): `handleInput` dispatches it; `handleCopyCmd` checks readiness, snapshots the analysis, and registers a copy ID and cancel function **before** starting the worker.
2. [`app/commands.go`](app/commands.go): `copyFiltered` builds `cardcopy.Options`. For `photos` the predicate is `analyze.IsPhoto(ext)`; for `selects` it looks up the relative path in `FileRatings`. [`app/deps.go`](app/deps.go) connects `a.runCopy` to `cardcopy.Run` (tests can supply a substitute).
3. [`cardcopy/copy.go`](cardcopy/copy.go): `PlanCopy` **walks again**. The summary isn't the inventory: copy-all can include files analysis ignored. The planner groups files by directory and stem, so the matching XMP follows the selected NEF; the unrelated movie doesn't. Timestamp sequence numbers are assigned across the whole inventory before unselected files are left out. Selecting fewer files on an unchanged card doesn't renumber them.
4. Inspect `PlannedFile`, especially `SourceRelPath`, `DestPath`, and `Action`. With original naming and a capture date of March 8, 2026, the photo goes to `<destination>/2026-03-08/100CAM/FRAME.NEF`. [`cardcopy/naming.go`](cardcopy/naming.go) changes the basename for timestamp naming. An existing destination can mean a size-match skip, a byte-identical skip, or a conflict; **same size is not proof of identical content**.

## 4. Publish a file, then record the ingest

`Execute` in [`cardcopy/copy.go`](cardcopy/copy.go) forks here. A dry run reports the mappings and returns **before** creating destination directories. A real copy reopens the planned destination ([`cardcopy/destination.go`](cardcopy/destination.go)), checks space when available, and revisits planned skips. For new files, read `copyToDir`: create an exclusive `.part`, stream, check length, sync, optionally compare bytes in `full` mode, close, then publish without replacing an existing name. This is **per file**, not one transaction for the card. In an all-files run, cancelling a movie transfer after a photo finished doesn't undo the photo.

Back in [`app/commands.go`](app/commands.go), the worker sends a `copyOutcome`; `handleCopyDone` checks its ID and whether removal was processed. A late worker doesn't get to mark a newer copy as finished. On real success, `handleCopySuccess` updates two different kinds of state:

| State | Lifetime and meaning |
| --- | --- |
| `a.copiedModes` | In memory for this card session. Used to block repeating a completed menu choice. |
| `<card>/.cardbot` | On-card receipt, one entry per mode. [`dotfile.Write`](dotfile/dotfile.go) counts new files **plus accepted skips**; `FormatStatus` shows the latest valid recorded ingest. Not a manifest or proof of a backup. |

Dry runs update neither. This is a copy preview, not a sandbox for the whole CLI: startup can still log or save config, and the explicit eject command still ejects. A history write can fail on a read-only card **after** the destination files are in place. For the limits of today's completeness checks, see the [code review](CODE_REVIEW_2026-11-18.md).

## Try it without a camera card

- Read [`TestCopy_Filter`](cardcopy/copy_filter_test.go), then run `go test ./cardcopy -run '^TestCopy_Filter$' -v`. Find the XMP in the fixture: the predicate never selects its extension. Where does `PlanCopy` promote it?
- Read [`TestCopyLifecycle_RemovalDuringCopy_DefersNextCardUntilWorkerDone`](app/copy_lifecycle_test.go), then run `go test ./app -run '^TestCopyLifecycle_RemovalDuringCopy_DefersNextCardUntilWorkerDone$' -v`. Follow `copyID` through removal and completion: which handler is allowed to start the next card?

Both exercises use temporary fixtures or fakes. They do not require a daemon, LaunchAgent, or eject. [`TESTING.md`](TESTING.md) separates automated checks from hardware QA.

## Where to go for other changes

| If you want to change… | Start here |
| --- | --- |
| A menu command, prompt, or phase | [`app/input.go`](app/input.go) → [`app/handlers.go`](app/handlers.go); the allowed transitions live in [`app/state.go`](app/state.go). |
| A saved setting or setup question | [`config/config.go`](config/config.go) owns defaults, schema, and saves. [`cmd/setup.go`](cmd/setup.go) and [`pick/`](pick/) choose a destination; [`app/setup.go`](app/setup.go) prompts for naming. User config is **not** the on-card `.cardbot`. |
| Terminal output, log rotation, or progress | [`term/`](term/) formats timestamps/counts, [`fsutil/`](fsutil/) formats sizes and defines hidden entries, [`cblog/`](cblog/) owns the rotating log. [`cardcopy/throughput.go`](cardcopy/throughput.go) formats transfer samples. |
| Card arrival at login | [`launch/agent.go`](launch/agent.go) installs the macOS login job. [`daemon/daemon.go`](daemon/daemon.go) holds the watcher lock and calls an insertion callback. [`cmd/daemon_cmd.go`](cmd/daemon_cmd.go) checks [`instance/`](instance/) and calls [`launch/terminal.go`](launch/terminal.go) to open a **separate foreground process** for the mount. The watcher does not ingest. |
| Detection on another platform | Start at the build constraints in [`detect/`](detect/): Linux has its own poller, native macOS needs `cardbot_native` **and** cgo, and the unsupported-platform stub builds but cannot start detection. |
| Version checks, install, or release | [`app/update.go`](app/update.go) separates the startup notification from explicit self-update. [`update/update.go`](update/update.go) selects the release asset, checks its SHA-256 and replaces the executable. [`scripts/`](scripts/) owns install/uninstall; [CI](.github/workflows/ci.yml) tests builds and the [release workflow](.github/workflows/release.yml) publishes tagged artifacts. |
