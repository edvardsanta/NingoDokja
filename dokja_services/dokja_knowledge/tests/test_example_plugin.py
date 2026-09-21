import importlib.util
from pathlib import Path

import pytest

from extractors import Registry
from fakes import FakeEmbedder
from service import KnowledgeService
from store import Store

EXAMPLE = Path(__file__).resolve().parents[1] / "plugins.example" / "local_folder.py"


@pytest.fixture
def plugin(tmp_path):
    spec = importlib.util.spec_from_file_location("example_plugin", EXAMPLE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.ROOT = tmp_path
    return module


def test_the_example_plugin_reads_a_folder_note_end_to_end(plugin, tmp_path):
    (tmp_path / "ideas.notes").write_text("Rates: policy rates were held\nEmpty:\nGrowth: output slowed\n")
    registry = Registry()
    plugin.register(registry)
    service = KnowledgeService(Store(":memory:"), embedder=FakeEmbedder(), model="fake", registry=registry)

    result = service.ingest({"source": "folder:ideas.notes"})

    assert result["count"] == 2, "the entry without text is dropped"
    assert {d["title"] for d in service.list_documents({})["documents"]} == {"Rates", "Growth"}


def test_the_example_plugin_never_reads_outside_its_folder(plugin, tmp_path):
    (tmp_path.parent / "secret.txt").write_text("private")
    for ref in ("folder:../secret.txt", "folder:/../secret.txt", "folder:a/../../secret.txt"):
        with pytest.raises(ValueError, match="outside"):
            plugin.fetch_from_folder(ref)
