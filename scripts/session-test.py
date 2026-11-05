#!/usr/bin/env python3
"""Exercise task sessions using a bare local remote and a controlled reviewer."""

import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import unittest


SESSION = Path(__file__).resolve().with_name("session")
REVIEWER = """#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
import sys

args = sys.argv[1:]
assert args[:4] == ["exec", "--ephemeral", "--sandbox", "read-only"], args
base = subprocess.check_output(["git", "rev-parse", "origin/main"], text=True).strip()
head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
assert f"git diff {base} {head}" in args[-1], args[-1]
reviewer_repo = Path(os.environ["SESSION_TEST_REVIEWER_REPO"])
subprocess.run(["git", "init", "-q", "--template=", "--bare", str(reviewer_repo)], check=True)
assert (reviewer_repo / "HEAD").is_file(), "reviewer Git escaped its repository"
output = Path(args[args.index("--output-last-message") + 1])
mode = os.environ["SESSION_TEST_REVIEW"]
if mode == "change-push-url":
    subprocess.run([
        "git", "-C", os.environ["SESSION_TEST_TASK"],
        "config", "remote.origin.pushurl", os.environ["SESSION_TEST_MIRROR"],
    ], check=True)
if mode == "rewrite-push-url":
    subprocess.run([
        "git", "-C", os.environ["SESSION_TEST_TASK"], "config",
        f'url.{os.environ["SESSION_TEST_MIRROR"]}.insteadOf', os.environ["SESSION_TEST_REMOTE"],
    ], check=True)
if mode == "advance":
    subprocess.run([
        "git", "--git-dir", os.environ["SESSION_TEST_REMOTE"],
        "update-ref", "refs/heads/main", os.environ["SESSION_TEST_REMOTE_HEAD"],
    ], check=True)
if mode == "fail":
    sys.exit(42)
if mode == "invalid":
    output.write_text("not a verdict")
elif mode == "findings":
    output.write_text(json.dumps({"status": "findings", "report": "regression"}))
elif mode == "incomplete":
    output.write_text(json.dumps({"status": "incomplete", "report": "timeout"}))
else:
    output.write_text(json.dumps({"status": "clean", "report": ""}))
"""


class SessionTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory(prefix="homepodctl-session-")
        self.addCleanup(tmp.cleanup)
        self.fixture = Path(tmp.name).resolve()
        self.source = self.fixture / "source"
        self.task = self.fixture / "source-task"
        self.remote = self.fixture / "remote.git"
        reviewer = self.fixture / "codex"
        reviewer.write_text(REVIEWER)
        reviewer.chmod(0o755)
        self.env = os.environ.copy()
        local_git_vars = subprocess.check_output(
            ["git", "rev-parse", "--local-env-vars"], text=True
        ).splitlines()
        for key in local_git_vars:
            self.env.pop(key, None)
        self.env.update({
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_ALLOW_PROTOCOL": "file",
            "GIT_AUTHOR_NAME": "Fixture",
            "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
            "GIT_COMMITTER_NAME": "Fixture",
            "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
            "HOMEPODCTL_CODEX": str(reviewer),
            "HOMEPODCTL_SESSION_LOG_DIR": str(self.fixture / "logs"),
            "SESSION_TEST_REVIEW": "clean",
            "SESSION_TEST_REVIEWER_REPO": str(self.fixture / "reviewer.git"),
            "SESSION_TEST_VERIFY": "0",
        })
        self.run_ok(self.fixture, "git", "init", "-q", "--template=", "--bare", "-b", "main", str(self.remote))
        self.run_ok(self.fixture, "git", "init", "-q", "--template=", "-b", "main", str(self.source))
        (self.source / "scripts").mkdir()
        shutil.copy2(SESSION, self.source / "scripts")
        (self.source / "base.txt").write_text("original\n")
        (self.source / ".gitignore").write_text("/homepodctl\n/dist/\n/GNUmakefile\n")
        (self.source / "Makefile").write_text(
            'verify:\n\t@test -z "$$GIT_DIR$$GIT_COMMON_DIR$$HOMEPODCTL_LANDING"\n'
            '\t@if [ "$$SESSION_TEST_GO_ENV" = 1 ]; then test -z "$$GOFLAGS" && '
            'test "$$GOENV" = off && test "$$GOWORK" = off; fi\n'
            '\t@echo verify-ran\n\t@exit "$$SESSION_TEST_VERIFY"\n'
        )
        self.git(self.source, "add", ".")
        self.git(self.source, "commit", "-qm", "Fixture base")
        self.git(self.source, "remote", "add", "origin", str(self.remote))
        self.git(self.source, "push", "-q", "origin", "main")

    def execute(self, cwd, *args):
        return subprocess.run(args, cwd=cwd, env=self.env, capture_output=True, text=True, timeout=30)

    def run_ok(self, cwd, *args):
        result = self.execute(cwd, *args)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip()

    def git(self, cwd, *args):
        return self.run_ok(cwd, "git", *args)

    def run_session(self, cwd, *args):
        return self.run_ok(cwd, "./scripts/session", *args)

    def ref(self, cwd):
        return self.git(cwd, "rev-parse", "HEAD")

    def start_task(self):
        self.assertEqual(self.run_session(self.source, "start", "task"), str(self.task))
        self.assertEqual(self.ref(self.task), self.ref(self.remote))

    def commit_file(self, cwd, name, content):
        (cwd / name).write_text(content)
        self.git(cwd, "add", name)
        self.git(cwd, "commit", "-qm", f"Update {name}")

    def assert_stopped(self, message, *args):
        before = self.ref(self.remote)
        result = self.execute(self.task, *args)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(message, result.stderr)
        self.assertEqual(self.ref(self.remote), before)

    def install_git_shim(self, before_exec, **variables):
        tools = self.fixture / "tools"
        tools.mkdir()
        shim = tools / "git"
        shim.write_text("""#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
import sys

args = sys.argv[1:]
git = os.environ["SESSION_TEST_REAL_GIT"]
""" + before_exec + '\nos.execv(git, [git, *args])\n')
        shim.chmod(0o755)
        self.env.update({
            "SESSION_TEST_REAL_GIT": shutil.which("git"),
            "PATH": f"{tools}{os.pathsep}{self.env['PATH']}",
            **variables,
        })

    def test_start_isolates_hook_setup_from_caller_environment(self):
        start_path = self.run_ok(
            self.source, "env", f"GIT_DIR={self.source / '.git'}",
            f"GIT_WORK_TREE={self.source}", "./scripts/session", "start", "task",
        )
        self.assertEqual(start_path, str(self.task))
        self.assertNotIn("core.hookspath", self.git(self.source, "config", "--list"))
        hook_path = Path(self.git(self.task, "config", "--worktree", "--get", "core.hooksPath"))
        self.assertTrue((hook_path / "pre-push").is_file())

    def test_start_from_task_does_not_copy_its_gate(self):
        self.start_task()
        self.install_git_shim("""
if args[:2] == ["worktree", "add"] and Path.cwd() == Path(os.environ["SESSION_TEST_TASK"]):
    hooks = subprocess.check_output([git, "config", "--worktree", "core.hooksPath"], text=True).strip()
    subprocess.run([git, *args], check=True)
    subprocess.run([
        git, "-C", args[-2], "config", "--worktree", "core.hooksPath", hooks,
    ], check=True)
    sys.exit(0)
""", SESSION_TEST_TASK=str(self.task))
        sibling = Path(self.run_session(self.task, "start", "next"))
        self.assertEqual(sibling, self.fixture / "source-next")
        self.assertEqual(self.git(sibling, "config", "--worktree", "homepodctl.sessionBranch"), "fix/next")
        self.assertNotEqual(
            self.git(self.task, "config", "--worktree", "core.hooksPath"),
            self.git(sibling, "config", "--worktree", "core.hooksPath"),
        )

    def test_start_requires_workflow_on_main(self):
        self.git(self.source, "rm", "scripts/session")
        self.git(self.source, "commit", "-qm", "Remove task tool")
        self.git(self.source, "push", "-q", "origin", "main")
        result = self.execute(self.source, str(SESSION), "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("origin/main needs an executable scripts/session", result.stderr)
        self.assertFalse(self.task.exists())
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))

    def test_first_landing_bootstraps_gate(self):
        self.git(self.source, "rm", "scripts/session")
        tree = self.git(self.source, "write-tree")
        legacy = self.git(self.source, "commit-tree", tree, "-m", "Legacy main")
        self.git(self.source, "update-ref", "refs/heads/main", legacy)
        self.git(self.source, "push", "-q", "origin", "HEAD:refs/heads/legacy")
        self.git(self.remote, "update-ref", "refs/heads/main", legacy)
        self.git(self.source, "update-ref", "refs/remotes/origin/main", legacy)
        self.git(self.source, "worktree", "add", "-qb", "fix/task", str(self.task), "origin/main")
        (self.task / "scripts").mkdir()
        shutil.copy2(SESSION, self.task / "scripts")
        self.git(self.task, "add", "scripts/session")
        self.git(self.task, "commit", "-qm", "Install task tool")
        self.assertIn("Landed", self.run_session(self.task, "land"))
        self.assertEqual(self.ref(self.remote), self.ref(self.task))

    def test_land_refuses_bootstrap_if_main_removed_the_workflow(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.git(self.source, "rm", "scripts/session")
        self.git(self.source, "commit", "-qm", "Remove task tool")
        self.git(self.source, "push", "-q", "origin", "main")
        self.assert_stopped("removed or disabled scripts/session", "./scripts/session", "land")

    def test_land_adopts_an_already_landed_worktree(self):
        self.git(self.source, "worktree", "add", "-qb", "fix/task", str(self.task), "origin/main")
        self.assertIn("Already on origin/main", self.run_session(self.task, "land"))
        self.run_session(self.task, "end")
        self.assertFalse(self.task.exists())

    def test_rebase_dry_run_land_and_end(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.assert_stopped("use scripts/session land", "git", "push", "origin", "HEAD:main")
        self.commit_file(self.source, "upstream.txt", "upstream\n")
        self.git(self.source, "push", "-q", "origin", "main")
        (self.source / "unrelated.txt").write_text("preserve")
        base = self.ref(self.remote)

        self.assert_stopped("not on origin/main", "./scripts/session", "end")
        git_dir = self.git(self.task, "rev-parse", "--absolute-git-dir")
        dry_run = self.run_ok(
            self.task, "env", f"GIT_DIR={git_dir}", f"GIT_WORK_TREE={self.task}",
            "./scripts/session", "land", "--dry-run",
        )
        self.assertIn("Dry run passed", dry_run)
        self.assertEqual(self.ref(self.remote), base)
        self.assertEqual(self.git(self.task, "rev-parse", "HEAD^"), base)
        self.assertEqual(self.git(self.task, "rev-parse", "--is-bare-repository"), "false")
        self.assertIn("Landed", self.run_session(self.task, "land"))
        self.assertEqual(self.ref(self.remote), self.ref(self.task))
        logs = self.fixture / "logs" / self.task.name
        self.assertEqual(len(list(logs.glob("land-*/verify.log"))), 2)
        self.git(self.task, "branch", "--set-upstream-to=origin/main")
        (self.task / "dist/verify").mkdir(parents=True)
        (self.task / "dist/verify/homepodctl").write_text("build output")
        self.run_session(self.task, "end")
        self.assertFalse(self.task.exists())
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))
        self.assertNotIn("branch.fix/task.", self.git(self.source, "config", "--list"))
        self.assertEqual((self.source / "unrelated.txt").read_text(), "preserve")
        self.assertEqual(self.ref(self.source), base)

    def test_review_and_verify_fail_closed(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        for mode, message in {
            "fail": "exited 42",
            "invalid": "no usable review",
            "findings": "did not return a clean review",
            "incomplete": "did not return a clean review",
        }.items():
            with self.subTest(review=mode):
                self.env["SESSION_TEST_REVIEW"] = mode
                self.assert_stopped(message, "./scripts/session", "land")
        logs = self.fixture / "logs" / self.task.name
        self.assertEqual(list(logs.glob("land-*/verify.log")), [])
        self.env["SESSION_TEST_REVIEW"] = "clean"
        self.env["SESSION_TEST_VERIFY"] = "1"
        self.assert_stopped("make exited", "./scripts/session", "land")

    def test_dirty_or_conflicted_work_is_preserved(self):
        self.start_task()
        (self.task / "untracked.txt").write_text("preserve")
        self.assert_stopped("uncommitted changes", "./scripts/session", "land")
        self.assert_stopped("uncommitted changes", "./scripts/session", "end")
        self.commit_file(self.task, "untracked.txt", "preserve")
        self.commit_file(self.task, "base.txt", "task\n")
        self.commit_file(self.source, "base.txt", "upstream\n")
        self.git(self.source, "push", "-q", "origin", "main")
        self.assert_stopped("rebase stopped", "./scripts/session", "land")
        self.assert_stopped("Git operation in progress", "./scripts/session", "end")
        self.git(self.task, "rebase", "--abort")
        self.assertEqual((self.task / "base.txt").read_text(), "task\n")

    def test_rebase_preserves_ignored_files(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        local = self.task / "dist/private.env"
        local.parent.mkdir()
        local.write_text("local settings")
        upstream = self.source / "dist/private.env"
        upstream.parent.mkdir()
        upstream.write_text("tracked settings")
        self.git(self.source, "add", "-f", "dist/private.env")
        self.git(self.source, "commit", "-qm", "Track settings")
        self.git(self.source, "push", "-q", "origin", "main")
        self.assert_stopped("move ignored local files", "./scripts/session", "land", "--dry-run")
        self.assertEqual(local.read_text(), "local settings")

    def test_land_refuses_a_branch_switch_during_fetch(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.git(self.task, "switch", "-qc", "unrelated", "origin/main")
        self.commit_file(self.task, "unrelated.txt", "unrelated\n")
        self.git(self.task, "switch", "-q", "fix/task")
        self.install_git_shim("""
if "fetch" in args:
    subprocess.run([git, "switch", "-q", "unrelated"], check=True)
""")
        self.assert_stopped("this session owns fix/task", "./scripts/session", "land")
        self.assertEqual(self.git(self.task, "branch", "--show-current"), "unrelated")

    def test_inherited_make_flags_do_not_skip_verification(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.env.update({"MAKEFLAGS": "n", "GNUMAKEFLAGS": "n", "SESSION_TEST_VERIFY": "1"})
        self.assert_stopped("make exited", "./scripts/session", "land")
        logs = self.fixture / "logs" / self.task.name
        verify_log, = logs.glob("land-*/verify.log")
        self.assertIn("verify-ran", verify_log.read_text())

    def test_verification_ignores_inherited_go_options(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.env.update({
            "GOFLAGS": "-run=^$",
            "GOENV": str(self.fixture / "go-env"),
            "GOWORK": str(self.fixture / "go.work"),
            "SESSION_TEST_GO_ENV": "1",
        })
        self.assertIn("Landed", self.run_session(self.task, "land"))

    def test_review_findings_stop_landing_before_git_push(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        push_attempt = self.fixture / "push-attempt"
        self.install_git_shim("""
if "push" in args:
    Path(os.environ["SESSION_TEST_PUSH_ATTEMPT"]).touch()
""", SESSION_TEST_PUSH_ATTEMPT=str(push_attempt))
        self.env["SESSION_TEST_REVIEW"] = "findings"
        self.assert_stopped("did not return a clean review", "./scripts/session", "land")
        self.assertFalse(push_attempt.exists())

    def test_land_pins_the_qualified_push_destination(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        base = self.ref(self.remote)
        mirror = self.fixture / "mirror.git"
        self.git(self.source, "clone", "--quiet", "--template=", "--bare", str(self.remote), str(mirror))
        self.env.update({
            "SESSION_TEST_REVIEW": "change-push-url",
            "SESSION_TEST_TASK": str(self.task),
            "SESSION_TEST_MIRROR": str(mirror),
        })
        self.assertIn("Landed", self.run_session(self.task, "land"))
        self.assertEqual(self.ref(self.remote), self.ref(self.task))
        self.assertEqual(self.ref(mirror), base)

    def test_land_does_not_rewrite_the_pinned_url_again(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        base = self.ref(self.remote)
        mirror = self.fixture / "mirror.git"
        self.git(self.source, "clone", "--quiet", "--template=", "--bare", str(self.remote), str(mirror))
        self.env.update({
            "SESSION_TEST_REVIEW": "rewrite-push-url",
            "SESSION_TEST_TASK": str(self.task),
            "SESSION_TEST_MIRROR": str(mirror),
            "SESSION_TEST_REMOTE": str(self.remote),
        })
        self.assertIn("Landed", self.run_session(self.task, "land"))
        self.assertEqual(self.ref(self.remote), self.ref(self.task))
        self.assertEqual(self.ref(mirror), base)

    def test_land_refuses_grafted_ancestry(self):
        self.start_task()
        base = self.ref(self.remote)
        tree = self.git(self.task, "rev-parse", "HEAD^{tree}")
        unrelated = self.git(self.task, "commit-tree", tree, "-m", "Unrelated history")
        self.git(self.task, "update-ref", "refs/heads/fix/task", unrelated)
        grafts = self.source / ".git/info/grafts"
        grafts.parent.mkdir(exist_ok=True)
        grafts.write_text(f"{unrelated} {base}\n")
        self.assert_stopped("remove local Git grafts", "./scripts/session", "land")

    def test_end_preserves_an_unlanded_task_with_grafts(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        head = self.ref(self.task)
        base = self.ref(self.remote)
        grafts = self.source / ".git/info/grafts"
        grafts.parent.mkdir(exist_ok=True)
        grafts.write_text(f"{base} {head}\n{head}\n")
        self.assert_stopped("remove local Git grafts", "./scripts/session", "end")
        self.assertEqual(self.ref(self.task), head)
        self.assertEqual(self.git(self.source, "rev-parse", "refs/heads/fix/task"), head)

    def test_land_refuses_a_new_base_even_if_git_skips_the_hook(self):
        self.start_task()
        self.commit_file(self.task, "first.txt", "first\n")
        intermediate = self.ref(self.task)
        self.git(self.task, "push", "-q", "origin", "HEAD:refs/heads/concurrent")
        self.commit_file(self.task, "second.txt", "second\n")
        self.install_git_shim("""
if "push" in args:
    args = [
        "core.hooksPath=/dev/null" if arg.startswith("core.hooksPath=") else arg
        for arg in ["-c", "core.hooksPath=/dev/null", *args]
    ]
    subprocess.run([
        git, "--git-dir", os.environ["SESSION_TEST_REMOTE"],
        "update-ref", "refs/heads/main", os.environ["SESSION_TEST_REMOTE_HEAD"],
    ], check=True)
""", SESSION_TEST_REMOTE=str(self.remote), SESSION_TEST_REMOTE_HEAD=intermediate)
        result = self.execute(self.task, "./scripts/session", "land")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stale info", result.stderr)
        self.assertEqual(self.ref(self.remote), intermediate)

    def test_verify_uses_only_committed_build_rules(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.env["SESSION_TEST_VERIFY"] = "1"
        local_rules = "verify:\n\t@echo skipped-committed-verification\n"
        ignored = self.task / "GNUmakefile"
        ignored.write_text(local_rules)
        self.assert_stopped("make exited", "./scripts/session", "land")
        self.assertEqual(ignored.read_text(), local_rules)
        ignored.unlink()

        self.git(self.task, "update-index", "--assume-unchanged", "Makefile")
        (self.task / "Makefile").write_text(local_rules)
        self.assert_stopped("make exited", "./scripts/session", "land")
        self.assertEqual((self.task / "Makefile").read_text(), local_rules)

    def test_verify_ignores_git_replacement_objects(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        local_rules = self.fixture / "local.mk"
        local_rules.write_text("verify:\n\t@echo skipped-committed-verification\n")
        original = self.git(self.task, "rev-parse", "HEAD:Makefile")
        replacement = self.git(self.task, "hash-object", "-w", str(local_rules))
        self.git(self.task, "replace", original, replacement)
        self.env["SESSION_TEST_VERIFY"] = "1"
        self.assert_stopped("make exited", "./scripts/session", "land")

    def test_verify_ignores_local_checkout_filters(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        local_rules = self.fixture / "local.mk"
        local_rules.write_text("verify:\n\t@echo skipped-committed-verification\n")
        attributes = self.source / ".git/info/attributes"
        attributes.parent.mkdir(exist_ok=True)
        attributes.write_text("Makefile filter=session-test\n")
        self.git(self.task, "config", "filter.session-test.smudge", f"cat {shlex.quote(str(local_rules))}")
        self.env["SESSION_TEST_VERIFY"] = "1"
        self.assert_stopped("make exited", "./scripts/session", "land")

    def test_verify_ignores_user_attributes(self):
        self.start_task()
        script = self.task / "verify.sh"
        script.write_text("#!/bin/sh\nexit 0\n")
        script.chmod(0o755)
        self.git(self.task, "add", "verify.sh")
        self.git(self.task, "commit", "-qm", "Add fixture verifier")
        self.commit_file(self.task, "Makefile", "verify:\n\t./verify.sh\n")
        config = self.fixture / "xdg"
        (config / "git").mkdir(parents=True)
        (config / "git/attributes").write_text("*.sh text eol=crlf\n")
        self.env["XDG_CONFIG_HOME"] = str(config)
        self.assertIn("Landed", self.run_session(self.task, "land"))

    def test_task_cannot_replace_its_installed_gate(self):
        self.start_task()
        tool = (self.task / "scripts/session").read_text()
        bypass = (
            '    if "_pre-push" in sys.argv or Path(sys.argv[0]).name == "pre-push":\n'
            '        return\n'
        )
        self.commit_file(
            self.task, "scripts/session", tool.replace("def main():\n", "def main():\n" + bypass)
        )
        self.env["SESSION_TEST_REVIEW"] = "findings"
        self.assert_stopped("did not return a clean review", "./scripts/session", "land")

    def test_end_refuses_a_different_branch(self):
        self.start_task()
        self.git(self.task, "switch", "-qc", "release-local")
        self.assert_stopped("this session owns fix/task", "./scripts/session", "end")
        self.assertTrue(self.task.is_dir())
        self.assertEqual(self.git(self.task, "branch", "--show-current"), "release-local")

    def test_end_preserves_ignored_local_files(self):
        self.start_task()
        ignored = self.task / "dist/private.env"
        ignored.parent.mkdir()
        ignored.write_text("local settings")
        self.assert_stopped("move ignored local files", "./scripts/session", "end")
        self.assertEqual(ignored.read_text(), "local settings")

    def test_end_preserves_ignored_files_created_during_fetch(self):
        self.start_task()
        ignored = self.task / "dist/private.env"
        ignored.parent.mkdir()
        self.install_git_shim("""
if args[:1] == ["fetch"]:
    Path(os.environ["SESSION_TEST_IGNORED"]).write_text("local settings")
""", SESSION_TEST_IGNORED=str(ignored))
        self.assert_stopped("move ignored local files", "./scripts/session", "end")
        self.assertEqual(ignored.read_text(), "local settings")

    def test_end_preserves_edits_hidden_by_index_flags(self):
        self.start_task()
        tracked = self.task / "base.txt"
        for flag in ("assume-unchanged", "skip-worktree"):
            with self.subTest(flag=flag):
                self.git(self.task, "update-index", f"--{flag}", tracked.name)
                tracked.write_text("local changes\n")
                self.assert_stopped("clear Git index flags", "./scripts/session", "end")
                self.assertEqual(tracked.read_text(), "local changes\n")
                tracked.write_text("original\n")
                self.git(self.task, "update-index", f"--no-{flag}", tracked.name)

    def test_no_op_dry_run_requires_gate_receipt(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.git(self.task, "push", "-q", "origin", "HEAD:refs/heads/concurrent")
        head = self.ref(self.task)
        self.install_git_shim("""
if "push" in args and "--dry-run" in args:
    subprocess.run([
        git, "--git-dir", os.environ["SESSION_TEST_REMOTE"],
        "update-ref", "refs/heads/main", os.environ["SESSION_TEST_REMOTE_HEAD"],
    ], check=True)
""", SESSION_TEST_REMOTE=str(self.remote), SESSION_TEST_REMOTE_HEAD=head)
        result = self.execute(self.task, "./scripts/session", "land", "--dry-run")
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("main moved during landing", result.stderr)
        self.assertEqual(self.ref(self.remote), head)
        self.assertTrue(list((self.fixture / "logs").glob("**/verify.log")))

    def test_evidence_path_cannot_enter_a_checkout(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        inside = self.source / "evidence"
        self.env["HOMEPODCTL_SESSION_LOG_DIR"] = str(inside)
        self.assert_stopped("outside Git checkouts", "./scripts/session", "land")
        self.assertFalse(inside.exists())
        logs = self.fixture / "logs"
        logs.mkdir()
        (logs / self.task.name).symlink_to(inside, target_is_directory=True)
        self.env["HOMEPODCTL_SESSION_LOG_DIR"] = str(logs)
        self.assert_stopped("outside Git checkouts", "./scripts/session", "land")
        self.assertFalse(inside.exists())

    def test_remote_movement_stops_landing(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.commit_file(self.source, "upstream.txt", "upstream\n")
        self.git(self.source, "push", "-q", "origin", "HEAD:refs/heads/concurrent")
        self.env.update({
            "SESSION_TEST_REVIEW": "advance",
            "SESSION_TEST_REMOTE": str(self.remote),
            "SESSION_TEST_REMOTE_HEAD": self.ref(self.source),
        })
        result = self.execute(self.task, "./scripts/session", "land")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("main moved during landing", result.stderr)
        self.assertEqual(self.ref(self.remote), self.ref(self.source))
        self.assertEqual(list((self.fixture / "logs").glob("**/verify.log")), [])

    def test_existing_hook_is_preserved(self):
        hook = self.source / ".git" / "hooks" / "pre-push"
        hook.parent.mkdir(exist_ok=True)
        hook.write_text("#!/bin/sh\nexit 0\n")
        hook.chmod(0o755)
        result = self.execute(self.source, "./scripts/session", "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("active Git hooks", result.stderr)
        self.assertEqual(hook.read_text(), "#!/bin/sh\nexit 0\n")
        self.assertFalse(self.task.exists())
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))
        hook.unlink()
        self.start_task()

    def test_start_rolls_back_partial_worktree_creation(self):
        self.install_git_shim("""
if args[:2] == ["worktree", "add"]:
    subprocess.run([git, *args], check=True)
    sys.exit(19)
""")
        result = self.execute(self.source, "./scripts/session", "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.task.exists())
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))

    def test_start_does_not_remove_a_competing_worktree(self):
        self.install_git_shim("""
if args[:2] == ["worktree", "move"]:
    subprocess.run([git, "worktree", "add", "-qb", "unrelated", args[-1], "main"], check=True)
""")
        result = self.execute(self.source, "./scripts/session", "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.git(self.task, "branch", "--show-current"), "unrelated")
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))

    def test_land_checks_hook_compatibility_before_rebasing(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        original = self.ref(self.task)
        self.commit_file(self.source, "upstream.txt", "upstream\n")
        self.git(self.source, "push", "-q", "origin", "main")
        self.git(self.task, "config", "--worktree", "core.hooksPath", str(self.fixture / "custom"))
        self.assert_stopped("custom hooks path", "./scripts/session", "land")
        self.assertEqual(self.ref(self.task), original)


if __name__ == "__main__":
    unittest.main()
