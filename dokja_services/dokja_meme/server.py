from __future__ import annotations

import json
import os

import zmq

from bootstrap import build_service


def _get_logger():
    from logging_config import get_logger

    return get_logger(__name__)


class MemeServiceServer:
    def __init__(self, endpoint: str | None = None):
        self.endpoint = endpoint or os.getenv("MEME_SERVICE_ENDPOINT", "tcp://*:5557")
        self.context = zmq.Context()
        self.socket = self.context.socket(zmq.REP)
        self.service = build_service()
        self.logger = _get_logger()

    def start(self):
        self.socket.bind(self.endpoint)
        self.logger.info("meme service listening endpoint=%s", self.endpoint)

        try:
            while True:
                raw_request = self.socket.recv_string()
                self.logger.info("meme service received raw request chars=%d", len(raw_request))
                response = self.handle_request(raw_request)
                self.socket.send_string(json.dumps(response))
        finally:
            self.socket.close()
            self.context.term()

    def handle_request(self, raw_request: str) -> dict:
        try:
            request = json.loads(raw_request)
            event = self._normalize_request(request)
            payload = event.get("payload") or {}
            context = event.get("context") or {}
            self.logger.info(
                "meme service handling type=%s payload_keys=%d context_keys=%d",
                event.get("type", ""),
                len(payload),
                len(context),
            )
            result = self.service.dispatch(event)
            self.logger.info(
                "meme service completed type=%s result_keys=%d",
                event.get("type", ""),
                len(result),
            )
            return {"status": "ok", "result": result}
        except Exception as err:
            self.logger.exception("Meme service request failed")
            return {"status": "error", "message": str(err)}

    def _normalize_request(self, request: dict) -> dict:
        if "type" in request:
            self.logger.info("meme service received orchestrator envelope type=%s", request.get("type", ""))
            return request

        if "event_type" in request:
            legacy_event_type = request["event_type"]
            mapped_type = {
                "meme": "meme.fetch",
                "meme.fetch": "meme.fetch",
                "meme.pool.refresh": "meme.pool.refresh",
                "meme.status": "meme.status",
            }.get(legacy_event_type, legacy_event_type)
            self.logger.info(
                "meme service mapped legacy event_type=%s to type=%s",
                legacy_event_type,
                mapped_type,
            )

            return {
                "type": mapped_type,
                "payload": request.get("payload") or {},
                "context": request.get("context") or {},
            }

        raise ValueError("request must contain either 'type' or 'event_type'")
