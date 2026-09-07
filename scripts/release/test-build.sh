#!/usr/bin/env bash
# Test build orchestration without cross-compiling the application 25 times.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
export TMPDIR="${TMPDIR:-$root/.tools/tmp}"
mkdir -p "$TMPDIR"
work="$(mktemp -d "$TMPDIR/build-test.XXXXXXXX")"
trap 'rm -rf -- "$work"' EXIT
cat > "$work/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$CGO_ENABLED" == 0 ]]
[[ "${FAIL_BUILD:-0}" != 1 ]] || exit 1
while [[ $# -gt 0 ]]; do
  if [[ "$1" == -o ]]; then printf 'fake %s/%s/%s\n' "$GOOS" "$GOARCH" "$GOARM" > "$2"; exit; fi
  shift
done
exit 1
STUB
chmod +x "$work/go"
export WEKANGO_GO="$work/go" WEKANGO_DIST_DIR="$work/dist"
[[ "$(bash "$root/rebuild-wekan.sh" platforms)" == "$(bash "$root/build.sh" platforms)" ]]
bash "$root/build.sh" dist-seq > "$work/output"
[[ $(find "$work/dist" -name '*.sha256sum' | wc -l) -eq 25 ]]
[[ -f "$work/dist/wekan-win64.exe" && -f "$work/dist/wekan-arm64" ]]
if command -v sha256sum >/dev/null; then
  (cd "$work/dist" && sha256sum -c ./*.sha256sum >/dev/null)
else
  (cd "$work/dist" && shasum -a 256 -c ./*.sha256sum >/dev/null)
fi
if bash "$root/build.sh" build invalid > /dev/null 2>&1; then exit 1; fi
if WEKANGO_VERSION='x;echo injected' bash "$root/build.sh" build arm64 >/dev/null 2>&1; then exit 1; fi
if FAIL_BUILD=1 bash "$root/build.sh" build arm64 >/dev/null 2>&1; then exit 1; fi
[[ ! -e "$work/dist/wekan-arm64" && ! -e "$work/dist/wekan-arm64.sha256sum" ]]
echo 'Build orchestration passed (25 names/checksums, invalid inputs, stale artifact removal).'
