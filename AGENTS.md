# AGENTS.md

Scope: the entire `cardBot` repository.

## Project identity

- Canonical local home: `/Users/illwill/Code/cardBot`.
- Old sandbox copy: `/Users/illwill/Code/sandbox/pi/silk`. Do not touch or delete it unless the user explicitly confirms.
- Canonical GitHub repo: `https://github.com/wdphoto/cardBot.git`.
- Canonical Go module path: `github.com/wdphoto/cardBot`.
  - The camel-case `cardBot` is intentional and must match `go.mod`, imports, docs, install scripts, and update code.
  - Do not revert to `github.com/illwill/cardbot` or lowercase the module path.
- The remote should be named `origin` for this repo. Use `upstream` only for a true fork workflow.

## Branch and git hygiene

- Current working branch for active work is `scratch` unless the user asks otherwise.
- `main` is the default/release branch on GitHub.
- `dev` exists as a legacy/integration branch.
- Use short-lived fix/feature branches only when useful, then merge/fast-forward their commits back into the intended branch and delete the temporary branch.
- Before edits, check `git status --short --branch`. After edits, leave the tree clean unless the user asked for uncommitted work.
- Do not force-push, rewrite shared history, delete branches, delete tags, cut releases, or remove the old sandbox folder without explicit confirmation.

## Product overview

`cardBot` is a Go CLI app for camera memory-card ingest. It detects cards, analyzes contents, copies subsets of media, renames files, tracks copy state, and supports daemon/login workflows. macOS is the primary platform; Linux is best-effort; Windows is future/placeholder.

The built command is `cardbot`.

## Repo map

- `main.go`, `boot.go`, `*_cmd.go`: CLI entry points and top-level commands.
- `app/`: interactive app state, display, command handling, setup, update flow.
- `analyze/`: card content analysis and metadata summaries.
- `cardcopy/`: copy operations, filters, naming, progress/throughput, disk-space checks.
- `config/`: persisted configuration.
- `daemon/`, `launch/`, `instance/`: daemon mode, LaunchAgent integration, single-instance guard.
- `detect/`: card detection and hardware details by platform.
- `pick/`: folder picking helpers.
- `term/`: terminal formatting and ANSI helpers.
- `update/`: self-update/release lookup behavior. Default repo should remain `wdphoto/cardBot`.
- `cblog/`, `dotfile/`, `fsutil/`, `speedtest/`: support packages.
- `scripts/`: install, uninstall, and QA scripts.
- `.github/workflows/`: CI/release automation.

## Build and test commands

- `make test` — runs `go test ./... -count=1 -race`.
- `go test ./...` — faster local package test pass.
- `go build ./...` — compile all packages.
- `make build` — builds the `cardbot` binary with version ldflags.
- `make clean` — removes generated `cardbot` and coverage output.
- `bash -n scripts/*.sh` — quick syntax check after shell-script edits.

For code changes, run at least:

```bash
go test ./...
go build ./...
```

Prefer `make test` before committing meaningful Go changes.

## Coding rules

- Prefer idiomatic, boring Go over clever abstractions.
- Run `gofmt` on Go files you edit.
- Keep imports using the exact module prefix `github.com/wdphoto/cardBot`.
- Keep platform-specific behavior behind the existing OS-specific files/build constraints.
- Preserve CLI/user-facing text intentionally; update golden tests when text output changes.
- Avoid committing generated binaries, local configs, logs, caches, profiles, or secrets.
- Do not vendor dependencies unless the user explicitly asks.
- Keep install/update docs and scripts aligned with the canonical GitHub repo.
- Keep `TODO.md` updated when adding, closing, or intentionally deferring review findings.

## Security and local files

- Never commit tokens, credentials, private keys, `.env*`, local config, logs, or generated binaries.
- The root `cardbot` binary is generated and ignored.
- `.pi/`, `agent/`, `.DS_Store`, and local logo/binary drafts are intentionally ignored.
- If a task touches install/update/release behavior, check for accidental disclosure of local paths, usernames, tokens, or private repo names.

## Agent behavior

- Be conservative with destructive actions and ask first.
- Prefer small, focused diffs.
- Push back on requests that risk data loss, release confusion, security exposure, needless complexity, or non-idiomatic Go.
- Show file paths clearly.
- If tests cannot be run, say exactly why and what command should be run next.
