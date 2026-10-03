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

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

source_repo="$fixture_root/source"
git clone --quiet . "$source_repo"

# Qualification must exercise the working helpers/wrappers, even before a commit.
# Keep all snapshot commits inside disposable repositories.
python3 - "$PWD" "$source_repo" <<'PY'
import os
from pathlib import Path
import shutil
import subprocess
import sys
root, destination = map(Path, sys.argv[1:])
files = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=root)
for raw in files.split(b'\0'):
    if not raw:
        continue
    relative = Path(os.fsdecode(raw))
    source, target = root / relative, destination / relative
    if not source.exists() and not source.is_symlink():
        if target.exists():
            target.unlink()
        continue
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.is_symlink():
        target.unlink()
    shutil.copy2(source, target, follow_symlinks=False)
PY
git -C "$source_repo" -c user.name='Release fixture' -c user.email='fixture@example.invalid' add --all
git -C "$source_repo" -c user.name='Release fixture' -c user.email='fixture@example.invalid' commit --quiet --allow-empty -m 'Current working process snapshot'

# Every negative preflight must stop before ordinary validation. Block those
# commands even if a future fixture accidentally prepares a valid candidate.
negative_shim_dir="$fixture_root/negative-shims"
mkdir -p "$negative_shim_dir"
for tool in make go; do
  printf '#!/usr/bin/env bash\necho "preflight reached ordinary verification unexpectedly" >&2\nexit 98\n' > "$negative_shim_dir/$tool"
  chmod +x "$negative_shim_dir/$tool"
done

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
  output="$(cd "$repo" && PATH="$negative_shim_dir:$PATH" ./scripts/release-check.sh "$version" 2>&1)"
  status=$?
  set -e

  if [[ "$status" -eq 0 ]]; then
    die "$name unexpectedly passed"
  fi
  [[ "$output" != *'preflight reached ordinary verification unexpectedly'* ]] || die "$name reached the ordinary gate instead of failing preflight"
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
if [[ -n "${EXPECTED_GOTOOLCHAIN:-}" && "${GOTOOLCHAIN:-}" != "$EXPECTED_GOTOOLCHAIN" ]]; then
  echo "unexpected GOTOOLCHAIN: ${GOTOOLCHAIN:-unset}; expected $EXPECTED_GOTOOLCHAIN" >&2
  exit 97
fi

record_stage() {
  if [[ -n "${RELEASE_CHECK_STAGE_LOG:-}" ]]; then
    printf '%s\n' "$1" >> "$RELEASE_CHECK_STAGE_LOG"
  fi
}

fail_stage() {
  if [[ "${RELEASE_CHECK_FAILURE:-}" == "$1" ]]; then
    echo "controlled $1 failure" >&2
    exit 42
  fi
}

case "$*" in
  "mod tidy -modfile="*)
    record_stage module
    fail_stage module
    alternate="${3#-modfile=}"
    [[ "$alternate" != go.mod && "$alternate" == "$TMPDIR/"*/check.mod ]] || exit 99
    alternate_sum="${alternate%.mod}.sum"
    case "$TIDY_CASE" in
      success|modern|no_checkout_writes) exit 0 ;;
      sum_drift) printf 'changed sum\n' > "$alternate_sum" ;;
      sum_removed) rm -f "$alternate_sum" ;;
      *)
        printf 'changed module\n' > "$alternate"
        printf 'changed sum\n' > "$alternate_sum"
        ;;
    esac
    case "$TIDY_CASE" in
      failed) echo 'controlled tidy failure' >&2; exit 42 ;;
      HUP|INT|TERM) kill -s "$TIDY_CASE" "$PPID" ;;
    esac
    exit 0
    ;;
  "test ./...")
    record_stage test
    fail_stage test
    exit 0
    ;;
  "vet ./...")
    record_stage vet
    fail_stage vet
    exit 0
    ;;
