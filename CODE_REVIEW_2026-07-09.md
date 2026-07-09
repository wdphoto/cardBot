# cardBot Codebase Review — 2026-07-09

This review covers the tracked Go code, tests, shell scripts, GitHub Actions workflows, and current project documentation on `scratch` at `8ea6e30`. It is a point-in-time engineering review, not a claim that every hardware and filesystem combination has been exercised.

> Implementation status: the automated and code-level findings in this review were addressed on `codex/codebase-review-2026-07-09`. The original findings remain below as the rationale and audit trail. Current status and the two hardware/performance release checks that cannot be completed in a sandbox are tracked in [`TODO.md`](TODO.md).

## Executive assessment

The codebase is in substantially better shape than its experimental history suggests. Package boundaries are understandable, the core copy path has a real planning phase, partial files are cleaned up, cancellation is tested, source symlinks are skipped, destination traversal is rejected, full byte verification exists, and the race-enabled suite passes. The strongest packages by test coverage are the data-bearing ones: `cardcopy`, `config`, `daemon`, and `dotfile` are all around 87–89% statement coverage.

The next release should still be treated as a data-safety release. The most important unresolved behavior is destination conflict handling: when a planned destination exists but is not considered skippable, the final rename replaces it. That is acceptable for a deliberate repair operation, but unsafe as the default ingest policy because the existing file may be unrelated work. Timestamp naming also restarts its sequence on every copy, so it does not yet guarantee uniqueness or stable names across cards, selective modes, or repeated ingests.

No release-blocking test, build, vet, or race failure was found. One static-analysis failure, several durability gaps, and important platform/integration test gaps remain.

## Evidence collected

- 110 tracked files.
- 8,429 lines of non-test Go and 8,085 lines of Go tests.
- `go test ./...`: pass.
- `go build ./...`: pass.
- `go vet ./...`: pass.
- `make test` (`go test ./... -count=1 -race`): pass.
- `staticcheck ./...`: one failure, unused `runDaemonStatusCommand` in `cmd/daemon_status.go` (`U1000`).
- `gofmt` check: clean.
- `bash -n scripts/*.sh`: pass.
- Release cross-builds with `CGO_ENABLED=0`: pass for Darwin and Linux on amd64 and arm64.
- Overall statement coverage: 62.2%.
  - `cardcopy`: 87.4%
  - `config`: 87.1%
  - `daemon`: 89.2%
  - `dotfile`: 89.2%
  - `app`: 76.8%
  - `update`: 72.8%
  - `cmd`: 36.0%
  - `detect`: 13.3%
- A Windows cross-build currently fails because `detect.HardwareInfo` has no unsupported-platform definition. Windows is documented as future work, so this is a scope gap rather than a regression in a supported target.
- `govulncheck`, `shellcheck`, `gosec`, and `golangci-lint` were not installed, so they were not run. CI should own reproducible versions of the checks the project chooses.

## P1 — tackle before the next serious release

### 1. Make destination conflicts non-destructive

Relevant code: `cardcopy/copy.go` (`PlanCopy`, `shouldSkipExisting`, and `copyFileCtx`).

The planner currently records only a `Skip` boolean. A same-size destination is skipped in size mode; a byte-identical destination is skipped in full mode. Every other existing destination is treated as copy work. `copyFileCtx` then calls `os.Rename(partPath, dst)`, which replaces `dst` on Unix-like systems.

That means these cases are not distinguished:

- destination does not exist;
- destination is known identical;
- destination merely has the same size;
- destination differs and is safe to repair;
- destination differs because it belongs to another ingest;
- destination cannot be inspected.

The safe default for an ingest tool is never to destroy an existing, non-identical destination implicitly. A collision should fail the plan before any write, receive a deterministic alternate name, or be quarantined for an explicit repair workflow. The final commit must also re-check or reserve the destination so another process cannot create it between planning and rename.

Recommended acceptance criteria:

- Introduce an explicit planned action such as `copy`, `skip-identical`, `conflict`, and optionally `repair`.
- Fail the whole plan before writing when any unresolved conflict exists.
- Make overwrite/repair opt-in and clearly surfaced to the user.
- Prevent time-of-check/time-of-use replacement at the final rename step.
- Add tests for different-size conflicts, same-size/different-content conflicts, destination creation after planning, stale `.part` files, and concurrent ingests.
- Clarify that `verify_mode=size` establishes size equality, not content identity.

