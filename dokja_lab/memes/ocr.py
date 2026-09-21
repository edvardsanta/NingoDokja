"""Reads the text printed on a meme image so the word blacklist can see it."""

from __future__ import annotations

import os

from logging_config import get_logger

logger = get_logger(__name__)

MODEL_FILES = {
    "det_model_path": "ch_PP-OCRv3_det_infer.onnx",
    "rec_model_path": "ch_PP-OCRv3_rec_infer.onnx",
    "cls_model_path": "ch_ppocr_mobile_v2.0_cls_infer.onnx",
}


class TextReader:
    def __init__(self, model_dir: str | None = None, engine=None):
        self.model_dir = model_dir or None
        self._engine = engine

    def read(self, image) -> str:
        """Return the text found in a BGR image array, one detected line per row."""
        result, _ = self._get_engine()(image)
        return "\n".join(line[1] for line in (result or []))

    def _get_engine(self):
        if self._engine is not None:
            return self._engine

        kwargs = {}
        if self.model_dir:
            for key, filename in MODEL_FILES.items():
                path = os.path.join(self.model_dir, filename)
                if not os.path.isfile(path):
                    raise FileNotFoundError(f"ocr model not found at {path}")
                kwargs[key] = path

        from rapidocr_onnxruntime import RapidOCR

        self._engine = RapidOCR(**kwargs)
        logger.info("ocr model loaded dir=%s", self.model_dir or "bundled")
        return self._engine
