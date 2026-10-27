# Music in voice channels

Date: 2026-10-26. Status: phases 1 to 4 built (sources, queue, playback,
commands); Memory receipts are later.

Ningo should play music in a voice channel. The main path is a few typed words:
someone sends the play command with "monalisa jorge", and Ningo finds the song
wherever it is and plays it. A link to a song works too. There is a queue, skip,
pause and volume, without depending on a third-party bot or on one library's
maintainer.

## What it is

Music is **not a macro-component**. There is no music service and no music
domain: that was the first proposal, withdrawn because it rebuilds the
feature-shaped modules (`dokja_book`, `dokja_meme`) that
[ADR 0007](decisions/0007-faculties-not-features.md) (accepted) is migrating away
from. Music is a **workflow** across the existing concepts, like Meme and Digest.
[ADR 0010](decisions/0010-four-concepts.md) is still proposed, so each step names
both its concept and its ADR 0007 faculty.

| Step | Concept (ADR 0010) | Faculty (ADR 0007) | Where it runs |
| --- | --- | --- | --- |
| Turn words or a link into a song | Knowledge intake | Reading | Music sources: one Feeds plugin per source, capability `audio.resolve.v1` |
| Decide which result is the song, and whether it may play | Rules | Moderation | The Communications domain (`ResolveTrack`, `BestMatch`) |
| What plays next: order, repeat, shuffle, who may skip | Action | Action | The Communications domain decides; the queue itself is volatile orchestration state |
| Play it in the voice channel | Action | Action | The voice adapter, on the voice session it already owns |
| What played and what was skipped | Memory | Experience | Memory receipts (later) |

The orchestrator sequences these steps and keeps the per-guild queue as volatile
state. [ADR 0003](decisions/0003-state-ownership.md) allows volatile orchestration
state and forbids a new service, database or volume for it, so a restart empties
the queue.

## Sources are Feeds plugins

Each music source is a Feeds plugin, like a meme source: the repository ships the
capability, one generic example and the domain rules, and the owner installs the
sources in `feeds_plugins/`, which git ignores. Adding a source is copying a
folder there, with no change to the repository. The repository names no music
service.

A source answers two actions of `audio.resolve.v1` through `feeds.invoke`:

- `read {url}`: the song a link names.
- `search {query, limit}`: songs found for a few words.

Every reply is a result, never an error, unless the plugin itself broke:

- `{handled: false}`: not this source's link or not a search it does; the next
  source is asked. Feeds does not mark the source failed.
- `{handled: true, refused: "…"}`: this source's request, but it cannot be done
  (an album link, an unreadable page); the reason reaches the person.
- `{handled: true, tracks: [...]}`: each track has `id`, `source`, `url`,
  `title`, `uploader`, `duration_s`, `live` and `playable`.

A **playable** track can be played from its page. A track that is not playable
names a song (a catalogue knows its title, artist and length but serves protected
audio); the domain then searches the playable sources for the same recording.

A source must step aside (`handled: false`) before loading heavy libraries, since
every source asked runs a process in the host's bounded on-demand slots.

**Order.** The domain asks the enabled sources in the order of their ids, as Feeds
sorts eligible providers. The owner sets priority by naming: `10-…` is asked
before `20-…`. Words go to each source until one finds songs; a link goes to the
first source that recognizes it.

**The example.** `plugins.example/audio-pages` reads any page yt-dlp understands
and searches with a yt-dlp search prefix the owner sets in its `settings.json`
(without it, it does not search). An optional `hosts` list limits the pages it
reads. Addresses and prefixes are runtime input, never defaults in the
repository.

**Libraries.** Feeds installs libraries only when its image is built. The owner's
sources bring theirs in a requirements file of the owner's, named by the
`FEEDS_PLUGIN_REQUIREMENTS` build argument (for example
`feeds_plugins/requirements.txt`); the default is an empty list in the repository.
The example needs `yt-dlp` in that list.

## The same recording

When a catalogue names a song, `BestMatch` keeps the playable candidate with:

- every word of the song's title (a candidate may add "Remastered", not lose
  words);
- no word marking another version (cover, live, remix, karaoke, instrumental…)
  unless the song has it too;
- a length within 7 seconds, when both lengths are known.

The artist only orders the survivors, because one singer is credited under
different names on different services. Regression: a catalogue credits a song to
"Antônio Carlos Jobim" and a playable source to "Tom Jobim"; a stricter matcher
that compared artist names letter by letter rejected the right recording (same
title, lengths 242 and 243 seconds).

## The constraint that splits resolving from playing

