#!/usr/bin/env bash
# Backward-compatible entry point; build.sh owns all current build options.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec "$repo_dir/build.sh" "$@"
