import json
import zmq

from infra.transport import BaseTransport
from logging_config import get_logger

logger = get_logger(__name__)

class Responder(BaseTransport):
    def __init__(self, endpoint: str = "tcp://*:5556", handlers=None):
        self.context = zmq.Context()
        self.socket = self.context.socket(zmq.REP)
        self.socket.bind(endpoint)
        self.running = False
        self.handlers = handlers

    def receive(self) -> str:
        return self.socket.recv_string()

    def send(self, msg: str):
        self.socket.send_string(msg)

    def start(self):
        self.running = True
        logger.info("Responder running...")
        try:
            while self.running:
                raw_request = self.receive()
                logger.debug(f"Received request: {raw_request}")

                try:
                    request = json.loads(raw_request)
                    event_type = request.get("event_type")
                    payload = request.get("payload", {})

                    handler = self.handlers.get(event_type)
                    if not handler:
                        response = {"status": "error", "message": f"Unknown event_type '{event_type}'"}
                    else:
                        # Validate payload inside handler before doing anything
                        if not handler.validate_payload(payload):
                            response = {"status": "error", "message": "Invalid payload"}
                        else:
                            result = handler.handle(payload)
                            response = {"status": "ok", "result": result}

                except Exception as e:
                    logger.exception("Error handling request")
                    response = {"status": "error", "message": str(e)}

                self.send(json.dumps(response))
                logger.debug(f"Sent response: {response}")

        except KeyboardInterrupt:
            logger.warning("Stopping responder due to keyboard interrupt...")
        finally:
            self.close()

    def close(self):
        if not self.running:
            return
        self.running = False
        self.socket.close()
        self.context.term()
        logger.warning("Responder closed.")