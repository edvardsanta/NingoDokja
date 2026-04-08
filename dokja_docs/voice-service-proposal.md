# Voice Service Proposal

This document defines a concrete first version of `dokja_services/dokja_voice`.

The goal is not full conversational voice yet. The goal is a clean TTS service that can be integrated with Discord voice playback and later support bidirectional real-time voice conversation.

## Objective

Build a standalone voice service that:

- receives text
- synthesizes audio
- returns an audio artifact usable by the Discord interface

This first version is TTS only.

Current implementation note:

- `POST /tts` exists for simple request/response playback
- `WS /tts/stream` exists for incremental TTS streaming

## Service Name

Proposed service:

- `dokja_services/dokja_voice`

Suggested service identity:

- name: `dokja-voice`
- type: `service`
- language: `python`

## Responsibility Boundary

### What `dokja_voice` should do

- accept text-to-speech requests
- call a TTS provider
- return playable audio or a temporary audio artifact
- own provider-specific integration details

### What `dokja_voice` should not do

- join Discord voice channels
- manage Discord playback queues
- decide chat/business workflows
- own orchestrator session logic
- capture live Discord voice input

Discord playback remains an interface concern.

## First Architecture

Recommended first flow:

`Discord -> Orchestrator -> Chat Domain -> Chat AI -> Voice Service -> Discord Voice`

For direct TTS commands:

`Discord -> Orchestrator or Interface Command -> Voice Service -> Discord Voice`

For the first implementation, it is acceptable if the Discord interface calls `dokja_voice` directly only for pure voice transport behavior such as `/speak`.

For chat-derived speech, the cleaner long-term flow is still orchestrator-driven.

## First Endpoint Contract

### `POST /tts`

Purpose:

- synthesize text into audio

Example request:

```json
{
  "text": "Ola, eu sou o Ningo.",
  "voice": "default",
  "language": "pt-BR",
  "format": "mp3"
}
```

Suggested fields:

- `text`
  - required
- `voice`
  - optional
- `language`
  - optional
- `format`
  - optional
  - initial supported values:
    - `mp3`
    - `wav`

### Response Options

Two valid approaches exist.

#### Option A: Return audio bytes directly

Pros:

- simple
- stateless
- no artifact cleanup needed

Cons:

- larger HTTP payloads
- Discord interface has to keep bytes in memory

#### Option B: Return an artifact reference

Example response:

```json
{
  "status": "ok",
  "format": "mp3",
  "content_type": "audio/mpeg",
  "file_path": "/tmp/dokja-voice/tts-123.mp3"
}
```

Pros:

- easier if the Discord interface consumes local files
- simpler for some playback flows

Cons:

- cleanup required
- weaker portability if services are separated across containers

### Recommendation

Start with direct bytes over HTTP.

That keeps `dokja_voice` stateless and easier to deploy.

Suggested first response:

- HTTP `200`
- audio bytes as body
- proper `Content-Type`

Headers example:

- `Content-Type: audio/mpeg`
- `X-Dokja-Voice-Format: mp3`

### `WS /tts/stream`

Purpose:

- receive text progressively
- synthesize and emit audio before the full text is complete
- send audio frames to a browser or voice client
- avoid playback responsibility on the server

Current implementation:

- accepts:
  - `start`
  - `text`
  - `flush`
  - `close`
- emits:
  - `audio_format`
  - `segment_start`
  - binary audio frames
  - `segment_end`
  - `done`

Current output format:

- raw `pcm_s16le` over WebSocket binary frames
- optional WAV header in the first binary frame

## Why MP3 First

Discord playback already uses FFmpeg and a voice pipeline.

So the easiest first path is:

1. `dokja_voice` returns MP3
2. Discord interface feeds MP3 into FFmpeg
3. Discord voice transport handles playback

That avoids trying to optimize into raw Opus too early.

## Suggested Provider Strategy

### First version

Use one provider only.

Do not start with a provider abstraction layer that is too broad.

Good first choices:

- OpenAI TTS
- Edge TTS
- Piper

### Recommendation

Pick based on what matters most:

- fastest prototype:
  - OpenAI TTS or Edge TTS
- local/offline:
  - Piper

If the repo is already comfortable with managed AI endpoints, a managed TTS provider is the fastest route.

Current implementation note:

- the scaffold already supports:
  - `stub`
  - `piper`

## Proposed Environment Variables

For a managed provider:

- `DOKJA_VOICE_HOST`
- `DOKJA_VOICE_PORT`
- `DOKJA_VOICE_PROVIDER`
- `DOKJA_VOICE_API_KEY`
- `DOKJA_VOICE_BASE_URL`
- `DOKJA_VOICE_DEFAULT_VOICE`
- `DOKJA_VOICE_DEFAULT_LANGUAGE`

For a local provider, these may differ, but the service boundary stays the same.

## Integration With Discord

### First command set

Suggested first commands:

- `/speak prompt:<text>`
- `/speak_last`

Behavior:

- user must already be in a voice channel
- Discord interface resolves the user’s voice channel
- Discord interface requests audio from `dokja_voice`
- Discord interface plays the audio in that channel

### Why this is the right first Discord integration

It isolates the problem to:

- TTS generation
- playback reliability

It does not require:

- STT
- turn detection
- streaming input

## Future Extensions

After TTS works, `dokja_voice` can grow carefully.

Possible next endpoints:

### `POST /stt`

Purpose:

- convert user speech into text

### `POST /tts/stream`

Purpose:

- streaming audio output for lower latency

### `POST /voice-turn`

Purpose:

- combined voice turn processing

This should not be the first version.

## Risks

### Technical

- TTS latency
- Discord audio queue management
- audio format mismatch
- provider outages

### Architectural

- putting TTS logic into the Discord interface instead of a service
- bypassing the orchestrator for chat-derived voice workflows
- mixing voice transport and business logic

## Recommended First Implementation

Build this first:

1. `dokja_services/dokja_voice`
2. `POST /tts`
3. MP3 output
4. one provider only
5. Discord command `/speak`

Then build this second:

1. speech output for chat replies
2. per-session voice mode

Then only later:

1. STT
2. voice conversations
3. real-time streaming voice
