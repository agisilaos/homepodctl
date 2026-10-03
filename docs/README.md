# Documentation

## Core

- First-run setup and playback: [README quickstart](../README.md#quick-start-airplay)
- Automation CLI specification: `automation-v1-cli-spec.md`
- Generated command help: [setup](help/setup.txt), [play](help/play.txt), [automation](help/automation.txt), [plan](help/plan.txt), [root](help/root.txt) (update with `scripts/update-help.sh`)
- TUI Preview guide: [Interactive Music/AirPlay dashboard](tui-preview.md)
- TUI generated help: [tui](help/tui.txt)
- User quickstart: `automation/quickstart-user.md`
- Agent quickstart: `automation/quickstart-agent.md`
- Troubleshooting: `automation/troubleshooting.md`
- Config persistence design: `config-persistence-design.md`
- R11 flag compatibility decision: [Reject irrelevant command flags](adr/0001-reject-irrelevant-command-flags.md)
- R12 completion contract: [Shared completion vocabulary](completion-design.md)
- TUI application boundary: [Share playback operations in process](adr/0003-share-playback-operations-in-process.md)
- Default playlist behavior: [Use the default playlist for direct playback](adr/0004-use-default-playlist-for-direct-playback.md)

## Presets

- Automation presets: `automation/presets/`

## Release

- Unified release workflow commands and scripts: `../README.md#release`
- Preparation and publication: [Release guide](../RELEASING.md)
- Release history: `../CHANGELOG.md`
- Interrupted releases: [Manual recovery](release-recovery.md)
- R14 scope decision: [Report release failures for manual recovery](adr/0002-report-release-failures-for-manual-recovery.md)
