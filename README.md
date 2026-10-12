# EarthBuild Profiler

A standalone Go CLI and Bubble Tea TUI for recorded EarthBuild runs. It imports
EarthBuild's `--profile` JSON export, stores run history in a local bbolt database,
and shows target dependencies, per-step timings, and logs for individual calls.
It does not import the EarthBuild Go module or require BuildKit to browse history.

## Build

Go 1.27 or newer is required. With an older Go installation, `GOTOOLCHAIN=auto`
allows Go to download the required toolchain.

```sh
make build
```

The binary is `bin/earth-profile`. Put it on your `PATH`, or invoke it by its
absolute path. No installation or shell configuration changes are performed.

## Capture and browse

Run this from the directory containing the Earthfile you want to build:

```sh
earth-profile run +all
earth-profile
```

The wrapper invokes `earth --profile <temporary-file> +all`, keeps normal build
output visible, imports the final profile, and preserves the build's exit code.
Failed and handled canceled builds are saved when `earth` exports a profile. The
wrapper does not automatically open the TUI during a build. Abrupt termination
cannot produce a final profile.

Use a rebuilt EarthBuild CLI containing the `--profile` feature. To select a
particular executable:

```sh
earth-profile --earth /path/to/earth run --no-cache +all
```

Build arguments after `run` are passed directly to `earth`. Do not supply another
`--profile` option, because the wrapper manages the temporary export path.

You can also capture and import separately:

```sh
earth --profile run.json +all
earth-profile import run.json
earth-profile browse
```

Import profile JSON from stdin without consuming the TUI's keyboard input:

```sh
cat run.json | earth-profile import -
earth-profile browse
```

The importer accepts the structured profile export, not the parallel console
text. Profiles become available after a build finishes. It does not estimate
execution durations from the time console lines arrive.

## Viewer

The viewer has three panes: run history, targets/steps, and the selected item's
logs. It needs a terminal at least 90 columns wide and 20 rows tall.

| Key | Action |
| --- | --- |
| `tab`, `shift+tab` | Switch pane focus |
| `1`, `2`, `3` | Focus runs, tree, or logs |
| `j/k`, up/down | Navigate or scroll |
| `PgUp/PgDn`, `ctrl+u/ctrl+d` | Page through the focused pane |
| `g/G`, home/end | First/last item or log line |
| `enter`, space | Expand a target or focus a step's logs |
| `h/l`, left/right | Collapse/expand targets; scroll logs horizontally |
| `E/C` | Expand/collapse all targets |
| `s` | Toggle target tree and slowest-step ranking |
| `a` | Toggle selected target's own logs and full dependency subtree |
| `t` | Cycle combined output, stdout, and stderr |
| `/`, `n/N`, escape | Search logs, next/previous match, clear search |
| `r` | Refresh run history, including imports from another terminal |
| `?` | Show help |
| `q`, `ctrl+c` | Quit |

Shared targets appear once as full nodes and again as `[shared]` references.
Enter on a shared reference jumps to the original target. IDs distinguish target
instances with different arguments or platforms. Dependency links describe a
build graph rather than a chronological function call stack. Subtree output
counts each shared target once.

Cached steps display `[cache]`. `unknown` means the profile lacks a complete
recorded interval; zero remains a valid duration. Parallel durations overlap, so
adding them does not give the build's wall-clock duration. Target duration can
include conversion, waiting, and dependencies.

Command output chunks retain their original bytes and stream order in the
database. The viewer removes terminal escape sequences and control characters
before displaying them. Combined output includes stdout, stderr, and unclassified
CLI messages, while execution statistics are excluded. Select an individual
stdout/stderr stream to reconstruct lines written in partial chunks without
interleaving other streams. Target output merges chunks by recorded timestamp.

## Database and CLI

The database defaults to
`$XDG_STATE_HOME/earthbuild-profiler/runs.db`, or
`~/.local/state/earthbuild-profiler/runs.db` when `XDG_STATE_HOME` is unset. New
state directories use mode `0700` and database/export files use mode `0600`.
Reimporting the same build ID updates the saved profile while keeping its original
import date. Imports are atomic per profile. Profiles can contain sensitive logs
and build arguments; the database is local and is not encrypted.

Global flags go **before** the command:

```sh
earth-profile --db /tmp/project-runs.db import run.json
earth-profile --db /tmp/project-runs.db browse
earth-profile runs
earth-profile browse BUILD_ID
earth-profile logs --stream 2 BUILD_ID COMMAND_ID
earth-profile export BUILD_ID archived-run.json
earth-profile delete BUILD_ID
```

`logs` writes original bytes, allowing redirection to a file. Stream `0` selects
combined text output, `1` stdout, `2` stderr, and another number selects that raw
stream (including execution statistics). `export` defaults to stdout. TUI
browsing releases database locks between operations, so another process can
import runs while it is open. Press `r` to see new history.

Imports validate profile version 1, manifest version 2, target references, exact
integer timestamps, and base64 log data. Unknown metadata is retained for export.
A profile's entire JSON is decoded into memory; imports have a 512 MiB size limit.
Browsing loads only the selected run's full profile. Very large selected logs
also occupy memory while displayed.

## Demo and verification

`testdata/demo.json` is synthetic. It includes parallel work, a cached step, a
shared dependency, stdout/stderr, execution statistics, and a failed test.

```sh
make demo
make test
make lint
```

Tests cover JSON precision and validation, metadata round trips, persistent
history and upserts, graph cycles/shared targets, log scope and stream selection,
search/navigation, terminal control-sequence removal, layouts at several sizes,
CLI imports/exports, and capturing a failed build using a stand-in executable.
The capture test does not launch a container runtime or use a BuildKit image.

The UI follows the [Bubble Tea v2 model/update/view API](https://github.com/charmbracelet/bubbletea/blob/main/README.md).
Local storage uses [bbolt](https://github.com/etcd-io/bbolt).
