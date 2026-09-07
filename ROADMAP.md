# WeKan Go implementation roadmap

Target: a **drop-in replacement for Meteor WeKan**, distributed as one executable
per platform at [wekan/wekango releases](https://github.com/wekan/wekango/releases).
The binary names are `wekan-amd64`, `wekan-arm64`, `wekan-mac-arm64`,
`wekan-win64.exe`, etc., following the FerretDB platform suffixes.

**Current status: compatibility foundation, not yet a drop-in release.** An
existing production installation must not be told that the incomplete preview
supports its full configuration or migration history. Draft prereleases remain
explicitly experimental until the acceptance gates below pass.

## Non-negotiable compatibility contracts

- Preserve existing environment names, default values, deployment paths, REST
  routes, response shapes, error/status behavior, authentication and authorization.
- Open the **same FerretDB SQLite files**, using the pinned fork's exported library.
  Do not copy documents into a new application-specific SQLite schema, rename
  collections, change IDs, or translate database files into a new format.
- Preserve attachments, avatars, thumbnails, versions and metadata paths. Existing
  files must not need to be relocated merely to switch executables.
- Port the same WeKan migrations, markers, ordering and restart semantics. Never
  mark the complete migration finished while only some steps have been ported.
- Preserve browser URLs, locale placeholders, DDP/SockJS, methods/publications,
  optimistic changes, reconnect, subscriptions and access revocation. Embedding
  browser assets alone does not provide Meteor compatibility.
- Keep the application MIT licensed; include only maintained dependencies under
  MIT-compatible **non-GPL** terms, satisfying their individual notices and source
  obligations. Apache/BSD/ISC code is not relabeled MIT. MPL-2.0 source-file
  obligations are retained; GPL/AGPL code is excluded.
- Build, test and prepare release workflows locally. Repository policy forbids
  assistant pushes, release uploads, tag creation on GitHub or publishing.

## Source baseline and copied documentation

The initial source is WeKan commit `2037d4acb` (2026-09-07).
`docs/` is copied from that checkout; original product documentation is retained
as the target specification, **not a statement of implemented Go features**.
The design starts at [Go.md](docs/Design/Multiverse/Go.md).
Go-specific operating instructions are in [Go compatibility](docs/Go-Compatibility.md).

The generated [source manifest](internal/compatibility/source-manifest.json)
currently identifies **140 environment names, 134 REST route registrations,
57 literal Mongo collection names and 12 schema-upgrade steps**. The scanner
counts literal JavaScript references only; dynamic routes, aliases, launchers,
FilesCollection and package-defined contracts require a separate audit. Refresh
with `python3 scripts/sync-compatibility.py /path/to/wekan` and review its diff.

## Implemented and selected building blocks

| Component | Selection | Status and evidence |
| --- | --- | --- |
| Native executable | Go 1.27.0 | One Go process; browser HTML embedded; `--version`, `--check-config`, `--compatibility` |
| HTTP/TLS | Caddy `v2.11.5-0.20260906132044-9dd286c5e49e`, selected modules only | Caddy and application HTTP server in one process; private loopback upstream; admin API/config persistence disabled; automatic TLS opt-in |
| Database | wekan/FerretDB v1.71.0 exported `ferretdb` package | Same SQLite implementation; reopen fixtures created directly by FerretDB; no external database executable needed |
| MongoDB protocol client | Official MongoDB Go Driver v2.9.0 | External MONGO_URL or private embedded endpoint; BSON document shapes retained |
| Password hashing | golang.org/x/crypto v0.56.0 | Existing Meteor SHA256-then-bcrypt local password verification; other mechanisms remain gated |
| Environment and files | Go standard library | Bundle `PORT=8080`; `ROOT_URL`; external `MONGO_URL`; `WRITABLE_PATH`; `FERRETDB_SQLITE_DIR` / `FERRETDB_SQLITE_URL`; attachment/avatar paths |
| REST slice | Project-owned Go handlers | Local-password login, logout/revocation, authorized board read; full REST parity pending |
| Current schema upgrades | Project-owned Go implementation | All twelve current steps, version-gated background startup, exact one-time checklist marker, historical file path recovery and live HTML/JSON dashboard; older Meteor migration history remains pending |
| Release automation | `build.sh` + GitHub Actions | FerretDB's 25 candidate targets; checksums, native smoke CI, pinned actions, dependency audit, human-triggered draft release |
| License/security evidence | `scripts/dependency-audit/check.sh` | Exact dependency closure, notices, MPL source snapshots, govulncheck; full release audit must pass on every target |

See [DEPENDENCIES.md](DEPENDENCIES.md) for verified upstream version/license links,
selected versus future packages, and dependency-maintenance rules.
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) records actual bundled components.
Do not interpret a version pin as a perpetual security guarantee: rerun audits for
any dependency, toolchain, platform or release change.

