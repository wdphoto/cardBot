# cardBot TODO

This is the concise project backlog. Rationale, evidence, and acceptance guidance from the latest audit live in [`CODE_REVIEW_2026-07-09.md`](CODE_REVIEW_2026-07-09.md).

## P1 — before the next serious release

- [x] Make destination conflicts non-destructive: classify copy/size-match/identical actions, fail planning on unresolved conflicts, and use a no-replace final commit.
- [x] Make timestamp names stable across selective modes and repeated ingests of the same card. Cross-card collisions fail safely instead of overwriting, and setup text now states that invariant.
- [x] Make config saves atomic and never autosave defaults over malformed or unsupported-schema config files.
- [x] Use the polling macOS detector for normal releases; keep the hardened DiskArbitration backend behind the explicit `cardbot_native` build tag.

## P2 — reliability and workflow

- [x] Remove the nested copy event loop. `App.Run` is now the single consumer of detector, removal, input, and shutdown events while copy work runs in a cancellable worker.
- [x] Enforce a real daemon singleton with a lifetime lock and reject stale/reused PIDs by checking process identity.
- [x] Model primary media plus sidecars (`.XMP`, `.WAV`, `.THM`, `.LRV`, `.AAE`) as one ingest asset for filtering and timestamp naming; normal copy planning, verification, and status apply to every member.
- [x] Add external XMP rating support.
- [x] Add reproducible CI checks for `staticcheck`, `govulncheck`, formatting, shell lint, race tests, and release build targets.
- [x] Add restart/concurrent-lifecycle detector tests and compile/test both release polling and opt-in native macOS backends.
- [x] Harden updater SemVer/checksum parsing, response limits, release provenance, and supported-platform replacement behavior.
- [x] Make uninstall target only recorded or identity-verified installations by default, with temporary-home script QA.

## P3 — maintainability and decisions

- [x] Split the analysis collection/EXIF phases and simplify copy orchestration. Further `runInteractive`/planner extraction is deferred until those paths need behavioral changes; splitting them now would be churn without a testability gain.
- [x] Surface persistent logger and config metadata write failures instead of silently discarding them.
- [x] Align `NOTES.md` daemon claims and setup naming claims with implemented behavior.
- [x] Keep the LaunchAgent label `com.illwill.cardbot` for upgrade stability; changing it would strand existing installed agents.
- [x] Keep Windows runtime unsupported but compile it with stub hardware support so cross-build regressions are visible.
- [x] Add representative analysis, planning, and full-verification benchmarks. Cancellation remains covered by deterministic tests; hardware profiling is a release QA activity.
- [x] Keep line-based input. Raw single-key mode would add terminal-state and accessibility risk without improving ingest safety.

## Manual release QA

- [ ] On each supported macOS release line, exercise real card insert/removal, eject, sleep/wake, permission denial, and cancellation with the polling backend.
- [ ] Run the benchmark suite on representative RAW/video cards and local, external, and network destinations before changing worker or buffer defaults.

## Completed safeguards to preserve

- [x] Plan copy operations before execution and reject duplicate destinations within one plan.
- [x] Reject destination traversal and destinations located on the source card.
- [x] Use temporary partial files, sync them, and clean them on copy failure/cancellation.
- [x] Reject timestamp sequence overflow instead of silently wrapping after `9999`.
- [x] Implement byte-level `verify_mode=full` for copied and existing files.
- [x] Skip source symlinks and exercise the core copy path under the race detector.
