"""Bounded LaTeX math rendering for chat formulas.

matplotlib's mathtext parses a real subset of LaTeX math — subscripts,
superscripts, fractions, accents, text — which the built-in Go fallback
cannot. Expressions arrive without $...$ delimiters; this module wraps,
renders on a white card (black on white reads on Discord's dark themes)
and bounds everything the same way the Go renderer does.
"""

import base64
import io
import logging
import re

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot  # noqa: E402

logger = logging.getLogger("dokja_math.render")

MAX_EXPRESSION_CHARS = 1024
MAX_PNG_BYTES = 256 << 10
MAX_WIDTH_PX = 2200
MAX_HEIGHT_PX = 1000
DPI = 200

_FORBIDDEN = re.compile(r"[$%#&\r\n\x00]")
_WEB_URL = re.compile(r"https?://\S+", re.IGNORECASE)


class MathRenderError(ValueError):
    """The expression is not renderable math."""


def render_math(expression: str) -> bytes:
    """Render one bounded LaTeX expression to PNG bytes, or raise MathRenderError."""
    if not expression or len(expression) > MAX_EXPRESSION_CHARS:
        raise MathRenderError("expression length out of bounds")
    if _FORBIDDEN.search(expression) or _WEB_URL.search(expression):
        raise MathRenderError("expression carries non-math content")

    figure = matplotlib.pyplot.figure(figsize=(0.01, 0.01), dpi=DPI)
    try:
        # A tiny invisible figure: mathtext measures and draws only the text.
        figure.text(0, 0, f"${expression}$", fontsize=20, color="black")
        buffer = io.BytesIO()
        try:
            figure.savefig(
                buffer,
                format="png",
                dpi=DPI,
                bbox_inches="tight",
                pad_inches=0.08,
                facecolor="white",
                edgecolor="none",
                transparent=False,
            )
        except ValueError as error:
            raise MathRenderError(f"unrenderable expression: {error}") from error
        data = buffer.getvalue()
    finally:
        matplotlib.pyplot.close(figure)

    if not data or len(data) > MAX_PNG_BYTES:
        raise MathRenderError("rendered image out of bounds")
    width, height = png_size(data)
    if width < 1 or height < 1 or width > MAX_WIDTH_PX or height > MAX_HEIGHT_PX:
        raise MathRenderError("rendered image size out of bounds")
    return data


def png_size(data: bytes) -> tuple[int, int]:
    """Read PNG dimensions from the IHDR chunk without decoding pixels."""
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n" or data[12:16] != b"IHDR":
        raise MathRenderError("renderer did not produce a PNG")
    width = int.from_bytes(data[16:20], "big")
    height = int.from_bytes(data[20:24], "big")
    return width, height


def render_math_b64(expression: str) -> str:
    """Base64-encode one render, for the wire shape the callers expect."""
    return base64.b64encode(render_math(expression)).decode("ascii")
