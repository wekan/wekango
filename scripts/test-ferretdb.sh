#!/usr/bin/env bash
# Test the copied runtime without modifying its verified source inventory.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
export TMPDIR="${TMPDIR:-$root/.tools/tmp}"
mkdir -p "$TMPDIR"
work="$(mktemp -d "$TMPDIR/ferretdb-handler.XXXXXXXX")"
trap 'rm -rf -- "$work"' EXIT
GO="${WEKANGO_GO:-go}"
python3 scripts/sync-ferretdb.py --verify
version="$("$GO" list -m -f '{{.Version}}' github.com/FerretDB/FerretDB)"
[[ "$version" =~ ^v1\.[0-9]+\.[0-9]+$ ]]
# Standalone FerretDB tests require generated version metadata. Overlay this
# test-only file so no generated file enters the source copy or native binary.
python3 - "$root" "$work" "$version" <<'PY'
import json
from pathlib import Path
import sys
root, work = map(Path, sys.argv[1:3])
metadata = work / 'version.txt'
metadata.write_text(sys.argv[3] + '\n')
(work / 'overlay.json').write_text(json.dumps({'Replace': {
    str(root / 'internal/compat/ferretdb/build/version/version.txt'): str(metadata),
}}))
PY
"$GO" test -mod=readonly -overlay="$work/overlay.json" \
  github.com/FerretDB/FerretDB/internal/handler -count=1 "$@"
