# Dokja Voice

Standalone FastAPI service for Dokja voice synthesis.

Local speech providers for synthesis, recognition and speech-activity detection.

Current status:

- Piper TTS and Whisper STT
- WebRTC speech detection before Whisper and for short activity probes
- supports streaming TTS over WebSocket
- supports:
  - a local stub synthesizer
  - `piper` via Python API

Endpoints:

- `GET /health`
- `POST /tts`
- `POST /stt`
- `POST /activity`: a WAV probe up to 640 ms; returns `speech`, `speech_ms` and `speech_ratio`
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

## Continuous Discord listening

Install both `requirements.txt` and `requirements-vad.txt`; both Dockerfiles do
this. Activity probes use WebRTC VAD in aggressive mode. Whisper rejects silence,
short segments and non-speech before bounded deterministic decoding. This avoids
using a language decoder as a noise detector. STT runs outside the HTTP event
loop with serialized access to the shared model.

The service detects speech; the Chat domain decides whether to answer. The
Discord adapter keeps capturing speech while finishing playback, then submits
the collected next turn. Voice requests pass through the orchestrator.

Run provider regressions in an environment with the service dependencies:
`python -m unittest discover -s dokja_services/dokja_voice -p 'test_*.py' -v`.

## Audio conversion and optional GPU recognition

Recognition and VAD receive mono PCM16 at 16 kHz. Rate conversion now uses a
polyphase low-pass FIR resampler before decimation, including the Discord
48 kHz stereo path. It preserves amplitude instead of boosting quiet noise;
regression tests cover speech-band gain, out-of-band alias rejection, fractional
rates, digital silence and integer overflow. This is signal preservation, not
an echo canceller or a claim that inaccurate speech is now understood correctly.
Per-transcription logs include normalized RMS/peak dBFS and clipped percentage,
never audio samples or transcript text.

The default Transformers image installs CPU PyTorch. Its adapter now selects an
accelerator only when the installed runtime reports one, otherwise CPU. GPU
hardware on the host alone does not accelerate that image; a compatible inference
build and explicitly exposed device are both required.

For an AMD GPU with a working Vulkan render node, the optional dev/lite overlay
`docker-compose.voice-gpu.yml` adds a pinned Whisper server image. The server
keeps the explicitly selected GGML model loaded; `whisper_cpp` forwards only
normalized, speech-gated WAV segments over a private provider network. It has
no published port and no connection to the application network. Provider logs
are disabled, the model mount is read-only, and uploaded WAV data is handled in
memory (the optional upstream ffmpeg file converter is not enabled).

Steady background noise can pass the speech gate, and Whisper then invents short
phrases from it. `whisper_cpp` asks the server for `verbose_json` and keeps only
confident segments: an average log probability of at least -0.8 and a mean word
probability of at least 0.5. Punctuation-only segments are always dropped. The
server's `no_speech_prob` stays near zero even for pure noise, so it is not used.
Each `stt completed` log line reports `segments_kept`, `segments_dropped`,
`min_avg_logprob` and `min_word_prob`, never the text, so the thresholds
(`MIN_AVG_LOGPROB`, `MIN_WORD_PROBABILITY`) can be tuned from real rooms.

Set `DOKJA_VOICE_CPP_MODEL_FILE` to an existing multilingual GGML model file and,
if needed, `DOKJA_VOICE_RENDER_DEVICE` to its host render node. The overlay selects
`DOKJA_VOICE_STT_PROVIDER=whisper_cpp` and the internal
`DOKJA_VOICE_CPP_ENDPOINT`. Apply it with the dev or lite base and rebuild voice
for its pinned resampling dependency. Base stacks retain their existing defaults;
production voice deployments must provision/review their own provider topology.
Do not apply this optional overlay directly to a production base that has no
voice service definition.

The HTTP adapter accepts pt, en and auto, sends no accumulated participant
history, bounds segments at 30 seconds and responses at 64 KiB, refuses redirects
and uses a ten-second request timeout. Backend failures are explicit, with no
silent switch to stub recognition. This provider remains segment-final, not
native streaming partial transcription. Check live health `stt_provider` before
comparing results; old development settings can still select tiny on CPU.
