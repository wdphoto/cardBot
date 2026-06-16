# cardBot Code Review — Round 2

Date: 2026-06-16  
Branch reviewed: `scratch`  
Commit reviewed: `61c5cec`  
Reviewer: coding agent, intentionally opinionated

## Commands run

```bash
git status --short --branch
go test ./...
go vet ./...
staticcheck ./...
bash -n scripts/*.sh
make test
go list -m -u all
```

Results:

- `go test ./...` passed.
- `go vet ./...` passed.
- `make test` / race tests passed.
- `bash -n scripts/*.sh` passed.
- `staticcheck ./...` failed on one real finding:
  - `app/commands.go:357:15: func (*App).runSpeedTest is unused (U1000)`
- `govulncheck` is not installed locally, so vulnerability scanning was not run.

## Executive take

The codebase is not a disaster. The package split is mostly sensible, tests are real, the copy engine has meaningful safety checks, and the prior review fixes landed cleanly.

But the project has drifted in three ways:

1. **Release/process drift is now the biggest risk.** `scratch` is healthier than `main`, but install docs and GitHub workflows still point at `main`. Release binaries are not stamped with tag versions. macOS releases are currently built with `CGO_ENABLED=0`, which means the native macOS detector path is not what users get.
2. **The file-copy model needs a second design pass before 1.0.** Timestamp naming can collide/wrap. Full verification does not verify already-skipped files. Sidecars are not modeled. These are data-trust issues, not polish.
3. **The app event loop works, but it is getting overgrown.** `runInteractive`, `copyFiltered`, `cardcopy.Run`, and `analyze.Analyze` are big multi-phase functions. The nested copy event loop is clever, but it is also the kind of clever that will make future bugs weird.

If I were steering this project, I would stop adding features for a minute and do a stabilization sprint: release pipeline, copy-plan correctness, sidecars/collisions, and branch discipline.

---

## High-priority findings

### 1. `main` and `dev` still have the old broken Go module path

**Severity:** High  
**Files/branches:** `origin/main:go.mod`, `origin/dev:go.mod`, `origin/scratch:go.mod`

Current state:

```txt
origin/main:    module github.com/illwill/cardbot
origin/dev:     module github.com/illwill/cardbot
origin/scratch: module github.com/wdphoto/cardBot
```

`scratch` is fixed, but the public install docs fetch from `main`:

```bash
curl -fsSL https://raw.githubusercontent.com/wdphoto/cardBot/main/scripts/install.sh | sh
```

That means the public/default branch still represents the old identity. Any external Go consumer or future release cut from `main` inherits the wrong module path.

**Opinion:** `scratch` should either become `main` soon, or the module-path fix needs to be cherry-picked/merged to `main` immediately. Keeping the good version on `scratch` while docs/releases point at `main` is asking for another confusing night.

**Recommendation:**

- Fast-forward or merge `scratch` into `main` once happy.
- Keep `dev` only if it has a real job. If not, delete it later after confirmation.
- Consider protecting `main` and requiring CI before releases.

---

### 2. Release binaries are not stamped with the tag version

**Severity:** High  
**File:** `.github/workflows/release.yml`

The Makefile stamps version/commit/date:

```make
-X 'main.version=$(VERSION)'
-X 'main.commit=$(COMMIT)'
-X 'main.date=$(DATE)'
```

But release workflow builds with only:

```bash
go build -ldflags="-s -w" -o cardbot-${{ matrix.suffix }} .
```

So release binaries use the source default:

```go
version = "0.9.0"
commit  = "none"
date    = "unknown"
```

Why this matters:

- `cardbot --version` can lie.
- `cardbot self-update` compares against the wrong current version.
- First-run changelog behavior keys off the wrong version.
- A `v0.9.1` release could still report `0.9.0`.

**Recommendation:** Build releases with the same ldflags as `make build`, derived from the tag and commit:

```bash
VERSION="${GITHUB_REF_NAME#v}"
COMMIT="${GITHUB_SHA::7}"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
go build -ldflags="-s -w \
  -X 'main.version=${VERSION}' \
  -X 'main.commit=${COMMIT}' \
  -X 'main.date=${DATE}'" \
  -o cardbot-${{ matrix.suffix }} .
```