## Filesystem contract

| Deployment input | Existing path retained |
| --- | --- |
| Standalone bundle, no WRITABLE_PATH | `<executable-directory>/data/files/` |
| `WRITABLE_PATH=/data` | `/data/files/attachments`, `/data/files/avatars`, `/data/files/db` |
| Snap `WRITABLE_PATH=.../files` | `.../files/attachments`, `.../files/avatars`, `.../files/db`; no second `files` |
| `WRITABLE_PATH=..` | Relative to working directory as in Meteor |
| `FERRETDB_SQLITE_DIR` | Exact directory override |
| `FERRETDB_SQLITE_URL` | Exact FerretDB file URI override, including SQLite options |
| `MONGO_URL` | Existing external MongoDB/FerretDB; no embedded database opened |

SQLite must have one owner while testing a binary replacement. FerretDB may apply
its own index-metadata upgrades when it opens older files; same storage engine
is not a promise of downgrade compatibility. Tests use independent fixtures,
not the live Meteor development database. Uploaded-file **path calculation** is
implemented, including current schema-upgrade filesystem path healing. File
download/upload handlers and historical CFS/GridFS conversion remain pending.

## Progress log — 2026-09-07

- [x] Read the Go design and current WeKan/FerretDB code; inspect existing demo.
- [x] Remove old demo and deprecated Mongo driver examples in separate commit
  `7aad9a3`; retain history rather than a duplicate source archive.
- [x] Copy current WeKan docs and add Go-specific status/provenance guidance.
- [x] Pin current core dependencies and audit license compatibility.
- [x] Embed the actual FerretDB library and prove same-file read/update/reopen.
- [x] Add path/configuration positive and negative tests.
- [x] Port all twelve current schema-upgrade steps with real SQLite fixtures,
  preserving archive/template/shared-list rules, stored false/null values,
  embedded checklist recovery and attachment metadata/path repair.
- [x] Enable background startup, skip/force environment flags, clean-only version
  stamps and `/schema-upgrade-status` HTML/JSON progress. Preserve the checklist
  once-ever marker even during forced version rechecks.
- [x] Verify missing-volume retry, failure continuation, concurrent status reads,
  restart version gating, graceful shutdown and dashboard escaping.
- [x] Verify the REST session/board slice and embedded preview page.
- [x] Native Linux ARM64 executable, checksum/version and Chromium/Firefox smoke checks pass.
- [x] Add cross-build orchestration, checksum and invalid-target tests.
- [x] Validate workflow syntax with actionlint; do not dispatch workflows.
- [x] Native symbol vulnerability scan and all 25 target package/license scans pass; notices/source archive generated.
- [x] Cross-compile all 25 FerretDB-named targets locally and verify every checksum,
  repeated after integrating the full current schema-upgrade pipeline.
- [ ] Run non-Linux-ARM64 binaries natively in CI before runtime certification.

## Verification evidence

- `go test ./...` passes on Linux ARM64 against isolated embedded SQLite fixtures.
- `go test -race ./internal/api ./internal/database ./internal/migrations` passes.
- The source differential suite compares current JavaScript and Go pipelines on
  the same FerretDB engine, including full documents, step counters, dashboard
  state and marker history across initial, gated, forced and new-version runs.
  It requires the reference WeKan checkout and is registered explicitly in CI.
  Generated IDs/dates are normalized with type/shape checks; this suite is still
  a fixture set, not proof of every historical deployment or old migration.
- The built native executable is statically linked; version stamping and its
  per-binary SHA256 file verify.
