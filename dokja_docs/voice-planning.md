# Voice Planning

This document outlines a practical plan for making Ningo speak and, later, support voice conversations.

The goal is to avoid jumping directly into full real-time voice complexity before the base architecture is ready.

## Problem Framing

"Ningo speaking" can mean different things:

1. TTS output only
   - text -> speech
   - Ningo reads responses aloud

2. Voice input and output
   - speech -> text -> LLM -> speech
   - user speaks, Ningo answers in voice

3. Real-time voice agent
   - streaming input
   - streaming output
   - turn detection
   - interruption handling

These are very different levels of complexity.

## Recommendation

Start with TTS output only.

This is the smallest step that:

- adds visible user value
- fits the current architecture
- keeps Discord voice integration useful
- avoids early STT and streaming complexity

## Suggested Architecture

Recommended flow for voice output:

`Discord -> Orchestrator -> Chat Domain -> Chat AI -> Voice Service -> Discord Voice`

Important rules:

- interfaces should not synthesize voice directly from business logic
- orchestrator should keep workflow ownership
- a dedicated voice service should own TTS integration
- Discord should remain responsible only for Discord voice transport/playback

## Proposed Service

Create a new service:

- `dokja_services/dokja_voice`

Initial responsibility:

- synthesize text into playable audio

Possible initial endpoint:

- `POST /tts`

Example input:

```json
{
  "text": "Ola, eu sou o Ningo.",
  "voice": "default",
  "language": "pt-BR"
}
```

Example output options:

- raw WAV bytes
- MP3 bytes
- Opus-ready audio file
- URL/path to generated audio artifact

## Phases

### Phase 1: Ningo Speaks

Scope:

- text-to-speech only
- no speech recognition
- no real-time conversational audio

Main goals:

- create `dokja_voice`
- synthesize orchestrator/chat responses into audio
- play them in Discord voice channels
- optionally add a direct command such as `/speak`

Suggested flow:

1. user triggers `/speak` or a voice-enabled chat command
2. orchestrator gets text reply
3. orchestrator requests TTS from `dokja_voice`
4. Discord interface receives audio payload reference
5. Discord interface joins voice and plays audio

Good first commands:

- `/speak prompt:<text>`
- `/speak_last`
- `/voice_chat` as a later extension, not first step

Success criteria:

- Ningo can read a response in a Discord voice channel
- playback is reliable
- latency is acceptable

### Phase 2: Voice Output Integrated With Chat

Scope:

- chat remains text-first
- voice becomes an output option

Main goals:

- allow chat replies to also be spoken
- allow per-channel or per-session voice mode
- maintain session behavior already used in chat

Possible behaviors:

- `/chat` replies in text and optional voice
- `/voice_mode on`
- `/voice_mode off`

Success criteria:

- same conversation pipeline works for text and voice output
- Discord voice transport is stable
- TTS audio queue is managed correctly

### Phase 3: Speech Recognition

Scope:

- add STT
- user speaks, system transcribes

Main goals:

- create speech-to-text capability
- forward transcribed text into the existing orchestrator chat workflow
- keep TTS as the response path

Suggested flow:

`Discord Voice Input -> STT -> Orchestrator -> Chat Domain -> Chat AI -> TTS -> Discord Voice`

Important note:

At this stage, voice is still not fully real-time conversation. It is still turn-based.

Success criteria:

- user speech can be transcribed reliably enough
- transcription can reuse the existing chat session model
- reply can be spoken back in the same voice channel

### Phase 4: Real-Time Voice Agent

Scope:

- streaming voice in both directions
- turn management
- interruptions

Main goals:

- detect when the user starts and stops speaking
- interrupt Ningo if the user speaks over it
- reduce latency with streaming pipelines
- keep conversation coherent under voice timing constraints

This is the most complex phase.

Key problems:

- turn detection
- buffering
- interruptibility
- latency
- partial transcripts
- playback cancellation
- session state under rapid back-and-forth

Success criteria:

- conversation feels real-time
- low delay between user speech and reply
- interruptions are handled safely

## Technology Options

### TTS

Possible choices:

- local / self-hosted
  - Piper
- cloud / managed
  - OpenAI TTS
  - ElevenLabs
  - Edge TTS

Recommendation for first implementation:

- use a simple TTS provider first
- optimize for fast prototype and decent quality
- avoid overengineering provider abstraction too early

### STT

Possible choices:

- Vosk
- Whisper or Whisper-compatible systems
- cloud speech APIs

Recommendation:

- do not start with STT first
- if local/offline is important, Vosk is a candidate
- if accuracy matters more for early prototyping, Whisper-style STT is usually a stronger starting point

## Why Not Start With STT

Starting with STT first increases complexity too quickly.

It adds:

- microphone/voice input capture
- segmentation and silence detection
- transcription quality concerns
- more latency
- harder debugging

TTS-first is a better architecture-first step because:

- the response side is under our control
- Discord voice playback can be stabilized first
- chat/orchestrator/domain integration stays simpler

## Main Risks

### Technical Risks

- Discord voice transport instability
- playback queue complexity
- audio format mismatch
- TTS latency
- STT accuracy
- interruption and cancellation handling

### Architectural Risks

- putting TTS logic inside the Discord interface
- bypassing the orchestrator
- mixing voice transport with domain logic
- making the Discord interface own too much business behavior

## Recommended Next Step

The next implementation step should be:

1. define `dokja_services/dokja_voice`
2. implement TTS only
3. integrate it with Discord playback
4. keep STT out for now

This gives the project a controlled path from:

- text platform
to
- voice-enabled assistant

without immediately turning the system into a streaming voice-agent project.
