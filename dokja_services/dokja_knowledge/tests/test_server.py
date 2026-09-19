import json
import threading

import zmq

from fakes import FakeEmbedder
from server import KnowledgeServiceServer
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
                     "payload": {"title": "Nota", "body": "texto sobre estoicismo e virtude"}})
    assert ingested["status"] == "ok" and ingested["result"]["created"] is True
    found = call({"type": "knowledge.search", "payload": {"query": "estoicismo virtude"}})
    assert found["result"]["hits"][0]["source_id"] == "note:nota"
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
