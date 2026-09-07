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
Trusted external proxy configuration / nonzero `HTTP_FORWARDED_COUNT` is pending
and currently rejected. The private Caddy-to-app hop is trusted for rate limiting.

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
