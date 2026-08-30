# Automation Quickstart (Agent)

For stable agent execution, use this contract:

```sh
homepodctl automation run -f routine.yaml --json --no-input
```

`--no-input` is explicit/safe for agents; automation is already non-interactive by default.

## Contract

- `stdout`: one result object on success or execution failure. A failed run has `ok=false`, an `error` on the failed step, and later steps marked `skipped=true`.
- `stderr`: diagnostics and pre-execution error envelopes only. Usage, file read, config, and validation failures leave stdout empty.
- Exit codes: `0` success, `1` runtime failure (including all failed steps and file read errors), `2` usage error, `3` config/automation validation error. Preconditions, backend errors, timeouts, and cancellation during execution all use `1`; `4` is for backend failures outside automation execution.

See the [authoritative help](../help/automation.txt) and
[full exit/output contract](../automation-v1-cli-spec.md#exit-codes).

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

- `plan-response` describes the top-level wrapper; `action-result` describes one-off playback actions, not automation results. Automation uses the shape in the specification above.
- `automation validate` checks the file without merging config defaults. `automation plan` and `automation run --dry-run` compile the same offline recipe with file/config defaults, with no backend calls.
- Playlist searches, native playlist-ID lookups, current-output inference, and native mapping checks happen only at the relevant execution step. Successful previews do not prove live preconditions will pass, and `resolved` fields remain the recipe after execution.
- `plan automation run` nests the dry-run result under `plan` in its envelope. A child-command failure uses the wrapper's generic exit `1`.

## stdin support

```sh
cat routine.yaml | homepodctl automation run -f - --json --no-input
```

## Safety

- Use `--dry-run` in planning pipelines.
- Treat non-zero exit as failed automation.
- Do not parse human output; always use `--json`.
- For common failures and fixes, see `docs/automation/troubleshooting.md`.
