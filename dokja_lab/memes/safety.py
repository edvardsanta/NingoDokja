"""NSFW screening applied to memes right before they are delivered."""

from __future__ import annotations

import os
import re
import unicodedata
from dataclasses import dataclass
from typing import Any, Iterable
from urllib.parse import urlsplit

import requests

from logging_config import get_logger

logger = get_logger(__name__)

# NudeNet labels that mean explicit content.
BLOCKED_LABELS = frozenset(
    {
        "FEMALE_GENITALIA_EXPOSED",
        "MALE_GENITALIA_EXPOSED",
        "ANUS_EXPOSED",
        "FEMALE_BREAST_EXPOSED",
        "BUTTOCKS_EXPOSED",
    }
)

# Suggestive but clothed body parts (swimwear, underwear, tight clothes, butt/chest
# close-ups). Blocked in strict mode, with a higher threshold since the model is
# less certain about these. Non-sexual parts (face, feet, belly, armpits) stay allowed.
STRICT_LABELS = frozenset(
    {
        "FEMALE_GENITALIA_COVERED",
        "FEMALE_BREAST_COVERED",
        "BUTTOCKS_COVERED",
        "ANUS_COVERED",
    }
)

# Matched as whole words, ignoring case and accents. Words that are also common in
# innocent memes (pelada, gostosa, rola, foda, palavrões) are left out on purpose.
BLOCKED_WORDS = (
    # marcadores gerais
    "nsfw",
    "18+",
    "xxx",
    "rule34",
    "r34",
    "hentai",
    "ecchi",
    "lewd",
    "erotico",
    "erotica",
    "erotic",
    "fetiche",
    "fetish",
    "bdsm",
    # pornografia e nudez
    "porn",
    "porno",
    "pornografia",
    "pornhub",
    "xvideos",
    "onlyfans",
    "nude",
    "nudes",
    "nudez",
    "nua",
    "naked",
    "stripper",
    "camgirl",
    "milf",
    "putaria",
    "orgia",
    # sexo e gíria sexual (pt-br)
    "sexo",
    "sexual",
    "transa",
    "transar",
    "transei",
    "transou",
    "transando",
    "torava",
    "toravam",
    "toravamos",
    "torei",
    "torou",
    "torando",
    "tesao",
    "safada",
    "safado",
    "safadeza",
    "ninfeta",
    "boquete",
    "punheta",
    "siririca",
    "masturbacao",
    "masturbar",
    "fudendo",
    # partes do corpo
    "buceta",
    "xoxota",
    "piroca",
    "penis",
    "vagina",
    "bunda",
    "bundinha",
    "raba",
    "rabuda",
    "peitos",
    "seios",
    "mamilo",
    "mamilos",
    # sexo e partes do corpo (en)
    "sex",
    "boobs",
    "tits",
    "pussy",
    "dick",
    "cumshot",
    "blowjob",
    "handjob",
    "anal",
)


def _fold(text: str) -> str:
    """Lowercase and strip accents so "Pornô" and "porno" match the same entry."""
    decomposed = unicodedata.normalize("NFKD", str(text or "").casefold())
    return "".join(ch for ch in decomposed if not unicodedata.combining(ch))


def _compile_words(words: Iterable[str]) -> re.Pattern | None:
    folded = sorted({_fold(word).strip() for word in words if _fold(word).strip()})
    if not folded:
        return None
    return re.compile(
        r"(?<!\w)(?:" + "|".join(re.escape(word) for word in folded) + r")(?!\w)"
    )


DEFAULT_THRESHOLD = 0.4
DEFAULT_STRICT_THRESHOLD = 0.5
DEFAULT_MAX_BYTES = 8_000_000
DEFAULT_TIMEOUT_SECONDS = 15


# OCR sometimes glues words together ("voceTORAVAe"), so long blacklist entries are
# also searched with the spaces removed. Short ones would match inside innocent words
# ("sex" in "sexta"), and these long ones are ambiguous ("sexual" in "bissexual").
MIN_GLUED_WORD_LENGTH = 6
WHOLE_WORD_ONLY = frozenset({"sexual"})


# The screen decodes still images only, so a video can never be approved.
VIDEO_EXTENSIONS = (".mp4", ".webm", ".mov", ".m4v", ".mkv", ".avi")


def _is_video(url: str) -> bool:
    return urlsplit(str(url or "")).path.lower().endswith(VIDEO_EXTENSIONS)


class ScreenUnavailable(RuntimeError):
    """The screening model could not run. Nothing should be delivered."""


@dataclass(frozen=True)
class Verdict:
    safe: bool
    reason: str = ""


@dataclass(frozen=True)
class Screening:
    """A verdict plus what the models saw, for calibrating thresholds."""

    verdict: Verdict
    detections: tuple = ()
    text: str = ""


