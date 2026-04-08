import logging
import os
from contextlib import asynccontextmanager
from typing import Literal

from fastapi import FastAPI, HTTPException, Request, Response, WebSocket, WebSocketDisconnect
from pydantic import BaseModel, Field

from piper_tts_service import PiperTTSService
from openwakeword_service import OpenWakeWordService
from streaming import (
    DEFAULT_AUDIO_CHUNK_BYTES,
    DEFAULT_MIN_BUFFER_CHARS,
    DEFAULT_TARGET_BUFFER_CHARS,
    build_wav_header,
    chunk_bytes,
    parse_wav_bytes,
    pop_streamable_segment,
)
from stub_stt_service import StubSTTService
from stub_wakeword_service import StubWakewordService
from stub_tts_service import StubTTSService
from transformers_stt_service import TransformersSTTService, decode_audio_b64

logger = logging.getLogger("dokja_voice.http")


class TTSRequest(BaseModel):
    text: str = Field(..., min_length=1)
    voice: str = "default"
    language: str = "pt-BR"
    format: Literal["wav"] = "wav"


class STTRequest(BaseModel):
    audio_bytes_b64: str = Field(..., min_length=1)
    audio_format: Literal["wav"] = "wav"
    language: str = "pt-BR"


class WakewordRequest(BaseModel):
    audio_bytes_b64: str = Field(..., min_length=1)
    audio_format: Literal["wav"] = "wav"
    hotword: str = "Ningo"


class TTSStreamStartRequest(BaseModel):
    voice: str = "default"
    language: str = "pt-BR"
    output_format: Literal["pcm_s16le"] = "pcm_s16le"
    include_wav_header: bool = False
    chunk_bytes: int = Field(default=DEFAULT_AUDIO_CHUNK_BYTES, ge=512, le=65536)
    min_buffer_chars: int = Field(default=DEFAULT_MIN_BUFFER_CHARS, ge=1, le=4096)
    target_buffer_chars: int = Field(default=DEFAULT_TARGET_BUFFER_CHARS, ge=8, le=8192)


class TTSStreamTextRequest(BaseModel):
    text: str = Field(..., min_length=1)


class TTSStreamControlRequest(BaseModel):
    type: Literal["flush", "close"]


class TTSStreamState:
    def __init__(self) -> None:
        self.started = False
        self.voice = "default"
        self.language = "pt-BR"
        self.output_format = "pcm_s16le"
        self.include_wav_header = False
        self.chunk_bytes = DEFAULT_AUDIO_CHUNK_BYTES
        self.min_buffer_chars = DEFAULT_MIN_BUFFER_CHARS
        self.target_buffer_chars = DEFAULT_TARGET_BUFFER_CHARS
        self.pending_text = ""
        self.audio_format_sent = False


@asynccontextmanager
async def lifespan(_app: FastAPI):
    logger.info(
        "starting dokja voice server host=%s port=%s provider=%s",
        os.getenv("DOKJA_VOICE_HOST", "0.0.0.0"),
        os.getenv("DOKJA_VOICE_PORT", "8081"),
        os.getenv("DOKJA_VOICE_PROVIDER", "piper").strip().lower(),
    )
    yield