- `tests/browser.cjs` passes in Chromium and Firefox against the real executable:
  embedded page, wrong-password rejection, existing Meteor bcrypt login, and an
  authorized board stored in SQLite before wekango startup. Credentials remain
  in memory. Both browsers also verify the actual twelve-step startup dashboard.
  `tests/startup.cjs` restarts the built executable against the same disposable
  SQLite files and verifies persisted gating, skip/force flags and clean shutdown.
  These are preview-page checks, not the full Meteor UI suite.
- Dependency audit covers all 25 candidate targets: 204-row license union and
  zero imported-package vulnerability findings. Native reachable-symbol scan
  passes. The unimported, unsupported `x/crypto/openpgp` module advisory is
  documented and imports are explicitly prohibited. Caddy uses a maintained
  upstream snapshot because stable v2.11.4 cannot build with fixed CEL versions.
- Release orchestration tests, dependency notice packaging tests and actionlint
  pass. All 25 candidate targets cross-compile and their checksums verify. Only Linux
  ARM64 has been run locally; other platform runtime checks remain CI work.

## Next implementation stages

1. **Compatibility harness and migration safety**
   - Add differential fixtures against Meteor WeKan and the supported FerretDB
     version for every route, method, publication, migration and environment flag.
   - Port the older Meteor migration history and remaining startup repairs beyond
     the twelve current schema-upgrade steps. Expand interrupted-upgrade and mixed
     historical deployment fixtures, plus read-only migration planning.
   - Expand filesystem tests to Windows launcher exceptions, Snap, Sandstorm,
     custom storage settings, old CFS/GridFS records and container mounts.
2. **Accounts and authorization parity**
   - Complete registration, reset/verification mail, password/hash migrations,
     persistent lockout, Argon2 hash support, 2FA, LDAP/AD, OIDC/OAuth2, CAS, SAML, Sandstorm, trusted
     headers, API tokens/scopes and disabled-account behavior.
   - Preserve native HttpOnly cookie routes, CSRF/origin checks, expiry/revocation
     and cross-tab/reconnect behavior. Preview browser bearer storage is temporary
     memory only and is not the final Meteor session implementation.
   - Port `HTTP_FORWARDED_COUNT` with explicitly trusted proxy handling. Until then
     a nonzero value is rejected rather than silently ignored; Caddy's private
     internal hop is accounted for by the API throttle.
3. **One real interactive board slice**
   - Port lists/cards/swimlanes, roles and all side effects: create, move, archive,
     activities, rules, notifications, hooks and indexing.
   - Implement authorized DDP/SockJS subscriptions with added/changed/removed,
     reconnect, ordering and immediate access revocation; retain/adapt existing
     Blaze/Tracker/Minimongo assets with reproducible embedding.
   - Pass existing WeKan browser tests on both servers, including negative access,
     touch/drag, keyboard, offline reconciliation and simultaneous editors.
4. **Attachments, import/export and database utilities**
   - Port FilesCollection metadata and existing filesystem routes, range requests,
     quotas, permissions, thumbnails, external object stores and path migrations.
   - Integrate only required Apache-licensed MongoDB tools packages from
     mongo-tools-patches through a stable library adapter; no embedded subprocess
     binary pretending to be an in-process port.
   - Preserve BSON/JSON/dump/restore formats, indexes/options, backup consistency,
     restoration limits and archive traversal defenses. Test old WeKan backups.
5. **Whole-product parity**
   - Bring every manifest item and package-defined contract under tests, including
     all REST routes, environment variables, automations, integrations, search,
     accessibility, themes, calendars, translations and admin settings.
   - Port remaining docs commands to Go only when those commands work; retain
     historical Meteor instructions explicitly as reference until then.
6. **Release readiness**
   - Build and run each supported platform; measure size, startup, resident memory,
     large-board latency, database locking and long-running resource use.
   - Run fuzz/race, archive/parser security, dependency/license/asset audits and
     positive/negative browser suites; preserve evidence and source notices.
   - Test replacing an existing stopped WeKan executable without data movement,
     changed configuration, reauthentication surprises or lost migration state.
   - Human maintainer reviews and publishes from existing tags to `wekan/wekango`.

## Release acceptance gates

A production drop-in claim requires all of the following, not merely a successful
`go build`: complete configuration/API/schema/migration/path compatibility;
passing full WeKan behavior and browser suites; database conformance; attachments
and backup round trips; supported authentication; platform runtime tests;
security/license evidence; and upgrade/rollback procedure tested on real fixtures.
