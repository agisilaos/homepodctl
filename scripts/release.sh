#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() { echo "error: $*" >&2; exit 1; }

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release.sh must be run on macOS (Darwin)"
fi

if ! command -v go >/dev/null 2>&1; then
  die "go is required"
fi

version="${1:-}"
if [[ -z "${version}" ]]; then
  die "usage: scripts/release.sh vX.Y.Z"
fi
if [[ "${version}" != v* ]]; then
  version="v${version}"
fi

repo_owner="agisilaos"
repo_name="homepodctl"
origin_url_default="git@github.com:${repo_owner}/${repo_name}.git"
origin_url="${ORIGIN_URL:-$origin_url_default}"
tap_repo="${HOMEBREW_TAP_REPO:-${repo_owner}/homebrew-tap}"
tap_remote_default="git@github.com:${tap_repo}.git"
tap_remote="${HOMEBREW_TAP_ORIGIN_URL:-$tap_remote_default}"

ensure_git_repo() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi

  echo "Initializing git repo..."
  git init -b main
  git add .
  git commit -m "chore: initial import"
}

ensure_github_repo() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "gh not found; skipping GitHub repo creation check"
    return 0
  fi
  if gh repo view "${repo_owner}/${repo_name}" >/dev/null 2>&1; then
    return 0
  fi
  echo "Creating GitHub repo ${repo_owner}/${repo_name} (public)..."
  gh repo create "${repo_owner}/${repo_name}" --public --confirm >/dev/null
}

ensure_origin_remote() {
  if git remote get-url origin >/dev/null 2>&1; then
    return 0
  fi
  echo "Adding origin remote: ${origin_url}"
  git remote add origin "${origin_url}"
}

require_clean_tree() {
  if ! git diff --quiet; then
    die "working tree has unstaged changes"
  fi
  if ! git diff --cached --quiet; then
    die "index has staged changes"
  fi
}

last_tag() {
  git describe --tags --abbrev=0 2>/dev/null || true
}

generate_changelog_section() {
  local prev_tag="$1"
  local range=""
  if [[ -n "${prev_tag}" ]]; then
    range="${prev_tag}..HEAD"
  fi

  # Keep it simple: list commit subjects.
  # Exclude merge commits by default to reduce noise.
  if [[ -n "${range}" ]]; then
    git log --no-merges --pretty=format:'- %s (%h)' "${range}"
  else
    git log --no-merges --pretty=format:'- %s (%h)'
  fi
}

update_changelog() {
  local ver="$1"
  local prev_tag="$2"
  local date_utc
  date_utc="$(date -u +%Y-%m-%d)"
  local notes
  notes="$(generate_changelog_section "${prev_tag}")"
  if [[ -z "${notes}" ]]; then
    notes="- No changes recorded."
  fi

  if [[ ! -f CHANGELOG.md ]]; then
    die "CHANGELOG.md not found"
  fi

  python3 - "$ver" "$date_utc" "$notes" <<'PY'
import sys

version = sys.argv[1]
date = sys.argv[2]
notes = sys.argv[3]

path = "CHANGELOG.md"
text = open(path, "r", encoding="utf-8").read()

unreleased = "## [Unreleased]"
idx = text.find(unreleased)
if idx == -1:
    raise SystemExit("error: CHANGELOG.md missing '## [Unreleased]' header")

insert_at = idx + len(unreleased)
section = f"\n\n## [{version}] - {date}\n\n{notes}\n"

# If this version already exists, bail.
if f"## [{version}]" in text:
    raise SystemExit(f"error: {version} already exists in CHANGELOG.md")

new_text = text[:insert_at] + section + text[insert_at:]
open(path, "w", encoding="utf-8").write(new_text)
PY

  git add CHANGELOG.md
}

build_dist() {
  local ver="$1"
  local commit
  commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  local date_utc
  date_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  rm -rf dist
  mkdir -p dist

  build_one() {
    local goarch="$1"
    local pkgdir="homepodctl_${ver}_darwin_${goarch}"
    local stage="dist/${pkgdir}"

    mkdir -p "${stage}"
    GOOS=darwin GOARCH="${goarch}" CGO_ENABLED=0 \
      go build -trimpath \
        -ldflags "-s -w -X main.version=${ver} -X main.commit=${commit} -X main.date=${date_utc}" \
        -o "${stage}/homepodctl" \
        ./cmd/homepodctl

    (cd dist && tar -czf "${pkgdir}.tar.gz" "${pkgdir}")
    rm -rf "${stage}"
  }

  build_one arm64
  build_one amd64

  (cd dist && shasum -a 256 *.tar.gz > SHA256SUMS.txt)
}

ensure_homebrew_tap_repo() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "gh not found; skipping Homebrew tap automation"
    return 1
  fi
  if gh repo view "${tap_repo}" >/dev/null 2>&1; then
    return 0
  fi
  echo "Creating Homebrew tap repo ${tap_repo} (public)..."
  gh repo create "${tap_repo}" --public --confirm >/dev/null
  return 0
}

