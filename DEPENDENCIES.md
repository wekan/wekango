# Dependency selection and release audit

Selection reviewed on 2026-09-07. `go.mod` and `go.sum` are the reproducible
build inputs; this document records why dependencies were selected and the
remaining work. A current release is not proof that a dependency has no
vulnerabilities. Every release must pass the vulnerability and license checks
below, including its actual transitive package closure on every target platform.
Full-matrix command: `AUDIT_ALL_PLATFORMS=1 scripts/dependency-audit/check.sh`.
Archive the entire output directory (`.tools/dependency-audit` by default) as
release materials: it contains complete notices and the exact MPL source ZIPs,
with hashes and provenance in `sources/manifest.json`. Run against a fresh output
directory for each release. Python 3 is required by the packaging step.

WeKan Go's own source is MIT. Apache-2.0, BSD and ISC components retain their
licenses, copyright statements and applicable NOTICE files in a combined
executable. Calling the entire combined work MIT would be incorrect. The policy
for newly linked dependencies is licensing compatible with distributing
WeKan Go's MIT source, with no GPL, LGPL, AGPL or SSPL dependency. MPL-2.0
components may be combined with the MIT application while retaining their
file-level license and making their covered source available.
Unknown licenses fail review rather than being assumed compatible.

## Selected core

