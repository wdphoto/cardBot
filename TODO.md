# cardBot TODO

This is the concise project backlog. Rationale, evidence, and acceptance guidance from the latest audit live in [`CODE_REVIEW_2026-07-09.md`](CODE_REVIEW_2026-07-09.md).

## P1 — before the next serious release

- [ ] Make destination conflicts non-destructive: classify absent/identical/conflicting targets, fail planning on unresolved conflicts, and prevent final-rename races from replacing existing work.
- [ ] Define stable timestamp naming across cards, selective modes, repeated ingests, and existing destination content; correct setup text to match the invariant.
- [ ] Make config saves atomic and never autosave defaults over malformed or unsupported-schema config files.
- [ ] Decide whether macOS releases use native DiskArbitration/cgo or the polling detector, then harden and integration-test the chosen path.

## P2 — reliability and workflow

- [ ] Simplify the nested copy/event loop into worker messages handled by one app loop.
- [ ] Enforce a real daemon singleton with stale/reused PID handling.
- [ ] Model primary media plus sidecars (`.XMP`, `.WAV`, `.THM`, `.LRV`, etc.) as one ingest asset for filtering, naming, verification, and status.
- [ ] Add external XMP rating support.
- [ ] Add reproducible CI checks for `staticcheck`, `govulncheck`, formatting, shell lint, and all release build targets.
- [ ] Add macOS detector/eject/sleep-wake integration coverage and CLI orchestration tests.
- [ ] Harden updater version/checksum parsing, response limits, release provenance, and cross-platform replacement tests.
- [ ] Make uninstall target only verified/recorded installations by default.

## P3 — maintainability and decisions

- [ ] Split large multi-phase functions when touched: `runInteractive`, `copyFiltered`, `analyze.Analyze`, and copy planning/execution.
- [ ] Surface persistent logger and config metadata write failures instead of silently discarding them.
- [ ] Align `NOTES.md` daemon claims and setup naming claims with implemented behavior.
- [ ] Decide whether the LaunchAgent label remains `com.illwill.cardbot` for stability.
- [ ] Decide whether unsupported Windows builds should compile with stub hardware support.
- [ ] Add representative benchmarks/profiles for analysis, planning, copying, cancellation, and full verification.
- [ ] Consider raw single-key input only after event-loop cleanup.

## Completed safeguards to preserve

- [x] Plan copy operations before execution and reject duplicate destinations within one plan.
- [x] Reject destination traversal and destinations located on the source card.
- [x] Use temporary partial files, sync them, and clean them on copy failure/cancellation.
- [x] Reject timestamp sequence overflow instead of silently wrapping after `9999`.
- [x] Implement byte-level `verify_mode=full` for copied and existing files.
- [x] Skip source symlinks and exercise the core copy path under the race detector.
