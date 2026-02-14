# Automation Quickstart (Agent)

For stable agent execution, use this contract:

```sh
homepodctl automation run -f routine.yaml --json --no-input
```

`--no-input` is explicit/safe for agents; automation is already non-interactive by default.

## Contract

- `stdout`: single JSON object only.
- `stderr`: diagnostics/errors only.
- Exit codes:
  - `0` success
  - `1` runtime failure
  - `2` usage error
  - `3` validation error
  - `4` unmet precondition/timeout

## Recommended flow

```sh
homepodctl schema plan-response --json
homepodctl schema action-result --json
homepodctl automation validate -f routine.yaml --json
homepodctl automation plan -f routine.yaml --json
homepodctl plan automation run -f routine.yaml --json
homepodctl automation run -f routine.yaml --dry-run --json --no-input
homepodctl automation run -f routine.yaml --json --no-input
```

Notes:

- `schema` calls define stable machine contracts for parsers.
- `plan` previews command expansion before execution.
- `--dry-run` validates mutating execution paths without side effects.

## stdin support

```sh
cat routine.yaml | homepodctl automation run -f - --json --no-input
```

## Safety

- Use `--dry-run` in planning pipelines.
- Treat non-zero exit as failed automation.
- Do not parse human output; always use `--json`.
- For common failures and fixes, see `docs/automation/troubleshooting.md`.
