# homepodctl

The language used to describe playback, routing, playback observation, and release publication for homepodctl.

## Language

### Playback

**AirPlay playback**:
Playback sent by Music.app through a playback route.

**Native playback**:
Playback initiated on a Room through a configured Shortcut rather than sent by Music.app.
_Avoid_: Assuming that Music.app's state describes native playback.

**Playlist target**:
The playlist selection supplied for playback, expressed as either playlist text or a persistent ID.
_Avoid_: Treating a room as a playlist target.

**Playlist query**:
Playlist text used to find a playlist. AirPlay treats it as a fuzzy search, while native playback requires the exact configured playlist name.

**Persistent ID**:
The identifier for a playlist in the Music.app library, distinct from its display name.

**Room**:
A named playback destination, including HomePods, Apple TVs, and other AirPlay speakers.
_Avoid_: Using “HomePod” when the destination may be another kind of AirPlay device.

**Playback route**:
The set of Rooms currently selected as Music.app's AirPlay outputs.
_Avoid_: Treating selection as proof that every Room is audibly playing.

**Pending playback route**:
A proposed set of Rooms that has not yet been applied as the playback route.
_Avoid_: Treating each selection change as an applied route change.

**Selected output**:
A Room included in the playback route.

**Active output**:
A Room that Music.app reports as active, distinct from merely being selected or available.

**Now-playing snapshot**:
The playback state, track details, and playback route observed at one point in time.
_Avoid_: Treating a snapshot as independent state reported directly by every Room.

**Stale snapshot**:
The most recent successful now-playing snapshot retained after a later observation failed.
_Avoid_: Presenting stale state as current without qualification.

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
