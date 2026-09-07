#!/bin/sh
# Audit actual target closures and package notices/source; never publishes.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
: "${TMPDIR:=$repo/.tools/tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
work=$(mktemp -d "$TMPDIR/wekango-license-audit.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
: "${AUDIT_OUTPUT:=$repo/.tools/dependency-audit}"
mkdir -p "$AUDIT_OUTPUT"
# Audit tools run on the host even when packages target another platform.
GOOS=$(go env GOHOSTOS) GOARCH=$(go env GOHOSTARCH) GOBIN="$work" go install github.com/google/go-licenses/v2@v2.0.1
GOOS=$(go env GOHOSTOS) GOARCH=$(go env GOHOSTARCH) GOBIN="$work" go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
if [ "$#" -eq 0 ]; then set -- .; fi
allowed=MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,0BSD,Unlicense,CC0-1.0,MPL-2.0,PostgreSQL,LicenseRef-MIT-Lucent
if [ "${AUDIT_ALL_PLATFORMS:-0}" = 1 ]; then
  awk '!/^#/ && NF {print}' scripts/release/platforms.txt > "$work/platforms"
else
  printf 'native %s %s %s\n' "$(go env GOOS)" "$(go env GOARCH)" "${GOARM:--}" > "$work/platforms"
fi
while read -r suffix target_os target_arch target_arm; do
  [ "$target_arm" != - ] || target_arm=''
  target="$AUDIT_OUTPUT/$suffix"
  mkdir -p "$target"
  export GOOS="$target_os" GOARCH="$target_arch" GOARM="$target_arm" CGO_ENABLED=0
  go list -deps "$@" > "$target/packages.txt"
  if grep -q '^golang.org/x/crypto/openpgp\($\|/\)' "$target/packages.txt"; then
    echo "Forbidden unmaintained OpenPGP package (GO-2026-5932)" >&2
    exit 1
  fi
  "$work/govulncheck" -scan=package "$@" > "$target/vulnerabilities-package.txt" 2>&1 || {
    cat "$target/vulnerabilities-package.txt"
    exit 1
  }
  "$work/go-licenses" check --allowed_licenses="$allowed" "$@"
  "$work/go-licenses" report "$@" > "$target/licenses.csv"
  # The classifier recognizes Lucent text but assigns unknown restriction type,
  # so save cannot process it. Check/report above still validate it; preserve
  # that one reviewed license separately, requiring its exact reviewed digest.
  "$work/go-licenses" save --ignore=github.com/dop251/goja/ftoa "$@" --save_path="$work/notices-$suffix"
  if grep -q '^github.com/dop251/goja/ftoa$' "$target/packages.txt"; then
    goja_dir=$(go list -f '{{.Dir}}' github.com/dop251/goja/ftoa)
    python3 scripts/dependency-audit/save-lucent.py "$goja_dir/LICENSE_LUCENE" "$work/notices-$suffix/github.com/dop251/goja/ftoa/LICENSE_LUCENE"
  fi
  mkdir -p "$target/notices"
  cp -R "$work/notices-$suffix/." "$target/notices/"
done < "$work/platforms"
mkdir -p "$AUDIT_OUTPUT/toolchain/notices/Go"
cp "$(go env GOROOT)/LICENSE" "$AUDIT_OUTPUT/toolchain/notices/Go/LICENSE"
go version > "$AUDIT_OUTPUT/toolchain/version.txt"
go list -m -json all > "$AUDIT_OUTPUT/modules.json"
python3 scripts/dependency-audit/package-notices.py "$AUDIT_OUTPUT"
# Source vulnerability analysis is run natively; binary analysis may be added
# for built cross-platform artifacts. Do not mistake an unsupported scan for pass.
export GOOS=$(go env GOHOSTOS) GOARCH=$(go env GOHOSTARCH) GOARM=''
"$work/govulncheck" "$@" > "$AUDIT_OUTPUT/vulnerabilities.txt" 2>&1 || {
  cat "$AUDIT_OUTPUT/vulnerabilities.txt"
  exit 1
}
cat "$AUDIT_OUTPUT/vulnerabilities.txt"
printf 'Dependency evidence: %s\n' "$AUDIT_OUTPUT"
