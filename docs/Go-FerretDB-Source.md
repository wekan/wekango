# Embedded FerretDB runtime source

The Go application uses the maintained FerretDB v1 fork's exported `ferretdb`
package, SQLite backend and document representation. Its runtime source is
included under `internal/compat/ferretdb` as a local Go module replacement.
This makes local fork corrections available to clean builds without requiring
an unpublished commit to be downloaded from GitHub.

The current copy is fork commit `5472f18fbbcde4c2572ff1f0c84cd77dd90d9400`,
based on v1.73.0 plus the mutation-isolation correction.

The copy retains the Apache-2.0 license and original file notices. It is not
relabeled MIT. No new database schema or alternate storage engine is introduced.
`internal/compat/ferretdb/PROVENANCE.json` records the exact fork commit and SHA-256 of each copied
file. The selection contains `ferretdb/`, `internal/`, `build/version/`, the
module files, license and NOTICE, including runtime tests and embedded data. Release
Dockerfiles and build workflows remain in the companion fork.

Refresh from the repository root using an exact local fork commit:

```sh
export TMPDIR="$PWD/.tools/tmp"
mkdir -p "$TMPDIR"
python3 scripts/sync-ferretdb.py ../FerretDB FULL_COMMIT_HASH
python3 scripts/sync-ferretdb.py --verify
```

`build.sh` verifies the source inventory before compilation. Its test command
also runs the importer regressions and copied handler tests. The test runner
uses a temporary Go overlay for FerretDB's required test version metadata;
generated metadata never modifies the source copy or enters release binaries.
The importer reads committed Git objects, not dirty working files, and rejects
unreviewed modifications to the previous copy. Make runtime corrections in
`.tools/FerretDB` first, commit locally, then refresh this copy. Do not hand-edit
it. All pushes and releases remain human maintainer actions.

The mutation-isolation correction prevents concurrent read/modify/write commands
from replacing each other's document snapshots. A shared handler gate covers
mutations and background cleanup; waiting reads and change streams must remain
able to make progress. This favors correctness over parallel write throughput.
It does not authorize two processes to own the same SQLite directory. The
existing single-owner constraint still applies. Gate waits honor connection
cancellation; handler-level `maxTimeMS` starts after acquisition, so queue wait
is not yet included in that command time budget.
