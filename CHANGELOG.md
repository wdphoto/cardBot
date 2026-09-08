# cardBot Changelog

## Unreleased

- Standardize development builds on `0.10.0-dev` in `VERSION`, with `v0.10.0` reserved for the next release; prevent future release-version rollbacks without rewriting historical tags.
- Support timestamp naming beyond 9,999 assets, including selective copies: sequence suffixes grow past four digits without rollover or changing existing mappings.
- Remove speculative card write-access probes; warn only if saving `.cardbot` after an ingest actually fails.
- Describe status as the latest recorded ingest, with historical selection labels rather than implying current backup coverage.
- Group displayed file counts, simplify the completed-scan line, and fix duplicate version prefixes at startup.
- Preserve significant spaces in source/destination paths, fixing planning for Nikon volumes with trailing spaces.
- Isolate detector hardware-lookup test dependencies to avoid races with background card enrichment.
- Bind copy completion to its session, queue insertions while cancellation finishes, and require cancellation before exit/eject during copy.
- Contain destination access with rooted directory handles and reject destination-base replacements between planning and execution.
- Cancel full verification between reads, verify partial files before publication, and recheck planned skips before reporting completion.
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
