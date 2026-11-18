"""HTTP surface for the math renderer: one bounded render route and a status probe."""

import logging
import time

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field

from math_render import MathRenderError, render_math_b64

logger = logging.getLogger("dokja_math.http")


class MathRenderRequest(BaseModel):
    expression: str = Field(..., min_length=1, max_length=1024)


class MathRenderResponse(BaseModel):
    data_b64: str
    content_type: str = "image/png"


def create_app() -> FastAPI:
    app = FastAPI(title="dokja-math", docs_url=None, redoc_url=None, openapi_url=None)

    @app.get("/status")
    def status() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/math/render", response_model=MathRenderResponse)
    def render(request: MathRenderRequest, http: Request) -> MathRenderResponse:
        started = time.monotonic()
        try:
            data_b64 = render_math_b64(request.expression)
        except MathRenderError as error:
            raise HTTPException(status_code=422, detail=str(error)) from error
        logger.info(
            "math render expression_chars=%d image_b64_chars=%d duration_ms=%d",
            len(request.expression),
            len(data_b64),
            int((time.monotonic() - started) * 1000),
        )
        return MathRenderResponse(data_b64=data_b64)

    return app