Also add a release smoke step:

```bash
./cardbot-${{ matrix.suffix }} --version
```

---

### 3. macOS release builds do not use the native macOS detector

**Severity:** High/Medium  
**Files:** `.github/workflows/release.yml`, `detect/detect_darwin.go`, `detect/detect_darwin_nocgo.go`

The release workflow cross-compiles Darwin binaries from Ubuntu with:

```yaml
CGO_ENABLED: 0
```

That means release binaries use:

```go
//go:build darwin && !cgo
```

not the native DiskArbitration implementation:

```go
//go:build darwin && cgo
```

So the implementation with native callbacks, `DADiskEject`, etc. is probably not what real users get from releases. They get the polling `diskutil` fallback.

**Opinion:** Either own the polling implementation and delete/de-emphasize the cgo path, or build Darwin releases on `macos-*` runners with cgo enabled. Right now the project gives itself credit for native macOS detection but ships the fallback.

**Recommendation options:**

1. **Prefer native:** build Darwin artifacts on macOS runners with cgo enabled.
2. **Prefer simple static binaries:** keep `CGO_ENABLED=0`, but document that release builds use polling and demote the cgo path to experimental/local.
3. Add a startup/debug line or `--version` detail showing detector backend (`darwin-cgo`, `darwin-poll`, `linux-poll`).

---

### 4. Timestamp naming silently wraps after 9999 and can collide

**Severity:** High for data safety  
**Files:** `cardcopy/naming.go`, `cardcopy/copy.go`

Current behavior:

```go
if seq > sequenceMax {
    seq = 1 // Loop back to 0001 after 9999
}
```

This is dangerous. If timestamp mode produces the same destination path twice in one run, later files can overwrite earlier files or be skipped as already existing depending on size.

The roadmap already hints at this:

> Multi-Camera Collision Prevention — Two cameras shooting same event can produce identical filenames in timestamp mode.

But this is broader than multi-camera. It can happen with:

- >9999 files in one date/subfolder sequence.
- Multiple cameras with identical timestamps.
- Burst/timelapse workflows.
- Sidecars if/when they get modeled.

**Opinion:** A file ingest tool cannot silently wrap names. That is the exact class of bug users will never forgive.

**Recommendation:** Create a destination-name allocator that guarantees uniqueness before copy starts.

Possible rules:

- Never wrap silently.
- If sequence exceeds `9999`, widen to 5+ digits or fail with a clear error.
- Detect duplicate destination paths in the planned batch before writing anything.
- If destination already exists with a different size/hash, generate a conflict suffix or stop.

The copy engine should build a copy plan first, then validate that plan is collision-free.

---

### 5. `verify_mode=full` does not verify skipped files

**Severity:** High/Medium  
**File:** `cardcopy/copy.go`

Current skip logic:

```go
if info, statErr := os.Stat(destPath); statErr == nil && info.Size() == f.size {
    filesSkipped++
    bytesSkipped += f.size
    bytesDone += f.size
    filesDone++
    continue
}
```

When `VerifyMode` is `full`, freshly copied files are byte-compared after copy, but existing same-size files are still skipped by size only. Later, the app writes the dotfile with:

```go
Verified:           true,
VerificationMethod: result.VerifyMethod,
```

So a card can be marked as fully verified even though already-present files were not byte-verified during that run.

**Recommendation:** Decide what `full` means and make it true.

Options:

- In `full` mode, byte-verify same-size existing files before counting them as skipped.
- Track `FilesVerified`, `BytesVerified`, or `SkippedVerified` separately.
- If full verification of skipped files is too slow, say so in output and do not mark the entire dotfile as `Verified: true`.

---

## Medium-priority findings

### 6. `speedtest` is dead product surface right now

**Severity:** Medium  
**Files:** `app/commands.go`, `speedtest/`

`staticcheck` reports:

```txt
app/commands.go:357:15: func (*App).runSpeedTest is unused (U1000)
```

There is a whole `speedtest` package that writes/reads a 256 MB test file, but no command maps to it. Help does not mention it. `parseInputAction` has no key for it.

