# cardBot Codebase Review — 2026-09-08

Spring-cleaning review branch `review/go-spring-cleaning`, base commit `521fe66`
(clean CI baseline). This is a point-in-time, read/document-only audit: it makes
**no production or test code changes**. Findings below are for a future,
user-approved implementation pass. Suggestions that were considered but will
not be implemented as stated are explicitly labelled.

This review supersedes the *actionable shortlist* point of reference in
[`CODE_REVIEW_2026-07-09.md`](CODE_REVIEW_2026-07-09.md). The 2026-07-09 report
remains the audit trail for the decisions that have since landed.

## Executive assessment

The codebase is in genuinely good shape. The packages I surveyed in depth
(`app`, `cmd`, `config`, `analyze`, `cardcopy`, `term`) are coherent, the data-
safety core (`cardcopy`) is transactionally careful, and the recent event-loop /
copy-lifecycle and destination-containment work holds up under review: copy
completion is event-loop-owned, per-copy identity rejects stale outcomes,
destination commits are no-replace and symlink-contained, path whitespace is
preserved through the copy path, daemon startup fails closed on bad config, and
the daemon/log/launch/platform seams are test-isolated.

I identified **no correctness or data-safety regressions** in my depth.
The strongest deliverables are a handful of small maintainability/robustness
findings (one genuine dead-code/duplication item, one reachable whitespace
edge, two cross-checked Linux hardware-detection defects, and one
documentation/installer-safety item), plus two clearly-labelled optional
hardening suggestions. The codebase does not need an architectural or cosmetic
churn pass.

## Baseline evidence (Conductor-cited)

Before these documentation edits, on a clean checkout at the baseline commit, Conductor ran:

- `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./... -count=1` — pass (all packages).
- `go build ./...` — pass.
- `go vet ./...` — pass.
- Clean working tree at the baseline commit `521fe66` on `review/go-spring-cleaning` (before the documentation edits that created `CODE_REVIEW_2026-09-08.md` and updated `TODO.md`).
- Review (wS:p3) additionally ran `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./pick ./update ./detect ./launch ./daemon ./instance ./cblog ./dotfile ./fsutil -count=1` — pass (synthetic/fixture-only). Attributed to Review (wS:p3).

Local checks in this report are prescribed as `GOTOOLCHAIN=local GOPROXY=off`
so that no toolchain or module downloads can occur. The native DiskArbitration
detector test stays opted out locally (`CARDBOT_TEST_LIVE_DETECTOR=0`).

