# Music in voice channels

Date: 2026-10-26. Status: phase 1 built. Every open question has an owner
decision (below).

Ningo should play music in a Discord voice channel: from a link or a search,
with a queue, skip, pause and volume, without depending on a third-party bot or
on one library's maintainer.

## What it is

Music is **not a macro-component**. There is no `dokja-music` service and no
`music` domain. That was the first proposal; it is withdrawn because it rebuilds
the feature-shaped modules (`dokja_book`, `dokja_meme`) that
[ADR 0007](decisions/0007-faculties-not-features.md) (accepted) is migrating away
from. Music is a **workflow** across the existing concepts, like Meme and Digest.
[ADR 0010](decisions/0010-four-concepts.md) is still proposed, so each step names
both its concept and its ADR 0007 faculty.

| Step | Concept (ADR 0010) | Faculty (ADR 0007) | Where it runs |
| --- | --- | --- | --- |
| Turn a link or a search into tracks | Knowledge intake | Reading | Feeds, capability `audio.resolve.v1`, plugin `audio-resolve` (phase 1, built) |
| May this track play here (length, live streams, sources) | Rules | Moderation | A rule in the Moderation domain, parameterized by the recipe |
| What plays next: order, repeat, shuffle, who may skip | Action | Action | The Communications domain decides (owner, 2026-10-26); the queue itself is volatile orchestration state |
| Play it in the voice channel | Action | Action | Discord communication adapter, on the voice session it already owns |
| What played and what was skipped | Memory | Experience | Memory receipts (later, phase 5) |

The orchestrator sequences these steps and keeps the per-guild queue as volatile
state. [ADR 0003](decisions/0003-state-ownership.md) allows volatile orchestration
state and forbids a new service, database or volume for it, so a restart empties
the queue.

## The constraint that splits resolving from playing

`feeds.invoke` is one subprocess per request: one JSON reply within the
manifest's time and output bounds. A track lasts minutes, so its audio cannot go
through Feeds. Resolving returns metadata only, and the stream is opened where it
is played:

- **Feeds resolves.** `audio.resolve.v1` returns each track's page address,
  never a stream address. Stream addresses expire, and some hosts bind them to
  the network that requested them.
- **The Discord adapter plays.** It already joins voice channels
  (`voice/voice.ts`) and plays audio through ffmpeg (`voice/radio_stream.ts`).
  Playback adds yt-dlp piped into ffmpeg next to the voice connection. The
  adapter plays only track pages the orchestrator gave it.

Playing inside the adapter adds no container, port or volume, so no Compose or
runbook change. A separate relay container would need every Compose file and
`production-runbook.md` updated together; it is not planned.

## Code taken from elsewhere

| Project | Licence | Decision |
| --- | --- | --- |
| `umutxyp/MusicBot` on GitHub | MIT | Phase 3 copies `src/music/ytdlp.js` and `src/music/relay.js` (yt-dlp feeding ffmpeg through a local relay, one proxy per guild, a concurrency cap) into the Discord adapter, with the MIT notice and the upstream commit `bf396aa48887333c2fc8f199221b16f0eeef1b2c` (2026-10-17). From then on the copy is ours; upstream is a reference for fixes. |
| `skick1234/DisTube` on GitHub | MIT | Not used. It joins voice itself in its own connection group, which would fight the adapter's voice session (one voice connection per bot per guild), and it has one maintainer. Its `TaskQueue` (serialized seek, jump and skip) and `Queue` states guide phase 2. |
| `jagrosh/MusicBot` on GitHub | Apache 2.0 | Not used. Java on JDA 4.4.1, no DAVE voice encryption, no code commits since August 2024. |

The only external dependency left is **yt-dlp**, pinned
(`yt-dlp==2026.10.21` in the Feeds extras) and updated on purpose. Phase 3 pins
the same version in the Discord image. When a host breaks yt-dlp, music stops and
the rest of Ningo carries on.

## Phases

1. **Resolve (built).** Feeds accepts `audio.resolve.v1` through `feeds.invoke`.
   The `audio-resolve` example plugin ships disabled. It takes exactly one of
   `url` or `query` (at most 200 characters) and an optional `limit` (1 to 5),
   and returns `tracks` with `id`, `source`, `url`, `title`, `uploader`,
   `duration_s` and `live`. A link that also names a playlist resolves to the one
   track. yt-dlp's own errors reach the caller as a generic failure; the detail
   stays in the Feeds log. Enabling it: build Feeds with
   `FEEDS_PLUGIN_EXTRAS=media` (or `content`), copy the plugin to `feeds_plugins/`
   and set `enabled: true`. A `query` is searched on YouTube (`ytsearch`); a
   YouTube or SoundCloud `url` resolves already. Spotify and Deezer links come in
   phase 2 (see "What the sources mean for resolving"). No route calls the
   capability yet.
