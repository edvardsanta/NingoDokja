"""Example plugin: read notes from a folder on this machine through a `folder:` reference.

Copy this file to the directory KNOWLEDGE_PLUGINS_DIR points at and adapt it. It uses no
network and names no site; it only shows the two things a plugin can do:

* a fetcher turns a reference (`folder:notes/idea.md`) into (bytes, file name);
* an extractor turns bytes of a file type into a list of Extracted documents.
"""

from pathlib import Path

from extractors import Extracted

ROOT = Path("/data/inbox")  # change to a folder you control


def fetch_from_folder(ref: str):
    relative = ref.split(":", 1)[1].lstrip("/")
    path = (ROOT / relative).resolve()
    if ROOT.resolve() not in path.parents:  # never read outside the folder
        raise ValueError("path is outside the inbox folder")
    return path.read_bytes(), path.name


def extract_notes(data: bytes, name: str):
    """A made-up `.notes` format: one note per line, 'title: text'."""
    documents = []
    for line in data.decode("utf-8").splitlines():
        title, _, text = line.partition(":")
        if text.strip():
            documents.append(Extracted(title=title.strip(), text=text.strip(), key=title.strip()))
    return documents


def register(registry):
    registry.add_fetcher("folder", fetch_from_folder)
    registry.add_extractor(".notes", extract_notes)