def create_app() -> FastAPI:
    tts_provider = os.getenv("DOKJA_VOICE_PROVIDER", "piper").strip().lower()
    if tts_provider == "piper":
        tts_service = PiperTTSService(
            model_path=os.getenv("DOKJA_VOICE_PIPER_MODEL", ""),
            default_voice=os.getenv("DOKJA_VOICE_DEFAULT_VOICE", "default"),
            default_language=os.getenv("DOKJA_VOICE_DEFAULT_LANGUAGE", "pt-BR"),
        )
    else:
        tts_service = StubTTSService(
            default_voice=os.getenv("DOKJA_VOICE_DEFAULT_VOICE", "default"),
            default_language=os.getenv("DOKJA_VOICE_DEFAULT_LANGUAGE", "pt-BR"),
        )

    stt_provider = os.getenv("DOKJA_VOICE_STT_PROVIDER", "stub").strip().lower()
    if stt_provider == "transformers":
        stt_service = TransformersSTTService(
            model_name=os.getenv("DOKJA_VOICE_STT_MODEL", "openai/whisper-tiny"),
            default_language=os.getenv("DOKJA_VOICE_DEFAULT_LANGUAGE", "pt-BR"),
            model_cache_dir=os.getenv("DOKJA_VOICE_MODEL_CACHE_DIR"),
        )
    else:
        stt_service = StubSTTService(
            default_language=os.getenv("DOKJA_VOICE_DEFAULT_LANGUAGE", "pt-BR"),
        )

    wakeword_provider = os.getenv("DOKJA_VOICE_WAKEWORD_PROVIDER", "stub").strip().lower()
    if wakeword_provider == "openwakeword":
        wakeword_service = OpenWakeWordService(
            model_path=os.getenv("DOKJA_VOICE_WAKEWORD_MODEL", ""),
            hotword=os.getenv("DOKJA_VOICE_WAKEWORD_HOTWORD", "Ningo"),
            threshold=float(os.getenv("DOKJA_VOICE_WAKEWORD_THRESHOLD", "0.5")),
        )
    else:
        wakeword_service = StubWakewordService(
            hotword=os.getenv("DOKJA_VOICE_WAKEWORD_HOTWORD", "Ningo"),
        )

    app = FastAPI(lifespan=lifespan)

    @app.get("/health")
    async def health(request: Request) -> dict:
        logger.info("received health request remote=%s", request.client.host if request.client else "")
        return {
            "status": "ok",
            "tts_provider": tts_provider,
            "stt_provider": stt_provider,
            "wakeword_provider": wakeword_provider,
        }

    @app.post("/tts")
    async def tts(payload: TTSRequest, request: Request) -> Response:
        logger.info(
            "received tts request remote=%s text_chars=%d voice=%s language=%s format=%s",
            request.client.host if request.client else "",
            len(payload.text),
            payload.voice,
            payload.language,
            payload.format,
        )

        if payload.format != "wav":
            raise HTTPException(status_code=400, detail="only wav format is supported in the initial scaffold")

        try:
            audio = tts_service.synthesize(
                text=payload.text,
                voice=payload.voice,
                language=payload.language,
                output_format=payload.format,
            )
        except Exception as exc:
            logger.exception("tts request failed")
            raise HTTPException(status_code=400, detail=str(exc)) from exc

        return Response(
            content=audio,
            media_type="audio/wav",
            headers={
                "X-Dokja-Voice-Format": payload.format,
                "X-Dokja-Voice-Voice": payload.voice,
                "X-Dokja-Voice-Language": payload.language,
            },
        )

    @app.post("/stt")
    async def stt(payload: STTRequest, request: Request) -> dict:
        logger.info(
            "received stt request remote=%s audio_format=%s language=%s audio_chars=%d",
            request.client.host if request.client else "",
            payload.audio_format,
            payload.language,
            len(payload.audio_bytes_b64),
        )

        try:
            audio_bytes = decode_audio_b64(payload.audio_bytes_b64)
            text = stt_service.transcribe(
                audio_bytes=audio_bytes,
                audio_format=payload.audio_format,
                language=payload.language,
            )
        except Exception as exc:
            logger.exception("stt request failed")
            raise HTTPException(status_code=400, detail=str(exc)) from exc

        return {
            "status": "ok",
            "text": text,
            "audio_format": payload.audio_format,
            "language": payload.language,
        }

    @app.post("/wakeword")
    async def wakeword(payload: WakewordRequest, request: Request) -> dict:
        logger.info(
            "received wakeword request remote=%s audio_format=%s hotword=%s audio_chars=%d",
            request.client.host if request.client else "",
            payload.audio_format,
            payload.hotword,
            len(payload.audio_bytes_b64),
        )

        try:
            audio_bytes = decode_audio_b64(payload.audio_bytes_b64)
            result = wakeword_service.detect(
                audio_bytes=audio_bytes,
                audio_format=payload.audio_format,
                hotword=payload.hotword,
            )
        except Exception as exc:
            logger.exception("wakeword request failed")
            raise HTTPException(status_code=400, detail=str(exc)) from exc

        return {
            "status": "ok",
            "detected": bool(result.get("detected", False)),
            "score": float(result.get("score", 0.0)),
            "hotword": str(result.get("hotword", payload.hotword)),
            "provider": str(result.get("provider", wakeword_provider)),
        }

    @app.websocket("/tts/stream")
    async def tts_stream(websocket: WebSocket) -> None:
        await websocket.accept()
        state = TTSStreamState()
        await websocket.send_json(
            {
                "type": "ready",
                "protocol": "dokja.voice.tts.v1",
                "transport": "websocket",
                "input": ["start", "text", "flush", "close"],
                "output": ["audio_format", "segment_start", "segment_end", "done", "error"],
            }
        )

        try:
            while True:
                message = await websocket.receive_json()
                message_type = str(message.get("type", "")).strip().lower()

                if message_type == "start":
                    payload = TTSStreamStartRequest(**message)
                    state.started = True
                    state.voice = payload.voice
                    state.language = payload.language
                    state.output_format = payload.output_format
                    state.include_wav_header = payload.include_wav_header
                    state.chunk_bytes = payload.chunk_bytes
                    state.min_buffer_chars = payload.min_buffer_chars
                    state.target_buffer_chars = payload.target_buffer_chars
                    state.pending_text = ""
                    state.audio_format_sent = False
                    await websocket.send_json(
                        {
                            "type": "started",
                            "voice": state.voice,
                            "language": state.language,
                            "output_format": state.output_format,
                            "include_wav_header": state.include_wav_header,
                            "chunk_bytes": state.chunk_bytes,
                        }
                    )
                    continue

                if not state.started:
                    raise ValueError("stream must start with a 'start' message")

                if message_type == "text":
                    payload = TTSStreamTextRequest(**message)
                    state.pending_text += f" {payload.text}"
                    await _drain_ready_segments(websocket, tts_service, state, force=False)
                    continue

                if message_type == "flush":
                    _ = TTSStreamControlRequest(**message)
                    await _drain_ready_segments(websocket, tts_service, state, force=True)
                    await websocket.send_json({"type": "done", "pending_text_chars": len(state.pending_text.strip())})
                    continue

                if message_type == "close":
                    _ = TTSStreamControlRequest(**message)
                    await _drain_ready_segments(websocket, tts_service, state, force=True)
                    await websocket.send_json({"type": "done", "pending_text_chars": len(state.pending_text.strip())})
                    await websocket.close()
                    return

                raise ValueError(f"unsupported stream message type: {message_type}")
        except WebSocketDisconnect:
            logger.info("tts websocket disconnected")
        except Exception as exc:
            logger.exception("tts websocket failed")
            await _send_ws_error(websocket, str(exc))
            await websocket.close(code=1011)

    return app


