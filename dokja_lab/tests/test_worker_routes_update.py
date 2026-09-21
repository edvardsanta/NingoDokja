"""The route that edits a worker's schedule.

worker_routes needs the git-ignored config module and creates ningo_memory.db in the working
directory when it is imported, so the tests stub the first and import from a temporary directory.
"""

import importlib
import sys
import types
from unittest.mock import Mock

import pytest
from flask import Flask


@pytest.fixture
def routes(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setitem(
        sys.modules,
        "config",
        types.SimpleNamespace(WORKERS=[], SCRAPERS=[], DB_FILE="x.db"),
    )
    for name in ("worker_routes", "scheduler"):
        monkeypatch.delitem(sys.modules, name, raising=False)
    module = importlib.import_module("worker_routes")

    job = Mock(func=Mock(), args=(), kwargs={}, id="job-1", input=None)
    job.name = "meme worker"
    scheduler = Mock()
    scheduler.get_job.return_value = job
    monkeypatch.setattr(module, "get_scheduler", lambda: scheduler)
    monkeypatch.setattr(module, "get_worker_config_by_id", lambda worker_id: None)

    app = Flask(__name__)
    app.secret_key = "test"
    app.register_blueprint(module.workers_bp)
    return app, scheduler


def flashed(client):
    with client.session_transaction() as session:
        return [message for _, message in session.get("_flashes", [])]


def test_a_request_without_params_is_refused_and_the_job_is_left_alone(routes):
    app, scheduler = routes
    client = app.test_client()

    response = client.post("/workers/config/job-1", data={"trigger": "interval"})

    assert response.status_code == 302
    assert any(message.startswith("Erro ao atualizar") for message in flashed(client))
    scheduler.remove_job.assert_not_called()
    scheduler.add_job.assert_not_called()


def test_valid_params_reschedule_the_job(routes):
    app, scheduler = routes
    client = app.test_client()

    response = client.post(
        "/workers/config/job-1",
        data={"trigger": "interval", "params": "seconds=60, minutes=5"},
    )

    assert response.status_code == 302
    scheduler.remove_job.assert_called_once_with("job-1")
    kwargs = scheduler.add_job.call_args.kwargs
    assert (
        kwargs["trigger"] == "interval"
        and kwargs["seconds"] == 60
        and kwargs["minutes"] == 5
    )
    assert "Configuração atualizada com sucesso!" in flashed(client)


def test_an_unknown_worker_is_reported(routes):
    app, scheduler = routes
    scheduler.get_job.return_value = None
    client = app.test_client()

    client.post(
        "/workers/config/missing", data={"trigger": "interval", "params": "seconds=1"}
    )

    assert "Worker não encontrado." in flashed(client)
    scheduler.remove_job.assert_not_called()
