#!/usr/bin/env python3
"""Exercise release diagnostics with local Git remotes and controlled tools."""

import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent / "release.sh"
GIT = shutil.which("git")
MKTEMP = shutil.which("mktemp")
VERSION = "v999.999.999"

# No fixture command may contact GitHub. Unexpected tool calls fail the test.
SHIM = r'''#!/usr/bin/env python3
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys

args = sys.argv[1:]
tool = Path(sys.argv[0]).name
case = os.environ["RELEASE_TEST_CASE"]
root = Path(os.environ["RELEASE_TEST_ROOT"])
event = None
if tool == "mktemp":
    assert args == ["-d"], args
    sys.exit(subprocess.call([os.environ["RELEASE_TEST_MKTEMP"], "-d", str(root / "tmp/XXXXXX")]))
if tool == "go":
    assert args[0] == "build", args
    if case == "build":
        sys.exit(41)
    Path(args[args.index("-o") + 1]).write_text("original " + os.environ["GOARCH"])
    sys.exit(0)
if tool == "gh":
    assert args[:2] == ["release", "create"], args
    event = "github"
elif tool == "git":
    command = args[2:] if args[:1] == ["-C"] else args
    if command[:1] == ["clone"]:
        assert args[1] == "git@github.com:fixture/tap.git", args
        args[1] = str(root / "tap.git")
        event = "clone"
    elif command[:1] == ["push"]:
        event = "tap" if args[:1] == ["-C"] else "tag"
    elif command[:1] == ["tag"]:
        event = "local_tag"
    elif command[:1] == ["commit"]:
        event = "commit"
    else:
        assert command[0] in {"rev-parse", "symbolic-ref", "status", "describe", "log", "add", "diff"}, args
else:
    raise AssertionError(tool)

if event:
    with (root / "events").open("a") as log:
        log.write(event + "\n")
    if case == event + "_before":
        print("controlled failure before " + event, file=sys.stderr)
        sys.exit(42)
    if case == "tag_signal" and event == "tag":
        os.kill(os.getppid(), signal.SIGTERM)
        sys.exit(143)

if tool == "gh":
    published = root / "published"
    published.mkdir()
    assets = args[3:args.index("--title")]
    for asset in assets:
        shutil.copy2(asset, published)
        if case == "github_partial":
            sys.exit(42)
    status = 0
else:
    status = subprocess.call([os.environ["RELEASE_TEST_GIT"], *args])
if status == 0 and event and case == event + "_after":
    print("controlled failure after " + event, file=sys.stderr)
    sys.exit(42)
sys.exit(status)
'''


