#!/usr/bin/env zsh

set -e
set -u
setopt pipefail

cd "$(dirname "$0")/.."

die() { print -u2 -- "error: $*"; exit 1; }

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release.sh must be run on macOS (Darwin)"
fi

command -v go >/dev/null 2>&1 || die "go is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"
command -v git >/dev/null 2>&1 || die "git is required"

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

# Homebrew tap repo name convention: homebrew-<tap> => brew tap <owner>/<tap>
tap_repo="${HOMEBREW_TAP_REPO:-${repo_owner}/homebrew-tap}"
tap_remote_default="git@github.com:${tap_repo}.git"
tap_remote="${HOMEBREW_TAP_ORIGIN_URL:-$tap_remote_default}"

ensure_git_repo() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi
  print -- "Initializing git repo..."
  git init -b main
  git add .
  git commit -m "chore: initial import"
}

ensure_origin_remote() {
  if git remote get-url origin >/dev/null 2>&1; then
    return 0
  fi
  print -- "Adding origin remote: ${origin_url}"
  git remote add origin "${origin_url}"
}

ensure_github_repo() {
  if ! command -v gh >/dev/null 2>&1; then
    print -- "gh not found; skipping GitHub repo creation"
    return 0
  fi
  if gh repo view "${repo_owner}/${repo_name}" >/dev/null 2>&1; then
    return 0
  fi
  print -- "Creating GitHub repo ${repo_owner}/${repo_name} (public)..."
  gh repo create "${repo_owner}/${repo_name}" --public --confirm >/dev/null
}

require_clean_tree() {
  git diff --quiet || die "working tree has unstaged changes"
  git diff --cached --quiet || die "index has staged changes"
}

last_tag() {
  git describe --tags --abbrev=0 2>/dev/null || true
}

extract_unreleased_notes() {
  python3 - <<'PY'
txt=open("CHANGELOG.md","r",encoding="utf-8").read().splitlines()
out=[]
in_un=False
for line in txt:
    if line.strip() == "## [Unreleased]":
        in_un=True
        continue
    if in_un and line.startswith("## ["):
        break
    if in_un:
        out.append(line)
print("\n".join(out).strip())
PY
}

generate_fallback_notes() {
  local prev_tag="$1"
  if [[ -n "${prev_tag}" ]]; then
    git log --no-merges --pretty=format:'- %s (%h)' "${prev_tag}..HEAD"
  else
    git log --no-merges --pretty=format:'- %s (%h)'
  fi
}