`feeds.invoke` is one subprocess per request: one JSON reply within the
manifest's time and output bounds. A track lasts minutes, so its audio cannot go
through Feeds. Resolving returns metadata and the page only, never a stream
address (those expire, and some hosts bind them to the network that requested
them). The voice adapter opens the stream from the page where it plays it: it
already joins voice channels (`voice/voice.ts`) and plays audio through ffmpeg
(`voice/radio_stream.ts`), and playback adds yt-dlp piped into ffmpeg there. It
plays only pages the orchestrator gave it. This adds no container, port or volume.

## Code taken from elsewhere

| Project | Licence | Decision |
| --- | --- | --- |
| `umutxyp/MusicBot` | MIT | Read, not copied. Its relay exists for stream addresses bound to the asking network; piping yt-dlp straight into ffmpeg avoids that, so playback needed none of its code. |
| `skick1234/DisTube` | MIT | Not used. It joins voice itself in its own connection group, which would fight the adapter's voice session, and has one maintainer. Its `TaskQueue` (serialized seek, jump and skip) and `Queue` states guide the queue. |
| `jagrosh/MusicBot` | Apache 2.0 | Not used. Java on an old voice library without the current voice encryption, no code commits since August 2024. |
| A catalogue-reading library | MIT | Used by the owner's catalogue source, outside the repository. Its own matching rejects artists credited under another name, so the domain decides with `BestMatch` instead. |

## Phases

1. **Sources and resolving (built).** The capability and its contract, the
   `audio-pages` example, the `FEEDS_PLUGIN_REQUIREMENTS` build argument, and
   `ResolveTrack` and `BestMatch` in the Communications domain. No route calls
   them yet.
2. **Queue and rules (built).** Events `music.play`, `music.skip`,
   `music.pause`, `music.resume`, `music.stop`, `music.queue`, `music.volume` and
   `music.ended` route to the Communications domain, which keeps one queue per
   server (`MusicQueue`: at most 3 waiting, volume 0 to 100, an "ended" report
   advances only for the track playing). They are not `communications.*`, which
   needs the operator token: people in a channel send them through the bridge,
   after the inbound authorization (the server and the allowed channels). The
   orchestrator keeps the queues in memory and serializes each server's commands,
   but searches without holding the queue, within 150 seconds, so skip and pause
   never wait for a search. A refusal (queue full, nothing found) is an answer,
   `{ok: false, reason}`, which the bridge turns into one plain line.
3. **Playback (built).** No new endpoint: every music reply carries the queue
   (with a per-process `epoch` and a `version` raised on each change), and the
   voice adapter brings its playback to it, ignoring late replies. When a track
   ends or fails it sends `music.ended`; when its voice connection is gone,
   `music.stop`. yt-dlp pipes straight into ffmpeg inside the adapter, so stream
   addresses never travel and the relay is not copied (no code from
   `umutxyp/MusicBot` is used). yt-dlp uses the image's node for pages that run
   JavaScript. Details in [the Discord interface](discord-interface.md#music).
4. **Commands (built).** `/play`, `/skip`, `/pause`, `/resume`, `/stop`,
   `/queue` and `/volume` on Discord. By voice through the existing voice
   conversation, and desktop or CLI controls, can follow.
5. **Memory.** Played and skipped tracks become episodes.

## Owner decisions (2026-10-26)

- **The Communications domain decides the queue** (Action: what Ningo is doing on
  a platform), next to the delivery policy it already owns.
- **Ningo's speech pauses the music**, which resumes where it stopped.
- **The radio has no precedence.** While a queue is playing or waiting, starting
  the radio is refused with a plain message; the queue must be stopped first.
- **Anyone** in the voice channel may control the queue.
- **The queue holds at most 3 tracks waiting**, besides the one playing.
- **Live streams may be queued**; they play until someone skips them.
- **Sources** are the four services the owner named, installed as their own
  plugins outside the repository; the owner accepts their terms for this private
  use.
- **The repository and its pull requests name no music service.**
- **Source libraries come from the owner's own requirements file**, not from the
  repository's lists.

## Compatibility

Additive. A new Feeds capability, a disabled example plugin, and a build argument
`FEEDS_PLUGIN_REQUIREMENTS` in both Feeds Dockerfiles and every Compose file,
defaulting to an empty list (documented in `production-runbook.md`). No change to
the event envelope, routes, actions, runtime environment variables, schemas or
interface commands; `ResolveTrack` has no caller until the queue phase.

## Empty command responses

The Discord adapter cannot play without a queue state. An empty bridge reply
without music state is unconfirmed, never a successful command acknowledgment.
Inbound authorization can intentionally produce that empty response before the
queue is reached; inspect server/channel access configuration rather than assuming
an audio decoder or source failure. Existing explicit bridge text is preserved.
The adapter reports an unconfirmed command and does not start playback or create
a music panel. This notice changes no inbound policy or access configuration.
