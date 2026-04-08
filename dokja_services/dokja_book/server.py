import os
from contextlib import asynccontextmanager
from typing import Any

import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from book_summary_service import BookSummaryService
from classifier import HTTPBookClassifier, LocalBookClassifier
from overview_generator import ExtractiveOverviewGenerator, ExternalOverviewGenerator, TransformersOverviewGenerator


class BookRequest(BaseModel):
    title: str | None = None
    author: str | None = None
    filename: str | None = None
    resource_uri: str | None = None
    mime_type: str | None = None
    format: str | None = None
    book_type: str | None = None
    language: str | None = None
    goal: str | None = None
    content: str | None = None
    resource_bytes_b64: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


@asynccontextmanager
async def lifespan(_: FastAPI):
    service = build_service()
    app.state.book_service = service
    yield


app = FastAPI(title="dokja-book", lifespan=lifespan)


def build_service() -> BookSummaryService:
    classifier_provider = os.getenv("DOKJA_BOOK_CLASSIFIER_PROVIDER", "local").strip().lower() or "local"
    overview_provider = os.getenv("DOKJA_BOOK_OVERVIEW_PROVIDER", "extractive").strip().lower() or "extractive"
    return BookSummaryService(
        default_language=os.getenv("DOKJA_BOOK_DEFAULT_LANGUAGE", "pt-BR"),
        default_goal=os.getenv("DOKJA_BOOK_DEFAULT_GOAL", "study"),
        classifier=build_classifier(classifier_provider),
        overview_generator=build_overview_generator(overview_provider),
    )


def build_classifier(provider: str):
    if provider == "external":
        endpoint = os.getenv("DOKJA_BOOK_CLASSIFIER_ENDPOINT", "").strip()
        if endpoint == "":
            raise ValueError("DOKJA_BOOK_CLASSIFIER_ENDPOINT is required for external classifier provider")
        return HTTPBookClassifier(endpoint=endpoint)
    return LocalBookClassifier(
        model_name=os.getenv("DOKJA_BOOK_CLASSIFIER_MODEL", "facebook/bart-large-mnli"),
        model_cache_dir=os.getenv("DOKJA_BOOK_MODEL_CACHE_DIR"),
    )


def build_overview_generator(provider: str):
    if provider == "transformers":
        return TransformersOverviewGenerator(
            model_name=os.getenv("DOKJA_BOOK_OVERVIEW_MODEL", "sshleifer/distilbart-cnn-12-6"),
            model_cache_dir=os.getenv("DOKJA_BOOK_MODEL_CACHE_DIR"),
        )
    if provider == "external":
        endpoint = os.getenv("DOKJA_BOOK_OVERVIEW_ENDPOINT", "").strip()
        if endpoint == "":
            raise ValueError("DOKJA_BOOK_OVERVIEW_ENDPOINT is required for external overview provider")
        return ExternalOverviewGenerator(endpoint=endpoint)
    return ExtractiveOverviewGenerator()


@app.get("/health")
async def health() -> dict[str, Any]:
    return {"status": "ok", "service": "dokja-book"}


@app.post("/books/classify")
async def classify_book(request: BookRequest) -> dict[str, Any]:
    try:
        result = app.state.book_service.classify(request.model_dump())
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "result": result}


@app.post("/books/summarize")
async def summarize_book(request: BookRequest) -> dict[str, Any]:
    try:
        result = app.state.book_service.summarize(request.model_dump())
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "result": result}


def run() -> None:
    uvicorn.run(
        "server:app",
        host=os.getenv("DOKJA_BOOK_HOST", "0.0.0.0"),
        port=int(os.getenv("DOKJA_BOOK_PORT", "8083")),
        reload=False,
    )
