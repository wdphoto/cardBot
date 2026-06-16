# cardBot Code Review

Date: 2026-06-16  
Reviewer: pi coding agent using the Go / Cobra-Viper skills from `.pi/git/github.com/spf13/go-skills`

## Scope

Reviewed the current Go codebase for correctness, data-safety, idiomatic Go design, concurrency, testing posture, and CLI ergonomics.

Commands run:

```bash
go test -count=1 ./...
go test -race ./...
go vet ./...
```

All commands passed.

## Follow-up Implementation Status

The straightforward findings have now been patched:

- Added source/destination collision guards in `cardcopy.Run`.
- Changed disk-space estimation to ignore files that will be skipped.
- Protected `App.lastTS` with a dedicated mutex.
- Made analyzer symlink handling match copier behavior.
- Replaced the macOS detector startup sleep with a readiness/error handshake.
- Serialized analyzer progress callbacks.

Verification after fixes:

```bash
gofmt -w ...
go test -count=1 ./...
go vet ./...
go test -race ./...
```

All commands passed.

## Overall Status

**Issues found.** The project is in good shape overall: packages are mostly domain-focused, interfaces are defined at consumer boundaries, tests are broad, errors are generally wrapped, and long-running work uses `context.Context`. The main risks are around file-copy safety, a few concurrent UI paths, and macOS detector startup reporting.

## Findings

### 1. Destination/source collision is not guarded before copying

**Severity:** High  
**Files:** `cardcopy/copy.go`, `app/commands.go`

`cardcopy.Run` validates that writes stay under `opts.DestBase`, but it does not reject destinations that are the card path itself, inside the card, or inside the source `DCIM` tree.

Why it matters: if a user accidentally configures the destination as the mounted card (or a directory under it), cardBot can write copied files back to the source media. That is surprising at best and can consume card space or pollute the source card.

Recommendation: after path expansion/cleaning, reject cases where `DestBase` is equal to `CardPath`, under `CardPath`, equal to `DCIM`, or under `DCIM`. Prefer doing this in `cardcopy.Run` so the library remains safe even if called outside the app.

### 2. Disk-space check can fail re-copies that would skip existing files

**Severity:** Medium  
**File:** `cardcopy/copy.go:226-233`, skip logic at `cardcopy/copy.go:274-281`

The free-space check compares free space against `totalBytes` for all source files before calculating which destination files already exist with the correct size.

Why it matters: a destination with little free space can fail with “not enough space” even when every file is already present and the copy would only skip files.

Recommendation: precompute destination paths and existing-size matches before the free-space check, then compare free space to `bytesToCopy` instead of `totalBytes`.

### 3. Timestamp state is accessed without synchronization

**Severity:** Medium  
**File:** `app/app.go:119-131`

`App.TsPrefix` and `SetLastTS` read/write `a.lastTS` without locking. `TsPrefix` is called from the main event loop and from goroutines such as `displayCard` and delayed scanning resume paths.

Why it matters: this is a potential data race and can also make timestamp indentation inconsistent under concurrent output.

Recommendation: add a small dedicated mutex for timestamp state, or make timestamp formatting stateless. Do not reuse `a.mu` inside `TsPrefix` because some callers already hold `a.mu`.

### 4. Analyzer does not skip symlinks, while copier does

**Severity:** Medium  
**Files:** `analyze/analyze.go:204-220`, `cardcopy/copy.go:114-117`

`cardcopy.Run` explicitly skips symlinks. `analyze.Analyze` does not. A symlink with a media-looking extension can be counted and opened during EXIF analysis, but later not copied.

Why it matters: analysis results can disagree with copy results. On untrusted media, following symlinks during analysis also risks reading outside the card mount.

Recommendation: mirror the copier behavior in the analyzer: skip `d.Type()&fs.ModeSymlink != 0` before `d.Info()` / `readExif`.

### 5. macOS detector startup always reports success

**Severity:** Medium  
**File:** `detect/detect_darwin.go:112-141`

`Detector.Start` launches a goroutine, sleeps for 100ms, and returns `nil` even if `DASessionCreate` fails inside the goroutine.

Why it matters: the app can enter scanning mode even though native card detection never started. This makes failures look like “no cards found” instead of a startup error.

Recommendation: replace the fixed sleep with a readiness channel that returns either success or an initialization error from the goroutine. Also consider a clear error if the detector is already started.

### 6. Analyzer progress callbacks may run concurrently

**Severity:** Low/Medium  
**File:** `analyze/analyze.go:251-278`

`OnProgress` is invoked from EXIF worker goroutines. The callback contract does not say it must be concurrency-safe, and the app callback writes directly to stdout.

Why it matters: consumers can accidentally introduce races in callbacks, and progress output may interleave when multiple workers hit the progress interval at the same time.

Recommendation: either serialize progress reporting inside `Analyze`, or document the callback as concurrent and make the app callback synchronize output.

## Positive Notes

- Package layout is flat and domain-oriented (`analyze`, `cardcopy`, `detect`, `daemon`, `launch`, etc.), which fits idiomatic Go.
- App-side dependency interfaces in `app/deps.go` are small and defined where consumed.
- Copy cancellation returns partial results, which is good UX for card removals and manual cancellation.
- Tests are extensive and include race-test coverage.
- The CLI is simple enough that using the standard `flag` package is reasonable; Cobra/Viper is not necessary here unless command/config complexity grows.

## Suggested Fix Order

1. Add destination/source collision checks in `cardcopy.Run`.
2. Move disk-space estimation after skip detection.
3. Synchronize timestamp/progress output paths.
4. Make analyzer symlink handling match copier behavior.
5. Improve macOS detector startup handshake.
