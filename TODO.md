# cardBot TODO

This is the tracked project backlog for things to revisit, rethink, or discuss. Keep it short and move detailed design notes into focused docs only when they are still current.

## Before the next serious release

- [ ] Decide whether macOS release artifacts should use native DiskArbitration/cgo or the polling no-cgo detector, then document/build accordingly.
- [ ] Add CI checks for `staticcheck` and vulnerability scanning.
- [ ] Add a benchmark/profile pass for card analysis and copy planning on a representative card dump.

## Copy correctness / data safety

- [x] Add a copy planning phase before execution.
- [x] Detect duplicate destination paths before copying anything.
- [x] Fix timestamp sequence overflow; never silently wrap after `9999`.
- [x] Define and implement true `verify_mode=full` behavior for skipped existing files.
- [x] Add tests for destination collisions, sequence overflow, and same-size/different-content existing files.

## Photo workflow design

- [ ] Decide whether sidecar files (`.XMP`, `.WAV`, `.THM`, `.LRV`, etc.) are first-class assets.
- [ ] If yes, model primary media + sidecars as one ingest asset.
- [ ] If yes, preserve sidecar relationships during timestamp renaming and selective copy modes.
- [ ] Revisit rating detection for external `.XMP` sidecars.

## Architecture cleanup

- [ ] Simplify the nested copy/event loop into worker messages handled by one app loop.
- [ ] Split large multi-phase functions when touched: `copyFiltered` and `analyze.Analyze`.
- [ ] Clarify detector lifecycle: make detectors one-shot or properly restartable.
- [ ] Consider raw single-key input later; do not let it complicate copy correctness work.

## Docs / decisions to discuss

- [ ] Decide whether the LaunchAgent label should remain `com.illwill.cardbot` for stability.
- [ ] Keep ignored `agent/` notes as scratchpad only; promote still-valid ideas into this file or tracked docs.
- [ ] Keep README user-focused and push implementation notes into `NOTES.md`, `TODO.md`, or code comments.
