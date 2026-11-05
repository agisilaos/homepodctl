# Task sessions and landing

## Kick off a task

Use a fresh Codex or Claude chat for a distinct task. Check the relevant
GitHub issue and run `scripts/session list` before assigning work. If another
worktree may own the change, inspect its status and diff against its own HEAD.
Give the new chat the goal, issue or other source of scope, acceptance checks,
and authorization to investigate, implement, commit or land. Carry forward
settled decisions and evidence by reference.

At the start of the chat, run `scripts/session start <name>` and work from the
absolute path it prints. This fetches `origin/main`, creates a sibling worktree
on `fix/<name>`, and installs the local pre-push gate for that worktree. Use a
short lowercase name such as `issue-21-playback-feedback`. The primary checkout
can have its own work in progress. The workflow must be on `origin/main` before
it can start new tasks.

## Work and review

Stage owned paths explicitly. Run focused checks during development, such as
`make docs-check` for docs or the relevant Go package tests for a behavior
change. For playback evidence, distinguish fixtures, observed Music.app state
and human-confirmed HomePod audio. Store evidence outside checkouts in
`~/Library/Logs/homepodctl/threads/<worktree>/`.

Commit when Agis authorizes committing. Land when Agis authorizes pushing.
Use GitHub Issues for task context and outstanding work.

## Land on main

Run `scripts/session land` from the clean task worktree. It takes a lock shared
by worktrees of this clone, fetches `origin/main`, and rebases the task commits
onto it. The pre-push gate reviews that committed range with Codex and runs
`make verify` once in a fresh local clone of the exact commit. A passing gate lets
the same command fast-forward `origin/main` directly and confirm the landed
commit on the remote. GitHub Actions also runs on the resulting push. The
installed hook runs a copy of
the tool from fetched `main`. The first landing bootstraps that copy from the
script used to invoke the landing.

- `scripts/session land --dry-run` rebases and exercises the same gate without
  updating remote refs.
- Move ignored local files and clear index flags before rebasing. If a rebase
  stops, resolve it and continue, or abort it before trying again.
- Findings, incomplete reviews and failed checks stop the push. The command
  prints the run directory containing the verdict and full logs. Address the
  failure and rerun. Set `HOMEPODCTL_CODEX` to a compatible Codex executable
  path if the client fails.
- If `main` moves during landing, rerun to qualify the new base. Each attempt
  reviews its own base and head.
- Landing also installs the gate in an existing linked task worktree. A custom
  hooks path or active default hooks require integration before the gate can
  be installed. The guard applies to worktrees with the gate installed.

When cleanup is requested, run `scripts/session end` from the task worktree. It
removes the worktree and its original branch after checking that the checkout
is clean and its commits are on `origin/main`. Clear index flags that hide
tracked edits and move ignored local files before cleanup. The command
recognizes the normal build and verification binaries as disposable. Keep
durable findings on the issue and retain evidence outside the worktree.