**Opinion:** This is either a hidden feature that got lost or a risky experiment that should be deleted until wanted. Writing a 256 MB benchmark file to a camera card is not a casual action; if exposed, it needs explicit user consent and scary copy.

**Recommendation:** Pick one:

- Wire it intentionally behind something like `[b] Benchmark card` with confirmation.
- Move it to a separate developer-only command.
- Delete `speedtest/` and `runSpeedTest` for now.

My bias: delete or hide behind a CLI-only diagnostic command. It does not belong in the default ingest loop yet.

---

### 7. Sidecars are not modeled, and that will hurt photo workflows

**Severity:** Medium/High depending target users  
**Files:** `analyze/analyze.go`, `cardcopy/copy.go`, `agent/FILE-TYPES.md`

The analyzer intentionally ignores non-media files like `.XMP`, `.THM`, `.LRV`, etc. The copy engine copies all non-hidden files for `all`, but selective modes (`photos`, `selects`, `today`, `yesterday`) only include files matching the filter.

Problems:

- External `.XMP` sidecars are not read for ratings.
- `selects` will miss ratings stored only in sidecars.
- `photos` copies RAW/JPEG but not necessarily their `.XMP` sidecars.
- Timestamp renaming would need to rename sidecars in lockstep with their media file, or the relationship breaks.

**Opinion:** If this is for real photo ingest, sidecars are not optional. They are a first-class part of the asset.

**Recommendation:** Introduce an asset model:

```txt
Asset
  primary: DSC_0001.NEF
  sidecars: DSC_0001.XMP, DSC_0001.WAV, DSC_0001.THM, ...
  captureTime
  rating
  destinationStem
```

Then copy assets, not raw files. Timestamp naming should assign a stem to the asset and preserve sidecar association.

---

### 8. The app event loop has become too clever

**Severity:** Medium  
**Files:** `app/app.go`, `app/commands.go`, `app/handlers.go`

`Run()` owns the main event loop, except during copy, where `copyFiltered()` takes over detector/input handling with its own select loop. The comment explains it well, but the design is still risky.

Risks:

- Any new event source must be remembered in two loops.
- Output comes from multiple goroutines and is only partially guarded by `printMu`.
- `copyFiltered` is doing user messaging, event routing, cancellation, progress, copy execution, queue handling, and result handling in one function.

Approximate long functions:

```txt
main.go:runInteractive       ~229 lines
app/commands.go:copyFiltered ~245 lines
cardcopy/copy.go:Run         ~277 lines
analyze/analyze.go:Analyze   ~222 lines
```

**Recommendation:** Move toward one event loop and message passing.

Sketch:

- Copy runs in a worker goroutine.
- Worker emits `copyProgress`, `copyDone`, `copyFailed` messages.
- Detector/input always flow through one app event loop.
- Rendering happens in one place.

This is a simplification, not an abstraction-for-abstraction's-sake refactor.

---

### 9. Copy engine should have an explicit planning phase

**Severity:** Medium  
**File:** `cardcopy/copy.go`

`cardcopy.Run` currently does everything:

1. normalize roots
2. walk files
3. filter
4. sort
5. compute destination names
6. dry-run preview
7. create/probe destination
8. disk-space check
9. copy
10. verify
11. summarize

It works, but this is why collision detection, sidecar support, skip verification, dry-run parity, and disk-space estimation are all tangled.

**Recommendation:** Split into concepts:

```go
type Plan struct {
    Files []PlannedFile
    BytesTotal int64
    BytesToWrite int64
    Collisions []Collision
}

func BuildPlan(opts Options) (*Plan, error)
func ValidatePlan(plan *Plan) error
func ExecutePlan(ctx context.Context, plan *Plan, onProgress ProgressFunc) (*Result, error)
```

Dry-run should print the same plan that execute uses.

---

### 10. Detector lifecycle is not cleanly restartable

**Severity:** Medium  
**Files:** `detect/detect_linux.go`, `detect/detect_darwin_nocgo.go`, `detect/detect_darwin.go`

The Linux and no-cgo Darwin detectors close `stopChan` in `Stop()`, but `Start()` does not recreate it. If a detector instance is stopped and then started again, the poll loop sees the already-closed channel and exits immediately.

