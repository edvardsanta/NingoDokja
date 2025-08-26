from dataclasses import dataclass


@dataclass
class Meme:
    url: str
    title: str
    source: str
    tags: str = ""
    sent_count: int = 0
