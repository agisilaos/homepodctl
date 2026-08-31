# homepodctl

The language used to describe playlist playback and release publication for homepodctl.

## Language

### Playback

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

### Release publication

**Release publication**:
Making a homepodctl version available through its version tag, downloadable release artifacts, and Homebrew formula.
_Avoid_: Using “release” without distinguishing the whole publication from the GitHub release alone.

**Release artifact identity**:
The exact bytes of a homepodctl release artifact and the source commit and version to which those bytes belong.
_Avoid_: Treating builds of the same source as necessarily identical artifacts.

**Release commit**:
The source commit designated by a homepodctl version tag.
_Avoid_: Treating the current branch tip as the release commit without checking the tag.

**Published artifact**:
A homepodctl release artifact already available for download from its GitHub release.
_Avoid_: Using a newly rebuilt local artifact as a synonym.

**Manual release recovery**:
An operator verifying the state of an interrupted publication and completing only its missing work.
_Avoid_: Treating a rerun of the release script as recovery.
