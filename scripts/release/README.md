# Binary builds and releases

`./build.sh build` builds the native candidate target. `./build.sh build arm64`
selects a target; `./build.sh platforms` lists the matrix and `./build.sh dist-seq`
builds every target sequentially. Go is taken from PATH, or `WEKANGO_GO`.
`WEKANGO_VERSION` stamps `--version`. Binaries and individual SHA-256 files go into
`dist/`, or `WEKANGO_DIST_DIR`. No build command publishes anything. `rebuild-wekan.sh` forwards all arguments to
`build.sh` for compatibility with existing local commands.

All 25 entries were successfully cross-compiled locally with Go 1.27.0 and the
pinned dependency graph on 2026-09-07; all executable checksums were verified.
Only the Linux arm64 executable was run locally. Cross-platform compilation
must remain a release gate after dependency changes.

The platform names in `platforms.txt` follow `wekan/FerretDB`'s SQLite release
script: `wekan-amd64`, `wekan-arm64`, `wekan-win64.exe`, `wekan-mac-arm64`, etc.
The database uses modernc's pure-Go SQLite, so `CGO_ENABLED=0` builds need no C
cross compiler. This is a **candidate** matrix. Every target must compile for the
release workflow to pass; no failed target is silently omitted. Compiling a
binary does not establish runtime compatibility. Native CI runs tests and the
version command on Linux amd64/arm64, macOS amd64/arm64 and Windows amd64.
The browser CI job restores a disposable existing SQLite fixture and checks the
login/board flow in Chromium and Firefox. Other targets require device/VM tests
before production support is claimed.

The `release-all.yml` workflow targets
<https://github.com/wekan/wekango/releases>. A human maintainer first creates and
pushes a version tag, then manually dispatches the workflow with that existing
tag. Source tests, module verification, dependency license checks for every candidate
target and govulncheck must succeed before a
separate job can create a **draft prerelease**. It cannot create tags or overwrite
an existing release. A human reviews the draft, compatibility status and notices
before publishing. Neither the workflow nor its publishing steps were executed
while implementing this scaffold.

The release contains one uncompressed executable per candidate platform, matching
checksums, the project license, third-party notices and the current roadmap.
`wekan-dependency-notices.tar.gz` contains the full audit evidence, dependency
licenses and exact module source archives required for MPL-2.0 dependencies.
Keep that archive available alongside the executable; the project MIT license
does not replace third-party license obligations. Check
a downloaded executable before running it:

```sh
sha256sum -c wekan-amd64.sha256sum
chmod +x wekan-amd64
./wekan-amd64 --version
```

GitHub actions are pinned by commit SHA to the official latest releases checked
on 2026-09-07: checkout 7.0.1, setup-go 7.0.0, upload-artifact 7.0.1 and
download-artifact 8.0.1 and setup-node 7.0.0. govulncheck is pinned to `golang.org/x/vuln v1.7.0`.
Dependency update reviews must refresh pins and rerun tests/security checks.
