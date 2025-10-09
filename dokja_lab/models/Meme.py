from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class Meme:
    url: str
    title: str
    source: str
    tags: str = ""
    sent_count: int = 0
    date_created: datetime = field(default_factory=datetime.now)
    date_sent: datetime = None
