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
| macOS | Uh huh | WIP |
| Linux | I heard it works | Untested |
| Windows | Ugh | Someday, Maybe |

Recommended minimum: macOS 10.13 High Sierra (according to the clankers)

## cardBot's Codebase

- **~7 MB** installed binary (single static executable, no dependencies)
- **~8,200** lines of Go source across 17 packages
- **~7,800** lines of tests — roughly 1:1 coverage ratio

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
| **0.7.0** | Code Refactor | Complete |
| **0.8.0** | Card copy operations | Complete |
| **0.9.0** | Card copy part deux | Next |
| **0.10.0** | Copyright check and injection | Planned |


## Uninstalling

```bash
# Full uninstall (daemon + binary)
sh scripts/uninstall.sh --install-dir ~/bin

# Full uninstall + purge config + logs
sh scripts/uninstall.sh --install-dir ~/bin --purge
```

## License

MIT — see [LICENSE](LICENSE).
