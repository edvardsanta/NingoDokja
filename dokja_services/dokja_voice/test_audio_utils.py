import io
import unittest
import wave

import numpy as np

from audio_utils import wav_bytes_to_mono_16k


def wav(pcm, rate):
    pcm = np.asarray(pcm, dtype=np.int16)
    output = io.BytesIO()
    with wave.open(output, "wb") as stream:
        stream.setnchannels(1 if pcm.ndim == 1 else pcm.shape[1])
        stream.setsampwidth(2)
        stream.setframerate(rate)
        stream.writeframes(pcm.tobytes())
    return output.getvalue()


def tone(frequency, rate=48000, seconds=0.25):
    t = np.arange(round(seconds * rate)) / rate
    return np.rint(12000 * np.sin(2 * np.pi * frequency * t)).astype(np.int16)


def rms(pcm):
    return float(np.sqrt(np.mean(pcm.astype(np.float64) ** 2)))


class AudioConversionTests(unittest.TestCase):
    def test_downsampling_rejects_out_of_band_aliases(self):
        audio = tone(10000)
        result = wav_bytes_to_mono_16k(wav(np.stack([audio, audio], axis=1), 48000))
        self.assertEqual(result.shape, (4000,))
        self.assertLess(rms(result[160:-160]) / rms(audio), 0.01)

    def test_speech_band_is_preserved_without_automatic_gain(self):
        audio = tone(1000)
        result = wav_bytes_to_mono_16k(wav(audio, 48000))
        self.assertAlmostEqual(rms(result[160:-160]) / rms(audio), 1, delta=0.02)
        self.assertEqual(result.dtype, np.int16)

    def test_fractional_conversion_preserves_duration_and_stereo_downmix(self):
        audio = tone(1000, 44100, 0.23)
        stereo = np.stack([audio, np.zeros_like(audio)], axis=1)
        result = wav_bytes_to_mono_16k(wav(stereo, 44100))
        self.assertEqual(len(result), round(len(audio) * 16000 / 44100))
        self.assertAlmostEqual(rms(result[160:-160]) / rms(audio), 0.5, delta=0.02)

    def test_native_rate_and_silence_remain_exact_and_overflow_is_clipped(self):
        native = np.array([-32768, -1000, 0, 1000, 32767], dtype=np.int16)
        np.testing.assert_array_equal(wav_bytes_to_mono_16k(wav(native, 16000)), native)
        np.testing.assert_array_equal(wav_bytes_to_mono_16k(wav(np.zeros(4800), 48000)), np.zeros(1600, dtype=np.int16))
        clipped = wav_bytes_to_mono_16k(wav(np.full(4800, 32767), 48000))
        self.assertTrue(np.all(clipped >= 0), "FIR overshoot wrapped signed PCM")

    def test_empty_input_has_no_invented_samples(self):
        self.assertEqual(wav_bytes_to_mono_16k(wav(np.zeros(0), 48000)).size, 0)


if __name__ == "__main__":
    unittest.main()