### 2. Make timestamp naming stable across ingests

Relevant code: `cardcopy/naming.go`, `cardcopy/copy.go`, and the setup text in `app/setup.go`.

Sequence numbers start at one for every `PlanCopy` call and are based on the files selected for that operation. Recopying a card, copying photos and videos separately, or ingesting another card with overlapping capture times can therefore map different sources onto an existing destination. The current setup promise that timestamp naming provides “automatic order across all cards” is stronger than the implementation.

Choose and document a naming invariant. Practical options include a stable source identity, a collision suffix derived from source metadata/content, or an allocator that considers the destination catalog before assigning names. Whatever is chosen should be deterministic across selective modes and repeated runs, and it must compose with the non-destructive conflict policy above.

Also decide whether the 9,999-file limit is per operation, per day, or per destination namespace. The current limit is per operation even though the names live in multiple date/folder namespaces.

### 3. Make configuration persistence atomic and forward-compatible

Relevant code: `config/config.go` and `cmd/root.go`.

`config.Save` writes directly to the live config file. A crash, full disk, or interrupted write can leave a truncated file. More importantly, `config.Load` returns defaults for malformed or unknown schemas, and startup later saves `Meta.LastSeenVersion`. That save can replace an incompatible future-schema config with defaults even when the user did not run setup.

Recommended acceptance criteria:

- Return a load status that distinguishes missing, valid, malformed, and unsupported-schema files.
- Never autosave over malformed or unsupported configuration.
- Save through a same-directory temporary file, flush/close it, preserve restrictive permissions, and atomically rename it.
- Keep a one-generation backup when replacing a user-edited config, or provide a clear recovery path.
- Report failures from the currently ignored startup metadata saves.
- Add interruption, permission, unsupported-schema, and recovery tests.

### 4. Decide the macOS detector shipped to users, then harden that path

Release artifacts currently use the polling implementation because the workflow sets `CGO_ENABLED=0`. Local cgo builds use DiskArbitration. Maintaining two primary-looking implementations doubles the lifecycle and integration surface while CI exercises neither against real macOS behavior.

If the native implementation is retained, address these concrete risks before shipping it:

- `getVolumeName` allocates `CFStringGetLength + 1` bytes, which is not sufficient for arbitrary UTF-8, and it does not handle `CFStringGetCString` failure before returning the buffer.
- `DADiskEject` is started without a completion callback, so the app can report success before the eject completes or fails.
- `Stop` stops and releases run-loop/session state without joining the locked-OS-thread goroutine.
- Detector state relies on a package-global singleton, making concurrent instances and teardown harder to reason about.

If polling remains the release backend, make that the explicit supported design, add restart-safe lifecycle handling, and add macOS integration tests for insert, removal, eject, sleep/wake, and permission failures.

## P2 — high-value follow-up

### 5. Replace the nested copy event loop with one owner of runtime events

`app.copyFiltered` temporarily takes over detector, removal, input, and shutdown events from `App.Run`. The comments correctly warn that every new event source must be duplicated in both loops. This design is testable today, but it is easy for future features to drop or mishandle events.

Run copy work in a worker and send progress/completion messages back to the single app loop. The app loop should remain the only owner of card queue, phase, removal, input, and shutdown transitions. This will also make duplicate shutdown output, copy cancellation, and queued-card transitions easier to test.

### 6. Enforce a real daemon singleton

The PID file is informational, not a lock. Two manually started daemons can overwrite the PID file, both monitor events, and one can remove the other’s PID file on exit. `daemon-status` also treats any live process with the stored PID as the daemon, so PID reuse can produce a false positive.

Use an advisory lock or another platform-appropriate singleton mechanism held for the daemon lifetime. Record enough process identity for status reporting to reject stale/reused PIDs, and test concurrent start plus crash recovery.

### 7. Define sidecars as ingest assets

Analysis intentionally counts only known photo/video extensions, while copy-all copies every non-hidden regular file. Selective modes can leave `.XMP`, `.WAV`, `.THM`, `.LRV`, and similar companions behind, and timestamp naming does not preserve a primary/sidecar basename relationship.

Model a primary media file and its companions as one ingest asset. Apply filtering, naming, conflict handling, verification, and status accounting to the asset as a unit. External XMP should also participate in rating detection.