| Component | Pin selected | License and evidence | Scope |
| --- | --- | --- | --- |
| Go toolchain / standard library | 1.27.0 | [BSD-3-Clause](https://go.dev/LICENSE) | HTTP, templates, embed, JSON, archives, cryptographic randomness, logging, tests. Use the latest security patch before releasing. |
| Caddy | `github.com/caddyserver/caddy/v2 v2.11.5-0.20260906132044-9dd286c5e49e` | [Apache-2.0](https://github.com/caddyserver/caddy/blob/9dd286c5e49e/LICENSE), [releases](https://github.com/caddyserver/caddy/releases) | Pinned maintained upstream snapshot (2026-09-06), newer than stable v2.11.4, required for fixed CEL compatibility. Embedded HTTP/TLS lifecycle. Import only required modules; third-party Caddy plugins need their own review. |
| Maintained WeKan FerretDB v1 fork | `github.com/FerretDB/FerretDB`, replaced by the exact-commit runtime copy in `internal/compat/ferretdb` | [Apache-2.0](internal/compat/ferretdb/LICENSE), [releases](https://github.com/wekan/FerretDB/releases) | Public embeddable API, SQLite backend, existing document representation. Do not substitute upstream FerretDB v2 or direct application-owned SQL tables. |
| MongoDB Go Driver | `go.mongodb.org/mongo-driver/v2 v2.9.0` | [Apache-2.0](https://github.com/mongodb/mongo-go-driver/blob/v2.9.0/LICENSE), [releases](https://github.com/mongodb/mongo-go-driver/releases) | Application BSON and MongoDB wire client. Replaces the old prototype's driver v1 and unmaintained `gopkg.in/mgo.v2`. The FerretDB fork still has its own driver v1 dependency; that is a separate migration. |
| Go crypto extensions | `golang.org/x/crypto v0.56.0` | [BSD-3-Clause](https://github.com/golang/crypto/blob/v0.56.0/LICENSE) | Compatible password-hash verification. Preserve Meteor's password preprocessing and token formats rather than inventing incompatible hashes. |
| SQLite Go implementation | FerretDB's resolved `modernc.org/sqlite`; the copied fork selects v1.57.0 | [BSD-3-Clause wrapper](https://gitlab.com/cznic/sqlite/-/blob/master/LICENSE), [SQLite public domain](https://sqlite.org/copyright.html) | Pure Go permits CGO-free builds. v1.58.0 is the newest available stable candidate; update only with FerretDB schema and platform conformance tests. Translated sources and transitive modernc libraries need retained notices. |

The core pins above were verified against the Go module proxy's release/commit
metadata and upstream license files. Caddy is an explicit prerelease snapshot,
not a claim that the pinned commit is a stable tagged release. FerretDB is deliberately a maintained fork
pin rather than the unrelated newest upstream major. SQLite v1.58.0 is recorded
as an update candidate, not a claim that the current graph uses that version.

Current patch pins also selected after a successful core compile are
`github.com/caddyserver/certmagic v0.25.4`, `github.com/cloudflare/circl v1.6.5`
and `github.com/go-sql-driver/mysql v1.10.1`. Module resolution retains the
versions needed by the tested Caddy snapshot instead of blindly upgrading APIs.

## Transitive MPL-2.0 source obligations

The copied FerretDB v1 runtime registers its MySQL backend even when the selected runtime
backend is SQLite. Therefore `github.com/go-sql-driver/mysql` is linked, under
[MPL-2.0](https://github.com/go-sql-driver/mysql/blob/master/LICENSE). This
license is not GPL; preserve its notices and publish/provide the corresponding
covered source for the exact resolved version, including any modifications.
The application's MIT files remain MIT. The generated third-party inventory must
also enumerate any additional MPL components brought by Caddy or other modules.
For an unchanged module, retain a version-specific source link and make its
source archive available with release materials; a moving master link is not a
corresponding-source pin. Audit and package this per resolved dependency graph.

The actual core import closure also includes two scanner identifiers needing
explicit interpretation: `github.com/jackc/pgerrcode` has MIT code and
[PostgreSQL-licensed underlying data](https://github.com/jackc/pgerrcode/blob/afb5586c32a6/LICENSE);
`github.com/dop251/goja/ftoa` has a
[Lucent permissive notice](https://github.com/dop251/goja/blob/b07b74453ea9/ftoa/LICENSE_LUCENE),
reported as `LicenseRef-MIT-Lucent`. Both permit copying, modification and
distribution with retained notices; the latter also forbids using Lucent's name
in advertising without permission. These exact license texts were read before
adding their identifiers to the audit allowlist. Neither is GPL.

## Next adapters, not yet implemented

These are candidates from `docs/Design/Multiverse/Go.md`; listing them does not
claim that WeKan functionality has been ported. Recheck release and maintenance
status when implementing each adapter, then audit the actual imported packages.

| Facility | Candidate checked | License / evidence | Decision |
| --- | --- | --- | --- |
| WebSocket transport | `github.com/coder/websocket v1.8.15` | [ISC](https://github.com/coder/websocket/blob/v1.8.15/LICENSE.txt) | Candidate for transport only; DDP, SockJS fallback, reactivity and access revocation remain application work. |
| OIDC | `github.com/coreos/go-oidc/v3 v3.21.0` | [Apache-2.0](https://github.com/coreos/go-oidc/blob/main/LICENSE) | Maintained candidate; pair with a reviewed current `golang.org/x/oauth2` pin. |
| SMTP | `github.com/wneessen/go-mail v0.8.1` | [MIT](https://github.com/wneessen/go-mail/blob/main/LICENSE) | Maintained candidate; enforce TLS and timeout policy. |
| Markdown | `github.com/yuin/goldmark v1.8.6` | [MIT](https://github.com/yuin/goldmark/blob/master/LICENSE) | Candidate; existing Markdown compatibility fixtures required. |
| HTML sanitization | `github.com/microcosm-cc/bluemonday v1.0.27` | [BSD-3-Clause](https://github.com/microcosm-cc/bluemonday/blob/master/LICENSE.md) | Candidate with slower recent activity; recheck maintenance and advisories before adoption. |
| Spreadsheets | `github.com/qax-os/excelize/v2 v2.11.0` | [BSD-3-Clause](https://github.com/qax-os/excelize/blob/master/LICENSE) | Maintained candidate; preserve import/export behavior with fixtures. |
| LDAP | `github.com/go-ldap/ldap/v3 v3.4.14` | [License file](https://github.com/go-ldap/ldap/blob/master/LICENSE) | Active candidate, but upstream metadata reports NOASSERTION: review the full bundled notices before accepting, not just a badge. |
| Slugs | Project-owned compatibility code | Existing WeKan fixtures | Prefer no extra dependency. `gosimple/slug` uses [MPL-2.0](https://github.com/gosimple/slug/blob/master/LICENSE), which is non-GPL and may be combined with MIT code if its file-level source obligations are met; it must not be relabeled MIT. |
| PDF | Unselected | Library-specific review required | Do not select GPL/AGPL or commercial-only PDF implementations. A new PDF adapter needs maintained permissive code and layout fixtures. |

Use standard-library routing, templates, CSV, ZIP/TAR and logging until a concrete
compatibility requirement justifies another package. Browser JS/CSS/fonts are
also dependencies when embedded: Go's module audit does not cover them. In
particular, retain Font Awesome font/artwork licenses and inspect copied bundles
for dual-license choices. Copying documentation is not adopting every package
mentioned in that documentation.

## MongoDB tools integration

The WeKan [`mongo-tools-patches`](https://github.com/wekan/mongo-tools-patches)
repository contains MIT build scripts, and patches to
[upstream MongoDB Database Tools](https://github.com/mongodb/mongo-tools), whose
[source license is Apache-2.0](https://github.com/mongodb/mongo-tools/blob/master/LICENSE.md).
This is distinct from the MongoDB server's SSPL license. Do not embed MongoDB
server source or a server executable.

The tools' root license does not certify every dependency, build-tag variant or
optional plugin. Before porting dump/restore/import/export into subcommands,
pin the upstream commit and patch set, inventory linked licenses, retain notices,
and test backup interoperability with WeKan/FerretDB. A release must not fetch
unreviewed moving `master` source. Prefer the official BSON driver and compatible
project-owned streaming adapters when integrating an entire tool adds unnecessary
runtime dependencies. No mongo-tools implementation is claimed integrated yet.

## Security findings during initial selection

The initial core graph imported Caddy's `github.com/google/cel-go v0.28.1`,
which contains [GO-2026-6094](https://pkg.go.dev/vuln/GO-2026-6094). The affected
functions were not reachable in the import probe, but the selected graph upgrades
to v0.31.0 (latest compatible module path; fix starts at v0.30.0). Caddy v2.11.4
cannot compile against fixed CEL because of an interpreter interface change.
The maintained upstream Caddy snapshot above includes its adapter update;
compilation of that combination was verified. The final `go.mod` records the pin.
The newest release v0.32.0 changed its module path to `cel.dev/cel-go`; it cannot
be required under the old path. Adopt that maintained path when Caddy migrates
its imports, rather than introducing an invalid module replacement.

`golang.org/x/crypto v0.56.0` produces a module-level advisory for the unused,
unmaintained `openpgp` packages ([GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)).
There is no fixed version of that package. The release audit explicitly rejects
any `golang.org/x/crypto/openpgp` import on any target; this does not prohibit
the maintained bcrypt package. Record this distinction in scan results instead
of claiming that a module-only warning means the password code is vulnerable.

## Verification recorded for this implementation

The final core package closure passed license checks on all 25 platform rows
in `scripts/release/platforms.txt`: 204 distinct package/license rows, with MIT,
Apache-2.0, BSD-2-Clause, BSD-3-Clause, CC0-1.0, MPL-2.0, PostgreSQL and the
reviewed Lucent notice. MySQL v1.10.1 is the only MPL component in that union.
`THIRD_PARTY_NOTICES.md` contains the generated union and its pinned source
link; the release workflow regenerates it and packages the matching source ZIP.
This checks package loading and notices, not execution on all those systems.

The native application symbol scan reported no reachable vulnerabilities and
no vulnerable imported packages. It reported the documented unused OpenPGP
module advisory. Release automation additionally requires a package-level
scan for every target, so an imported vulnerable package cannot pass merely
because the current call graph does not reach an affected function.

The scanner cannot infer further dependencies from assembly. Its 94 reported
non-Go source files across the matrix were also checked for GPL-family license
markers; none were found. Their enclosing module notices are preserved. This
is not a blanket certification of optional plugins, CGO builds or future assets.
The special Lucent save workaround preserves its exact reviewed text and fails
if its SHA-256 changes; it does not bypass the package license check.

## Required release evidence

Run `scripts/dependency-audit/check.sh` after resolving modules and before a
release. It uses pinned audit tools, rejects licenses outside the allowlist,
writes a CSV inventory using SPDX license identifiers plus complete upstream license material, and
runs Go's vulnerability analysis. The generated inventory is a review aid, not a
substitute for checking file-specific licenses or the non-Go assets. Use the same
`GOOS`, `GOARCH` and build tags as the executable; repeat for each release target.
Run vulnerability analysis on the native target and binary analysis on produced
cross-platform artifacts where source analysis is unsupported.

Retain the audit output with the release build, package/embed applicable license
and NOTICE text in the executable or distribute it alongside every artifact, and
record module versions and checksums. No release is considered cleared merely
because this selection document exists. New unrecognized license identifiers
require evidence and an explicit policy decision, not an automatic allowlist
expansion. Keep the previous pin until an upgrade passes compatibility tests,
then commit updated module checksums and audit evidence together.

## Embedded MongoDB Database Tools

`github.com/mongodb/mongo-tools` is pinned to
`v0.0.0-20260903204226-5df87866650a`, the upstream development head resolved
on 2026-09-07. [Source and license](https://github.com/mongodb/mongo-tools/tree/5df87866650a3ed661b5da6869f430a971ba25e5).
This is the same upstream used by `mongo-tools-patches`; that repository currently
has no applicable source patches in `dist`. All eight command entry points call
upstream libraries within this executable. Their adapted Apache-2.0 entry points
retain their notices and local package license; the surrounding WeKan code stays
MIT. No MongoDB server/SSPL component is linked.

The selected graph retains MongoDB Go Driver v2.9.0 and existing security pins.
Azure azcore v1.23.1, azidentity v1.14.1 and MSAL v1.9.0 use their newest
verified releases, including MSAL's certificate and authority-validation hardening.
The terminal renderer is maintained tcell/v3 v3.4.2 (Apache-2.0), behind a small
project-owned facade for the symbols used by upstream mongostat. No unmaintained
termbox implementation is shipped. The old YAML import path forwards types and
calls to security-maintained go.yaml.in/yaml/v2 v2.4.4; no archived YAML parser is
shipped. Local module replacements make both facades reproducible in every build,
and the audit rejects silently reverting those paths to their legacy backends.
Terminal input, styles, Unicode cells, resize and lifecycle are tested against
tcell's mock terminal; YAML configuration has positive and malformed-input tests.
These helpers remain in the audited non-GPL dependency closure. The obsolete AWS SDK v1 module was removed by tidy;
it is not imported into the executable.

Upstream's abbreviated `LICENSE.md` is not recognized by go-licenses. The audit
verifies the exact module path/version/checksum, package ownership and reviewed
SHA256 values of that notice, upstream third-party notices and the full official
Apache-2.0 license before enabling a narrowly scoped scanner exception. It still
scans every transitive dependency and preserves all source notices. Upstream's
verbatim third-party notice is a superset; `licenses.csv` identifies the packages
actually linked into this executable. Changed
module versions or license text fail until reviewed again.

## Browser test tooling

The separate `tests/package-lock.json` pins `@playwright/test` v1.63.0
([Apache-2.0](https://github.com/microsoft/playwright/blob/main/LICENSE)), verified
against the npm registry on 2026-09-07. It is a development dependency, not code
linked into the Go executable. Its Chromium/Firefox downloads are external test
tools, not bundled application dependencies. `npm audit` reports no findings for
this locked sixteen-package test graph. The test-only MongoDB Node driver is
pinned to v7.6.0 (Apache-2.0), matching the reference WeKan driver. It executes
the original JavaScript migrations for differential fixtures; it is not bundled
or launched by the Go product. Its transitive packages use Apache-2.0, MIT, BSD
or ISC licenses. The CI reference source is pinned to WeKan `2037d4acb32b12a9de35fbcbba506801371d751c`.

The runtime copy provenance and refresh procedure are documented in
[Go-FerretDB-Source.md](docs/Go-FerretDB-Source.md). It includes the fork correction
for concurrent whole-document update loss while preserving its module graph.
