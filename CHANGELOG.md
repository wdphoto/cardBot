# cardBot Changelog

## Unreleased

- Refuse non-identical destination conflicts and use no-replace final commits.
- Keep timestamp names stable across selective modes and ingest sidecars with their primary media.
- Preserve malformed/future configuration files and save valid configuration atomically.
- Simplify copy cancellation/event ownership and enforce a real daemon singleton.
- Harden macOS detector lifecycle, updater validation, install/uninstall safety, logging, CI, and release provenance.
- Add external XMP ratings, unsupported-platform build stubs, and representative benchmarks.

## 0.0.10

- Fix cardBot naming conventions
- Fix Go naming conventions
- Clean up project structure

## 0.8.3

- Minor fixes

## 0.8.2

- Changelog shown on first run after update
- Fix cancel copy [\] outside active copy
- Fix timestamp dimming in output

## 0.8.1

- Add changelog output for after cardBot updates

## 0.8.0

- Gear display — shows camera body + lenses from EXIF
- [t] copy today's photos, [y] copy yesterday's photos
- Cleaner timestamp output — no repeated timestamps
