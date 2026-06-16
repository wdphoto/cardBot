# cardBot TODO

This is the tracked project backlog for things to revisit, rethink, or discuss. It is intentionally short; detailed rationale lives in [`codex-review-rnd-2.md`](codex-review-rnd-2.md).

## Before the next serious release

- [ ] Merge or cherry-pick the fixed module path from `scratch` to `main` so the public branch uses `github.com/wdphoto/cardBot`.
- [ ] Fix release builds to stamp `version`, `commit`, and `date` with ldflags.
- [ ] Decide whether macOS release artifacts should use native DiskArbitration/cgo or the polling no-cgo detector, then document/build accordingly.
- [ ] Resolve the dead `speedtest` surface: wire it intentionally, move it to a diagnostic path, or delete it.
- [ ] Add CI checks for `go vet`, `staticcheck`, shell syntax, and vulnerability scanning.

## Copy correctness / data safety

- [ ] Add a copy planning phase before execution.
- [ ] Detect duplicate destination paths before copying anything.
- [ ] Fix timestamp sequence overflow; never silently wrap after `9999`.
- [ ] Define and implement true `verify_mode=full` behavior for skipped existing files.
- [ ] Add tests for destination collisions, sequence overflow, and same-size/different-content existing files.

## Photo workflow design

- [ ] Decide whether sidecar files (`.XMP`, `.WAV`, `.THM`, `.LRV`, etc.) are first-class assets.
- [ ] If yes, model primary media + sidecars as one ingest asset.
- [ ] If yes, preserve sidecar relationships during timestamp renaming and selective copy modes.
- [ ] Revisit rating detection for external `.XMP` sidecars.

## Architecture cleanup

- [ ] Simplify the nested copy/event loop into worker messages handled by one app loop.
- [ ] Split large multi-phase functions when touched: `runInteractive`, `copyFiltered`, `cardcopy.Run`, and `analyze.Analyze`.
- [ ] Clarify detector lifecycle: make detectors one-shot or properly restartable.
- [ ] Consider raw single-key input later; do not let it complicate copy correctness work.

## Docs / decisions to discuss

- [ ] Decide whether `scratch` is temporary or the real integration branch.
- [ ] Decide whether the LaunchAgent label should remain `com.illwill.cardbot` for stability.
- [ ] Keep ignored `agent/` notes as scratchpad only; promote still-valid ideas into this file or tracked docs.
- [ ] Keep README user-focused and push implementation notes into `NOTES.md`, `TODO.md`, or code comments.