esac
if [[ $# -eq 7 && "$1" == build && "$2" == -trimpath && "$3" == -ldflags && "$5" == -o && "$7" == ./cmd/homepodctl ]]; then
  record_stage build
  fail_stage build
  flags="$4"
  version="${flags#*-X main.version=}"
  version="${version%% *}"
  commit="${flags#*-X main.commit=}"
  commit="${commit%% *}"
  date="${flags#*-X main.date=}"
  printf '#!/usr/bin/env bash\necho %q\n' "homepodctl $version ($commit) $date" > "$6"
  chmod +x "$6"
  exit 0
fi
echo "unexpected go invocation: $*" >&2
exit 99
SH

orchestration_shim_dir="$fixture_root/orchestration-shims"
mkdir -p "$orchestration_shim_dir"
cat > "$orchestration_shim_dir/gofmt" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" != "-l cmd internal" ]]; then
  echo "unexpected gofmt invocation: $*" >&2
  exit 99
fi
if [[ -n "${RELEASE_CHECK_STAGE_LOG:-}" ]]; then
  printf '%s\n' format >> "$RELEASE_CHECK_STAGE_LOG"
fi
if [[ "${RELEASE_CHECK_FAILURE:-}" == format ]]; then
  echo 'controlled format failure' >&2
  echo 'cmd/homepodctl/main.go'
fi
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
if [[ "$2" == go.mod || "$2" == go.sum ]]; then
  echo 'forbidden checkout write' >&2
  exit 43
fi
exec /bin/cp "$@"
SH
chmod +x "$shim_dir/go" "$shim_dir/cp" "$orchestration_shim_dir/gofmt"

check_module_restoration() {
  local sum_state="$1"
  local tidy_case="$2"
  local expected_status="$3"
  local name="tidy-$sum_state-$tidy_case"
  local repo
  local scratch="$fixture_root/$name-check"
  local output status
  repo="$(clone_fixture "$name")"
  mkdir -p "$scratch/tmp"

  # Module restoration is independent of docs checking. The top-level
  # release-check invocation in make verify exercises the real docs checker.
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
    TIDY_CASE="$tidy_case" GO_SHIM_LOG="$scratch/go.log" ./scripts/cli-shared/module-check.sh 2>&1)"
  status=$?
  set -e
  if [[ "$status" -ne "$expected_status" ]]; then
    echo "$output" >&2
    die "$name: expected exit $expected_status, got $status"
  fi

  cmp -s "$scratch/go.mod" "$repo/go.mod" || die "$name: go.mod changed"
  [[ -z "$(ls -A "$scratch/tmp")" ]] || die "$name: temporary files leaked"
  git -C "$repo" diff --quiet || die "$name: tracked files changed"
  git -C "$repo" diff --cached --quiet || die "$name: index changed"
  [[ "$output" != *'forbidden checkout write'* ]] || die "$name: checkout restoration was attempted"
  if [[ "$sum_state" == present ]]; then
    cmp -s "$scratch/go.sum" "$repo/go.sum" || die "$name: go.sum changed"
  else
    [[ ! -e "$repo/go.sum" ]] || die "$name: go.sum was not removed"
  fi

  case "$tidy_case" in
    snapshot_mod|snapshot_sum)
      [[ "$output" == *'controlled snapshot failure'* ]] || die "$name: missing snapshot error"
      if [[ -s "$scratch/go.log" ]]; then
        die "$name: tidy ran without complete snapshots"
      fi
      ;;
    *)
      grep -Fq 'mod tidy -modfile=' "$scratch/go.log" || die "$name: alternate modfile not exercised"
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
  check_module_restoration "$sum_state" no_checkout_writes 0
  check_module_restoration "$sum_state" modern 0
done
check_module_restoration present snapshot_sum 41
check_module_restoration present sum_removed 1

