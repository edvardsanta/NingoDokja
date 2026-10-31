import json
import threading

import zmq

from fakes import FakeEmbedder
from server import BUSY_POLL_MS, IDLE_POLL_MS, KnowledgeServiceServer
from service import KnowledgeService
from store import Store


def test_the_server_answers_over_zeromq_and_reports_errors(tmp_path):
    endpoint = f"ipc://{tmp_path}/knowledge.sock"
    service = KnowledgeService(Store(":memory:"), embedder=FakeEmbedder(), model="fake")
    server = KnowledgeServiceServer(endpoint, service=service)
    threading.Thread(target=server.start, daemon=True).start()

    context = zmq.Context()
    client = context.socket(zmq.REQ)
    client.setsockopt(zmq.RCVTIMEO, 5000)
    client.connect(endpoint)

    def call(raw):
        client.send_string(raw if isinstance(raw, str) else json.dumps(raw))
        return json.loads(client.recv_string())

    ingested = call({"type": "knowledge.ingest",
                     "payload": {"title": "Note", "body": "text about stoicism and virtue"}})
    assert ingested["status"] == "ok" and ingested["result"]["created"] is True
    found = call({"type": "knowledge.search", "payload": {"query": "stoicism virtue"}})
    assert found["result"]["hits"][0]["source_id"] == "note:note"
    bad = call({"type": "knowledge.ingest", "payload": {"title": "", "body": "x"}})
    assert bad["status"] == "error" and "title" in bad["message"]
    assert call({"type": "nope"})["status"] == "error"
    assert call("not json")["status"] == "error"
    assert call({"payload": {}})["status"] == "error"

    client.close(0)
    context.destroy(linger=0)


def test_errors_and_logs_do_not_echo_the_payload(caplog, tmp_path):
    service = KnowledgeService(Store(":memory:"), embedder=None, model="none")
    server = KnowledgeServiceServer(f"ipc://{tmp_path}/x.sock", service=service)
    secret = "SEGREDO-DO-USUARIO"
    with caplog.at_level("DEBUG"):
        server.handle_request(json.dumps({"type": "knowledge.search", "payload": {"query": secret}}))
        server.handle_request(json.dumps({"type": "knowledge.ingest", "payload": {"title": "", "body": secret}}))
    assert secret not in caplog.text
    server.context.destroy(linger=0)


class Waiting:
    """A service with a few chunks to embed, which can also fail."""

    def __init__(self, batches, fails=False):
        self.batches, self.fails, self.calls = batches, fails, 0

    def embed_some(self):
        self.calls += 1
        if self.fails:
            raise RuntimeError("private text")
        self.batches -= 1
        return 8 if self.batches >= 0 else 0

    def dispatch(self, request):
        return {"seen": request["type"]}


def test_the_server_embeds_while_idle_and_answers_between_batches(tmp_path):
    endpoint = f"ipc://{tmp_path}/idle.sock"
    service = Waiting(batches=2)
    server = KnowledgeServiceServer(endpoint=endpoint, service=service)
    server.socket.bind(endpoint)
    client = server.context.socket(zmq.REQ)
    client.connect(endpoint)

    assert server.turn(0) == BUSY_POLL_MS and service.calls == 1
    client.send_string(json.dumps({"type": "knowledge.status"}))
    while not server.socket.poll(10):
        pass
    calls = service.calls
    assert server.turn(1000) == BUSY_POLL_MS
    assert service.calls == calls, "a waiting request goes before more embedding"
    assert json.loads(client.recv_string())["result"] == {"seen": "knowledge.status"}

    assert server.turn(0) == BUSY_POLL_MS
    assert server.turn(0) == IDLE_POLL_MS, "with nothing left it rests"
    client.close()
    server.socket.close()
    server.context.term()


def test_failed_background_work_never_stops_the_service(tmp_path, caplog):
    endpoint = f"ipc://{tmp_path}/fail.sock"
    server = KnowledgeServiceServer(endpoint=endpoint, service=Waiting(batches=1, fails=True))
    server.socket.bind(endpoint)
    assert server.turn(0) == IDLE_POLL_MS
    assert "private text" not in caplog.text
    server.socket.close()
    server.context.term()