The cgo Darwin detector has a different lifecycle smell: `Stop()` stops the run loop and releases the session from the caller goroutine without waiting for the locked OS-thread goroutine to exit. That may be fine most days, but this is exactly the kind of platform lifecycle edge that becomes a sleep/wake bug.

**Recommendation:**

- Make detector instances one-shot and document/enforce it, or make restart work.
- Prefer `Start(ctx)` / `Wait()` / `Close()` style lifecycle over manual stop channels.
- Add tests for Start → Stop → Start where possible.
- For cgo, track the run loop goroutine with a `WaitGroup` and release CF resources in one predictable place.

---

### 11. CI does not reflect how you actually work right now

**Severity:** Medium  
**Files:** `.github/workflows/ci.yml`

CI triggers only on `main`/`master` pushes and PRs:

```yaml
branches: [main, master]
```

But active work is happening on `scratch`. That means direct pushes to `scratch` do not get CI coverage unless GitHub has branch rules/PRs not visible here.

Also CI does not run:

- `go vet ./...`
- `staticcheck ./...`
- `bash -n scripts/*.sh`
- `go build ./...`
- cross-platform compile checks
- vulnerability scanning

**Recommendation:**

- If `scratch` remains active, include it in CI triggers or work through PRs.
- Add `go vet` and shell syntax checks now.
- Add `staticcheck` once the dead speedtest finding is resolved.
- Add `govulncheck` via `golang.org/x/vuln/cmd/govulncheck`.
- Add at least compile checks for Linux and Darwin build tags.

---

### 12. Update/version parsing is permissive and underspecified

**Severity:** Medium/Low  
**File:** `update/update.go`

`semverRe` is:

```go
^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?
```

Because it is not anchored at the end, strings like `v1.2.3-whatever` parse as `1.2.3`. Maybe that is fine, but it is not deliberate semver handling. Pre-releases are ignored. Build metadata is ignored. Invalid checksum lines are loosely accepted.

**Recommendation:**

- Either explicitly support only simple `vMAJOR.MINOR.PATCH` tags with an anchored regex, or use a small semver parser.
- Validate checksums are 64 hex chars.
- Add tests for prerelease tags and malformed checksum files.

---

### 13. Setup always tries the GUI folder picker first on macOS

**Severity:** Medium/Low  
**Files:** `setup.go`, `pick/pick_darwin.go`

`promptDestinationWithIO` opens the native picker before readline fallback. That is nice for normal desktop use, but surprising for:

- SSH sessions
- scripted setup
- terminal-only users
- CI/dev tests outside the current mocked paths

**Recommendation:**

- Only use the GUI picker when stdin/stdout are TTYs and the session looks graphical.
- Add `--setup --no-gui` or an env var like `CARDBOT_NO_PICKER=1`.
- Consider making readline the default and offering `[o] Open folder picker`.

---

### 14. `readInput` cannot stop until stdin returns

**Severity:** Low/Medium  
**File:** `app/app.go`

`readInput` blocks in:

```go
line, err := reader.ReadString('\n')
```

The context is checked only after a line is read. On shutdown, the goroutine may remain blocked until process exit or a newline. In practice this is probably harmless for a CLI process, but it is part of the broader input/event-loop awkwardness.

**Recommendation:** When you eventually do single-key/raw-terminal mode, fix this at the same time. Do not spend time on it separately unless it causes tests or daemon issues.

---

## Documentation and planning drift

### 15. Docs disagree with current behavior and branch reality

**Severity:** Medium  
**Files:** `README.md`, `NOTES.md`, `agent/*`

Examples:

- `README.md` roadmap says `0.9.0` is next, while `main.go` default version is `0.9.0`.
- `NOTES.md` says daemon terminal selection was simplified to Terminal.app, but code supports Default/Ghostty/custom launch args.
- Ignored `agent/ROADMAP.md` says 0.9.0 is copyright injection, but current 0.9.0 appears to be copy/refactor/module cleanup.
- Ignored `agent/DECISIONS.md` says environment vars are not used; code now has `CARDBOT_*` overrides.
- Ignored `agent/PERFORMANCE.md` says startup target is `<100ms`; current startup intentionally sleeps/spins and performs an update check.
- Ignored `agent/PERFORMANCE.md` says pre-allocate destination files; copy code does not do that.

