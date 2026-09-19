import sys
from pathlib import Path

# The service modules are plain scripts, not a package: import them from their folder.
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
