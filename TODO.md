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

## Resumed safety pass

- [x] Make the event loop own copy completion, register cancellation before worker launch, block exit/eject while copying, and isolate completion from replacement cards with per-copy identity.
- [x] Enforce destination containment with rooted operations, a verified destination-base identity, and directory-relative no-replace commits. Revalidate planned skips before reporting success.
- [x] Observe cancellation between full-verification reads and verify temporary copies before publishing final filenames. In-flight kernel reads can still block on device/network I/O.
- [x] Remove card write-access probes entirely; report actual `.cardbot` write failures after completed ingests without treating the media copy as failed.
- [ ] Decide a safe, explicit recovery workflow for pre-existing `.part` files. Preserve them for now: age or size alone cannot establish that another ingest no longer owns them.
- [ ] Resolve the version-number rollback (`v0.9.0` predates `v0.0.10`) before publishing another release; older installations' SemVer comparisons can otherwise suppress updates.

## Read-only usability pass

- [x] Display the latest valid ingest record without combining older modes under a newer timestamp; clarify historical today/yesterday selections and missing history.
- [x] Group displayed file counts, use completed-scan wording, and avoid a duplicate `v` prefix in startup versions.
- [x] Remove mutable global hardware-lookup test hooks; a connected card exposed a race with background detector enrichment during the full test run.
- [x] Exercise read-only scan and dry-run selections on the connected Nikon card (11,653 photos). Original-name previews passed: all 11,660 files, photos 11,654 including companions, selects 1, yesterday 301; videos/today correctly reported no matches. Card inventory/sizes/mtimes, `.cardbot` bytes, and destination-root metadata were unchanged; no media was copied or card ejected. Temporary naming override was not saved.
- [x] Preserve significant whitespace in copy roots. The Nikon mount's trailing spaces exposed a planner bug that could select a different, trimmed path; cover both dry-run and tiny synthetic copies with regression tests.
- [ ] Lift the 9,999-asset timestamp naming limit without sequence rollover or changing existing mappings; retain the guard until the naming policy and compatibility tests are updated. Real-card timestamp dry runs currently fail closed at 11,659 assets, including selective modes.

## Manual release QA

- [ ] On each supported macOS release line, exercise real card insert/removal, eject, sleep/wake, permission denial, and cancellation with the polling backend.
- [ ] Run the benchmark suite on representative RAW/video cards and local, external, and network destinations before changing worker or buffer defaults. Full-transfer hardware QA is deferred until sufficient destination space and suitable source protection are available.

## Completed safeguards to preserve

- [x] Plan copy operations before execution and reject duplicate destinations within one plan.
- [x] Reject destination traversal and destinations located on the source card.
- [x] Use exclusive temporary partial files, sync them, and clean up only the current copy's partial on failure/cancellation; never remove a pre-existing partial.
- [x] Reject timestamp sequence overflow instead of silently wrapping after `9999`.
- [x] Implement byte-level `verify_mode=full` for copied and existing files.
- [x] Skip source symlinks and exercise the core copy path under the race detector.
