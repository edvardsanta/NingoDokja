import io
import wave

import numpy as np


def load_wav_bytes(audio_bytes: bytes) -> tuple[np.ndarray, int, int]:
    with wave.open(io.BytesIO(audio_bytes), "rb") as wav_file:
        channels = wav_file.getnchannels()
        sample_width = wav_file.getsampwidth()
        sample_rate = wav_file.getframerate()
        frame_count = wav_file.getnframes()
        frames = wav_file.readframes(frame_count)

    if sample_width != 2:
        raise ValueError("only 16-bit PCM wav input is supported")

    pcm = np.frombuffer(frames, dtype=np.int16)
    if channels > 1:
        pcm = pcm.reshape(-1, channels)
    return pcm, sample_rate, channels


def wav_bytes_to_mono_16k(audio_bytes: bytes) -> np.ndarray:
    pcm, sample_rate, channels = load_wav_bytes(audio_bytes)

    if channels > 1:
        mono = pcm.astype(np.float32).mean(axis=1)
    else:
        mono = pcm.astype(np.float32)

    if sample_rate == 16_000:
        return mono.astype(np.int16)

    source_length = mono.shape[0]
    if source_length == 0:
        return np.zeros(0, dtype=np.int16)

    target_length = max(1, int(round(source_length * 16_000 / sample_rate)))
    source_positions = np.linspace(0, source_length - 1, num=source_length, dtype=np.float32)
    target_positions = np.linspace(0, source_length - 1, num=target_length, dtype=np.float32)
    resampled = np.interp(target_positions, source_positions, mono)
    return np.clip(resampled, -32768, 32767).astype(np.int16)
