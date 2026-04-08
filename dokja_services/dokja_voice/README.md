# Dokja Voice

Standalone FastAPI service for Dokja voice synthesis.

This is the first scaffold for voice support.

Current status:

- TTS only
- no STT
- supports streaming TTS over WebSocket
- supports:
  - a local stub synthesizer
  - `piper` via Python API

Endpoints:

- `GET /health`
- `POST /tts`
- `POST /stt`
- `WS /tts/stream`

## Request

`POST /tts`

```json
{
  "text": "Ola, eu sou o Ningo.",
  "voice": "default",
  "language": "pt-BR",
  "format": "wav"
}
```

Current limitation:

- only `wav` is supported in this initial scaffold

## Speech To Text

`POST /stt`

```json
{
  "audio_bytes_b64": "<base64 wav bytes>",
  "audio_format": "wav",
  "language": "pt-BR"
}
```

Example response:

```json
{
  "status": "ok",
  "text": "transcribed text",
  "audio_format": "wav",
  "language": "pt-BR"
}
```

## Response

- HTTP `200`
- body contains WAV bytes
- headers include:
  - `X-Dokja-Voice-Format`
  - `X-Dokja-Voice-Voice`
  - `X-Dokja-Voice-Language`

## Environment Variables

- `DOKJA_VOICE_HOST`
- `DOKJA_VOICE_PORT`
- `DOKJA_VOICE_PROVIDER`
- `DOKJA_VOICE_DEFAULT_VOICE`
- `DOKJA_VOICE_DEFAULT_LANGUAGE`
- `DOKJA_VOICE_PIPER_MODEL`

## Notes

Provider selection:

- `DOKJA_VOICE_PROVIDER=stub`
  - generates local placeholder WAV output
- `DOKJA_VOICE_PROVIDER=piper`
  - loads the Piper model through the Python API and returns generated WAV output
- `DOKJA_VOICE_STT_PROVIDER=stub`
  - returns a placeholder transcription from the received audio payload
- `DOKJA_VOICE_STT_PROVIDER=transformers`
  - loads a local ASR model through Hugging Face `transformers`

CPU runtime note:

- `dokja_voice` is currently configured for CPU-only inference
- PyTorch is installed from the CPU wheel index
- the `transformers` ASR pipeline is forced to `device=-1`
- `ffmpeg` is installed in the service container and required by the local ASR path

Example Piper configuration:

```bash
export DOKJA_VOICE_PROVIDER=piper
export DOKJA_VOICE_PIPER_MODEL=/path/to/model.onnx
export DOKJA_VOICE_STT_PROVIDER=transformers
export DOKJA_VOICE_STT_MODEL=openai/whisper-tiny
```

The HTTP boundary stays the same regardless of backend.

## Streaming TTS

`WS /tts/stream`

Purpose:

- receive text incrementally
- start sending audio before the full text is finished
- stream audio chunks to the client
- never play audio on the server

### Client Messages

Start the stream:

```json
{
  "type": "start",
  "voice": "default",
  "language": "pt-BR",
  "output_format": "pcm_s16le",
  "include_wav_header": false,
  "chunk_bytes": 4096,
  "min_buffer_chars": 48,
  "target_buffer_chars": 160
}
```

Send text chunks:

```json
{
  "type": "text",
  "text": "Primeiro trecho vindo do LLM."
}
```

Force synthesis of remaining text:

```json
{
  "type": "flush"
}
```

Finish the stream:

```json
{
  "type": "close"
}
```

### Server Messages

Metadata:

```json
{
  "type": "audio_format",
  "encoding": "pcm_s16le",
  "sample_rate": 22050,
  "channels": 1,
  "sample_width_bytes": 2,
  "include_wav_header": false
}
```

The server then sends binary WebSocket frames containing raw PCM bytes.

If `include_wav_header=true`, the first binary frame is a WAV header followed by PCM chunks.

Control messages:

- `ready`
- `started`
- `segment_start`
- `segment_end`
- `done`
- `error`

### Latency Model

This streaming path is incremental, not full phoneme-level synthesis.

The service buffers incoming text and synthesizes as soon as:

- a sentence boundary is detected
- or the buffered text reaches the configured threshold

That keeps the protocol ready for real-time conversational streaming while staying compatible with the current Piper backend.
