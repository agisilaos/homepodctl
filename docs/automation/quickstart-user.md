# Automation Quickstart (User)

Use automation routines to run repeatable playback flows without remembering each command.

## 1) Generate a starter routine

```sh
homepodctl automation init --preset morning > morning.yaml
```

Available presets:

- `morning`
- `focus`
- `winddown`
- `party`
- `reset`

## 2) Edit room names and playlist

Open the file and replace placeholders with your actual AirPlay device names and playlist names.

Tip: discover names via:

```sh
homepodctl devices
homepodctl playlists --query chill
```

## 3) Validate and preview

```sh
homepodctl automation validate -f morning.yaml
homepodctl automation plan -f morning.yaml
```

## 4) Run it

```sh
homepodctl automation run -f morning.yaml
```

## 5) Safe preview mode

```sh
homepodctl automation run -f morning.yaml --dry-run
```

## Troubleshooting

- Validation errors: include exact path (for example `steps[1].play.query`).
- Timeout in `wait` step: increase `timeout` or check Music app state.
- Device not found: verify exact room names from `homepodctl devices`.
- See `docs/automation/troubleshooting.md` for a full symptom/fix matrix.