2. **Queue and rules.** Orchestrator routes for adding, skipping, pausing,
   resuming, stopping, listing and setting volume, per guild. The Communications
   domain decides the queue (at most 3 waiting, anyone may control it, radio
   refused while it runs), and admissibility (allowed sources) goes to
   Moderation. The `audio-resolve` plugin gains Spotify and Deezer links. Commands are
   serialized per guild, as DisTube's `TaskQueue` does.
3. **Playback.** The Discord adapter plays a track page given by the
   orchestrator, using the copied yt-dlp relay, and reports when a track ends or
   fails so the queue can advance. This changes two contracts:
   - orchestrator to Discord: either a new kind on `POST /deliver` or a new
     internal endpoint (a service-contract change, reviewed before building);
   - Discord to orchestrator: "track ended" over the existing HTTP bridge.
   YouTube may need a JavaScript runtime for yt-dlp to read its formats; phase 3
   checks this and adds it to the Discord image if so.
4. **Commands.** `/play`, `/skip`, `/queue`, `/pause`, `/resume`, `/stop`,
   `/volume`, and "Ningo, toca …" through the existing voice conversation.
   Desktop and CLI controls can follow, since the queue is behind the
   orchestrator.
5. **Memory.** Played and skipped tracks become episodes, which later lets Ningo
   learn what a server likes.

## Owner decisions (2026-10-26)

- **The Communications domain decides the queue** (Action: what Ningo is doing on
  a platform), next to the delivery policy it already owns.
- **Ningo's speech pauses the music.** When Ningo speaks during a song, the song
  pauses and resumes where it stopped once he has finished.
- **The radio has no precedence.** While a queue is playing or waiting, starting
  the radio is refused with a plain message; the queue must be stopped first.
- **Anyone** in the voice channel may add, skip, pause, resume, stop and set the
  volume.
- **The queue holds at most 3 tracks waiting**, besides the one playing. Adding a
  fourth is refused, saying the queue is full.
- **Live streams may be queued.** They play until someone skips them.
- **Sources: YouTube, Spotify, Deezer and SoundCloud.** Playing YouTube on Discord
  goes against YouTube's terms of service; the owner accepts it for this private
  use.

### What the sources mean for resolving

yt-dlp (2026.10.21) reads YouTube and SoundCloud, links and searches. It has no
Spotify or Deezer extractor: both serve protected audio. A Spotify or Deezer link
is therefore resolved in two steps, as other music bots do:

1. Read the track's title and artist from the service. Deezer's public track API
   needs no key. Spotify's needs an application's client id and secret, which
   would be new settings passed to Feeds (every Compose file and the runbook
   change with them), or its public embed data, which gives the title only.
2. Search that title and artist on YouTube, and play the first match. The track
   keeps the original link and names the source it was found on.

A search typed by a person goes to YouTube; `scsearch` for SoundCloud is
available if the owner later wants a different default.

### Spotify through spotDL (owner, 2026-10-26)

Spotify and Deezer links are resolved with `spotDL/spotify-downloader` (MIT,
pinned at 4.6.0), used as a library in the `audio-resolve` plugin rather than
copied. It reads the track (name, artists, album, duration) and finds the same
song on YouTube Music with its own matching, which rejects covers and live
versions the original does not have. A Deezer track, read from Deezer's public
API, is given to the same matching.

The owner chose this over registering Ningo's own Spotify application, knowing
the trade-off:

- spotDL reads Spotify by default through `spotipyFree`, an unofficial client
  that imitates Spotify's web player; or, on its official path, with the client
  id and secret published in spotDL's source, shared by every spotDL user. Either
  can stop working when Spotify changes or limits them. Ningo's repository holds
  no Spotify credential.
- If it breaks, the fallback is Ningo's own Spotify application (two new
  settings), with the same matching.
- Checked in a scratch environment: spotDL 4.6.0 installs on Python 3.11+ (the
  Feeds image has 3.11), requires the same yt-dlp as the pin, and read a Spotify
  track's name, artist and duration with no setup. Its YouTube Music match was
  not yet verified.

## Compatibility

Phase 1 is additive: a new Feeds capability and a disabled example plugin, and
yt-dlp added to the optional `media` and `content` extras. It changes no event
envelope, route, action, environment variable, Compose file, schema or interface
command. Later phases add orchestrator routes, Discord commands and an internal
adapter contract, each reviewed when it is built.
