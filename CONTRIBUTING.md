# Contributing

Thanks for contributing!

## Development

- Requirements: macOS + Go toolchain
- Build: `make build`
- Format: `make fmt`
- Test: `make test`
- Vet: `make vet`

## Pull requests

- Keep changes focused and incremental.
- Update `README.md` and `CHANGELOG.md` when user-facing behavior changes.
- Prefer adding tests for logic that can be tested without macOS automation.

## Error Handling Convention

- Treat `main` as the only process-exit boundary.
- Command handlers should signal failure via `die(err)` and explicit early exits via `exitCode(code)`.
- Avoid direct `os.Exit(...)` calls outside `main` so command logic remains easier to test.
