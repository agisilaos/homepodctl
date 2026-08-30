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

# Exercise the actual release-check script without depending on the installed
# Go version or running unrelated builds for every cleanup scenario.
shim_dir="$fixture_root/shims"
mkdir -p "$shim_dir"
cat > "$shim_dir/go" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GO_SHIM_LOG"
case "$*" in
  "help mod tidy")
    if [[ "$TIDY_CASE" == modern ]]; then
      echo 'usage: go mod tidy -diff'
    fi
    exit 0
    ;;
  "mod tidy -diff")
    [[ "$TIDY_CASE" == modern ]]
    exit
    ;;
  "mod tidy")
    case "$TIDY_CASE" in
      success) exit 0 ;;
      sum_drift) printf 'changed sum\n' > go.sum ;;
      sum_removed) rm -f go.sum ;;
      *)
        printf 'changed module\n' > go.mod
        printf 'changed sum\n' > go.sum
        ;;
    esac
    case "$TIDY_CASE" in
      failed) echo 'controlled tidy failure' >&2; exit 42 ;;
      HUP|INT|TERM) kill -s "$TIDY_CASE" "$PPID" ;;
    esac
    exit 0
    ;;
  "test ./..."|"vet ./...") exit 0 ;;
esac
if [[ "$1" == build && "$2" == -ldflags && "$4" == -o ]]; then
  flags="$3"
  version="${flags#*-X main.version=}"
  version="${version%% *}"
  commit="${flags#*-X main.commit=}"
  commit="${commit%% *}"
  date="${flags#*-X main.date=}"
  printf '#!/usr/bin/env bash\necho %q\n' "homepodctl $version ($commit) $date" > "$5"
  chmod +x "$5"
  exit 0
fi
echo "unexpected go invocation: $*" >&2
exit 99
SH

cat > "$shim_dir/cp" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ ( "$TIDY_CASE" == snapshot_mod && "$1" == go.mod ) ||
      ( "$TIDY_CASE" == snapshot_sum && "$1" == go.sum ) ]]; then
  printf 'partial snapshot\n' > "$2"
  echo 'controlled snapshot failure' >&2
  exit 41
fi
if [[ "$TIDY_CASE" == restore_failed && "$1" == "$TMPDIR/"*/go.mod && "$2" == go.mod ]]; then
  echo 'controlled restoration failure' >&2
  exit 43
fi
exec /bin/cp "$@"
SH
chmod +x "$shim_dir/go" "$shim_dir/cp"

check_module_restoration() {
  local sum_state="$1"
  local tidy_case="$2"
  local expected_status="$3"
  local name="tidy-$sum_state-$tidy_case"
  local repo
  local scratch="$fixture_root/$name-check"
  local output status backup
  repo="$(clone_fixture "$name")"
  mkdir -p "$scratch/tmp"

  # Module restoration is independent of the docs checker, covered below by
  # the real-Go verification and preflight tests.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$repo/scripts/docs-check.sh"
  if [[ "$sum_state" == absent ]]; then
    rm -f "$repo/go.sum"
  fi
  git -C "$repo" add scripts/docs-check.sh go.sum
  git -C "$repo" commit --quiet -m "Prepare module restoration fixture"
  cp "$repo/go.mod" "$scratch/go.mod"
  if [[ "$sum_state" == present ]]; then
    cp "$repo/go.sum" "$scratch/go.sum"
  fi

  set +e
  output="$(cd "$repo" && PATH="$shim_dir:$PATH" TMPDIR="$scratch/tmp" \
    TIDY_CASE="$tidy_case" GO_SHIM_LOG="$scratch/go.log" ./scripts/release-check.sh 2>&1)"
  status=$?
  set -e
  if [[ "$status" -ne "$expected_status" ]]; then
    echo "$output" >&2
    die "$name: expected exit $expected_status, got $status"
  fi

  if [[ "$tidy_case" == restore_failed ]]; then
    backup="$(find "$scratch/tmp" -mindepth 1 -maxdepth 1 -type d)"
    [[ -n "$backup" && -d "$backup" ]] || die "$name: missing recovery backup"
    cmp -s "$scratch/go.mod" "$backup/go.mod" || die "$name: original go.mod backup lost"
    if [[ "$sum_state" == present ]]; then
      cmp -s "$scratch/go.sum" "$backup/go.sum" || die "$name: original go.sum backup lost"
    fi
    [[ "$output" == *"backups retained at $backup"* ]] || die "$name: missing recovery path"
    ! cmp -s "$scratch/go.mod" "$repo/go.mod" || die "$name: restore failure was not exercised"
  else
    cmp -s "$scratch/go.mod" "$repo/go.mod" || die "$name: go.mod changed"
    [[ -z "$(ls -A "$scratch/tmp")" ]] || die "$name: temporary files leaked"
    git -C "$repo" diff --quiet || die "$name: tracked files changed"
    git -C "$repo" diff --cached --quiet || die "$name: index changed"
  fi
  if [[ "$sum_state" == present ]]; then
    cmp -s "$scratch/go.sum" "$repo/go.sum" || die "$name: go.sum changed"
  else
    [[ ! -e "$repo/go.sum" ]] || die "$name: go.sum was not removed"
  fi

  case "$tidy_case" in
    modern)
      grep -Fxq 'mod tidy -diff' "$scratch/go.log" || die "$name: modern path not exercised"
      if grep -Fxq 'mod tidy' "$scratch/go.log"; then
        die "$name: unexpectedly ran fallback tidy"
      fi
      ;;
    snapshot_mod|snapshot_sum)
      [[ "$output" == *'controlled snapshot failure'* ]] || die "$name: missing snapshot error"
      if grep -Fxq 'mod tidy' "$scratch/go.log"; then
        die "$name: tidy ran without complete snapshots"
      fi
      ;;
    *)
      grep -Fxq 'mod tidy' "$scratch/go.log" || die "$name: fallback not exercised"
      ;;
  esac
  case "$tidy_case" in
    drift|sum_drift|sum_removed)
      [[ "$output" == *'go.mod/go.sum drift detected'* ]] || die "$name: missing drift error"
      ;;
    failed)
      [[ "$output" == *'controlled tidy failure'* ]] || die "$name: missing tidy error"
      ;;
  esac
  echo "[release-check-test] $name: ok"
}

for sum_state in present absent; do
  check_module_restoration "$sum_state" success 0
  check_module_restoration "$sum_state" drift 1
  check_module_restoration "$sum_state" sum_drift 1
  check_module_restoration "$sum_state" failed 42
  check_module_restoration "$sum_state" HUP 129
  check_module_restoration "$sum_state" INT 130
  check_module_restoration "$sum_state" TERM 143
  check_module_restoration "$sum_state" snapshot_mod 41
  check_module_restoration "$sum_state" restore_failed 1
  check_module_restoration "$sum_state" modern 0
done
check_module_restoration present snapshot_sum 41
check_module_restoration present sum_removed 1

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
