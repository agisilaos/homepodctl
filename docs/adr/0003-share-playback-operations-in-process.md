# Share playback operations in process

The interactive TUI and ordinary CLI commands share in-process application services for now-playing snapshots, transport, volume, and playback routing. The TUI does not invoke `homepodctl` as a subprocess or parse command output: subprocess composition would duplicate process startup, inherit the command's short-lived execution context, and expose only the deliberately narrower status output instead of the full playback model. Small service interfaces keep the AppleScript adapter replaceable and let both interfaces exercise the same behavior in tests.
