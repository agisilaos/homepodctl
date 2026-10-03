#!/usr/bin/env python3
"""Publisher contract fixtures: mocked compiler/GitHub, real disposable local Git/tar/Ruby.

No fixture invokes a real publication service, account, installer or CLI operation.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SHARED = Path(os.environ.get("CLI_SHARED_DIR", ROOT / "cli-release-process/references/shared"))
if not SHARED.is_dir():
    SHARED = ROOT / "scripts/cli-shared"
PUBLISHER = SHARED / "release.sh"
VERSION = "v1.2.3"
REAL_GIT = shutil.which("git")
REAL_RUBY = shutil.which("ruby")
REAL_GO = shutil.which("go")


def write(path, text, executable=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    if executable:
        path.chmod(0o755)


class Fixture:
    def __init__(self, test, cgo=0, license=False, names=None, branch="main"):
        scratch = tempfile.TemporaryDirectory(prefix="cli-publisher-")
        test.addCleanup(scratch.cleanup)
        self.root = Path(scratch.name)
        self.repo = self.root / "repo"
        self.shims = self.root / "shims"
        self.calls = self.root / "calls.jsonl"
        self.tap = self.root / "tap.git"
        self.origin = self.root / "origin.git"
        (self.root / "tmp").mkdir()
        self.repo.mkdir()
        self.env = dict(os.environ)
        for key in list(self.env):
            if key.startswith("HOMEBREW_") or key.startswith("RELEASE_") or key in ("GITHUB_REPO", "GIT_DIR", "GIT_WORK_TREE"):
                self.env.pop(key)
        self.env.update(
            GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
            GIT_AUTHOR_NAME="Publisher fixture", GIT_AUTHOR_EMAIL="fixture@example.invalid",
            GIT_COMMITTER_NAME="Publisher fixture", GIT_COMMITTER_EMAIL="fixture@example.invalid",
            TMPDIR=str(self.root / "tmp"), FIXTURE_ROOT=str(self.root),
            FIXTURE_CALLS=str(self.calls), FIXTURE_REAL_GIT=REAL_GIT,
            FIXTURE_REAL_RUBY=REAL_RUBY,
            GITHUB_REPO="fixture/tool", HOMEBREW_TAP_REPO="fixture/tap",
            HOMEBREW_TAP_URL=str(self.tap), HOMEBREW_TAP_BRANCH="main",
        )
        self.git("init", "--quiet", "--initial-branch=" + branch, str(self.repo))
        self.git("init", "--quiet", "--bare", "--initial-branch=main", str(self.origin))
        self.git("-C", str(self.repo), "remote", "add", "origin", str(self.origin))
        cli, formula, artifact = names or ("tool", "tool", "tool")
        self.cli, self.formula, self.artifact = cli, formula, artifact
        (self.repo / "cmd" / cli).mkdir(parents=True)
        write(self.repo / "cmd" / cli / "main.go", "package main\nfunc main() {}\n")
        write(self.repo / ".gitignore", "dist/\n")
        write(self.repo / "LICENSE", "Synthetic fixture license\n")
        write(self.repo / "CHANGELOG.md", f"# Changelog\n\n## [{VERSION}] - 2026-09-30\n\n- Fixture change ([commit](https://github.com/fixture/tool/commit/abcdef123456)).\n")
        config = {
            "CLI_NAME": cli, "FORMULA_NAME": formula, "ARTIFACT_NAME": artifact,
            "DEFAULT_BRANCH": "main", "DEFAULT_HOMEBREW_DESC": "Synthetic CLI",
            "DEFAULT_HOMEBREW_LICENSE": "MIT", "DEFAULT_HOMEBREW_TEST_ARG": "--version",
            "DEFAULT_FORMULA_PATH": f"Formula/{formula}.rb", "DEFAULT_BUILD_PKG": f"./cmd/{cli}",
            "RELEASE_LDFLAGS_TEMPLATE": "-s -w -X main.version={{VERSION}} -X main.commit={{COMMIT}} -X main.date={{DATE}}",
            "RELEASE_CGO_ENABLED": str(cgo), "RELEASE_INCLUDE_LICENSE": str(int(license)),
        }
        write(self.repo / "scripts/release-config.sh", "\n".join(f"{key}={shlex.quote(value)}" for key, value in config.items()) + "\n")
        write(self.repo / "scripts/release.sh", '#!/usr/bin/env bash\nset -euo pipefail\ncd "$(dirname "$0")/.."\nsource scripts/release-config.sh\nsource scripts/cli-shared/release.sh "$@"\n', True)
        shutil.copy2(PUBLISHER, self._shared_path())
        parser = SHARED / "changelog-section.py"
        if not parser.exists():
            parser = ROOT / "cli-release-process/references/changelog-section.py"
        shutil.copy2(parser, self.repo / "scripts/changelog-section.py")
        write(self.repo / "scripts/release-check.sh", '#!/usr/bin/env bash\nset -euo pipefail\n[[ "$1" == v1.2.3 ]]\n', True)
        self.git("-C", str(self.repo), "add", ".")
        self.git("-C", str(self.repo), "commit", "--quiet", "-m", "Fixture candidate")
        self.commit = self.git("-C", str(self.repo), "rev-parse", "HEAD").stdout.strip()
        self.git("-C", str(self.repo), "push", "--quiet", "origin", branch)
        tap_work = self.root / "tap-source"
        self.git("init", "--quiet", "--initial-branch=main", str(tap_work))
        write(tap_work / "README.md", "Main tap fixture\n")
        self.git("-C", str(tap_work), "add", ".")
        self.git("-C", str(tap_work), "commit", "--quiet", "-m", "Main tap")
        self.main_tap = self.git("-C", str(tap_work), "rev-parse", "HEAD").stdout.strip()
        self.git("-C", str(tap_work), "checkout", "--quiet", "-b", "releases")
        write(tap_work / "alternate-marker.txt", "Independent alternate branch content\n")
        self.git("-C", str(tap_work), "add", ".")
        self.git("-C", str(tap_work), "commit", "--quiet", "-m", "Alternate tap")
        self.alternate_tap = self.git("-C", str(tap_work), "rev-parse", "HEAD").stdout.strip()
        self.git("clone", "--quiet", "--bare", str(tap_work), str(self.tap))
        self.git("--git-dir", str(self.tap), "symbolic-ref", "HEAD", "refs/heads/main")
        self._tools()
        self.env["PATH"] = str(self.shims) + os.pathsep + self.env["PATH"]

    def _shared_path(self):
        path = self.repo / "scripts/cli-shared/release.sh"
        path.parent.mkdir(parents=True, exist_ok=True)
        return path

    def git(self, *args, check=True):
        return subprocess.run([REAL_GIT, *args], env=self.env, capture_output=True, text=True, check=check)

    def _tools(self):
        common = '''#!/usr/bin/env python3
import json, os, pathlib, signal, subprocess, sys
args = sys.argv[1:]
with open(os.environ['FIXTURE_CALLS'], 'a') as output:
    output.write(json.dumps({'tool': pathlib.Path(sys.argv[0]).name, 'args': args}) + '\\n')
fault = os.environ.get('FIXTURE_FAULT', '')
root = pathlib.Path(os.environ['FIXTURE_ROOT'])
'''
        write(self.shims / "go", common + '''
if args[0] == 'build':
    output = pathlib.Path(args[args.index('-o') + 1])
    arch = os.environ['GOARCH']
    metadata = {'GOOS': os.environ['GOOS'], 'GOARCH': arch,
                'CGO_ENABLED': os.environ['CGO_ENABLED'],
                '-ldflags': args[args.index('-ldflags') + 1]}
    if fault == 'wrong-arch': metadata['GOARCH'] = 'not-an-architecture'
    if fault == 'missing-stamp': metadata['-ldflags'] = '-s -w'
    tokens = metadata['-ldflags'].split()
    stamps = dict(token.split('=', 1) for token in tokens if '=' in token)
    version = stamps.get('main.version', 'missing')
    commit = stamps.get('main.commit', 'missing')
    date = stamps.get('main.date', 'missing')
    cli = output.name
    display = f'{cli} {version} ({commit}) {date}'
    if fault == 'runtime-stamp': display += ' mismatch'
    output.write_text('#!/usr/bin/env python3\\n# metadata: ' + json.dumps(metadata) + '\\nprint(' + repr(display) + ')\\n')
    output.chmod(0o755)
    if fault == 'source-changed':
        (root / 'repo/cmd/tool/main.go').write_text('package changed\\n')
    sys.exit(0)
if args[:2] == ['version', '-m']:
    line = next(line for line in pathlib.Path(args[2]).read_text().splitlines() if line.startswith('# metadata: '))
    metadata = json.loads(line[len('# metadata: '):])
    print(args[2] + ': go1.26.0')
    for key, value in metadata.items():
        print('\\tbuild\\t' + key + '=' + (json.dumps(value) if key == '-ldflags' else value))
    sys.exit(0)
if args == ['env', 'GOHOSTOS', 'GOHOSTARCH']:
    print('darwin\\narm64')
    sys.exit(0)
raise SystemExit('unsupported fixture Go command: ' + repr(args))
''', True)
        write(self.shims / "git", common + '''
command = args[2:] if args[:1] == ['-C'] else args
stage = None
if command[:1] == ['clone']:
    # Every clone must point to a fixture-local bare repository.
    if not command[-2].startswith(str(root) + '/'):
        raise SystemExit('fixture blocked a nonlocal clone')
    stage = 'clone'
elif command[:1] == ['tag']:
    stage = 'tag'
elif command[:1] == ['push']:
    stage = 'tap-push' if args[:1] == ['-C'] else 'tag-push'
if fault == stage:
    raise SystemExit('controlled failure: ' + stage)
result = subprocess.call([os.environ['FIXTURE_REAL_GIT'], *args])
if stage and fault == stage + '-accepted':
    raise SystemExit('controlled accepted-but-error failure: ' + stage)
sys.exit(result)
''', True)
        write(self.shims / "gh", common + '''
assert args[:2] == ['release', 'create'], args
assert '--repo' in args and args[args.index('--repo') + 1] == 'fixture/tool', args
if fault == 'github': raise SystemExit('controlled GitHub failure')
if fault == 'signal':
    os.kill(os.getppid(), signal.SIGTERM)
sys.exit(0)
''', True)
        write(self.shims / "ruby", common + '''
if fault == 'formula-invalid': raise SystemExit('controlled invalid Ruby')
result = subprocess.call([os.environ['FIXTURE_REAL_RUBY'], *args])
if fault == 'checksum-changed':
    (root / 'repo/dist/SHA256SUMS').write_text('0' * 64 + '  wrong.tar.gz\\n')
sys.exit(result)
''', True)

    def run(self, dry=False, env=None, fault="", version=VERSION):
        execution = dict(self.env, FIXTURE_FAULT=fault)
        execution.update(env or {})
        result = subprocess.run(["bash", "scripts/release.sh", *( ["--dry-run"] if dry else []), version], cwd=self.repo, env=execution, capture_output=True, text=True, timeout=30)
        self.output = result.stdout + result.stderr
        return result

    def events(self):
        if not self.calls.exists():
            return []
        return [json.loads(line) for line in self.calls.read_text().splitlines()]

    def publications(self):
        result = []
        for event in self.events():
            args = event["args"]
            command = args[2:] if args[:1] == ["-C"] else args
            if event["tool"] == "gh" or (event["tool"] == "git" and command[:1] in (["tag"], ["push"])):
                result.append(event)
        return result

    def retained_tap(self):
        match = re.search(r"Homebrew work directory retained for inspection: (.+)", self.output)
        return Path(match[1]) if match else None


@unittest.skipUnless(REAL_GIT and REAL_RUBY, "Git and Ruby are required")
class PublisherContract(unittest.TestCase):
    def assert_no_publication(self, fixture, result):
        self.assertNotEqual(result.returncode, 0, fixture.output)
        self.assertEqual(fixture.publications(), [], fixture.output)
        self.assertNotEqual(fixture.git("--git-dir", str(fixture.origin), "rev-parse", "--verify", "refs/tags/" + VERSION, check=False).returncode, 0)

    def test_offline_dry_run_validates_archives_notes_formula_and_stamps(self):
        fixture = Fixture(self)
        result = fixture.run(dry=True)
        self.assertEqual(result.returncode, 0, fixture.output)
        self.assertEqual(fixture.publications(), [])
        self.assertFalse(any(e["tool"] == "git" and e["args"][:1] == ["clone"] for e in fixture.events()))
        dist = fixture.repo / "dist"
        self.assertIn("Fixture change", (dist / "NOTES.md").read_text())
        sums = (dist / "SHA256SUMS").read_text().splitlines()
        self.assertEqual(len(sums), 2)
        for arch, line in zip(("amd64", "arm64"), sums):
            archive = dist / f"tool_1.2.3_darwin_{arch}.tar.gz"
            self.assertEqual(line, hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name)
            with tarfile.open(archive) as packed:
                self.assertEqual(packed.getnames(), ["tool"])
                content = packed.extractfile("tool").read().decode()
                metadata = json.loads(next(line[len('# metadata: '):] for line in content.splitlines() if line.startswith('# metadata: ')))
                self.assertEqual(metadata["GOARCH"], arch)
                self.assertEqual(metadata["CGO_ENABLED"], "0")
                self.assertIn("main.version=" + VERSION, metadata["-ldflags"])
                self.assertIn("main.commit=" + fixture.commit[:12], metadata["-ldflags"])
        self.assertIn("sha256 '", (dist / "homebrew/Formula/tool.rb").read_text())

    def test_native_license_and_hyphenated_formula_settings(self):
        fixture = Fixture(self, cgo=1, license=True, names=("todoist", "todoist-cli", "todoist-cli"))
        result = fixture.run(dry=True)
        self.assertEqual(result.returncode, 0, fixture.output)
        with tarfile.open(fixture.repo / "dist/todoist-cli_1.2.3_darwin_arm64.tar.gz") as packed:
            self.assertEqual(set(packed.getnames()), {"todoist", "LICENSE"})
            content = packed.extractfile("todoist").read().decode()
            metadata = json.loads(next(line[len('# metadata: '):] for line in content.splitlines() if line.startswith('# metadata: ')))
            self.assertEqual(metadata["CGO_ENABLED"], "1")
        self.assertIn("class TodoistCli < Formula", (fixture.repo / "dist/homebrew/Formula/todoist-cli.rb").read_text())

    def test_quoted_metadata_and_interpolation_remain_literal(self):
        fixture = Fixture(self)
        sentinel = fixture.root / "interpolation-executed"
        desc = "A 'quoted' \\ description \"double\" #{File.write(" + json.dumps(str(sentinel)) + ", 'bad')}"
        arguments = ["--version", "quote'\\\"", "; touch " + str(sentinel), "#{raise 'executed'}"]
        result = fixture.run(dry=True, env={"HOMEBREW_DESC": desc, "HOMEBREW_LICENSE": "MIT\\'#{raise 'executed'}", "HOMEBREW_TEST_ARG": shlex.join(arguments)})
        self.assertEqual(result.returncode, 0, fixture.output)
        formula = fixture.repo / "dist/homebrew/Formula/tool.rb"
        loader = '''require 'json'
$values = {}
class Hardware
  module CPU
    def self.arm?; true; end
  end
end
class Formula
  def self.desc(value); $values['desc'] = value; end
  def self.license(value); $values['license'] = value; end
  def self.homepage(value); end
  def self.version(value); end
  def self.url(value); end
  def self.sha256(value); end
  def self.on_macos(&block); class_eval(&block); end
  def self.test(&block); new.instance_eval(&block); end
  def bin; '/synthetic bin'; end
  def version; '1.2.3'; end
  def shell_output(command); $values['command'] = command; '1.2.3'; end
  def assert_match(expected, actual); raise 'version assertion failed' unless actual.include?(expected); end
end
load ARGV[0]
puts JSON.generate($values)
'''
        checked = subprocess.run([REAL_RUBY, "-e", loader, str(formula)], capture_output=True, text=True, check=True)
        values = json.loads(checked.stdout)
        self.assertEqual(values["desc"], desc)
        self.assertEqual(shlex.split(values["command"]), ["/synthetic bin/tool", *arguments])
        self.assertFalse(sentinel.exists())

    def test_selected_alternate_branch_is_prepared_before_publication(self):
        fixture = Fixture(self)
        result = fixture.run(env={"HOMEBREW_TAP_BRANCH": "releases"})
        self.assertEqual(result.returncode, 0, fixture.output)
        self.assertEqual(fixture.git("--git-dir", str(fixture.tap), "rev-parse", "main").stdout.strip(), fixture.main_tap)
        self.assertEqual(fixture.git("--git-dir", str(fixture.tap), "show", "releases:alternate-marker.txt").stdout, "Independent alternate branch content\n")
        self.assertEqual(fixture.git("--git-dir", str(fixture.tap), "rev-parse", "releases^").stdout.strip(), fixture.alternate_tap)
        self.assertIn("class Tool < Formula", fixture.git("--git-dir", str(fixture.tap), "show", "releases:Formula/tool.rb").stdout)
        events = fixture.events()
        clone = next(i for i, e in enumerate(events) if e["tool"] == "git" and e["args"][:1] == ["clone"])
        tag = next(i for i, e in enumerate(events) if e["tool"] == "git" and e["args"][:1] == ["tag"])
        self.assertLess(clone, tag)
        self.assertIn("releases", events[clone]["args"])
        github = next(e for e in events if e["tool"] == "gh")
        self.assertEqual(github["args"][github["args"].index("--notes-file") + 1], "dist/NOTES.md")
        self.assertEqual(fixture.git("--git-dir", str(fixture.origin), "rev-parse", "refs/tags/" + VERSION).stdout.strip(), fixture.commit)

    def test_clone_failure_prevents_any_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(fault="clone"))
        self.assertIn("stopped during: clone Homebrew tap", fixture.output)

    def test_missing_tap_branch_prevents_any_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(env={"HOMEBREW_TAP_BRANCH": "absent"}))

    def test_tag_named_tap_target_is_rejected_as_detached(self):
        fixture = Fixture(self)
        fixture.git("--git-dir", str(fixture.tap), "tag", "tag-only", "main")
        self.assert_no_publication(fixture, fixture.run(env={"HOMEBREW_TAP_BRANCH": "tag-only"}))

    def test_tap_symlink_prevents_any_publication(self):
        fixture = Fixture(self)
        working = fixture.root / "symlink-tap"
        fixture.git("clone", "--quiet", str(fixture.tap), str(working))
        (working / "Formula").symlink_to(fixture.root / "outside")
        fixture.git("-C", str(working), "add", ".")
        fixture.git("-C", str(working), "commit", "--quiet", "-m", "Symlink fixture")
        fixture.git("-C", str(working), "push", "--quiet", "origin", "main")
        self.assert_no_publication(fixture, fixture.run())
        self.assertIn("symlink", fixture.output)
        self.assertFalse((fixture.root / "outside").exists())

    def test_formula_syntax_failure_prevents_any_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(fault="formula-invalid"))
        self.assertIn("formula is not valid Ruby", fixture.output)

    def test_checksum_mismatch_prevents_any_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(fault="checksum-changed"))
        self.assertIn("checksum", fixture.output)

    def test_wrong_architecture_and_missing_binary_stamp_prevent_publication(self):
        for fault in ("wrong-arch", "missing-stamp"):
            with self.subTest(fault=fault):
                fixture = Fixture(self)
                self.assert_no_publication(fixture, fixture.run(fault=fault))
                self.assertIn("artifact metadata mismatch", fixture.output)

    def test_native_runtime_stamp_mismatch_prevents_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(fault="runtime-stamp"))
        self.assertIn("built artifact version output mismatch", fixture.output)

    def test_same_named_branch_does_not_make_tag_push_ambiguous(self):
        fixture = Fixture(self)
        fixture.git("-C", str(fixture.repo), "branch", VERSION)
        result = fixture.run()
        self.assertEqual(result.returncode, 0, fixture.output)
        self.assertEqual(fixture.git("--git-dir", str(fixture.origin), "rev-parse", "refs/tags/" + VERSION).stdout.strip(), fixture.commit)

    @unittest.skipUnless(REAL_GO, "Go is required for actual build metadata integration")
    def test_actual_trimpath_go_archives_and_native_version_output(self):
        fixture = Fixture(self)
        write(fixture.repo / "go.mod", "module fixture/tool\n\ngo 1.22\n")
        write(fixture.repo / "cmd/tool/main.go", '''package main
import "fmt"
var version = "dev"
var commit = "none"
var date = "unknown"
func main() { fmt.Printf("tool %s (%s) %s\\n", version, commit, date) }
''')
        fixture.git("-C", str(fixture.repo), "add", ".")
        fixture.git("-C", str(fixture.repo), "commit", "--quiet", "-m", "Actual Go integration")
        (fixture.shims / "go").unlink()
        result = fixture.run(dry=True)
        self.assertEqual(result.returncode, 0, fixture.output)
        self.assertEqual(fixture.publications(), [])
        for arch in ("amd64", "arm64"):
            archive = fixture.repo / f"dist/tool_1.2.3_darwin_{arch}.tar.gz"
            with tarfile.open(archive) as packed:
                self.assertEqual(packed.getnames(), ["tool"])
                binary = fixture.root / ("actual-tool-" + arch)
                binary.write_bytes(packed.extractfile("tool").read())
                binary.chmod(0o755)
            inspected = subprocess.run([REAL_GO, "version", "-m", str(binary)], capture_output=True, text=True, check=True).stdout
            self.assertIn("GOOS=darwin", inspected)
            self.assertIn("GOARCH=" + arch, inspected)
            self.assertIn("CGO_ENABLED=0", inspected)
            # The publisher's native execution is exercised on Darwin; foreign
            # architecture files are only inspected, with no Rosetta dependency.

    def test_source_changed_during_build_prevents_publication(self):
        fixture = Fixture(self)
        self.assert_no_publication(fixture, fixture.run(fault="source-changed"))
        self.assertIn("working tree changed during preparation", fixture.output)

    def test_wrong_release_branch_is_error_but_dry_run_warns(self):
        fixture = Fixture(self, branch="feature")
        self.assert_no_publication(fixture, fixture.run())
        self.assertIn("release must run from main", fixture.output)
        self.assertEqual(fixture.run(dry=True).returncode, 0, fixture.output)
        self.assertIn("warning: current branch is feature", fixture.output)

    def test_untracked_file_and_existing_tag_are_rejected(self):
        fixture = Fixture(self)
        write(fixture.repo / "untracked", "fixture\n")
        self.assert_no_publication(fixture, fixture.run())
        self.assertIn("working tree is not clean", fixture.output)
        (fixture.repo / "untracked").unlink()
        fixture.git("-C", str(fixture.repo), "tag", VERSION)
        self.assert_no_publication(fixture, fixture.run())
        self.assertIn("already exists", fixture.output)

    def test_unsafe_settings_fail_before_publication(self):
        for env in (
            {"HOMEBREW_FORMULA_PATH": "../outside.rb"},
            {"HOMEBREW_FORMULA_PATH": "/absolute.rb"},
            {"GITHUB_REPO": "fixture/tool;unsafe"},
            {"RELEASE_BUILD_PKG": "../other"},
            {"HOMEBREW_TEST_ARG": "'unterminated"},
            {"RELEASE_LDFLAGS": "-s -w"},
            {"RELEASE_LDFLAGS": "-X main.version={{VERSION}} -X main.version=wrong -X main.commit={{COMMIT}} -X main.date={{DATE}}"},
            {"HOMEBREW_TAP_BRANCH": "-option"},
        ):
            with self.subTest(env=env):
                fixture = Fixture(self)
                self.assert_no_publication(fixture, fixture.run(env=env))

    def test_output_symlink_is_preserved_and_rejected(self):
        fixture = Fixture(self)
        outside = fixture.root / "outside"
        outside.mkdir()
        (fixture.repo / "dist").symlink_to(outside)
        self.assert_no_publication(fixture, fixture.run(dry=True))
        self.assertEqual(list(outside.iterdir()), [])
        self.assertTrue((fixture.repo / "dist").is_symlink())

    def test_partial_publication_reports_uncertainty_and_retains_evidence(self):
        for fault, phase in (("tag", "create local tag"), ("tag-push", "push version tag"), ("tag-push-accepted", "push version tag"), ("github", "publish GitHub release/assets"), ("tap-push", "push Homebrew formula"), ("tap-push-accepted", "push Homebrew formula")):
            with self.subTest(fault=fault):
                fixture = Fixture(self)
                result = fixture.run(fault=fault)
                self.assertNotEqual(result.returncode, 0, fixture.output)
                self.assertIn("stopped during: " + phase, fixture.output)
                self.assertIn("outcome unknown", fixture.output)
                self.assertIn("Expected release commit: " + fixture.commit, fixture.output)
                self.assertIn("Do not rerun release.sh", fixture.output)
                self.assertIn("Manual recovery: docs/release-recovery.md", fixture.output)
                for name in ("NOTES.md", "SHA256SUMS", "tool_1.2.3_darwin_amd64.tar.gz", "tool_1.2.3_darwin_arm64.tar.gz", "homebrew/Formula/tool.rb"):
                    self.assertTrue((fixture.repo / "dist" / name).is_file(), name)
                retained = fixture.retained_tap()
                self.assertIsNotNone(retained)
                self.assertTrue((retained / "Formula/tool.rb").is_file())
                if fault in ("tag-push-accepted", "github", "tap-push", "tap-push-accepted"):
                    self.assertEqual(fixture.git("--git-dir", str(fixture.origin), "rev-parse", "refs/tags/" + VERSION).stdout.strip(), fixture.commit)

    def test_signal_reports_unknown_github_outcome_and_retains_originals(self):
        fixture = Fixture(self)
        result = fixture.run(fault="signal")
        self.assertEqual(result.returncode, 143, fixture.output)
        self.assertIn("GitHub release/assets: outcome unknown", fixture.output)
        self.assertIn("Homebrew push: not attempted", fixture.output)
        self.assertTrue(fixture.retained_tap().is_dir())
        self.assertTrue((fixture.repo / "dist/NOTES.md").is_file())


if __name__ == "__main__":
    unittest.main()
