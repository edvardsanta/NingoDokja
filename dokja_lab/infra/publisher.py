from abc import ABC, abstractmethod
from typing import Dict
import zmq

class BasePublisher(ABC):
    @abstractmethod
    def publish_item(self, item: Dict):
        """Publish a new item to subscribers."""
        pass

class MemePublisher(BasePublisher):
    def __init__(self, endpoint: str = "tcp://*:5555"):
        self.context = zmq.Context()
        self.socket = self.context.socket(zmq.PUB)
        self.socket.bind(endpoint)

    def publish_item(self, item: Dict):
        self.socket.send_string(str(item))

    def close(self):
        self.socket.close()
        self.context.term()