update_homebrew_formula() {
  local ver="$1"
  local ver_nov="${ver#v}"

  if ! ensure_homebrew_tap_repo; then
    return 0
  fi

  local sha_arm sha_amd
  sha_arm="$(awk -v f=\"homepodctl_${ver}_darwin_arm64.tar.gz\" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  sha_amd="$(awk -v f=\"homepodctl_${ver}_darwin_amd64.tar.gz\" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  if [[ -z "${sha_arm}" || -z "${sha_amd}" ]]; then
    die "failed to parse SHA256SUMS.txt for ${ver}"
  fi

  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT

  git clone "${tap_remote}" "${tmp}/tap" >/dev/null 2>&1 || die "failed to clone tap repo: ${tap_remote}"

  mkdir -p "${tmp}/tap/Formula"
  cat >"${tmp}/tap/Formula/homepodctl.rb" <<RUBY
class Homepodctl < Formula
  desc "macOS CLI for Apple Music + HomePod control"
  homepage "https://github.com/${repo_owner}/${repo_name}"
  version "${ver_nov}"

  on_macos do
    on_arm do
      url "https://github.com/${repo_owner}/${repo_name}/releases/download/${ver}/homepodctl_${ver}_darwin_arm64.tar.gz"
      sha256 "${sha_arm}"
    end
    on_intel do
      url "https://github.com/${repo_owner}/${repo_name}/releases/download/${ver}/homepodctl_${ver}_darwin_amd64.tar.gz"
      sha256 "${sha_amd}"
    end
  end

  def install
    bin.install "homepodctl"
  end

  test do
    system "#{bin}/homepodctl", "version"
  end
end
RUBY

  pushd "${tmp}/tap" >/dev/null
  if [[ "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo)" != "main" ]]; then
    git checkout -B main >/dev/null 2>&1 || git checkout -b main
  fi
  git add Formula/homepodctl.rb
  if git diff --cached --quiet; then
    echo "Homebrew formula already up to date"
    popd >/dev/null
    return 0
  fi
  git commit -m "homepodctl ${ver}"
  git push origin HEAD:main
  popd >/dev/null

  echo "Updated Homebrew formula in ${tap_repo}"
}

create_github_release() {
  local ver="$1"

  if command -v gh >/dev/null 2>&1; then
    # Best-effort: extract just the notes for this version.
    local notes_file
    notes_file="$(mktemp)"
    python3 - "$ver" >"${notes_file}" <<'PY'
import sys
ver=sys.argv[1]
txt=open("CHANGELOG.md","r",encoding="utf-8").read().splitlines()
out=[]
in_sec=False
for line in txt:
    if line.startswith("## [") and line.startswith(f"## [{ver}]"):
        in_sec=True
        continue
    if in_sec and line.startswith("## ["):
        break
    if in_sec:
        out.append(line)
print("\n".join(out).strip() or f"Release {ver}")
PY
    gh release create "${ver}" dist/*.tar.gz dist/SHA256SUMS.txt --notes-file "${notes_file}" --latest
    rm -f "${notes_file}"
    return 0
  fi

  if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    echo "Skipping GitHub release creation (install gh or set GITHUB_TOKEN)."
    echo "Artifacts are ready in ./dist"
    return 0
  fi

  # Minimal GitHub API release creation + upload.
  local api="https://api.github.com"
  local release_json

  # Extract notes for this version from CHANGELOG.md (best-effort).
  local notes
  notes="$(python3 - "$ver" <<'PY'
import sys, re
ver=sys.argv[1]
txt=open("CHANGELOG.md","r",encoding="utf-8").read().splitlines()
out=[]
in_sec=False
for line in txt:
    if line.startswith("## [") and line.startswith(f"## [{ver}]"):
        in_sec=True
        continue
    if in_sec and line.startswith("## ["):
        break
    if in_sec:
        out.append(line)
print("\n".join(out).strip())
PY
)"
  if [[ -z "${notes}" ]]; then
    notes="Release ${ver}"
  fi

  release_json="$(curl -sS -X POST \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    "${api}/repos/${repo_owner}/${repo_name}/releases" \
    -d "$(python3 - <<PY
import json
print(json.dumps({
  "tag_name": "${ver}",
  "name": "${ver}",
  "body": """${notes}""",
  "draft": False,
  "prerelease": False
}))
PY
    )")"

  local upload_url
  upload_url="$(python3 - <<'PY'
import json,sys
data=json.loads(sys.stdin.read())
print(data.get("upload_url","").split("{",1)[0])
PY
  <<<"${release_json}")"
  if [[ -z "${upload_url}" ]]; then
    die "failed to create GitHub release: ${release_json}"
  fi

  for f in dist/*.tar.gz dist/SHA256SUMS.txt; do
    local name
    name="$(basename "$f")"
    curl -sS -X POST \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      -H "Content-Type: application/octet-stream" \
      --data-binary @"$f" \
      "${upload_url}?name=${name}" >/dev/null
  done
}

main() {
  ensure_git_repo
  ensure_github_repo
  ensure_origin_remote
  require_clean_tree

  if git rev-parse "${version}" >/dev/null 2>&1; then
    die "tag already exists: ${version}"
  fi

  prev="$(last_tag)"
  update_changelog "${version}" "${prev}"

  git commit -m "chore(release): ${version}"
  git tag "${version}"

  echo "Pushing main + tags..."
  git push origin main
  git push origin "${version}"

  echo "Building dist artifacts..."
  build_dist "${version}"

  echo "Creating GitHub release (optional)..."
  create_github_release "${version}"

  echo "Updating Homebrew tap (optional)..."
  update_homebrew_formula "${version}"

  echo "Done."
  echo "Artifacts: dist/"
}

main