prepare_verification_fixture() {
  local repo="$1"
  cat > "$repo/scripts/docs-check.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 0 ]]; then
  echo "unexpected docs-check arguments: $*" >&2
  exit 99
fi
printf '%s\n' docs >> "$RELEASE_CHECK_STAGE_LOG"
if [[ "${RELEASE_CHECK_FAILURE:-}" == docs ]]; then
  echo 'controlled docs failure' >&2
  exit 42
fi
SH
  chmod +x "$repo/scripts/docs-check.sh"
  # Isolate orchestration assertions from recursive fixture execution. The real
  # make verify invocation still runs every fixture outside these local shims.
  python3 - "$repo/Makefile" <<'PY'
from pathlib import Path
import re
import sys
path = Path(sys.argv[1])
text, count = re.subn(r'(?m)^release-fixtures:\n(?:\t[^\n]*\n)+', 'release-fixtures:\n\t@:\n', path.read_text())
if count != 1:
    raise SystemExit('could not isolate release fixture recursion')
path.write_text(text)
PY
  git -C "$repo" add scripts/docs-check.sh Makefile
  git -C "$repo" commit --quiet -m "Prepare verification orchestration fixture"
}

check_verification_orchestration() {
  local name="$1"
  local repo="$2"
  local version="$3"
  local failure="$4"
  local expected_status="$5"
  local expected_stages="$6"
  local expected_mode="$7"
  local expected_version="$8"
  local expected_binary="$9"
  local scratch="$fixture_root/$name-check"
  local output status stages
  local expected_toolchain="go1.22.12"
  [[ -z "$version" ]] || expected_toolchain="go1.27.1"
  local -a command=(./scripts/release-check.sh)

  if [[ -n "$version" ]]; then
    command+=("$version")
  fi
  mkdir -p "$scratch/tmp"
  set +e
  output="$(cd "$repo" && PATH="$orchestration_shim_dir:$shim_dir:$PATH" TMPDIR="$scratch/tmp" \
    TIDY_CASE=modern GO_SHIM_LOG="$scratch/go.log" \
    GOTOOLCHAIN=go1.22.12 EXPECTED_GOTOOLCHAIN="$expected_toolchain" \
    RELEASE_CHECK_STAGE_LOG="$scratch/stages.log" RELEASE_CHECK_FAILURE="$failure" \
    "${command[@]}" 2>&1)"
  status=$?
  set -e

  if [[ "$status" -ne "$expected_status" ]]; then
    echo "$output" >&2
    die "$name: expected exit $expected_status, got $status"
  fi
  stages="$(paste -sd ' ' "$scratch/stages.log")"
  if [[ "$stages" != "$expected_stages" ]]; then
    echo "$output" >&2
    die "$name: stages were '$stages', expected '$expected_stages'"
  fi
  if [[ -n "$failure" ]]; then
    [[ "$output" == *"controlled $failure failure"* ]] || die "$name: missing controlled failure"
  else
    [[ "$output" == *'[release-check] ok'* ]] || die "$name: missing success output"
    [[ "$output" == *"mode: $expected_mode"* ]] || die "$name: wrong mode"
    [[ "$output" == *"version: $expected_version"* ]] || die "$name: wrong version"
    [[ "$output" == *"binary: $expected_binary"* ]] || die "$name: wrong binary path"
  fi
  echo "[release-check-test] $name: ok"
}

verify_repo="$(clone_fixture verify)"
prepare_verification_fixture "$verify_repo"
echo "[release-check-test] versionless verification orchestration"
check_verification_orchestration \
  "versionless verification" "$verify_repo" "" "" 0 \
  "module format vet test docs build" "verify" "dev" "dist/verify/homepodctl"

printf '\nLocal development fixture.\n' >> "$verify_repo/README.md"
git -C "$verify_repo" add README.md
printf '\nUnstaged development fixture.\n' >> "$verify_repo/README.md"
printf 'Local untracked fixture.\n' > "$verify_repo/local-notes.txt"
check_verification_orchestration \
  "versionless development changes" "$verify_repo" "" "" 0 \
  "module format vet test docs build" "verify" "dev" "dist/verify/homepodctl"

