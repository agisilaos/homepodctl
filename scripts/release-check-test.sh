#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release-check-test.sh must be run on macOS (Darwin)"
fi

for tool in git python3; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

git diff --quiet || die "working tree has unstaged changes"
git diff --cached --quiet || die "index has staged changes"

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

source_repo="$fixture_root/source"
git clone --quiet . "$source_repo"

configure_fixture_git() {
  local repo="$1"
  git -C "$repo" config user.name "release-check test"
  git -C "$repo" config user.email "release-check-test@example.invalid"
}

clone_fixture() {
  local name="$1"
  local repo="$fixture_root/$name"
  git clone --quiet "$source_repo" "$repo"
  configure_fixture_git "$repo"
  echo "$repo"
}

expect_failure() {
  local name="$1"
  local repo="$2"
  local version="$3"
  local expected="$4"
  local output
  local status

  set +e
  output="$(cd "$repo" && ./scripts/release-check.sh "$version" 2>&1)"
  status=$?
  set -e

  if [[ "$status" -eq 0 ]]; then
    die "$name unexpectedly passed"
  fi
  if [[ "$output" != *"$expected"* ]]; then
    echo "$output" >&2
    die "$name failed without expected message: $expected"
  fi

  echo "[release-check-test] $name: ok"
}

echo "[release-check-test] versionless verification"
verify_repo="$(clone_fixture verify)"
(cd "$verify_repo" && ./scripts/release-check.sh)

candidate_version="v999.999.999"
valid_repo="$(clone_fixture valid)"
python3 - "$valid_repo/CHANGELOG.md" "$candidate_version" <<'PY'
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
version = sys.argv[2]
text = path.read_text(encoding="utf-8")
updated, count = re.subn(
    r"^## \[v[0-9]+\.[0-9]+\.[0-9]+\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$",
    f"## [{version}] - 2099-01-01",
    text,
    count=1,
    flags=re.MULTILINE,
)
if count != 1:
    raise SystemExit("could not replace top release heading")
path.write_text(updated, encoding="utf-8")
PY
git -C "$valid_repo" add CHANGELOG.md
git -C "$valid_repo" commit --quiet -m "Prepare release-check test candidate"
echo "[release-check-test] valid unpublished candidate"
(cd "$valid_repo" && ./scripts/release-check.sh "$candidate_version")

mismatch_repo="$(clone_fixture mismatch)"
expect_failure \
  "mismatched candidate" \
  "$mismatch_repo" \
  "$candidate_version" \
  "CHANGELOG.md top release heading must be ## [$candidate_version] - YYYY-MM-DD before release"

tagged_repo="$(clone_fixture tagged)"
top_version="$(sed -nE 's/^## \[(v[0-9]+\.[0-9]+\.[0-9]+)\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$/\1/p' "$tagged_repo/CHANGELOG.md" | head -n 1)"
[[ -n "$top_version" ]] || die "could not determine fixture release version"
if ! git -C "$tagged_repo" rev-parse -q --verify "refs/tags/$top_version" >/dev/null 2>&1; then
  git -C "$tagged_repo" tag "$top_version"
fi
expect_failure \
  "existing tag" \
  "$tagged_repo" \
  "$top_version" \
  "tag already exists: $top_version"

echo "[release-check-test] ok"
