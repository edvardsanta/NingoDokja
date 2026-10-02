"""Reads the text printed on a meme image so the word blacklist can see it."""

from __future__ import annotations

import os
from typing import Any

from logging_config import get_logger

logger = get_logger(__name__)

MODEL_FILES = {
    "det_model_path": "ch_PP-OCRv3_det_infer.onnx",
    "rec_model_path": "ch_PP-OCRv3_rec_infer.onnx",
    "cls_model_path": "ch_ppocr_mobile_v2.0_cls_infer.onnx",
}


class TextReader:
    """Reads the text printed on an image with RapidOCR.

    `rec_model` swaps the text recognizer for another file in `model_dir`. The default
    one only knows Chinese and English letters, so it reads "coração" as "coracao"; a
    recognizer trained on the Latin alphabet keeps the accents. The detector and the
    orientation classifier do not depend on the language, so they stay as they are. The
    file must carry its character list (the ONNX `character` metadata), as RapidOCR's
    own converted models do.
    """

    def __init__(
        self,
        model_dir: str | None = None,
        engine: Any = None,
        rec_model: str | None = None,
    ) -> None:
        self.model_dir = model_dir or None
        self.rec_model = rec_model or None
        self._engine = engine

    def read(self, image: Any) -> str:
        """Return the text found in a BGR image array, one detected line per row."""
        result, _ = self._get_engine()(image)
        return "\n".join(line[1] for line in (result or []))

    def _get_engine(self) -> Any:
        if self._engine is not None:
            return self._engine

        if self.rec_model and not self.model_dir:
            raise ValueError(
                "a custom ocr recognizer needs the model directory (DOKJA_OCR_MODEL_DIR)"
            )
        if self.rec_model and os.path.basename(self.rec_model) != self.rec_model:
            raise ValueError(
                f"ocr recognizer must be a file name inside the model directory: {self.rec_model!r}"
            )

        kwargs = {}
        if self.model_dir:
            files = dict(MODEL_FILES)
            if self.rec_model:
                files["rec_model_path"] = self.rec_model
            for key, filename in files.items():
                path = os.path.join(self.model_dir, filename)
                if not os.path.isfile(path):
                    raise FileNotFoundError(f"ocr model not found at {path}")
                kwargs[key] = path

        from rapidocr_onnxruntime import RapidOCR

        self._engine = RapidOCR(**kwargs)
        logger.info(
            "ocr model loaded dir=%s recognizer=%s",
            self.model_dir or "bundled",
            self.rec_model or "default",
        )
        return self._engine
