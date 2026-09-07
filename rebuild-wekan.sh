#!/usr/bin/env bash
# Preserve the documented local build entry point.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$root/build.sh" "$@"