class NsfwScreen:
    def __init__(
        self,
        model_path: str | None = None,
        threshold: float = DEFAULT_THRESHOLD,
        strict: bool = True,
        strict_threshold: float = DEFAULT_STRICT_THRESHOLD,
        blocked_words: Iterable[str] = BLOCKED_WORDS,
        text_reader: Any = None,
        max_bytes: int = DEFAULT_MAX_BYTES,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        session: Any = None,
        detector: Any = None,
    ) -> None:
        self.model_path = model_path or None
        self.threshold = threshold
        self.strict = strict
        self.strict_threshold = strict_threshold
        self._blocked_words_re = _compile_words(blocked_words)
        self._glued_words = tuple(
            word
            for word in sorted({_fold(w).strip() for w in blocked_words})
            if len(word) >= MIN_GLUED_WORD_LENGTH and word not in WHOLE_WORD_ONLY
        )
        self._text_reader = text_reader
        self.max_bytes = max_bytes
        self.timeout = timeout
        self._session = session or requests.Session()
        self._detector = detector

    def check(self, meme: Any) -> Verdict:
        return self.inspect(meme).verdict

    def inspect(self, meme: Any) -> Screening:
        caption = " ".join(str(part or "") for part in (meme.title, meme.tags))
        word = self._find_blocked_word(caption)
        if word:
            return Screening(Verdict(False, f"blocked word {word!r}"))

        if _is_video(meme.url):
            # Say so instead of downloading a whole video to fail on decoding it.
            return Screening(
                Verdict(
                    False,
                    "video is not screened, so it is not sent to restricted channels",
                )
            )

        try:
            raw = self._download(meme.url)
        except Exception as err:
            return Screening(Verdict(False, f"download failed: {err}"))

        image = self._decode(raw)
        if image is None:
            return Screening(Verdict(False, "unsupported or unreadable media"))

        text = ""
        if self._text_reader is not None:
            try:
                text = self._text_reader.read(image)
            except Exception as err:
                raise ScreenUnavailable(f"ocr failed: {err}") from err
            word = self._find_blocked_word(text, glued=True)
            if word:
                return Screening(
                    Verdict(False, f"blocked word {word!r} in image text"), (), text
                )

        try:
            detections = tuple(self._get_detector().detect(image))
        except ScreenUnavailable:
            raise
        except Exception as err:
            raise ScreenUnavailable(f"nsfw detector failed: {err}") from err

        return Screening(self._judge(detections), detections, text)

    def _find_blocked_word(self, text: str, glued: bool = False) -> str | None:
        folded = _fold(text)
        if self._blocked_words_re:
            match = self._blocked_words_re.search(folded)
            if match:
                return match.group(0)
        if glued:
            squeezed = re.sub(r"\s+", "", folded)
            for word in self._glued_words:
                if word in squeezed:
                    return word
        return None

    def _judge(self, detections: Iterable[dict[str, Any]]) -> Verdict:
        for detection in detections:
            label = detection.get("class")
            score = float(detection.get("score", 0))
            if label in BLOCKED_LABELS and score >= self.threshold:
                return Verdict(False, f"{label} score={score:.2f}")
            if (
                self.strict
                and label in STRICT_LABELS
                and score >= self.strict_threshold
            ):
                return Verdict(False, f"{label} score={score:.2f} (strict)")
        return Verdict(True)

    def _download(self, url: str) -> bytes:
        with self._session.get(url, timeout=self.timeout, stream=True) as response:
            response.raise_for_status()
            declared = int(response.headers.get("Content-Length") or 0)
            if declared > self.max_bytes:
                raise ValueError(f"file too large ({declared} bytes)")
            data = bytearray()
            for chunk in response.iter_content(chunk_size=65536):
                data.extend(chunk)
                if len(data) > self.max_bytes:
                    raise ValueError("file too large")
        return bytes(data)

    @staticmethod
    def _decode(raw: bytes) -> Any:
        import cv2
        import numpy as np

        return cv2.imdecode(np.frombuffer(raw, np.uint8), cv2.IMREAD_COLOR)

    def _get_detector(self) -> Any:
        if self._detector is not None:
            return self._detector
        if self.model_path and not os.path.isfile(self.model_path):
            raise ScreenUnavailable(f"nsfw model not found at {self.model_path}")
        try:
            from nudenet import NudeDetector

            self._detector = NudeDetector(model_path=self.model_path)
        except Exception as err:
            raise ScreenUnavailable(f"could not load nsfw model: {err}") from err
        logger.info("nsfw model loaded path=%s", self.model_path or "bundled")
        return self._detector


def build_text_reader_from_env() -> Any:
    if os.getenv("MEME_NSFW_OCR", "on").strip().lower() in {"off", "0", "false"}:
        logger.warning("meme ocr is disabled; text inside images is not checked")
        return None
    from memes.ocr import TextReader

    return TextReader(model_dir=os.getenv("DOKJA_OCR_MODEL_DIR", "").strip() or None)


def build_screen_from_env() -> NsfwScreen | None:
    if os.getenv("MEME_NSFW_FILTER", "on").strip().lower() in {"off", "0", "false"}:
        logger.warning("meme nsfw filter is disabled")
        return None
    return NsfwScreen(
        model_path=os.getenv("DOKJA_NSFW_MODEL_PATH", "").strip() or None,
        threshold=float(os.getenv("MEME_NSFW_THRESHOLD", str(DEFAULT_THRESHOLD))),
        strict=os.getenv("MEME_NSFW_STRICT", "on").strip().lower()
        not in {"off", "0", "false"},
        strict_threshold=float(
            os.getenv("MEME_NSFW_STRICT_THRESHOLD", str(DEFAULT_STRICT_THRESHOLD))
        ),
        blocked_words=(
            *BLOCKED_WORDS,
            *_split_words(os.getenv("MEME_NSFW_EXTRA_WORDS", "")),
        ),
        text_reader=build_text_reader_from_env(),
    )


def _split_words(raw: str) -> list[str]:
    return [word.strip() for word in raw.split(",") if word.strip()]
