# homepodctl agent instructions

homepodctl is a macOS Go CLI for controlling Apple Music playback and routing audio to HomePods.

## Sessions

Start each new task chat with `scripts/session start <name>` and work in the
printed checkout. Commit and land when separately authorized. Use
`scripts/session land` to review, verify and push directly to `main`.
Read [task sessions and landing](docs/agents/landing.md) when preparing a
handoff, starting or landing a task, or cleaning up a landed worktree.

## Common commands

- Build: `make build`
- Test: `make test`
- Vet: `make vet`
- Format: `make fmt`
- Format check: `make fmt-check`
- Help/docs check: `make check-help` and `make docs-check`
- Full verification: `make verify`

## Code style

- Use idiomatic Go.
- Keep CLI behavior documented in `README.md`.
- Run `gofmt` on Go changes.
- Prefer small, testable helpers in `internal/*`.
- Avoid changing release scripts without running the relevant release checks.

## Domain language

Read `CONTEXT.md` before changing playback or release-publication behavior.

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues for `agisilaos/homepodctl`; external PRs are not a triage/request surface. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the default five-label triage vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo with root `CONTEXT.md` and ADRs in `docs/adr/`. See `docs/agents/domain.md`.
