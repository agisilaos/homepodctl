# R12 completion design

Status: accepted.

R11 provides command-specific flag specifications. R12 uses those specifications to replace the independently maintained Bash, Zsh, and Fish completion vocabularies while retaining shell-specific rendering and escaping.

## Agreed behavior

- Suggest flags accepted by the selected command, consistently across the three shells. For example, suggest `--choose` for `play`, but not for `setup`.
- Suggest canonical long options and explicit short aliases such as `-f`, `-h`, `-v`, and `-q` only where accepted. Keep accepting legacy spellings such as `-json` without advertising them in completion.
- Share command, flag, and preset semantics. Preserve safe handling of configuration-derived aliases, rooms, and playlists in each shell.
- Add cross-shell parity tests; independent snapshots alone do not establish parity.
- Complete ordinary subcommands and only the supported `plan` targets. A help target is one top-level command, not another execution context.
- Share configured aliases, rooms and playlists, preset names, backend names, shell names, and canonical `true`/`false` values for explicit boolean assignments. A bare boolean does not force value completion.
- Compare candidate sets using literal prefixes. Shell-specific descriptions, ordering, quoting, and presentation may differ.

## Existing authorities and constraints

- `cmd/homepodctl/cli_flag_specs.go` owns command flag names and whether they take values. `flagsForCommand` handles command aliases. The `f` entry represents `-f`, not `--f`.
- Help flags are handled outside the individual flag maps. Root options are handled by `parseGlobalOptions` and must not be treated as universally valid command flags.
- Top-level names are collected from command flag specifications and their aliases; command execution remains in the existing dispatch switch. Descriptions and positional hints belong to completion metadata.
- Plan target names and preset names come from the same tables used by their runtime handlers. Preset construction still returns fresh mutable values, and error text is preserved.
- The compatibility contract in [ADR 0001](adr/0001-reject-irrelevant-command-flags.md) remains unchanged. R12 changes suggestions, not accepted invocations.

## Argument boundaries and exclusions

- Both separated values and `--option=value` assignments retain ownership of their values, even when a value resembles a flag, command, or delimiter. Repeated room options remain available.
- A real `--` ends flag suggestions. `config set` owns literal text after its setting name. A delimiter before a `plan` target is consumed by the wrapper, as in the runtime parser.
- New filesystem completion, config-key inventories, richer multiword playlist matching, duplicate-flag suppression, mutually exclusive selector filtering, and backend-dependent filtering are outside this change.
- Completion remains self-contained: configuration candidates are captured when the script is generated, and pressing Tab does not launch homepodctl or call a backend. Regenerate installed scripts after changes to the CLI or configuration.

## Verification

Behavior tests run generated scripts in Bash, Zsh, and Fish and compare candidate sets for every command's flags plus nested paths, aliases, values, literal tails, and rejected suggestions. Zsh's `compadd` boundary is captured by the test harness; Fish uses `complete -C`. Separate hostile-value tests retain coverage for quoting, whitespace, substitutions, and duplicate candidates. A Bash PTY test presses Tab and Enter against an inert command to verify that Readline inserts one literal argument, including quoted prefixes, assignments, and names that also exist as directories. Generated snapshots and shell syntax checks cover the emitted scripts.