candidate_version="$(python3 - "$source_repo" <<'PY'
from pathlib import Path
import re
import subprocess
import sys
repo = Path(sys.argv[1])
existing = set(re.findall(r'^## \[(v[0-9]+\.[0-9]+\.[0-9]+)\]', (repo / 'CHANGELOG.md').read_text(), re.M))
existing.update(subprocess.check_output(['git', 'tag', '--list'], cwd=repo, text=True).splitlines())
patch = 999
while f'v999.999.{patch}' in existing:
    patch += 1
print(f'v999.999.{patch}')
PY
)"
valid_repo="$(clone_fixture valid)"
python3 - "$valid_repo/CHANGELOG.md" "$candidate_version" <<'PY'
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
version = sys.argv[2]
text = path.read_text(encoding="utf-8")
first = text.index('\n## ')
section = f'\n## [{version}] - 2099-01-01\n\n- Candidate fixture. [Source](https://github.com/agisilaos/homepodctl/commit/abcdef123456)\n'
path.write_text(text[:first] + section + text[first:], encoding="utf-8")
PY
git -C "$valid_repo" add CHANGELOG.md
git -C "$valid_repo" commit --quiet -m "Prepare release-check test candidate"
prepare_verification_fixture "$valid_repo"
echo "[release-check-test] release preflight orchestration"
check_verification_orchestration \
  "valid unpublished candidate" "$valid_repo" "$candidate_version" "" 0 \
  "module format vet test docs build build" "preflight" "$candidate_version" "dist/release-check/homepodctl"

for failure in module format vet test docs build; do
  failure_repo="$(clone_fixture "failure-$failure")"
  prepare_verification_fixture "$failure_repo"
  expected_status=2
  case "$failure" in
    module) expected_stages="module" ;;
    format) expected_stages="module format" ;;
    vet) expected_stages="module format vet" ;;
    test) expected_stages="module format vet test" ;;
    docs) expected_stages="module format vet test docs" ;;
    build) expected_status=42; expected_stages="module format vet test docs build" ;;
  esac
  check_verification_orchestration \
    "$failure failure" "$failure_repo" "" "$failure" "$expected_status" \
    "$expected_stages" "" "" ""
done

mismatch_repo="$(clone_fixture mismatch)"
expect_failure \
  "mismatched candidate" \
  "$mismatch_repo" \
  "$candidate_version" \
  "top release heading must be ## [$candidate_version] - YYYY-MM-DD"

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

ci_repo="$(clone_fixture historical-ci)"
prepare_verification_fixture "$ci_repo"
check_verification_orchestration \
  "historical CI" "$ci_repo" "--ci" "" 0 \
  "module format vet test docs build build" "ci" "$top_version" "dist/release-check/homepodctl"

untracked_repo="$(clone_fixture untracked-preflight)"
printf 'untracked fixture\n' > "$untracked_repo/untracked.txt"
expect_failure "untracked preflight" "$untracked_repo" "$candidate_version" "working tree is not clean"

unlinked_repo="$(clone_fixture unlinked-candidate)"
python3 - "$unlinked_repo/CHANGELOG.md" "$candidate_version" <<'PY'
from pathlib import Path
import sys
path, version = Path(sys.argv[1]), sys.argv[2]
text = path.read_text()
first = text.index('\n## ')
path.write_text(text[:first] + f'\n## [{version}] - 2099-01-01\n\n- Unlinked fixture.\n' + text[first:])
PY
git -C "$unlinked_repo" add CHANGELOG.md
git -C "$unlinked_repo" commit --quiet -m 'Unlinked candidate fixture'
expect_failure "unlinked candidate" "$unlinked_repo" "$candidate_version" "must link to a GitHub pull request or commit"

echo "[release-check-test] ok"
