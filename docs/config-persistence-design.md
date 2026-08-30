# Config persistence consolidation

Status: accepted.

## Agreed scope

Use `native.SaveConfig(*Config) error` as the sole production writer of
`config.json`. `internal/native` owns path resolution, directory creation,
JSON encoding, file permissions, and `ConfigError` wrapping. Route writes
from `InitConfig`, setup, and `config set` through this function.

Preserve the JSON schema and indentation, `0600` creation permissions,
and `InitConfig` returning without overwriting an existing config.
Keep default construction and command-specific edits with their current
callers. Do not add a repository interface, storage abstraction, or atomic
write mechanism.

## Decisions

1. Preserve creation-only `0600` permissions. Saving an existing `0644`
   file leaves it `0644`; permission hardening is a separate change.
2. Reject nil with an encode `ConfigError` before creating directories or
   writing a file, so accidental nil input cannot replace a config with
   JSON `null`. Saving otherwise does not validate or normalize values.
3. Return only an error, matching the former setup helper. Callers can use
   `native.ConfigPath()` to display the path without expanding the writer's
   return contract.

## Consequences to preserve or acknowledge

- Centralized `ConfigError` wrapping makes setup and `config set`
  persistence failures use the CLI's config error classification (exit
  code 3), replacing their former untyped errors.
- `InitConfig` treats any successful file stat as an existing
  config, without parsing it. Consolidation must not silently turn
  initialization into validation or repair.
- Direct writes retain their existing interruption and concurrency risks.
  Addressing those would require a separate justification.

## Verification scope

- Round-trip a config with all supported sections through save and load.
- Check serialized formatting and permissions on a newly created file.
- Exercise deterministic path-resolution, directory-creation, and write
  failures; assert `ConfigError` metadata and underlying error unwrapping.
- Verify that initialization leaves existing config contents unchanged.
- Cover unchanged existing-file permissions and nil input without disk changes.
- Verify all production config writes delegate to the shared writer.
- Update command tests that stub only the command-side path resolver so
  the shared writer remains isolated from the real user config.
