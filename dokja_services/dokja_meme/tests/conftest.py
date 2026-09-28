import sys
from pathlib import Path

# The service modules are plain scripts, not a package: import them from their folder,
# and from dokja_lab, where the legacy meme model and storage still live.
_service_dir = Path(__file__).resolve().parents[1]
_repo_root = _service_dir.parents[1]
for path in (_service_dir, _repo_root / "dokja_lab", _repo_root):
    path_str = str(path)
    if path_str not in sys.path:
        sys.path.insert(0, path_str)
