# Playback

The language used to describe playlist playback through homepodctl.

## Language

**Playlist target**:
The playlist selection supplied for playback, expressed as either playlist text or a persistent ID.
_Avoid_: Treating a room as a playlist target.

**Playlist query**:
Playlist text used to find a playlist. AirPlay treats it as a fuzzy search, while native playback requires the exact configured playlist name.

**Persistent ID**:
The identifier for a playlist in the Music.app library, distinct from its display name.

**Room**:
A named playback destination.

**Explicit volume**:
A volume supplied for the current playback request.

**Default volume**:
A configured volume used when the current playback request supplies no volume.

**Absent volume**:
The absence of both an explicit volume and a configured default volume.