update_changelog() {
  local ver="$1"
  local prev_tag="$2"
  local date_utc
  date_utc="$(date -u +%Y-%m-%d)"

  [[ -f CHANGELOG.md ]] || die "CHANGELOG.md not found"

  local notes
  notes="$(extract_unreleased_notes)"
  if [[ -z "${notes}" ]]; then
    notes="$(generate_fallback_notes "${prev_tag}")"
  fi
  if [[ -z "${notes}" ]]; then
    notes="- No changes recorded."
  fi

  python3 - "$ver" "$date_utc" "$notes" <<'PY'
import sys

version=sys.argv[1]
date=sys.argv[2]
notes=sys.argv[3]

path="CHANGELOG.md"
lines=open(path,"r",encoding="utf-8").read().splitlines()

target_header=f"## [{version}]"
if any(line.startswith(target_header) for line in lines):
    raise SystemExit(f"error: {version} already exists in CHANGELOG.md")

out=[]
in_unreleased=False
inserted=False

for line in lines:
    if line.strip() == "## [Unreleased]":
        out.append(line)
        out.append("")
        out.append(f"## [{version}] - {date}")
        out.append("")
        out.extend(notes.splitlines() if notes.strip() else ["- No changes recorded."])
        out.append("")
        inserted=True
        in_unreleased=True
        continue

    if in_unreleased:
        if line.startswith("## ["):
            in_unreleased=False
            out.append(line)
        else:
            continue
    else:
        out.append(line)

if not inserted:
    raise SystemExit("error: CHANGELOG.md missing '## [Unreleased]' header")

open(path,"w",encoding="utf-8").write("\n".join(out).rstrip() + "\n")
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

create_github_release() {
  local ver="$1"
  if ! command -v gh >/dev/null 2>&1; then
    print -- "gh not found; skipping GitHub release creation"
    return 0
  fi

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
}

ensure_homebrew_tap_repo() {
  if ! command -v gh >/dev/null 2>&1; then
    print -- "gh not found; skipping Homebrew tap automation"
    return 1
  fi
  if gh repo view "${tap_repo}" >/dev/null 2>&1; then
    return 0
  fi
  print -- "Creating Homebrew tap repo ${tap_repo} (public)..."
  gh repo create "${tap_repo}" --public --confirm >/dev/null
  return 0
}

update_homebrew_formula() {
  local ver="$1"
  local ver_nov="${ver#v}"
  ensure_homebrew_tap_repo || return 0

  local sha_arm sha_amd
  sha_arm="$(awk -v f="homepodctl_${ver}_darwin_arm64.tar.gz" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  sha_amd="$(awk -v f="homepodctl_${ver}_darwin_amd64.tar.gz" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  [[ -n "${sha_arm}" && -n "${sha_amd}" ]] || die "failed to parse SHA256SUMS.txt for ${ver}"

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

  # Ensure tap README contains install instructions for available formulae (without removing other tools).
  python3 - <<'PY' "${tmp}/tap"
import pathlib, re, sys

tap_dir = pathlib.Path(sys.argv[1])
readme = tap_dir / "README.md"
formula_dir = tap_dir / "Formula"

formulae = sorted(p.stem for p in formula_dir.glob("*.rb") if p.is_file())
if "homepodctl" not in formulae:
    formulae.append("homepodctl")
    formulae.sort()

def read_text(p: pathlib.Path) -> str:
    return p.read_text(encoding="utf-8") if p.exists() else ""

def write_text(p: pathlib.Path, s: str) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(s.rstrip() + "\n", encoding="utf-8")

text = read_text(readme)
if not text.strip():
    lines = [
        "# agisilaos/homebrew-tap",
        "",
        "Homebrew formulae for agisilaos.",
        "",
        "## Install",
        "",
        "```bash",
        "brew tap agisilaos/tap",
    ]
    for f in formulae:
        lines.append(f"brew install {f}")
    lines.append("```")
    write_text(readme, "\n".join(lines))
    sys.exit(0)

joined = text

# Find a bash code block under an Install section (best effort).
install_block = re.search(
    r"(##\\s+Install\\b[\\s\\S]*?```bash\\n)([\\s\\S]*?)(\\n```)",
    joined,
    flags=re.M,
)

def normalize_cmds(body: str) -> list[str]:
    cmds = [line.strip() for line in body.splitlines() if line.strip()]
    # Keep order but remove duplicates.
    seen = set()
    out = []
    for c in cmds:
        if c not in seen:
            seen.add(c)
            out.append(c)
    return out

def ensure_lines(cmds: list[str], want: list[str]) -> list[str]:
    # Ensure tap line first, then keep existing, then add missing wanted.
    tap = "brew tap agisilaos/tap"
    installs = [c for c in cmds if c != tap]
    out = [tap]
    out.extend(installs)
    for w in want:
        if w not in out:
            out.append(w)
    return out

want_installs = [f"brew install {f}" for f in formulae]

if install_block:
    head, body, tail = install_block.group(1), install_block.group(2), install_block.group(3)
    cmds = normalize_cmds(body)
    cmds = ensure_lines(cmds, want_installs)
    new_body = "\n".join(cmds)
    updated = joined[: install_block.start(2)] + new_body + joined[install_block.end(2) :]
    write_text(readme, updated)
    sys.exit(0)

# If there is no Install section with a bash block, append one (do not remove existing docs).
append_lines = [
    "",
    "## Install",
    "",
    "```bash",
    "brew tap agisilaos/tap",
]
append_lines.extend(want_installs)
append_lines.append("```")
write_text(readme, joined + "\n" + "\n".join(append_lines))
PY

  pushd "${tmp}/tap" >/dev/null
  if [[ "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo)" != "main" ]]; then
    git checkout -B main >/dev/null 2>&1 || git checkout -b main
  fi
  git add Formula/homepodctl.rb README.md
  if git diff --cached --quiet; then
    print -- "Homebrew formula already up to date"
    popd >/dev/null
    return 0
  fi
  git commit -m "homepodctl ${ver}"
  git push origin HEAD:main
  popd >/dev/null

  print -- "Updated Homebrew formula in ${tap_repo}"
}

main() {
  ensure_git_repo
  ensure_github_repo
  ensure_origin_remote
  require_clean_tree

  if git rev-parse "${version}" >/dev/null 2>&1; then
    die "tag already exists: ${version}"
  fi

  local prev
  prev="$(last_tag)"

  update_changelog "${version}" "${prev}"
  git commit -m "chore(release): ${version}"

  git tag "${version}"

  print -- "Pushing main + tags..."
  git push origin main
  git push origin "${version}"

  print -- "Building dist artifacts..."
  build_dist "${version}"

  print -- "Creating GitHub release..."
  create_github_release "${version}"

  print -- "Updating Homebrew tap..."
  update_homebrew_formula "${version}"

  print -- "Done. Artifacts: dist/"
}

main
