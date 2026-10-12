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

## Web viewer and upload API

Serve the same database through a browser:

```sh
make build
./bin/earth-profile serve
# Open http://localhost:8080
```

The default listen address is `127.0.0.1:8080`. To listen on a container or
remote host, use `earth-profile --db /data/runs.db serve --listen 0.0.0.0:8080`.
The frontend is embedded in the Go binary; no Node installation or separate
frontend server is required. Existing CLI imports also appear after Refresh.

The viewer includes searchable run history, target dependency links, step
source locations and cache status, an overlapping execution timeline, slowest
step ranking, and target/command logs with stream and dependency filters.
Timeline bars use recorded start times relative to the build's wall time;
unknown intervals are not inferred. Export JSON downloads the complete stored
profile, including original log bytes and metadata.

Upload a completed build from another machine or CI job:

```sh
earth --profile run.json +all
curl --fail-with-body \
  -H 'Content-Type: application/json' \
  --data-binary @run.json http://localhost:8080/api/runs
```

Failed builds can also be uploaded if EarthBuild exported a profile. A CI job
should run the upload after the build, preserving the build's exit status.
The browser's **Upload profile** button accepts the same JSON file. Uploads
validate the full profile and atomically upsert by build ID, preserving the
original import date. The request body limit is 512 MiB. Use raw JSON request
bodies, rather than multipart forms.

### Container

The [Earthfile](Earthfile) builds the binary and runtime image with Earthly 0.8
or a compatible EarthBuild CLI. `+image` loads `ghcr.io/rdunne2020/earthbuild-profiler:latest` into
your local container runtime. Compose uses that image rather than building it.

```sh
earthly +image
docker compose up -d
# Open http://localhost:8080
```

After source changes, run `earthly +image` again and then `docker compose up -d`
to recreate the service with the new image. `make image` is a shortcut for the
Earthly image build.

Compose binds the port to localhost and stores history in the persistent
`profiles` volume. The image runs as a non-root user and shuts down gracefully
on SIGTERM. Without Compose:

```sh
earthly +image
docker run --rm -p 127.0.0.1:8080:8080 \
  -v earth-profile-data:/data ghcr.io/rdunne2020/earthbuild-profiler:latest
```

Other Earthfile targets are available for local development and CI:

```sh
earthly +build  # Export bin/earth-profile
earthly +test   # Run Go race tests
earthly +lint   # Check formatting and run Go vet
earthly +all    # Run checks, export the binary, and build the image
```

Use `earth +image` with an EarthBuild executable named `earth`, or set
`make image EARTHLY=/path/to/earth`. To customize the saved image name, use
`earthly +image --IMAGE=my-profiler:dev`; run that name directly or update the
Compose image setting to match.

For shared access, set `EARTH_PROFILE_TOKEN` on the server (or in the environment
when starting Compose), and provide it as a bearer token on every API request:

```sh
export EARTH_PROFILE_TOKEN='your-secret-token'
earthly +image
docker compose up -d
curl --fail-with-body \
  -H "Authorization: Bearer $EARTH_PROFILE_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @run.json http://localhost:8080/api/runs
```

Use **API token** in the browser to connect; the token stays in memory for that
tab and is not stored in local storage. Setting a token protects reads, logs,
exports, and uploads. With no token, anyone who can reach the server can read
and upload profiles. Logs and build arguments may contain secrets: keep the
service on a trusted network or put it behind an HTTPS reverse proxy. For
remote access, adjust the Compose port binding deliberately. No CORS access
is enabled; the frontend uses the same origin as the API.

| Endpoint | Behavior |
| --- | --- |
| `GET /healthz` | Public HTTP liveness check |
| `GET /api/runs` | Run summaries, most recently started first |
| `POST /api/runs` | Validate and save a raw JSON profile; returns its summary |
| `GET /api/runs/{id}` | Profile metadata, graph, and durations, without log chunks |
| `GET /api/runs/{id}/export` | Complete profile JSON download |
| `GET /api/runs/{id}/logs?command=STEP&stream=0` | Original command log bytes |
| `GET /api/runs/{id}/logs?target=TARGET&dependencies=true&stream=0` | Target output, optionally including dependencies |

URL-encode build and step IDs. Log stream `0` combines text output, `1` selects
stdout, and `2` stderr; other nonnegative 32-bit stream IDs select raw streams.
API failures return a JSON `error`, with HTTP 400 for invalid profiles/queries,
401 for missing or invalid tokens, 404 for missing runs/items, and 413 for
oversized requests. Reimporting an existing ID returns HTTP 200, as does a new
import. The API intentionally offers no deletion endpoint; use the local CLI
for deletion. History and each selected profile are loaded into memory, so
size the container for your largest profiles and number of stored runs.

