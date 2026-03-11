import logging
import os
from typing import Literal

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field, model_validator

from openai_chat_service import OpenAIChatService

logger = logging.getLogger("dokja_chat_ai.http")


class ChatRequest(BaseModel):
    message: str = ""
    messages: list["ChatMessage"] = Field(default_factory=list)
    temperature: float = 0

    @model_validator(mode="after")
    def validate_input(self) -> "ChatRequest":
        if self.messages:
            return self
        if not self.message.strip():
            raise ValueError("message must be a non-empty string when messages are absent")
        return self


class ChatMessage(BaseModel):
    role: Literal["system", "user", "assistant"] = "user"
    content: str = Field(..., min_length=1)


def create_app() -> FastAPI:
    service = OpenAIChatService(
        api_key=os.getenv("CHAT_AI_API_KEY", ""),
        base_url=os.getenv("CHAT_AI_BASE_URL", ""),
        model=os.getenv("CHAT_AI_MODEL", "n/a"),
    )

    app = FastAPI()

    @app.on_event("startup")
    async def log_startup() -> None:
        logger.info(
            "starting chat ai server host=%s port=%s model=%s base_url=%s",
            os.getenv("CHAT_AI_SERVICE_HOST", "0.0.0.0"),
            os.getenv("CHAT_AI_SERVICE_PORT", "8080"),
            os.getenv("CHAT_AI_MODEL", "n/a"),
            os.getenv("CHAT_AI_BASE_URL", ""),
        )

    @app.post("/chat")
    async def chat(payload: ChatRequest, request: Request) -> dict:
        message = payload.message.strip()
        messages = [
            {"role": entry.role, "content": entry.content.strip()}
            for entry in payload.messages
            if entry.content.strip()
        ]
        if not messages and message:
            messages = [{"role": "user", "content": message}]
        logger.info(
            "received chat request remote=%s path=%s message_chars=%d history_messages=%d temperature=%s",
            request.client.host if request.client else "",
            request.url.path,
            len(message),
            len(messages),
            payload.temperature,
        )

        if not messages:
            raise HTTPException(status_code=400, detail="messages must include at least one non-empty message")

        try:
            result = service.send_messages(messages, temperature=payload.temperature)
        except Exception as exc:
            logger.exception("chat request failed path=%s", request.url.path)
            raise HTTPException(status_code=400, detail=str(exc)) from exc

        logger.info("sending chat success response path=%s", request.url.path)
        return {"status": "ok", "result": result}

    @app.get("/health")
    async def health(request: Request) -> dict:
        logger.info("received health request remote=%s", request.client.host if request.client else "")
        return {"status": "ok"}

    return app