**Opinion:** The ignored `agent/` notes are useful archaeology but dangerous as truth. Either archive them as historical notes or periodically distill the still-valid ideas into tracked docs/issues.

**Recommendation:**

- Make `README.md` short and user-focused.
- Make `NOTES.md` either a real manual or delete/rename it.
- Move still-valid roadmap items into tracked `ROADMAP.md` or GitHub issues.
- Treat ignored `agent/` docs as scratchpad only.

---

## Lower-priority observations

### LaunchAgent label still says `com.illwill.cardbot`

**Severity:** Low/Decision  
**File:** `launch/agent.go`, `scripts/uninstall.sh`, docs

This may be fine as a stable reverse-DNS-ish identifier, but the public repo is now `wdphoto/cardBot`. If you rename it, you need migration cleanup for the old plist. If you keep it, document that the launchd label is intentionally legacy/stable.

My bias: keep it for now unless there is a branding reason. LaunchAgent migrations are annoying.

### Log rotation keeps only one `.old`

**Severity:** Low  
**File:** `cblog/log.go`

Fine for a small CLI, but if daemon debugging gets verbose, one backup may be too little. Consider `cardbot.log.1`, `.2`, etc. later.

### Unknown config schema can be overwritten by setup

**Severity:** Low  
**File:** `config/config.go`, `app/setup.go`

Unknown config schema loads defaults with a warning. If setup then saves, it can overwrite the unknown config. Dotfile handling is more conservative and refuses unknown schemas. Not urgent, but worth aligning before schema v2.

### Dependency hygiene is okay, but add automation

**Severity:** Low  
**File:** `go.mod`

`go list -m -u all` showed:

```txt
github.com/evanoberholster/imagemeta v0.3.1 [v1.0.0]
golang.org/x/sys v0.42.0 [v0.46.0]
golang.org/x/term v0.1.0 [v0.44.0]
```

Do not blindly upgrade `imagemeta` across a major version in the middle of copy-engine work. But add Dependabot or a periodic dependency chore branch. Also add `govulncheck` to CI.

---

## Refactor proposal I would actually do

### Phase 1: Release/branch hygiene

1. Merge/fix module path on `main`.
2. Fix release ldflags.
3. Decide macOS detector backend for releases.
4. Add CI checks: `go vet`, `staticcheck`, shell syntax, `govulncheck`.

### Phase 2: Copy correctness

1. Add a copy planning phase.
2. Detect duplicate destination paths before copying.
3. Fix timestamp sequence overflow.
4. Define full-verify semantics for skipped files.
5. Add tests for collisions, >9999 sequence, existing same-size wrong-content, and sidecars.

### Phase 3: Asset model

1. Model primary media + sidecars.
2. Read ratings from external `.XMP` sidecars.
3. Copy/rename sidecars with their primary file.
4. Revisit `selects`, `photos`, `today`, and `yesterday` using assets instead of raw files.

### Phase 4: App/event simplification

1. Replace the nested copy event loop with worker messages.
2. Centralize rendering/output.
3. Move toward raw single-key input if desired.
4. Reduce `runInteractive`, `copyFiltered`, `cardcopy.Run`, and `analyze.Analyze` into smaller units.

---

## Things I would delete or quarantine

- `runSpeedTest` / `speedtest` unless you intentionally expose it.
- Stale ignored roadmap claims from `agent/` after salvaging the useful ideas.
- Any branch that is not part of the real workflow (`dev` maybe, later, with confirmation).

---

## Open questions for the project owner

1. Is `scratch` now supposed to become `main`, or is `scratch` a permanent working branch?
2. Do you care enough about native macOS DiskArbitration to build Darwin releases on macOS runners?
3. Is speed testing a real user feature, a dev diagnostic, or dead code?
4. Do you want sidecar-aware photo ingest before 1.0?
5. Should the LaunchAgent label remain `com.illwill.cardbot` for stability, or migrate to a `wdphoto` label?

## Bottom line

The next best work is not more UI polish. It is release correctness and copy-plan correctness. Once those are solid, the project will feel much less spooky to move, install, update, and trust with real cards.
