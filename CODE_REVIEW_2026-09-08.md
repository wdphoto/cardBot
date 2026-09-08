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

The first pass found no data-safety corruption and no architectural issue in
my depth. A subsequent adversarial second pass (Code, cross-checked by Review
and Conductor) surfaced additional **path-boundary correctness issues** beyond
the stored-destination whitespace case: root argument classification rejecting
a bare-relative edge-space target, and daemon/launcher trims of the executable
path and the destination-derived working directory (finding **9**; see the
second-pass audit trail). The earlier "no correctness regressions / one gap"
framing is corrected here. Deliverables are the corrected findings **#1–#9**
plus clearly-labelled optional hardening suggestions and deferred items. The
codebase does not need an architectural or cosmetic churn pass.

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

Each notes baseline `file:line`, symbol, smallest safe
change, payoff, behavior/risk caveats, focused validation, effort (S/M/L),
and confidence (high/med/low).

Finding IDs (#1–#9) are **stable identifiers, not a priority ranking**;
grouping into Recommended / Recommended correctness / Optional reflects current
judgment and may be non-sequential.

### Recommended

#### 1. Manual destination entry strips significant whitespace (reachable)
- **Baseline:** `cmd/setup.go:43-57` `promptDestinationReadlineIO`, specifically `cmd/setup.go:53` `line = strings.TrimSpace(line)`; persisted via `app/setup.go:39-52` `RunSetup` → `config.ContractPath`.
- **What:** When the folder picker is unavailable/declined (fallback readline path is used at `cmd/setup.go:40`), a manually typed target ending in spaces (e.g. a directory literally named `Client ` → the line `/dest/Client \n` after the trailing-space component) is trimmed to `/dest/Client`; `ContractPath` then **preserves** the already-trimmed string rather than restoring the spaces. A legal POSIX directory name beginning or ending in spaces is silently shortened in the saved destination. This conflicts with the project's explicit significant-whitespace preservation invariant (README "timestamp/copy roots"; TODO read-only usability pass; the recent Nikon trailing-space regression `c02c970`).
- **Smallest change:** strip only the line terminator (`\n` plus an optional preceding `\r`) instead of all whitespace; keep blank/whitespace-only input falling back to the default.
- **Payoff:** addresses an identified path-whitespace gap in the setup flow, bringing manual entry in line with copy-root handling.
- **Caveats:** low user frequency (manual typing), so low severity — but it is the only trim of the **stored interactive destination** in `app/cmd`; other path-trim surfaces affect the daemon/launcher and root argument classification rather than the saved destination — see finding **9**. The **native picker** (`pick/pick_darwin.go:18`) is **not proven** reachable: `POSIX path of (choose folder …)` normally ends in `/` for a directory, so trailing basename spaces survive `TrimSpace`; no GUI test was gathered, so it is not reported as a defect.
- **Validation:** `GOTOOLCHAIN=local GOPROXY=off go test ./cmd ./app` plus a synthetic readline test for a trailing-space path and a blank default.
- **Effort:** S. **Confidence:** high (reachability), low-medium (impact).

#### 2. Dead + duplicated production flag parser `parseDaemonStatusOptions`
- **Baseline:** `cmd/daemon_status.go:90-109`.
- **What:** `parseDaemonStatusOptions(args []string)` is only called from `cmd/daemon_status_test.go`; production never invokes it. It re-implements `--json` / `--recent-launches` parsing with a separate stdlib `flag.FlagSet`, duplicating the cobra flags declared in `newDaemonStatusCommand` (`cmd/root.go:209-227`). It is the same U1000-style dead/duplicate pattern the 2026-07-09 review already removed once (`runDaemonStatusCommand`).
- **Smallest change:** remove `parseDaemonStatusOptions` and migrate its meaningful cases onto the real cobra command. For positive parsing, exercise `newDaemonStatusCommand`'s `ParseFlags`, `Flags` getters, and `Args` **without** a live `RunE`. For the negative case, test the actual `RunE` directly: `newDaemonStatusCommand.RunE` returns `--recent-launches must be >= 0` at `cmd/root.go:215-218` **before** `runDaemonStatus` runs, so that failure branch is live-host-free and needs no seam. **No new validator or helper is required.** A positive full-`RunE` path is **not** testable via the constructor alone: `collectDaemonStatusReportWith` is a `runDaemonStatus`/`collectDaemonStatusReport` helper not wired into the command's `RunE`, so a positive `RunE` remains a live-host path until a narrow runner seam is added. Do **not** relocate the duplicate parser to a `_test.go` file — that preserves a meaningless parallel implementation and parallel tests.
- **Payoff:** removes duplicated parsing surface that can drift from the real command (the drift risk is real: `newDaemonStatusCommand` already validates `--recent-launches >= 0` itself).
- **Caveats:** none behavioral; tests already cover the same flags via the parser.
- **Validation:** `GOTOOLCHAIN=local GOPROXY=off go test ./cmd`. A private overlay reproduction confirmed `newDaemonStatusCommand`'s flag getters return the values set through pflag and the negative `RunE` returns the early error without reaching a live host.
- **Effort:** S. **Confidence:** high.

#### 3. Linux volume UUID matched by substring (`sda1` vs `sda10`)
- **Baseline:** `detect/hardware_linux.go:176-193` `getVolumeUUID`, specifically `:189` `strings.Contains(target, device)`.
- **What:** a symlink target like `../../sda10` contains `sda1`, so requesting device `sda1` can return the wrong by-UUID entry (or a wrong display). Hardware-display only, not copy behavior.
- **Smallest change:** compare `filepath.Base(target) == device`.
- **Payoff:** corrects a wrong-UUID/hardware-display edge: the matched `/dev/disk/by-uuid` entry feeds `HardwareInfo.VolumeUUID` (surfaced via `FormatHardwareInfo`), so a spurious `sda1`→`sda10` match can display the wrong volume UUID.
- **Caveats:** Linux-only hardware-metadata display: `HardwareInfo` consumers are enrichment/display only (`app.handleCardEvent` uses `DevicePath` for detection text; `showHardwareInfo` `[i]` formats metadata) — no copy/eject/filter decision reads the UUID. `filepath.Base(target)==device` fixes conventional `sda1`/`sda10`; it is not full symlink/source canonicalization if a `/proc/mounts` source is noncanonical.
- **Validation:** a small pure test over readlink-base matching (no host `/dev/disk/by-uuid` needed).
- **Effort:** S. **Confidence:** high.
- **Attribution:** Review (wS:p3); independently confirmed at source.

#### 4. Linux `/proc/mounts` octal escaping breaks hardware-metadata lookup for spaced mounts
- **Baseline:** `detect/hardware_linux.go:117-133` `findBlockDevice`, comparing `fields[1] == mountPath`.
- **What:** kernel mount lines escape spaces in mount paths as octal `\040`; a mounted path containing a space is never equal to the unescaped `mountPath`, so hardware lookup fails for it.
- **Smallest change:** decode the kernel mount-field octal escapes in **one pass** — `\040` (space), `\011` (tab), `\012` (newline), `\134` (backslash). A literal backslash is itself escape-encoded: literal `\040` bytes appear as `\134040` and must decode back to a literal `\040`, not a space. Factor the line → (device, path) parsing into a pure function so it can be unit-tested synthetically. The current `strings.Fields` compare definitely misses escaped targets.
- **Payoff:** restores Linux hardware-metadata lookup for spaced mount points (Quick/Get info paths).
- **Caveats:** Linux-only, hardware-metadata only. **Source/protocol reasoning** (not executed on Linux on this host). Restores lookup for conventional device-source forms only, not general source canonicalization.
- **Validation:** a table-driven test with a literal `\040`-escaped mount line.
- **Effort:** M. **Confidence:** high.
- **Attribution:** Review (wS:p3); confirmed at source.

#### 5. `NOTES.md` "Quick Teardown" teaches destructive manual removal, contradicting the hardened uninstaller
- **Baseline:** `NOTES.md:225-239` (Quick Teardown block) and `:247-251` (uninstall script section).
- **What:** The quick-teardown block instructs `pkill -f "cardbot --daemon"`, direct `launchctl bootout` + plist deletion, direct binary `rm`, and `rm -rf` of config/logs. This contradicts the project's hardened uninstall path. Note that guard is narrower than a full identity/role check: `stop_recorded_daemon` (`scripts/uninstall.sh:143-160`) accepts a recorded PID only if `ps -o comm=` basename equals `cardbot` (no `--daemon` role proof; a stale-PID/PID-reuse by a foreground `cardbot` would pass), so under a rare stale-PID precondition an uninstall could signal an unrelated foreground ingest. Candidates are discovered via `candidate --version` (even `--dry-run`, accepting `^cardbot `); `--install-dir` removes `<dir>/cardbot` unconditionally as authoritative; `--purge` removes only known default filenames, not an arbitrary `Advanced.LogFile`/XDG path.
- **Smallest change:** replace the broad quick-teardown block (`pkill -f "cardbot --daemon"`, direct `launchctl bootout`, direct plist/binary `rm`, `rm -rf` of config/logs) with the uninstaller path, retaining the script's existing POSIX `sh` invocation (`sh scripts/uninstall.sh`). Do not add bash-only constructs; focus on removing the unsafe direct-removal guidance.
- **Payoff:** the documented path routes teardown through the uninstall script — which is **narrower than broad `pkill -f "cardbot --daemon"` / direct `rm`** (its guard is basename/PID-only, not daemon-role/instance identity). `NOTES.md` should use the default `sh scripts/uninstall.sh`, reserve `--install-dir` as explicit authoritative deletion, and describe `--purge` narrowly; it must **not** promise full identity verification.
- **Caveats:** docs-only. The uninstaller guard is basename/PID-only, not daemon-role/instance identity; a deeper role-proof design is deliberately **deferred** (matching `ps` argv is ambiguous — e.g. `--daemon` vs `--daemon=false`, quoting/rendering). No broad process abstraction and no host `ps`/`kill` in tests. Not executed live.
- **Validation:** `bash -n scripts/uninstall.sh`.
- **Effort:** S. **Confidence:** high.

### Recommended correctness fixes

#### 9. Path-boundary whitespace losses beyond the stored destination (second pass)

Three independently reproduced subcases where a significant-whitespace path is altered or rejected outside the stored-destination readline path (finding **1**). Reproduction: private temp-overlay tests (no added dependencies — the overlay imports only existing Cobra/Viper and the repository's own packages; `GOTOOLCHAIN=local GOPROXY=off`, no repository source/test edits) plus source tracing; no live terminal/daemon/card.

- **9A. Root argument classification rejects a bare-relative edge-space target.** `cmd/root.go:546-556` `looksLikeCommandToken` runs `strings.TrimSpace(arg)` **before** `os.Stat`, so a valid relative target whose name has leading/trailing whitespace (raw path exists; trimmed sibling absent) is rejected as `unknown command %q` in root `RunE` (`cmd/root.go:107-109`). Proven: temp CWD containing a real `Client ` dir → `looksLikeCommandToken("Client ") == true` (rejected); `looksLikeCommandToken("./Client ") == false` (path-like bypass). The consumed target is the raw `args[0]` (`cmd/root.go:361-362`), so classification is inconsistent with the path actually used. Existing tests (`cmd/main_commands_test.go:32-67`) cover embedded-space and `./`-prefixed paths only. Smallest change: stat only the original (untrimmed) argument. Low frequency (bare relative path with edge whitespace), functional rejection. Effort S. Confidence high.

- **9B. Launcher trims a legal edge-space executable path.** `cmd/daemon_cmd.go:45-80` passes `os.Executable()` as `CardBotBinary`; `launch/terminal.go:29` `strings.TrimSpace` destroys a legal edge-space executable path before terminal launch (proven: `/pfx/cardbot ` → launched `/pfx/cardbot`). The mount target (`MountPath`) is preserved (existing `TestOpenWith_GhosttyDefault_PreservesTrailingSpacesInMountPath` and this pass's reproduction). Smallest change: preserve edge spaces for `CardBotBinary` (fake-runner test via the `openWith` seam; no live terminal). Effort S. Confidence high (reproduced).

- **9C. Daemon/Ghostty trims the destination-derived working directory.** `cmd/daemon_helpers.go:48-60` trims `cfg.Destination.Path` to form the daemon working directory; `ghosttyWorkingDirectory` (`launch/terminal.go:287-300`) trims again; `daemon-status` shows the trimmed value (`cmd/daemon_status.go:156`). A legal trailing-space destination yields a wrong Ghostty cwd and status display — proven: `/dest/Client ` → `--working-directory=/dest/Client`. The mount target and the stored/copied destination are **not** changed; `app/handlers.go:85-92` `formatDetectedVolume` trim is display-only. Smallest change: preserve edge spaces for the derived working directory. Effort S. Confidence high (reproduced).

Caveats: 9A, 9B, and 9C are distinct from the stored-destination loss (finding **1**); they affect command selection, launch, and status rather than the copy destination, and are low-frequency. No code changed; recommendations only.

### Optional / suggestions (not defects)

#### 6. Updater temp file is not `fsync`ed before rename
- **Baseline:** `update/update.go:137-148` (`tmp.Chmod` → `tmp.Close` → `os.Rename`; no `tmp.Sync()`).
- **What:** the verified replacement binary is closed and atomically renamed without an explicit sync, so a crash/power loss shortly after rename can leave an undurable replacement.
- **Smallest change:** `tmp.Sync()` before `tmp.Close()`, returning a contextual error on failure.
- **Caveats:** this is **incremental durability hardening, not a power-loss guarantee** — `tmp.Sync()` before rename does not persist the target-directory rename or the parent directory, nor prove device durability; such durability still needs manual/real-device QA (Conductor's framing, agreed). Do not introduce a filesystem abstraction purely to test it.
- **Validation:** existing `update` failure-preservation tests plus `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./update -count=1` (synthetic only; no direct `Sync`-fault test is required).
- **Effort:** S. **Confidence:** high that `tmp.Sync()` is absent (a factual, evidence-backed gap) — distinct from priority/payoff, which is optional and low (incremental durability only, not a power-loss guarantee).
- **Attribution:** Review (wS:p3); confirmed at source.

#### 7. App stdin reader goroutine is not context-aware while blocked
- **Baseline:** `app/app.go:254-267` `readInput` uses `bufio.Reader.ReadString`, so while it is blocked waiting on a stdin line it cannot observe `ctx` cancellation (the `<-ctx.Done()` case only applies after a line is read).
- **Smallest change:** none required for correctness (the process exits after the event loop returns). If joinable shutdown is desired, select over `os.File` reads is complex; not worth it for a CLI.
- **Caveats:** benign; documented as a deferred/latent pattern, not a bug.
- **Effort:** n/a (deferred). **Confidence:** n/a.

#### 8. Unused viper flag bindings for `--dry-run`, `--setup`, `--daemon`
- **Baseline:** `cmd/root.go:159-162` `v.BindPFlag(...)` for `dry-run`, `setup`, `daemon` are unused: `applyConfigOverrides` (`cmd/root.go:502-517`) is the only viper consumer and reads only `destination`, `naming`, `log-file`, `verify-mode`. `--dest` is intentionally read through the required `destination` binding (and the nil-viper path via `flags.GetString("dest")` at `cmd/root.go:520`); `opts.Dest` (`cmd/root.go:126`) is pflag backing storage read via `flag.Value.String()` — **not** dead.
- **Smallest change:** drop the three unused `BindPFlag` lines (the flags are consumed directly via `opts.*`). Retain the `destination` binding and its backing storage; do not add or remove `opts.Dest`.
- **Caveats:** removal must preserve existing CLI behavior. Do **not** "use" these bindings: wiring them into `applyConfigOverrides` could turn previously inert `CARDBOT_DAEMON`/`CARDBOT_SETUP`/`CARDBOT_DRY_RUN` environment variables into new activation routes. The goal is only to delete the dead bindings.
- **Validation:** existing `cmd` Cobra parsing tests plus `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./cmd -count=1` to confirm flag/environment behavior is unchanged after dropping the bindings.
- **Effort:** S. **Confidence:** high (that the bindings are unused).

## Rejected / deferred ideas (not reported as findings)

- **Native picker `TrimSpace` (`pick/pick_darwin.go:18`).** Unproven, not impossible: folder-`choose` output normally ends in `/`, protecting trailing basename spaces; no GUI test was gathered. Suppressed per Conductor/Review correction. (The reachable path is finding **1**, the readline entry.)
- **`config.LoadWithStatus` missing-`$schema` forward-compat nuance.** A future config that adds new top-level fields without bumping `$schema` would be accepted and later rewritten. Speculative edge; the schema-marker mechanism already covers the realistic case.
- **`analyze.extractEXIF` final `reportProgress(len(files))` after cancellation.** Displays an over-count on cancel / counts only EXIF candidates. Cosmetic.
- **Duplicate mode→label maps (`app/commands.go` `modeLabel` vs `app/input.go` `modeDisplayName`).** Minor drift risk, not worth churn now.
- **`app/handlers.go:85-92` `formatDetectedVolume` `TrimSpace`.** Display-only formatting; does not alter the stored card/destination path. Not a data-path finding.
- **Daemon working-directory / launcher binary trimming and bare-relative root-target rejection.** Promoted to finding **9** (path-boundary subcases 9A/9B/9C).
- **Speculative performance claims.** None raised: no profiling-led buffer/worker changes were proposed. (Release QA benchmark notes in `TODO.md` Manual Release QA already cover hardware tuning.)

## Second-pass audit trail (2026-09-08)

This document was revised after an adversarial second pass. Explicitly recorded corrections from the first revision (not silent polish):

- **TODO contradiction (finding #2):** the first-pass TODO shortlist listed a "pure negative-options validator"; the report instead recommends no new helper (reuse the existing early negative return in `newDaemonStatusCommand.RunE` at `cmd/root.go:215-218`). The TODO entry is corrected; the report never required a new validator.
- **Uninstaller overclaim (finding #5):** the first pass described the uninstall path as "identity-verified". It is narrower — basename/PID guard only (`ps -o comm=`), no `--daemon` role proof — so the wording and the `NOTES.md` guidance are corrected, and a stale-PID/PID-reuse caveat is added.
- **Missed path-boundary surfaces:** the first pass said the setup readline was "the one concrete reachable space-loss path". A second trace found the root argument-classification rejection and the daemon/launcher executable-path + destination-derived working-dir trims (finding **9**); the wording was corrected and the executive "no regressions / one gap" framing softened.
- **Viper #8:** an initial note treated `opts.Dest` (`cmd/root.go:126`) as dead storage. Correction: `flags.StringVar(&opts.Dest, ...)` makes it pflag backing storage read via `flag.Value.String()` (not dead). #8 is scoped strictly to the three unused `BindPFlag` calls; `--dest` handling is retained.

## Retained-good patterns (do not regress)

Explicitly preserved invariants this review kept in scope and found intact:

- **Copy containment / no-replace / commit safety** — `cardcopy`: destination rooted via `os.Root`, component-by-component identity checks (`destination.go`), no-replace commit (`commit_darwin` `RENAME_EXCL`, `commit_linux` `RENAME_NOREPLACE`, `commit_other` hard-link), re-validate skips before success, late-conflict abort (`copy.go`, `copyToDir`), own-part-only cleanup.
- **Cancellation** — full-verification `verifyBytes` checks `ctx` between reads; `trackingReader`/`copyStream` propagate cancellation and short writes; `copyToDir` aborts before publish on `ctx.Err()`.
- **Path whitespace** — copy roots and the stored destination preserve significant internal/trailing spaces; the path-trim gaps identified in this pass are the setup readline path (finding **1**), the daemon/launcher and root-classification boundaries (finding **9**), and the Linux hardware-metadata `/proc/mounts` decode (finding **4**).
- **Daemon fail-closed config** — headless/daemon startup refuses to run unless config loads `LoadValid` (`cmd/daemon_helpers.go:validateDaemonConfig`); malformed/unsupported files are never autosaved over.
- **Ingest-history / `.cardbot` safety** — exclusive unique temp publication, sync/close before rename, pre-existing temp preservation (`dotfile`).
- **Platform boundaries** — polling macOS detector as the shipped backend, DiskArbitration behind the explicit `cardbot_native` build tag, opt-in with `CARDBOT_TEST_LIVE_DETECTOR=1` on hosted CI only; Windows runtime/detection is unsupported, but the Windows cross-build is supported via stub hardware (`detect/hardware_other.go`) and exercised by a CI cross-build target.
- **Test isolation** — native detector/live checks gated in CI, synthetic fixtures, injected detector/scan/status/stdin seams, bounded fuzz targets, `GOPROXY=off` reproducible checks.

## Evidence & limitations

- Line numbers are against `521fe66` on `review/go-spring-cleaning`.
- Findings 3 and 4 (Linux `detect`) are **source/protocol reasoning only**; they were not executed (host is Darwin).
- Findings 1, 2, 8, and the path-boundary finding **9** were verified by **private temp-overlay reproductions** using **production functions** (`looksLikeCommandToken`, `promptDestinationReadlineIO`, `NewRootCommand`, `applyConfigOverrides`, `openWith`) added as same-package tests with no repository source/test edits (`GOTOOLCHAIN=local GOPROXY=off`). These defect-asserting overlay tests **fail as expected** while the defect is present and would pass once fixed — they are **not** correctness-passing evidence. Outcomes: (a) readline `TrimSpace` drops a trailing edge space; (b) `looksLikeCommandToken("Client ")` classifies a real bare-relative `Client ` dir as a command token while `./Client ` passes; (c) `openWith` trims a legal edge-space `CardBotBinary` (while `MountPath` is preserved); (d) Ghostty trims a supplied edge-space working directory; (e) cobra `daemon-status` flag getters return the values set through pflag and the negative `RunE` returns the early error before `runDaemonStatus` (no live host); (f) `dry-run`/`setup`/`daemon` viper keys are inert via the only consumer.
- Independent reproduction confirmation: a private temp-overlay harness (same-package tests on the production functions above; no repository source/test edits) yields **four expected defect-preservation failures** (readline trailing-space trim; #9 9A bare-relative `Client ` target rejection; #9 9B `CardBotBinary` edge-space trim; #9 9C Ghostty working-dir edge-space trim) and **two characterization passes** (cobra `daemon-status` flag getters + host-free negative `RunE`; `dry-run`/`setup`/`daemon` viper keys inert via the only consumer). Conductor (wS:p1) independently ran the same overlay inputs and observed exactly these four FAIL + two PASS, then `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./... -count=1` — all PASS. Review (wS:p3) reviewed the reproduction logic and findings.
- The updater durability item (6) and app stdin item (7) are hardening notes, not observed failures.
- Real-device verification (transfer, power loss, device sync/close, permissions, sleep/wake, physical removal, login/launchd) is explicitly out of scope for automated review and remains manual/hardware QA per `TESTING.md`.

## Status

- This report is a point-in-time review at `521fe66`; the findings below remain intact as the audit trail.
- Implementation status (2026-09-08 cleanup pass): findings #1–#4, #8, #9 are implemented and covered by regression tests; #5 (NOTES teardown) is a documentation change; #6 (updater `Sync`) is implemented with its success path covered by existing update tests (no direct `Sync`-fault injection). #7 (stdin joinability), daemon PID role identity, `.part` recovery, and hardware QA remain deferred. See `TODO.md` ("Spring-cleaning review — 2026-09-08").
- Linux evidence (findings #3/#4): on the Darwin host, the shared pure helpers (`unescapeMountField`, `parseMountLine`, `volumeUUIDMatches`) and their tests ran locally; the Linux `detect` package compiled (`GOOS=linux GOTOOLCHAIN=local GOPROXY=off go build ./detect`) and its `hardware_linux_test.go` test binary compiled (`GOOS=linux GOTOOLCHAIN=local GOPROXY=off go test -c ./detect`); the `hardware_linux_test` runtime awaits Linux CI. Hardware QA remains deferred. Generic regression execution does not cover Linux runtime.
- Reproduction and full-suite evidence: the baseline defect-asserting overlays produced the expected four FAIL + two characterization PASS (now superseded by passing regression tests); Conductor (wS:p1) independently re-ran the overlay results and `GOTOOLCHAIN=local GOPROXY=off CARDBOT_TEST_LIVE_DETECTOR=0 go test ./... -count=1` (all PASS). Review (wS:p3) reviewed the reproduction logic and findings. Implementation is complete; final checkpoint/commit is Conductor's.