### 8. Turn CI into the reproducible review gate

Add pinned, versioned jobs for:

- `staticcheck` after removing the dead daemon-status parser path;
- `govulncheck`;
- formatting and shell lint, not only shell parsing;
- the four release cross-builds;
- a native macOS build/test job if the cgo detector remains supported;
- targeted CLI tests for currently uncovered orchestration (`runInteractive`, daemon install/uninstall/debug, and self-update UX).

Do not chase a single coverage percentage. Prioritize behavior that currently depends on hardware, OS commands, process state, or top-level orchestration. The low `detect` and `cmd` figures identify those seams clearly.

### 9. Harden release and updater semantics

The updater’s version regex is not anchored, accepts partial versions, and ignores prerelease semantics. Checksum parsing accepts any token without validating 64 hexadecimal characters. GitHub API/checksum responses are read without explicit size limits. These are mostly correctness and hardening issues because the calculated binary hash must still match, but they should be deliberate rather than incidental.

Recommended work:

- Support a documented version grammar or a small semver implementation, with prerelease tests.
- Validate checksum length/encoding and reject duplicate/conflicting entries.
- Bound release metadata and checksum response sizes.
- Validate that a release tag comes from the intended release branch/process before publishing.
- Pin third-party GitHub Actions by commit SHA and add artifact provenance/signing if release trust warrants it.
- Test update interruption and replacement behavior on each supported OS.

### 10. Make installer and uninstaller safety symmetric with the application

The installer verifies a checksum and quotes paths carefully, which is a good baseline. The uninstaller is broader than necessary: it kills any process whose command line matches `cardbot --daemon` and removes `cardbot` from every known location, including whichever binary happens to be first in `PATH`, without verifying identity.

Prefer uninstalling the recorded/current installation, verify candidate binaries where feasible, and require explicit confirmation or a flag before removing additional discovered copies. Add script tests using a temporary fake home/PATH so dry-run and purge behavior are continuously checked.

## P3 — maintainability and product polish

### 11. Split large orchestration functions at natural phase boundaries

The clearest candidates are `cmd.runInteractive`, `app.copyFiltered`, `analyze.Analyze`, and `cardcopy.PlanCopy`/`Execute`. Keep the existing straightforward data flow, but isolate configuration/bootstrap, planning, execution, event handling, and reporting so failure cases can be tested without capturing global stdout or invoking OS behavior.

### 12. Align documentation with actual daemon and naming behavior

`NOTES.md` says daemon terminal selection has been simplified to Terminal.app and that `terminal_app` is retained only for compatibility, while the implementation supports Default, Ghostty, arbitrary apps, custom launch arguments, and working-directory behavior. Timestamp setup text also overstates cross-card ordering. Update those claims when the behavioral decisions above are made.

Keep ignored `agent/` material as scratch history only; promote current decisions into tracked documentation before relying on them.

### 13. Improve observability without hiding write failures

The logger silently drops write and rotation errors, and several config metadata saves ignore errors. For a daemonized ingest tool, persistent log failure is worth surfacing once to stderr/status rather than disappearing. Consider returning or recording the last logging error while avoiding recursive logging.

### 14. Keep unsupported-platform scope explicit

Windows currently does not compile because hardware types exist only on Darwin/Linux. Either add a minimal unsupported-platform hardware implementation so `GOOS=windows go build ./...` remains green, or document that even compilation is intentionally unsupported until Windows work starts.

### 15. Profile before optimizing

The analysis worker pool and buffered copy path are sensible. Use representative card trees, RAW files, videos, slow cards, and network/external destinations to measure scan time, memory, throughput, cancellation latency, and full-verification cost. Add stable benchmarks for planning and analysis before changing concurrency or buffer defaults.

## Suggested implementation order

1. Non-destructive destination conflict policy and tests.
2. Stable timestamp naming invariant built on that policy.
3. Atomic/forward-compatible config persistence.
4. macOS detector decision and integration hardening.
5. Single-owner app event loop and daemon singleton.
6. Sidecar asset model.
7. CI, release, updater, installer, and documentation hardening.
8. Profile-guided performance work and unsupported-platform cleanup.

The first three items are cohesive and should land before adding more ingest modes: they define whether existing work, future configuration, and repeated card ingests are safe by default.
