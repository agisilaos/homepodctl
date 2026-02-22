#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "docs-check.sh must be run on macOS (Darwin)"
fi

[[ -f README.md ]] || die "README.md not found"
[[ -f CHANGELOG.md ]] || die "CHANGELOG.md not found"

echo "[docs-check] validating shared docs contract"
python3 ./scripts/docs-contract-check.py

echo "[docs-check] checking CLI help snapshots"
./scripts/check-help.sh

echo "[docs-check] validating release command references"
grep -Fq 'make release-check VERSION=vX.Y.Z' README.md || die "README missing make release-check usage"
grep -Fq 'make release-dry-run VERSION=vX.Y.Z' README.md || die "README missing make release-dry-run usage"
grep -Fq 'make release VERSION=vX.Y.Z' README.md || die "README missing make release usage"
grep -Fq 'scripts/release-check.sh' README.md || die "README missing scripts/release-check.sh reference"
grep -Fq 'scripts/release.sh' README.md || die "README missing scripts/release.sh reference"

echo "[docs-check] validating README command examples are parseable"
python3 - <<'PY'
import shlex
import sys
from pathlib import Path

lines = Path("README.md").read_text(encoding="utf-8").splitlines()
count = 0
for i, line in enumerate(lines, start=1):
    s = line.strip()
    if not s.startswith("homepodctl "):
        continue
    count += 1
    try:
        shlex.split(s)
    except ValueError as exc:
        print(f"README.md:{i}: invalid command example: {exc}: {s}", file=sys.stderr)
        raise SystemExit(1)

if count == 0:
    print("no homepodctl command examples found in README.md", file=sys.stderr)
    raise SystemExit(1)

print(f"[docs-check] verified {count} parseable homepodctl command examples")
PY

echo "[docs-check] ok"
