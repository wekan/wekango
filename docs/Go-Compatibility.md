# Operating the Go compatibility preview

`docs/` began as a copy of WeKan's current documentation at commit `2037d4acb`.
Those documents describe the required full product. They do not mean the Go
implementation already implements every screen, command or integration.
[ROADMAP.md](../ROADMAP.md) is the authoritative implementation status.

## Configuration and files

The standalone defaults match `releases/ferretdb/start-wekan.sh` in WeKan:
`PORT=8080`, `ROOT_URL=http://localhost:8080`, database `wekan`,
`WRITABLE_PATH=<executable-directory>/data`, and files beneath `files/`.
`WRITABLE_PATH` ending in `files` does not receive an extra `files` segment.
Attachments, avatars and SQLite remain in `files/attachments`, `files/avatars`
and `files/db`. Explicit relative paths remain relative to the working directory.
`FERRETDB_SQLITE_DIR` and `FERRETDB_SQLITE_URL` preserve existing overrides.
An explicit `MONGO_URL` uses that external database and skips embedded SQLite.

`--check-config` displays the effective layout without opening storage or
printing database credentials. `--compatibility` prints the source inventory.
The embedded database uses a private ephemeral loopback endpoint and the real
FerretDB library, not a new SQLite schema or document-conversion layer.
Do not open the same SQLite files concurrently in two server processes.

Caddy normally serves plain HTTP on `PORT`, preserving Meteor deployments whose
`ROOT_URL` is HTTPS behind an existing TLS proxy. Set `CADDY_AUTO_HTTPS=true`
explicitly to let Caddy manage TLS; then use an HTTPS ROOT_URL and an appropriate
port (443 by default). Certificate storage is under `WRITABLE_PATH/caddy`.
The Caddy admin API and automatic config persistence are disabled.
`HTTP_FORWARDED_COUNT` now preserves the REST login throttle's WeKan semantics:
positive decimal prefixes select that many nonempty `X-Forwarded-For` entries
from the right; missing, zero, negative or invalid values use the socket peer.
`0x2` is zero because the source parses in radix 10. A chain shorter than the
configured count also falls back to the socket. Configure the count for your
trusted external proxy chain and ensure those proxies control the forwarded
headers, as in Meteor WeKan.

Caddy resolves the client before rewriting forwarding headers and overwrites
`X-Wekan-Client-IP` for the private application listener. An external client
cannot choose that private identity header. Do not include the in-process Caddy
to application hop in `HTTP_FORWARDED_COUNT`. This covers REST throttling and
API usage reports;
DDP and other Meteor client-address consumers remain future work.

## Implemented preview commands

- `--version`: embedded release version; no database connection.
- `--check-config`: read-only path/configuration report; no database connection.
- `--compatibility`: source contracts and implemented-slice inventory.
- `--migrate-checklist-minicard`: run only that exact historical migration,
  preserving its one-time marker. Never marks the complete schema upgraded.
- No argument: Caddy + Go app + embedded FerretDB (unless MONGO_URL supplied).

The embedded preview page supports local-password sign-in and authorized board
reads. It is not the complete Meteor browser bundle. Missing functionality is
listed in the roadmap; absent APIs do not silently accept writes.

## API usage reports

Requests under `/api` update WeKan's existing `eventlog` collection with
`stream: 'api'`. Rows identify the account ID and route pattern, so different
board IDs share one endpoint row; unmatched paths share `(no route)`. The
disabled-API gate, login routes, health checks and static files are not counted.
This writes the existing report format; the Go Admin Panel is still pending.

`WEKAN_API_USAGE_FLUSH_MS` retains the source's numeric/timer behavior, defaulting
to ten seconds. Distinct pending account/endpoint pairs are capped at 500 with
an overflow row, and 200 pairs trigger an early flush. Graceful shutdown joins
the final bounded flush before closing storage. A crash or failed database write
can lose the pending reporting window, as in the source.

The current source writer ignores a batch's `count` and increments the stored
summary once per flush. The port preserves this known discrepancy; these stored
counts must not be interpreted as exact numbers of API requests. The accumulator
itself counts calls correctly. Resolving the writer discrepancy is tracked in
ROADMAP.md.

Existing row IDs, first-seen timestamps and unknown fields survive updates.
Actor tallies use the original hashed keys and fifty-actor cap. Proxy geography
headers supply display labels only; they never determine authorization or row
identity. The public socket/client address follows `HTTP_FORWARDED_COUNT`.

With a freshly built binary, validate persisted shutdown reports locally:

```sh
TMPDIR="$PWD/.tools/tmp" WEKANGO_BINARY="$PWD/dist/wekan-arm64" node tests/api-usage.cjs
```

## Embedded database tools

The same executable provides `bsondump`, `mongodump`, `mongorestore`,
`mongoexport`, `mongoimport`, `mongofiles`, `mongostat` and `mongotop`:

```sh
./wekan-arm64 mongodump --db=wekan --archive=backup.archive
./wekan-arm64 mongorestore --archive=backup.archive
./wekan-arm64 mongoexport --db=wekan --collection=boards --out=boards.json
./wekan-arm64 bsondump --bsonFile=boards.bson
```

These are the upstream MongoDB tools called within the Go process, with their
usual arguments and BSON/archive/Extended JSON formats. They run before Caddy
and application writers start. `--help` and `--version` require no database.
A symlink named `mongodump` (or another tool name) selects that command directly.

