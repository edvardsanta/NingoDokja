from abc import ABC, abstractmethod
from typing import TypeVar

from infra.storage import BaseStorage
from logging_config import get_logger

T = TypeVar('T')

logger = get_logger(__name__)

class BaseHandler(ABC):
    def __init__(self, storage : BaseStorage[T], publisher):
        self.storage = storage

    def __call__(self, event_data):
        logger.info(f"Handling {self.__class__.__name__} event: {event_data}")
        self.persist(event_data)

    @abstractmethod
    def persist(self, event_data):
        pass