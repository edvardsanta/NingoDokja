import io
import wave
from dataclasses import dataclass


DEFAULT_AUDIO_CHUNK_BYTES = 4096
DEFAULT_MIN_BUFFER_CHARS = 48
DEFAULT_TARGET_BUFFER_CHARS = 160


@dataclass(frozen=True)
class PCMFormat:
    sample_rate: int
    channels: int
    sample_width_bytes: int


def parse_wav_bytes(audio: bytes) -> tuple[PCMFormat, bytes]:
    with wave.open(io.BytesIO(audio), "rb") as wav_file:
        pcm_format = PCMFormat(
            sample_rate=wav_file.getframerate(),
            channels=wav_file.getnchannels(),
            sample_width_bytes=wav_file.getsampwidth(),
        )
        pcm_bytes = wav_file.readframes(wav_file.getnframes())
    return pcm_format, pcm_bytes


def build_wav_header(pcm_format: PCMFormat) -> bytes:
    buffer = io.BytesIO()
    with wave.open(buffer, "wb") as wav_file:
        wav_file.setnchannels(pcm_format.channels)
        wav_file.setsampwidth(pcm_format.sample_width_bytes)
        wav_file.setframerate(pcm_format.sample_rate)
        wav_file.writeframes(b"")
    return buffer.getvalue()


def chunk_bytes(payload: bytes, chunk_size: int = DEFAULT_AUDIO_CHUNK_BYTES) -> list[bytes]:
    if chunk_size <= 0:
        raise ValueError("chunk_size must be greater than zero")
    return [payload[idx : idx + chunk_size] for idx in range(0, len(payload), chunk_size)]


def pop_streamable_segment(
    buffer: str,
    *,
    min_chars: int = DEFAULT_MIN_BUFFER_CHARS,
    target_chars: int = DEFAULT_TARGET_BUFFER_CHARS,
    force: bool = False,
) -> tuple[str, str]:
    normalized = buffer.lstrip()
    if not normalized:
        return "", ""

    if force:
        return normalized.strip(), ""

    if len(normalized) < min_chars:
        return "", normalized

    scan_limit = min(len(normalized), max(target_chars, min_chars))
    candidate = normalized[:scan_limit]

    punctuation_indexes = [candidate.rfind(token) for token in (". ", "! ", "? ", ".\n", "!\n", "?\n", ":", ";")]
    split_at = max(punctuation_indexes)
    if split_at >= 0:
        segment = normalized[: split_at + 1].strip()
        remainder = normalized[split_at + 1 :].lstrip()
        return segment, remainder

    if len(normalized) >= target_chars:
        whitespace_at = max(candidate.rfind(" "), candidate.rfind("\n"), candidate.rfind("\t"))
        if whitespace_at >= min_chars:
            segment = normalized[:whitespace_at].strip()
            remainder = normalized[whitespace_at:].lstrip()
            return segment, remainder

    return "", normalized
