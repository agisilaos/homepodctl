#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./scripts/release-config.sh
source ./scripts/cli-shared/release-core.sh
cli_tooling_check "$CLI_TEMPLATE_FINGERPRINT"
./scripts/cli-shared/module-check.sh
make fmt-check vet test docs-check
