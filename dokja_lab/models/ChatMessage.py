import hashlib
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional, Dict, List

@dataclass
class ChatMessage:
    id: int = field(init=False)
    id_hash: str = field(init=False)
    type: str
    role: str
    request_message: str
    response_message: str
    timestamp: datetime = field(default_factory=datetime.now)
    retrieval_info: Optional[Dict] = None
    cached: bool = False
    emojis: List[str] = field(default_factory=list)
    tone: str = ""
    language: str = ""

    def __post_init__(self):
        self.id_hash = hashlib.sha256(
            self.request_message.encode("utf-8")
        ).hexdigest()