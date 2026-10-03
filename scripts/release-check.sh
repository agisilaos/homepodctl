#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./scripts/release-config.sh
source ./scripts/cli-shared/release-core.sh
if [[ $# -eq 0 ]]; then
  # Ordinary verification deliberately accepts development changes.
  [[ "$(uname -s)" == Darwin ]] || cli_release_die "release-check.sh must be run on macOS (Darwin)"
  make verify-core
  cli_release_build_check dev dist/verify
  echo "  mode: verify"
else
  export GOTOOLCHAIN="$RELEASE_GO_TOOLCHAIN"
  cli_release_preflight "$@"
  make verify
  cli_release_build_check "$version"
  if [[ "$ci_mode" -eq 1 ]]; then echo "  mode: ci"; else echo "  mode: preflight"; fi
fi
