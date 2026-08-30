# Config persistence consolidation

`native.SaveConfig(*Config) error` is the sole production writer of
`config.json`. `internal/native` owns path resolution, directory creation,
JSON encoding, file permissions, and `ConfigError` wrapping. `InitConfig`,
setup, and `config set` delegate writes to it. Defaults, command-specific
edits, and validation stay with callers; saving does not normalize values.

## Compatibility

- JSON keeps its existing schema, two-space indentation, and no trailing newline.
- New files use `0600`; existing modes are unchanged. Permission hardening
  is a separate change.
- `InitConfig` treats any successful file stat as an existing config and
  returns without parsing or overwriting it, even when the JSON is invalid.
- Nil input returns an encode `ConfigError` before filesystem changes,
  preventing accidental replacement of a config with JSON `null`.
- Setup and `config set` persistence failures use exit code 3 / `CONFIG_ERROR`,
  replacing their former untyped errors.

## Design choices

An error-only result matches the former setup helper. Callers can use
`native.ConfigPath()` to display the path without expanding the writer's
return contract. A repository interface or storage abstraction would add
indirection for a single local file.

Direct writes retain their existing interruption and concurrency risks.
Atomic replacement would require a separate justification.
