from dataclasses import dataclass
from typing import Optional


@dataclass
class WorkerStatus:
    job_name: str
    status: str
    timestamp: Optional[str] = None
    exception: Optional[str] = None
    next_run: Optional[str] = None
    trigger: Optional[str] = None
