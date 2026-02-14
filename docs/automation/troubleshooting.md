# Automation Troubleshooting

| Symptom | Likely cause | Check | Fix |
|---|---|---|---|
| `Not authorised to send Apple events` | Terminal/binary missing Automation permission | `homepodctl doctor --plain` | Open System Settings -> Privacy & Security -> Automation, allow terminal app to control Music |
| `Could not connect to Music app` | Music is closed or not responsive | `homepodctl status --json` | Open Music.app, retry, then run `homepodctl doctor` |
| `no playlists match` | Query mismatch or ambiguous naming | `homepodctl playlists --query "<text>" --json` | Use `--playlist-id` for exact selection |
| `no native mapping for room=... playlist=...` | Missing config mapping | `homepodctl config get native.playlists.<room>.<playlist>` | Add mapping with `homepodctl config set native.playlists.<room>.<playlist> "<shortcut>"` |
| `shortcuts run "<name>" failed` | Shortcut missing or runtime failure | `homepodctl doctor --json` and `shortcuts list` | Fix or recreate shortcut, then retry |
| `no rooms provided` | Defaults missing and no room flags | `homepodctl config get defaults.rooms` | Set defaults: `homepodctl config set defaults.rooms "Bedroom"` |
| Automation validation error (e.g. `steps[1].play.query`) | YAML shape/type error | `homepodctl automation validate -f routine.yaml --json` | Correct the reported path/field and re-run validation |
| `plan target did not return valid JSON` | Target command not run in JSON mode | `homepodctl plan ... --json` | Keep `--json` at the end of `plan` command |

Use this preflight when uncertain:

```sh
homepodctl doctor --json
homepodctl automation validate -f routine.yaml --json
homepodctl automation plan -f routine.yaml --json
homepodctl automation run -f routine.yaml --dry-run --json --no-input
```
