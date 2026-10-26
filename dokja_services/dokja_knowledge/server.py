from __future__ import annotations

import json
import logging
import os
import sys

import zmq

from bootstrap import build_service

logger = logging.getLogger("dokja_knowledge.server")


class KnowledgeServiceServer:
    def __init__(self, endpoint: str | None = None, service=None):
        self.endpoint = endpoint or os.getenv("KNOWLEDGE_SERVICE_ENDPOINT", "tcp://*:5561")
        self.context = zmq.Context()
        self.socket = self.context.socket(zmq.REP)
        self.service = service if service is not None else build_service()

    def start(self):
        self.socket.bind(self.endpoint)
        logger.info("knowledge service listening endpoint=%s", self.endpoint)
        try:
            while True:
                raw_request = self.socket.recv_string()
                self.socket.send_string(json.dumps(self.handle_request(raw_request)))
        finally:
            self.socket.close()
            self.context.term()

    def handle_request(self, raw_request: str) -> dict:
        try:
            request = json.loads(raw_request)
            if "type" not in request:
                raise ValueError("request must contain 'type'")
            payload = request.get("payload") or {}
            logger.info("knowledge service handling type=%s payload_keys=%d", request["type"], len(payload))
            return {"status": "ok", "result": self.service.dispatch(request)}
        except Exception as err:
            # The optional PostgreSQL adapter loads its driver. Keep SQLite
            # usable without it, and never log database row diagnostics.
            driver = sys.modules.get("psycopg")
            if driver is not None and isinstance(err, driver.Error):
                logger.error("knowledge storage request failed error_type=%s", type(err).__name__)
                return {"status": "error", "message": "knowledge storage request failed"}
            # Log the type and the error, never the payload: it is the user's private notes.
            logger.exception("knowledge request failed")
            return {"status": "error", "message": str(err)}