async def _drain_ready_segments(
    websocket: WebSocket,
    service: PiperTTSService | StubTTSService,
    state: TTSStreamState,
    *,
    force: bool,
) -> None:
    while True:
        segment, remainder = pop_streamable_segment(
            state.pending_text,
            min_chars=state.min_buffer_chars,
            target_chars=state.target_buffer_chars,
            force=force,
        )
        if not segment:
            state.pending_text = remainder
            return
        state.pending_text = remainder
        await _stream_segment(websocket, service, state, segment)
        if force:
            force = False


async def _stream_segment(
    websocket: WebSocket,
    service: PiperTTSService | StubTTSService,
    state: TTSStreamState,
    text_segment: str,
) -> None:
    logger.info(
        "streaming tts segment text_chars=%d voice=%s language=%s",
        len(text_segment),
        state.voice,
        state.language,
    )
    wav_bytes = service.synthesize(
        text=text_segment,
        voice=state.voice,
        language=state.language,
        output_format="wav",
    )
    pcm_format, pcm_bytes = parse_wav_bytes(wav_bytes)

    if not state.audio_format_sent:
        await websocket.send_json(
            {
                "type": "audio_format",
                "encoding": state.output_format,
                "sample_rate": pcm_format.sample_rate,
                "channels": pcm_format.channels,
                "sample_width_bytes": pcm_format.sample_width_bytes,
                "include_wav_header": state.include_wav_header,
            }
        )
        if state.include_wav_header:
            await websocket.send_bytes(build_wav_header(pcm_format))
        state.audio_format_sent = True

    await websocket.send_json({"type": "segment_start", "text_chars": len(text_segment)})
    for chunk in chunk_bytes(pcm_bytes, state.chunk_bytes):
        await websocket.send_bytes(chunk)
    await websocket.send_json({"type": "segment_end", "text_chars": len(text_segment)})


async def _send_ws_error(websocket: WebSocket, detail: str) -> None:
    try:
        await websocket.send_json({"type": "error", "detail": detail})
    except Exception:
        logger.debug("failed to send websocket error payload")