Host toolchain observed locally is Go `1.27.1`; the pinned baseline
CI/release toolchain is `1.26.8` (per `go.mod` and the CI/release workflows).
The green baseline run referenced is CI
[34205821049](https://github.com/wdphoto/cardBot/actions/runs/34205821049). No
new CI run was triggered for this documentation-only pass; the local baseline
is therefore **not** the exact CI toolchain, so results were not cross-checked
against `1.26.8` locally.

## Reviewed-package scope

Joint read across Code (wS:p2) and Review (wS:p3). "Primary depth" = read in
full; "cross-check" = reviewed for candidates / integration evidence.

| Package | Read depth | Notes |
|---|---|---|
| `app` | primary | Event loop, copy lifecycle, commands, input, display, setup, deps, path, update glue — all read in full. |
| `cmd` | primary | root/interactive, daemon subcommands + status, setup, changelog, verbose, boot/logo — all read in full. |
| `config` | primary | Load/Save/status, atomic temp+rename, env overrides — read in full. |
| `analyze` | primary | Walk, parallel EXIF, XMP rating, merge — read in full. |
| `cardcopy` | primary | Plan/Execute, copyStream, no-replace commit, verify, naming, destination containment, throughput, diskspace — read in full. |
| `term` | primary | ANSI/format helpers — read in full. |
| `pick` | cross-check | `script.go`, `pick_darwin.go` reviewed for the destination-whitespace finding. |
| `update` | cross-check | temp publication + Sync durability finding. |
| `detect` | cross-check | Linux hardware detection candidates (Review-owned). |
| `daemon`, `launch`, `instance`, `cblog`, `dotfile`, `fsutil` | cross-check | Review-owned; no new candidates from my pass. |

Coverage guidance below is **historical** and not re-measured on this pass. At the 2026-07-09 review (`CODE_REVIEW_2026-07-09.md`) the data-bearing packages (`config`, `cardcopy`, `dotfile`, `daemon`) carried the strongest statement coverage (reported `87–89%`). `TESTING.md` records the current isolation and manual-QA boundaries. This spring-cleaning pass did not re-run coverage measurement, so no current-coverage ranking is claimed.

## Findings

Ordered by priority. Each notes baseline `file:line`, symbol, smallest safe
change, payoff, behavior/risk caveats, focused validation, effort (S/M/L),
and confidence (high/med/low).

### Recommended

#### 1. Manual destination entry strips significant whitespace (reachable)
- **Baseline:** `cmd/setup.go:43-57` `promptDestinationReadlineIO`, specifically `cmd/setup.go:53` `line = strings.TrimSpace(line)`; persisted via `app/setup.go:39-52` `RunSetup` → `config.ContractPath`.
- **What:** When the folder picker is unavailable/declined (fallback readline path is used at `cmd/setup.go:40`), a manually typed target ending in spaces (e.g. a directory literally named `Client ` → the line `/dest/Client \n` after the trailing-space component) is trimmed to `/dest/Client`; `ContractPath` then **preserves** the already-trimmed string rather than restoring the spaces. A legal POSIX directory name beginning or ending in spaces is silently shortened in the saved destination. This conflicts with the project's explicit significant-whitespace preservation invariant (README "timestamp/copy roots"; TODO read-only usability pass; the recent Nikon trailing-space regression `c02c970`).
- **Smallest change:** strip only the line terminator (`\n` plus an optional preceding `\r`) instead of all whitespace; keep blank/whitespace-only input falling back to the default.
- **Payoff:** addresses an identified path-whitespace gap in the setup flow, bringing manual entry in line with copy-root handling.
- **Caveats:** low user frequency (manual typing), so low severity — but it is the one concrete, reachable space-loss path in `app/cmd` depth. The **native picker** (`pick/pick_darwin.go:18`) is **not proven** reachable: `POSIX path of (choose folder …)` normally ends in `/` for a directory, so trailing basename spaces survive `TrimSpace`; no GUI test was gathered, so it is not reported as a defect.
- **Validation:** `GOTOOLCHAIN=local GOPROXY=off go test ./cmd ./app` plus a synthetic readline test for a trailing-space path and a blank default.
- **Effort:** S. **Confidence:** high (reachability), low-medium (impact).

#### 2. Dead + duplicated production flag parser `parseDaemonStatusOptions`
- **Baseline:** `cmd/daemon_status.go:90-109`.
- **What:** `parseDaemonStatusOptions(args []string)` is only called from `cmd/daemon_status_test.go`; production never invokes it. It re-implements `--json` / `--recent-launches` parsing with a separate stdlib `flag.FlagSet`, duplicating the cobra flags declared in `newDaemonStatusCommand` (`cmd/root.go:209-227`). It is the same U1000-style dead/duplicate pattern the 2026-07-09 review already removed once (`runDaemonStatusCommand`).
- **Smallest change:** remove `parseDaemonStatusOptions` and migrate its meaningful cases onto the real cobra command. For positive parsing, exercise `newDaemonStatusCommand`'s `ParseFlags`, `Flags` getters, and `Args` **without** a live `RunE`. For the negative case, test the actual `RunE` directly: `newDaemonStatusCommand.RunE` returns `--recent-launches must be >= 0` at `cmd/root.go:215-218` **before** `runDaemonStatus` runs, so that failure branch is live-host-free and needs no seam. **No new validator or helper is required.** A positive full-`RunE` path is **not** testable via the constructor alone: `collectDaemonStatusReportWith` is a `runDaemonStatus`/`collectDaemonStatusReport` helper not wired into the command's `RunE`, so a positive `RunE` remains a live-host path until a narrow runner seam is added. Do **not** relocate the duplicate parser to a `_test.go` file — that preserves a meaningless parallel implementation and parallel tests.
- **Payoff:** removes duplicated parsing surface that can drift from the real command (the drift risk is real: `newDaemonStatusCommand` already validates `--recent-launches >= 0` itself).
- **Caveats:** none behavioral; tests already cover the same flags via the parser.
- **Validation:** `GOTOOLCHAIN=local GOPROXY=off go test ./cmd`.
- **Effort:** S. **Confidence:** high.

#### 3. Linux volume UUID matched by substring (`sda1` vs `sda10`)
- **Baseline:** `detect/hardware_linux.go:176-193` `getVolumeUUID`, specifically `:189` `strings.Contains(target, device)`.
- **What:** a symlink target like `../../sda10` contains `sda1`, so requesting device `sda1` can return the wrong by-UUID entry (or a wrong display). Hardware-display only, not copy behavior.
- **Smallest change:** compare `filepath.Base(target) == device`.
- **Payoff:** corrects a wrong-UUID/hardware-display edge: the matched `/dev/disk/by-uuid` entry feeds `HardwareInfo.VolumeUUID` (surfaced via `FormatHardwareInfo`), so a spurious `sda1`→`sda10` match can display the wrong volume UUID.
- **Caveats:** Linux-only, display-only; no data path involvement.
- **Validation:** a small pure test over readlink-base matching (no host `/dev/disk/by-uuid` needed).
- **Effort:** S. **Confidence:** high.
- **Attribution:** Review (wS:p3); independently confirmed at source.

#### 4. Linux `/proc/mounts` space escaping breaks spaced mount paths
- **Baseline:** `detect/hardware_linux.go:117-133` `findBlockDevice`, comparing `fields[1] == mountPath`.
- **What:** kernel mount lines escape spaces in mount paths as octal `\040`; a mounted path containing a space is never equal to the unescaped `mountPath`, so hardware lookup fails for it.
- **Smallest change:** decode octal escapes in the mount path field before comparison; factor the line → (device, path) parsing into a pure function so it can be unit-tested synthetically.
- **Payoff:** restores Linux hardware ID lookup for spaced mount points (Quick/Get info paths).
- **Caveats:** Linux-only, detection/display only.
- **Validation:** a table-driven test with a literal `\040`-escaped mount line.
- **Effort:** M. **Confidence:** high.
- **Attribution:** Review (wS:p3); confirmed at source.

#### 5. `NOTES.md` "Quick Teardown" teaches destructive manual removal, contradicting the hardened uninstaller
- **Baseline:** `NOTES.md:225-239` (Quick Teardown block) and `:247-251` (uninstall script section).
- **What:** The quick-teardown block instructs `pkill -f "cardbot --daemon"`, direct `launchctl bootout` + plist deletion, direct binary `rm`, and `rm -rf` of config/logs. This contradicts the identity-verified, recorded-installation uninstall path the project hardened (TODO "Make uninstall target only recorded or identity-verified installations").
- **Smallest change:** replace the broad quick-teardown block (`pkill -f "cardbot --daemon"`, direct `launchctl bootout`, direct plist/binary `rm`, `rm -rf` of config/logs) with the verified uninstaller path, retaining the script's existing POSIX `sh` invocation (`sh scripts/uninstall.sh`). Do not add bash-only constructs; focus on removing the unsafe direct-removal guidance.
- **Payoff:** the documented path routes teardown through the existing identity-verified uninstaller instead of bypassing it with broad `pkill -f "cardbot --daemon"` / direct `rm`; it does not claim to prevent user misuse of the script itself.
- **Caveats:** docs-only; the script itself already enforces recorded/verified removal.
- **Validation:** `bash -n scripts/uninstall.sh`.
- **Effort:** S. **Confidence:** high.

### Optional / suggestions (not defects)

#### 6. Updater temp file is not `fsync`ed before rename
- **Baseline:** `update/update.go:137-148` (`tmp.Chmod` → `tmp.Close` → `os.Rename`; no `tmp.Sync()`).
- **What:** the verified replacement binary is closed and atomically renamed without an explicit sync, so a crash/power loss shortly after rename can leave an undurable replacement.
- **Smallest change:** `tmp.Sync()` before `tmp.Close()`, returning a contextual error on failure.
- **Caveats:** this is **incremental durability hardening, not a power-loss guarantee** — device-level durability still needs manual/real-device QA (Conductor's framing, agreed). Do not introduce a filesystem abstraction purely to test it.
- **Validation:** existing `update` failure-preservation tests plus `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./update -count=1` (synthetic only; no direct `Sync`-fault test is required).
- **Effort:** S. **Confidence:** high that `tmp.Sync()` is absent (a factual, evidence-backed gap) — distinct from priority/payoff, which is optional and low (incremental durability only, not a power-loss guarantee).
- **Attribution:** Review (wS:p3); confirmed at source.

#### 7. App stdin reader goroutine is not context-aware while blocked
- **Baseline:** `app/app.go:254-267` `readInput` uses `bufio.Reader.ReadString`, so while it is blocked waiting on a stdin line it cannot observe `ctx` cancellation (the `<-ctx.Done()` case only applies after a line is read).
- **Smallest change:** none required for correctness (the process exits after the event loop returns). If joinable shutdown is desired, select over `os.File` reads is complex; not worth it for a CLI.
- **Caveats:** benign; documented as a deferred/latent pattern, not a bug.
- **Effort:** n/a (deferred). **Confidence:** n/a.

#### 8. Unused viper flag bindings for `--dry-run`, `--setup`, `--daemon`
- **Baseline:** `cmd/root.go:159-162` `v.BindPFlag(...)` for `dry-run`, `setup`, `daemon`; `applyConfigOverrides` only reads `destination`, `naming`, `log-file`, `verify-mode`.
- **Smallest change:** drop the unused `BindPFlag` lines (the flags are consumed directly via `opts.*`).
- **Caveats:** removal must preserve existing CLI behavior. Do **not** "use" these bindings: wiring them into `applyConfigOverrides` could turn previously inert `CARDBOT_DAEMON`/`CARDBOT_SETUP`/`CARDBOT_DRY_RUN` environment variables into new activation routes. The goal is only to delete the dead bindings.
- **Validation:** existing `cmd` Cobra parsing tests plus `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./cmd -count=1` to confirm flag/environment behavior is unchanged after dropping the bindings.
- **Effort:** S. **Confidence:** high (that the bindings are unused).

## Rejected / deferred ideas (not reported as findings)

- **Native picker `TrimSpace` (`pick/pick_darwin.go:18`).** Unproven, not impossible: folder-`choose` output normally ends in `/`, protecting trailing basename spaces; no GUI test was gathered. Suppressed per Conductor/Review correction. (The reachable path is finding **1**, the readline entry.)
- **`config.LoadWithStatus` missing-`$schema` forward-compat nuance.** A future config that adds new top-level fields without bumping `$schema` would be accepted and later rewritten. Speculative edge; the schema-marker mechanism already covers the realistic case.
- **`analyze.extractEXIF` final `reportProgress(len(files))` after cancellation.** Displays an over-count on cancel / counts only EXIF candidates. Cosmetic.
- **Duplicate mode→label maps (`app/commands.go` `modeLabel` vs `app/input.go` `modeDisplayName`).** Minor drift risk, not worth churn now.
- **Speculative performance claims.** None raised: no profiling-led buffer/worker changes were proposed. (Release QA benchmark notes in `TODO.md` Manual Release QA already cover hardware tuning.)

## Retained-good patterns (do not regress)

Explicitly preserved invariants this review kept in scope and found intact:

- **Copy containment / no-replace / commit safety** — `cardcopy`: destination rooted via `os.Root`, component-by-component identity checks (`destination.go`), no-replace commit (`commit_darwin` `RENAME_EXCL`, `commit_linux` `RENAME_NOREPLACE`, `commit_other` hard-link), re-validate skips before success, late-conflict abort (`copy.go`, `copyToDir`), own-part-only cleanup.
- **Cancellation** — full-verification `verifyBytes` checks `ctx` between reads; `trackingReader`/`copyStream` propagate cancellation and short writes; `copyToDir` aborts before publish on `ctx.Err()`.
- **Path whitespace** — copy roots preserve significant internal/trailing spaces; the path-whitespace gaps identified in this pass are the setup readline path (finding **1**) and the Linux `/proc/mounts` decode (finding **4**).
- **Daemon fail-closed config** — headless/daemon startup refuses to run unless config loads `LoadValid` (`cmd/daemon_helpers.go:validateDaemonConfig`); malformed/unsupported files are never autosaved over.
- **Ingest-history / `.cardbot` safety** — exclusive unique temp publication, sync/close before rename, pre-existing temp preservation (`dotfile`).
- **Platform boundaries** — polling macOS detector as the shipped backend, DiskArbitration behind the explicit `cardbot_native` build tag, opt-in with `CARDBOT_TEST_LIVE_DETECTOR=1` on hosted CI only; Windows runtime/detection is unsupported, but the Windows cross-build is supported via stub hardware (`detect/hardware_other.go`) and exercised by a CI cross-build target.
- **Test isolation** — native detector/live checks gated in CI, synthetic fixtures, injected detector/scan/status/stdin seams, bounded fuzz targets, `GOPROXY=off` reproducible checks.

## Evidence & limitations

- Line numbers are against `521fe66` on `review/go-spring-cleaning`.
- Findings 3 and 4 (Linux `detect`) were not exercised on hardware; they are evident from the parsing logic alone.
- The updater durability item (6) and app stdin item (7) are hardening notes, not observed failures.
- Real-device verification (transfer, power loss, device sync/close, permissions, sleep/wake, physical removal, login/launchd) is explicitly out of scope for automated review and remains manual/hardware QA per `TESTING.md`.

## Status

- Findings/documentation only. No production or test code changed.
- Shortlist mirrored to `TODO.md` ("Spring-cleaning review — 2026-09-08") as unchecked items.
- Independently reviewed and approved by Review (wS:p3); all findings remain unimplemented (documentation only). Hand-off to Conductor (wS:p1) for the final checkpoint.
