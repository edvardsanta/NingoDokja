from abc import ABC, abstractmethod
from typing import Any


class BaseWorker(ABC):
    @abstractmethod
    def run(self) -> Any:
        """Execute the worker task"""
        pass
