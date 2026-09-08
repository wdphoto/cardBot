# Automated safety QA

## Routine checks

```bash
go test ./...
go build ./...
make test                 # full race suite
go vet ./...
bash scripts/qa_fuzz.sh    # five targets, 20 seconds each, two workers
```

Fuzz seed cases also run in ordinary `go test` and CI. Fuzz inputs are capped
at 4–16 KiB. History, version, checksum, and byte-verification targets operate
in memory; destination/naming inputs are evaluated lexically, never opened.
Go may retain interesting inputs in its normal fuzz cache. Do not commit caches.

## Isolation audit (2026-09-08)

The audit followed test filesystem operations, command execution, default
constructors, and environment-dependent paths—not just literal `/Volumes`
strings, which also appear in harmless string fixtures.

- Analysis, copy, history, configuration, logger, and filesystem writes use
  temporary fixtures. New copy faults use a few bytes, not disk-filling files.
- Daemon tests inject detectors and private PID paths. Polling detector restart
  tests now inject scans rather than enumerating mounted cards.
- Daemon status tests now use temporary configuration and fake process,
  daemon-instance, and LaunchAgent checks.
- App event-loop tests use EOF instead of the developer's stdin. Inputs are fed
  through the test channels; fake detectors handle simulated eject commands.
- LaunchAgent and terminal tests use fake command runners. Generated terminal
  scripts are temporary files and are not executed by the tests.
- Updater tests use loopback HTTP fixtures or in-memory transports and replace
  only synthetic executables in temporary directories. No test calls GitHub or
  updates the running CLI.
- Home-directory string expansion and filesystem capacity queries still use
  platform APIs; these are not writes to user state. Golden files change only
  when the separate `-update` test flag is explicitly supplied.
- Installer/uninstaller QA is a separate temporary-home dry-run script.
  Permission and sleep/wake capture scripts remain **manual** workflows, not
  unattended tests.

This is an audit of current paths, not an OS sandbox or a guarantee that future
test changes cannot introduce side effects.

## Native detector integration boundary

`go test -tags cardbot_native ./detect` compiles and tests shared helpers, but
skips live DiskArbitration restart unless `CARDBOT_TEST_LIVE_DETECTOR=1` is set.
That opt-in is enabled on the hosted macOS CI runner. **Do not set it locally
when attached cards must remain outside the test's scope.** Ordinary polling
restart tests remain active and use injected scans.

## Added fault regressions

- Copy streaming propagates short writes, disk-full errors, and closed-writer
  errors. These are synthetic writer faults, not a real full-volume test.
- Complete copy transactions reject source read/rewind errors, changed source
  length, cancellation, and destinations created immediately before publication.
  Only the transaction's own partial is cleaned up; another ingest's partial
  and a competing destination are preserved.
- Verification rejects read errors even when the returned bytes match.
- Failed updater downloads/checksums leave the old executable and unrelated
  update temporary files intact.
- History writes preserve pre-existing `.cardbot.tmp` files and symlinks. The
  old fixed-name temporary could truncate the symlink target; writes now use
  exclusive unique temporaries, sync/close before rename, and own-file cleanup.
- Checksum manifests reject empty filenames, including a bare binary marker.

The initial bounded run completed 3,468,989 executions across five targets on
Go 1.27.1 without a mutation-found failure after the empty-filename seed fix.
These runs are not proof of exhaustive correctness. Actual media transfers,
power loss, device-level sync/close failures, permissions, sleep/wake, physical
removal, and login behavior still require separate hardware/manual QA.
Pre-existing `.part` recovery remains deliberately deferred.
