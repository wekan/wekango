#!/usr/bin/env bash
# Local builds only. Publishing is confined to the human-triggered release workflow.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$repo_dir"
export TMPDIR="${TMPDIR:-$repo_dir/.tools/tmp}"
mkdir -p "$TMPDIR"
export CGO_ENABLED=0
GO="${WEKANGO_GO:-go}"
out="${WEKANGO_DIST_DIR:-$repo_dir/dist}"
version="${WEKANGO_VERSION:-dev}"
[[ "$version" =~ ^[A-Za-z0-9._+-]+$ ]] || { echo 'Invalid WEKANGO_VERSION' >&2; exit 2; }
platforms="$repo_dir/scripts/release/platforms.txt"
usage() {
  echo 'Usage: ./build.sh {build [platform]|dist-seq|test|platforms}'
  echo 'Builds dist/wekan-<platform>[.exe] and a SHA-256 checksum beside each binary.'
  echo 'WEKANGO_GO, WEKANGO_DIST_DIR and WEKANGO_VERSION override local defaults.'
}
build_target() {
  local name="$1" row target_os target_arch target_arm ext='' file
  row="$(awk -v name="$name" '$1 == name {print $2, $3, $4}' "$platforms")"
  [[ -n "$row" ]] || { echo "Unknown platform: $name" >&2; return 2; }
  read -r target_os target_arch target_arm <<< "$row"
  [[ "$target_os" != windows ]] || ext=.exe
  [[ "$target_arm" != - ]] || target_arm=''
  mkdir -p "$out"
  file="wekan-$name$ext"
  # A failed build must not leave an older executable eligible for publication.
  rm -f -- "$out/$file" "$out/$file.sha256sum"
  GOOS="$target_os" GOARCH="$target_arch" GOARM="$target_arm" "$GO" build \
    -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/wekan/wekango/internal/version.Version=$version" \
    -o "$out/$file" .
  chmod +x "$out/$file"
  if command -v sha256sum >/dev/null; then
    (cd "$out" && sha256sum "$file" > "$file.sha256sum")
  else
    (cd "$out" && shasum -a 256 "$file" > "$file.sha256sum")
  fi
  echo "Built $file"
}
case "${1:-build}" in
  platforms) awk '!/^#/ && NF {print $1}' "$platforms" ;;
  build)
    target="${2:-}"
    if [[ -z "$target" ]]; then
      host_os="$("$GO" env GOOS)"; host_arch="$("$GO" env GOARCH)"
      target="$(awk -v os="$host_os" -v arch="$host_arch" '$2 == os && $3 == arch {print $1; exit}' "$platforms")"
      [[ -n "$target" ]] || { echo "No target for $host_os/$host_arch; choose a listed platform." >&2; exit 2; }
    fi
    build_target "$target"
    ;;
  dist-seq)
    while read -r name rest; do
      [[ -n "$name" && "$name" != \#* ]] || continue
      build_target "$name"
    done < "$platforms"
    ;;
  test) "$GO" test -mod=readonly ./...; bash scripts/release/test-build.sh ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