Without an explicit connection argument, database tools use `MONGO_URL` when set,
or open the existing configured FerretDB SQLite directory in the same process.
Stop the application before opening those SQLite files with a tool command.
Tools retain upstream database/collection selection flags; use `--db=wekan`
when the operation should select only that database. An explicit `--uri`,
`--host`, `-h`, `--port` or `--config` retains upstream connection behavior.
`bsondump` always processes files/streams without starting storage.

BSON dump/restore, canonical JSON export/import and GridFS put/get/delete have
passed against embedded FerretDB. Two monitoring operations currently fail
against the pinned FerretDB: `mongostat` cannot decode its fractional `uptime`,
and `mongotop` requires the unimplemented `top` command. Their upstream CLI
implementations are embedded, but these backend gaps remain in ROADMAP.md.
External MongoDB authentication, cluster monitoring and every tool option still
need their broader integration matrix.

## Startup schema upgrades

The twelve current steps from `server/lib/schemaUpgradeSteps.js` run in the
background after HTTP starts. `/schema-upgrade-status` shows progress; append
`?json` for machine-readable state. The dashboard uses the existing product name
from settings and is public and read-only, matching Meteor WeKan.

The `_wekan_migration` collection and `schema-upgrade` marker retain the original
per-step history and `lastCheck: {version, at}` shape. A successful recheck skips
on subsequent boots of that version. Failed steps or unresolved files leave the
version unstamped and retry on the next boot. `WEKAN_FORCE_SCHEMA_UPGRADE=true`
forces a recheck; `WEKAN_SKIP_SCHEMA_UPGRADE=true` skips background upgrades.
The separate `checklist-minicard-unset` marker remains once-ever, so later user
choices survive forced rechecks. Shutdown cancels and joins the upgrade before
closing its database connection.

Historical attachment/avatar recovery copies or repoints known filesystem
versions using the existing WeKan candidate paths. Existing recorded paths,
source files, checksums and explicit storage choices are preserved. This does
not implement the older CFS/GridFS conversion or the full historical Meteor
migration chain; those remain in ROADMAP.md.

## Build, test and release

Use the repository `build.sh` and [release instructions](../scripts/release/README.md).
The platform list is a candidate cross-build matrix until each binary also passes
runtime tests. Run `scripts/dependency-audit/check.sh` before releases and include
its generated notices/source archive. The workflow prepares a draft prerelease
from an existing tag in `wekan/wekango`; a human maintainer controls publication.

## Reproduce the preview browser test

Create an isolated fixture directory that does not exist yet. Never pass a live
installation's directory to the fixture command. For example, from this checkout:

```sh
export TMPDIR="$PWD/.tools/tmp"
mkdir -p "$TMPDIR"
fixture=$(mktemp -d "$TMPDIR/browser.XXXXXX")
go run ./tests/browserfixture "$fixture/data/files/db"
./build.sh build
npm ci --prefix tests
npx --prefix tests playwright install chromium firefox
PORT=3900 ROOT_URL=http://127.0.0.1:3900 BIND_IP=127.0.0.1 \
  WRITABLE_PATH="$fixture/data" ./dist/wekan-arm64
```

Replace the executable suffix with your platform's name. In another terminal run
`WEKANGO_SCREENSHOTS="$fixture" node tests/browser.cjs` with that same fixture
path exported. Linux CI installs browser system dependencies using
`playwright install --with-deps`. `PLAYWRIGHT_MODULE` can instead point to an
existing compatible WeKan Playwright installation. Test users exist only in the
new disposable fixture. Stop the process before removing that exact fixture
folder.

## Compare migration behavior with WeKan

The differential suite runs the original JavaScript pipeline and the Go pipeline
against separate databases in one disposable embedded FerretDB. It compares
logical documents, step results, dashboard state and marker history. Generated
IDs are normalized while checking their shape and relationships; generated dates
are normalized while checking BSON date types. Existing IDs and dates must match.

```sh
npm ci --prefix tests
WEKAN_SOURCE_ROOT=/path/to/wekan \
  WEKAN_MONGODB_MODULE="$PWD/tests/node_modules/mongodb" \
  go test -race ./internal/migrations -run TestSchemaSourceDifferential -count=1
WEKAN_SOURCE_ROOT=/path/to/wekan \
  go test -race ./internal/clientip -run TestSourceClientKeyDifferential -count=1
```

CI checks out the pinned reference source without installing Meteor. Ordinary
`go test ./...` explicitly skips this differential test when the reference source
is unavailable; the real SQLite unit and integration tests still run. The source
comparison is required before changing migration behavior or its reference pin.

The client-address differential compares 90 count/header combinations directly
with WeKan's `resolveClientKey`, including decimal/hex prefixes, whitespace,
empty entries and insufficient hops. `go test -race ./internal/transport` runs
real Caddy ingress cases. To check the newly built executable's throttle through
its public listener and embedded database, run:

```sh
export TMPDIR="$PWD/.tools/tmp"
mkdir -p "$TMPDIR"
WEKANGO_BINARY="$PWD/dist/wekan-amd64" node tests/clientip.cjs
```

The runtime test creates and removes its own SQLite fixtures, checks counts 0,
1, 2 and `0x2`, and verifies that forged public/private headers cannot split a
locked client's throttle key. Use the binary filename for your host platform.
