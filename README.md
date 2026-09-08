# cardBot

A CLI tool for camera memory cards.

![cardBot screenshot](screenshot.png)

## DISCLAIMER - Built with AI

cardBot was built with the assistance of AI coding tools. This project is an experiment in using large language models to prototype and build an application in Go, a programming language I have minimal experience with. That will likely show throughout the codebase. Use this software at your own risk. There is no warranty — but does anything really have a warranty anymore? It should be the fastest ingestion tool in the West, though. Enjoy.

## What cardBot does

- Detects camera memory cards on macOS
- Generates an overview of card content (file count, type, dates, equipment data, etc.)
- Copy modes: all, selects (starred), photos only, videos only, etc
- Rename files during copy operations
- Tracks card copy status

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| macOS | Primary | Release and normal local builds use polling; native DiskArbitration is opt-in for development |
| Linux | Best effort | Untested |
| Windows | Ugh | Someday, Maybe |

Recommended minimum: macOS 10.13 High Sierra (according to the clankers)

## cardBot's Codebase

- **~7 MB** installed binary (single executable, no runtime dependencies)
- **~8,500** lines of Go source across 16 packages
- **~8,100** lines of tests

## Installation

The easy install:

```bash
curl -fsSL https://raw.githubusercontent.com/wdphoto/cardBot/main/scripts/install.sh | sh
```

## Usage

Start cardBot:

```bash
cardbot
```

For read-only analysis and copy previews (no media copies or `.cardbot` writes):

```bash
cardbot --dry-run "/Volumes/Your Card"
```

Copy commands become previews in this mode; `e` still ejects the card if requested. Quit with `Ctrl+C` to leave the card mounted.

Quit cardBot:

`Ctrl+C`

cardBot will automatically run the setup if no config file is present.

Update to the latest version:

```bash
cardbot self-update
```


To run the setup again:

```bash
cardbot --setup
```


## Commands

| Key | Action |
|-----|--------|
| `a` | Copy all files to destination |
| `s` | Copy selects (starred/picked) |
| `p` | Copy photos only |
| `v` | Copy videos only |
| `t` | Copy today's photos |
| `y` | Copy yesterday's photos |
| `e` | Eject card |
| `x` | Exit current card |
| `i` | Show card hardware info |
| `\` | Cancel copy in progress |
| `?` | Help |


## Roadmap

| Version | Focus | Status |
|---------|-------|--------|
| **0.0.7** | Code Refactor | Complete |
| **0.0.8** | Card copy operations | Complete |
| **0.0.10** | Release/copy correctness pass | Complete |
| **0.0.11** | Docs, release workflow, and performance profiling | Planned |
| **0.0.12** | Copyright check and injection | Planned |

See [`TODO.md`](TODO.md) for the current technical backlog and discussion items.

Timestamp naming is deterministic across selective copies of the same card. The sequence suffix uses a minimum of four digits (`0001` … `9999`, `10000` …) and grows without rollover, so cards with 10,000+ assets work in all and selective modes. Existing four-digit mappings are preserved; padding does not change based on the card's total file count. cardBot never replaces an existing destination during ingest. Default `verify_mode=size` skips same-size files without proving content identity; use `advanced.verify_mode=full` for byte-level comparison. Full verification checks the temporary copy before publishing its final filename.

During a copy, cancel with `\` and Enter and wait for completion before ejecting or exiting the card. A pre-existing `.part` file blocks that file's copy rather than being deleted or overwritten automatically.

cardBot never deletes source media or formats cards. A successful ingest may update the card's `.cardbot` history; if that write fails (for example, on a write-protected card), cardBot warns without treating the completed ingest as failed. It does not create write-access probes.

The displayed status describes the **last recorded ingest**, not whether every current file is backed up. “Photos from ingest day” and “Photos from day before ingest” refer to that recorded operation, not today's date. Formatting in the camera erases `.cardbot` along with the media; “No recorded ingest” means no readable record was found, not that the card has never been copied. Verify your destination independently before formatting.


## Uninstalling

```bash
# Full uninstall (daemon + binary)
sh scripts/uninstall.sh --install-dir ~/bin

# Full uninstall + purge config + logs
sh scripts/uninstall.sh --install-dir ~/bin --purge
```

## License

MIT — see [LICENSE](LICENSE).