### Links in CI output

Every successful upload returns the saved run's summary plus an absolute `url`
that opens that exact run, including when an older build finishes uploading
later than a newer one. It also sets the HTTP `Location` header. For example:

```json
{"id":"demo-failed-run","url":"https://profiler.example.com/?run=demo-failed-run"}
```

Other summary fields are included in the actual response. A normal `curl`
prints this JSON in the job output. To print just a clickable results line:

```sh
response=$(curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $EARTH_PROFILE_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @run.json https://profiler.example.com/api/runs) || exit $?
printf 'Build profile: %s\n' "$(printf '%s' "$response" | jq -r .url)"
```

Behind an HTTPS reverse proxy, configure the public origin so generated links
use the address your team opens:

```sh
earth-profile serve --listen 0.0.0.0:8080 \
  --public-url https://profiler.example.com
```

Alternatively set `EARTH_PROFILE_PUBLIC_URL=https://profiler.example.com`, which
Compose forwards to the server. The public URL must be an HTTP(S) origin without
a path, query, or fragment. By default, links use the incoming request's scheme
and host; forwarded headers are not trusted. Links contain no API token. If token
protection is enabled, the viewer prompts for it and then opens the linked run.
Selecting a run also updates the address bar so its URL can be shared.

### Swagger API documentation

With the server running, open **http://localhost:8080/docs/**, or use **API docs**
in the viewer header. This interactive Swagger UI documents all API endpoints,
profile JSON schemas, errors, authentication, and upload responses with their
browser result links. The OpenAPI 3.0 specification is available at
**http://localhost:8080/openapi.json** and maintained in
[`internal/web/docs/openapi.json`](internal/web/docs/openapi.json).

Use **Authorize** to enter `EARTH_PROFILE_TOKEN` when configured, then **Try it
out** to call the server. For uploads, paste the completed profile JSON into the
request body. Requests use the same origin as the documentation page. Tokens
are kept in page memory and forgotten on reload. Documentation is public, while
API requests still enforce the server's token setting.

Swagger UI assets and the specification are embedded in the binary and container;
no CDN, external validator, Node installation, or separate docs server is needed.
Vendored asset version and licensing are recorded in
[`internal/web/docs/vendor/README.md`](internal/web/docs/vendor/README.md).

## GitHub CI and releases

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) uses the Earthfile for builds:

- Pull requests, pushes to `main`, and manual runs execute `+all` (race tests,
  formatting, Go vet, binary and image builds), then cross-compile downloadable
  CLI/TUI archives for Linux, macOS (`darwin`), and Windows on amd64 and arm64.
  These archives are available as workflow artifacts for 14 days.
- Pushes to `main` publish the Linux amd64 container to
  `ghcr.io/rdunne2020/earthbuild-profiler:main` and `:sha-<full-commit>`.
- Version tags such as `v1.2.3` publish `:v1.2.3` and `:latest`, then create a
  GitHub release with all six CLI/TUI archives and a `SHA256SUMS` file. Tags such
  as `v1.2.3-rc.1` create prereleases and do not replace `:latest`.

After committing and pushing the implementation, publish a version with:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Release publishing waits for all checks, binary builds, and image publishing to
succeed. Re-running a release workflow updates that tag's assets. No publishing
occurs for pull requests or manual runs. Authentication uses `GITHUB_TOKEN` with
job-specific `packages: write` and `contents: write` permissions; no PAT or registry
password needs to be added. The repository must allow these workflow permissions.
If the GHCR package already exists, grant this repository Actions access to it.
Set the package visibility to public in GitHub if anonymous image pulls are wanted.

Download binaries from the repository's GitHub Releases page. Unix archives retain
executable permissions; Windows archives contain `earth-profile.exe`. The same
binary includes the CLI, TUI, web server, and embedded Swagger documentation.
For a local cross-build, for example:

```sh
earthly +release-binary --GOOS=darwin --GOARCH=arm64
# dist/darwin-arm64/earth-profile
```

To run the published stable image without a local build:

```sh
docker run --rm -p 127.0.0.1:8080:8080 \
  -v earth-profile-data:/data ghcr.io/rdunne2020/earthbuild-profiler:latest
```

The workflow derives the registry namespace from the repository name, so forks
publish under their own owner. Adjust the local Earthfile/Compose defaults if
using a different namespace.
