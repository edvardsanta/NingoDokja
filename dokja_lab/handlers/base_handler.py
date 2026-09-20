from abc import ABC, abstractmethod
from typing import Any, Optional, TypeVar

from infra.storage import BaseStorage
from logging_config import get_logger

T = TypeVar("T")

logger = get_logger(__name__)


class BaseHandler(ABC):
    def __init__(
        self, storage: Optional[BaseStorage] = None, publisher: Optional[Any] = None
    ) -> None:
        self.storage = storage
        self.publisher = publisher

    @abstractmethod
    def validate_payload(self, payload: dict) -> bool:
        """
        Return True if payload is valid, False otherwise.
        Must be overridden in subclasses.
        """
        return True

    @abstractmethod
    def handle(self, payload: dict) -> Any:
        """
        Handle the request and return a result.
        Must be overridden in subclasses.
        """
        raise NotImplementedError
