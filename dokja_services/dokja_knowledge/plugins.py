"""User-owned plugins: the place for anything specific to one site, provider or format.

The repository ships generic mechanisms only. If you want to read a source the built-in
extractors do not cover (a particular site, an internal system, a proprietary format), write
a plugin and keep it out of the repository:

* put `*.py` files in a directory of your own and point KNOWLEDGE_PLUGINS_DIR at it
  (mount it read-only in a container, and keep it git-ignored);
* each file may define `register(registry)`, which can call
  `registry.add_extractor(extensions, fn)` with `fn(data: bytes, name: str) -> list[Extracted]`
  and `registry.add_fetcher(scheme, fn)` with `fn(ref: str) -> (data: bytes, name: str)`.

Plugins are ordinary Python running inside the service with its permissions, so only load a
directory you control. A plugin that fails to load is logged and skipped.
"""

from __future__ import annotations

import importlib.util
import logging
from pathlib import Path

from extractors import Registry

logger = logging.getLogger("dokja_knowledge.plugins")


def load_plugins(registry: Registry, directory: str) -> list[str]:
    """Load every plugin file in `directory`; return the names that registered successfully."""
    root = Path(directory)
    if not root.is_dir():
        logger.warning("plugins directory %s does not exist; no plugins loaded", directory)
        return []
    loaded = []
    for path in sorted(root.glob("*.py")):
        if path.name.startswith("_"):
            continue
        try:
            spec = importlib.util.spec_from_file_location(f"knowledge_plugin_{path.stem}", path)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            register = getattr(module, "register", None)
            if register is None:
                logger.warning("plugin %s has no register(registry); skipped", path.name)
                continue
            register(registry)
            loaded.append(path.stem)
        except Exception as err:  # a broken plugin must not keep the service from starting
            logger.error("plugin %s failed to load: %s: %s", path.name, type(err).__name__, err)
    return loaded