class ReleasePublicationTest(unittest.TestCase):
    def run_case(self, case, *, dry_run=False):
        with tempfile.TemporaryDirectory(prefix="release-publication-") as directory:
            root = Path(directory)
            repo = root / "repo"
            repo.mkdir()
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith(("GIT_", "HOMEBREW_", "RELEASE_"))}
            env.update({
                "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                "GIT_AUTHOR_NAME": "Release test", "GIT_COMMITTER_NAME": "Release test",
                "GIT_AUTHOR_EMAIL": "test@example.invalid", "GIT_COMMITTER_EMAIL": "test@example.invalid",
                "GITHUB_REPO": "fixture/source", "HOMEBREW_TAP_REPO": "fixture/tap",
                "RELEASE_TEST_CASE": case, "RELEASE_TEST_ROOT": str(root),
                "RELEASE_TEST_GIT": GIT,
                "RELEASE_TEST_MKTEMP": MKTEMP,
            })

            def git(*args, check=True):
                return subprocess.run([GIT, *args], cwd=repo, env=env, check=check,
                                      capture_output=True, text=True)

            git("-c", "init.defaultBranch=main", "init", "--quiet")
            (repo / "scripts").mkdir()
            shutil.copy2(SOURCE, repo / "scripts/release.sh")
            preflight = repo / "scripts/release-check.sh"
            preflight.write_text('#!/usr/bin/env bash\n'
                                 'if [[ "$RELEASE_TEST_CASE" == preflight ]]; then exit 41; fi\n')
            preflight.chmod(0o755)
            (repo / ".gitignore").write_text("dist/\n")
            git("add", ".")
            git("commit", "--quiet", "-m", "fixture source")
            commit = git("rev-parse", "HEAD").stdout.strip()
            git("clone", "--quiet", "--bare", str(repo), str(root / "origin.git"))
            git("remote", "add", "origin", str(root / "origin.git"))
            git("clone", "--quiet", "--bare", str(repo), str(root / "tap.git"))
            tap_before = git("--git-dir", str(root / "tap.git"), "rev-parse", "main").stdout

            shims = root / "shims"
            shims.mkdir()
            for tool in ("go", "git", "gh", "mktemp"):
                path = shims / tool
                path.write_text(SHIM)
                path.chmod(0o755)
            scratch = root / "tmp"
            scratch.mkdir()
            env["PATH"] = str(shims) + os.pathsep + env["PATH"]
            env["TMPDIR"] = str(scratch)
            command = ["bash", "scripts/release.sh", VERSION]
            if dry_run:
                command.append("--dry-run")
            result = subprocess.run(command, cwd=repo, env=env, capture_output=True, text=True)
            output = result.stdout + result.stderr
            events_path = root / "events"
            events = events_path.read_text().splitlines() if events_path.exists() else []

            # A failing phase must stop later publication, without automatic retries.
            sequence = ["local_tag", "tag", "github", "clone", "commit", "tap"]
            if dry_run or case in {"preflight", "build"}:
                expected_events = []
            elif case == "success":
                expected_events = sequence
            else:
                phase = "local_tag" if case.startswith("local_tag") else case.split("_")[0]
                expected_events = sequence[:sequence.index(phase) + 1]
            self.assertEqual(events, expected_events, output)

            expected_code = 0 if dry_run or case == "success" else (
                41 if case in {"preflight", "build"} else 143 if case == "tag_signal" else 42)
            self.assertEqual(result.returncode, expected_code, output)
            if expected_code:
                phases = {
                    "preflight": "preflight", "build": "prepare artifacts",
                    "local_tag": "create local tag", "tag": "push version tag",
                    "github": "publish GitHub release/assets", "clone": "clone Homebrew tap",
                    "commit": "commit Homebrew formula", "tap": "push Homebrew formula",
                }
                phase = "local_tag" if case.startswith("local_tag") else case.split("_")[0]
                self.assertIn("stopped during: " + phases[phase], output)
                self.assertIn("Manual recovery: docs/release-recovery.md", output)
                if case in {"preflight", "build"}:
                    self.assertIn("No publication commands were attempted in this run.", output)
                else:
                    self.assertIn("Expected release commit: " + commit, output)
                    self.assertIn("Do not rerun release.sh", output)
                    self.assertIn("an error does not prove it failed remotely", output)
                    self.assertIn("Original archives and SHA256SUMS retained in:", output)
                    labels = {"local_tag": "Local tag", "tag": "Tag push",
                              "github": "GitHub release/assets", "tap": "Homebrew push"}
                    for event, label in labels.items():
                        if event not in events:
                            state = "not attempted"
                        elif event == events[-1]:
                            state = "outcome unknown"
                        else:
                            state = "completed (command reported success)"
                        self.assertIn(label + ": " + state, output)
            else:
                self.assertNotIn("stopped during", output)
                if not dry_run:
                    self.assertIn("release completed for " + VERSION, output)

            # Prove before/after failures have different remote outcomes despite
            # intentionally identical 'unknown' diagnostics.
            tag = git("--git-dir", str(root / "origin.git"), "rev-parse", "--verify",
                      "refs/tags/" + VERSION, check=False)
            remote_tag_expected = "github" in events or case == "tag_after"
            self.assertEqual(tag.returncode == 0, remote_tag_expected, output)
            if remote_tag_expected:
                self.assertEqual(tag.stdout.strip(), commit)
            assets = list((root / "published").glob("*"))
            expected_assets = 1 if case == "github_partial" else (
                3 if "clone" in events or case == "github_after" else 0)
            self.assertEqual(len(assets), expected_assets, output)
            for asset in assets:
                self.assertEqual(asset.read_bytes(), (repo / "dist" / asset.name).read_bytes())
            sums = repo / "dist/SHA256SUMS"
            if sums.exists():
                for line in sums.read_text().splitlines():
                    digest, name = line.split()
                    self.assertEqual(hashlib.sha256((repo / "dist" / name).read_bytes()).hexdigest(), digest)
            tap_after = git("--git-dir", str(root / "tap.git"), "rev-parse", "main").stdout
            self.assertEqual(tap_after != tap_before, case in {"success", "tap_after"} and not dry_run)
            retained = list(scratch.iterdir())
            if case in {"commit_before", "tap_before", "tap_after"} and not dry_run:
                self.assertEqual(len(retained), 1, output)
                self.assertTrue((retained[0] / "Formula/homepodctl.rb").is_file())
                self.assertIn("Homebrew work directory retained for inspection:", output)
            else:
                self.assertEqual(retained, [], output)

    def test_publication_outcomes(self):
        for case in ("success", "preflight", "build", "local_tag_before",
                     "tag_before", "tag_after", "tag_signal", "github_before",
                     "github_partial", "github_after", "clone_before", "commit_before",
                     "tap_before", "tap_after"):
            with self.subTest(case=case):
                self.run_case(case)

    def test_dry_run_never_publishes(self):
        self.run_case("success", dry_run=True)


if __name__ == "__main__":
    unittest.main()
