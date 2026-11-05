#!/usr/bin/env python3
"""Exercise task sessions using a bare local remote and a controlled reviewer."""

import os
from pathlib import Path
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
subprocess.run(["git", "init", "-q", "--bare", str(reviewer_repo)], check=True)
assert (reviewer_repo / "HEAD").is_file(), "reviewer Git escaped its repository"
output = Path(args[args.index("--output-last-message") + 1])
mode = os.environ["SESSION_TEST_REVIEW"]
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
        self.run_ok(self.fixture, "git", "init", "-q", "--bare", "-b", "main", str(self.remote))
        self.run_ok(self.fixture, "git", "init", "-q", "-b", "main", str(self.source))
        (self.source / "scripts").mkdir()
        shutil.copy2(SESSION, self.source / "scripts")
        (self.source / "base.txt").write_text("original\n")
        (self.source / "Makefile").write_text(
            'verify:\n\t@test -z "$$GIT_DIR$$GIT_COMMON_DIR$$HOMEPODCTL_LANDING"\n'
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

    def test_start_isolates_hook_setup_from_caller_environment(self):
        start_path = self.run_ok(
            self.source, "env", f"GIT_DIR={self.source / '.git'}",
            f"GIT_WORK_TREE={self.source}", "./scripts/session", "start", "task",
        )
        self.assertEqual(start_path, str(self.task))
        self.assertNotIn("core.hookspath", self.git(self.source, "config", "--list"))
        hook_path = Path(self.git(self.task, "config", "--worktree", "--get", "core.hooksPath"))
        self.assertTrue((hook_path / "pre-push").is_file())

    def test_start_requires_workflow_on_main(self):
        self.git(self.source, "rm", "scripts/session")
        self.git(self.source, "commit", "-qm", "Remove task tool")
        self.git(self.source, "push", "-q", "origin", "main")
        result = self.execute(self.source, str(SESSION), "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("origin/main needs an executable scripts/session", result.stderr)
        self.assertFalse(self.task.exists())
        self.assertNotIn("fix/task", self.git(self.source, "branch", "--list"))

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

    def test_inherited_make_flags_do_not_skip_verification(self):
        self.start_task()
        self.commit_file(self.task, "task.txt", "task\n")
        self.env.update({"MAKEFLAGS": "n", "GNUMAKEFLAGS": "n", "SESSION_TEST_VERIFY": "1"})
        self.assert_stopped("make exited", "./scripts/session", "land")
        logs = self.fixture / "logs" / self.task.name
        verify_log, = logs.glob("land-*/verify.log")
        self.assertIn("verify-ran", verify_log.read_text())

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
        hook.write_text("#!/bin/sh\nexit 0\n")
        hook.chmod(0o755)
        result = self.execute(self.source, "./scripts/session", "start", "task")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("active Git hooks", result.stderr)
        self.assertEqual(hook.read_text(), "#!/bin/sh\nexit 0\n")
        self.assertNotIn("core.hookspath", self.git(self.task, "config", "--list"))


if __name__ == "__main__":
    unittest.main()
