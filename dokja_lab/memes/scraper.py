from abc import ABC, abstractmethod
from typing import List, Dict

class BaseScraper(ABC):
    @abstractmethod
    def scrape(self, max_items: int = 50) -> List[Dict]:
        """
        Return a list of memes in standard format:
        [
            {
                "url": "http://...",
                "title": "Meme title",
                "source": "hello_world",
                "tags": ["funny", "readers"]
            },
            ...
        ]
        """
        pass
