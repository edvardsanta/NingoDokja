from typing import Any


class BaseTransport:
    def receive(self) -> Any:
        raise NotImplementedError

    def send(self, msg: Any) -> None:
        raise NotImplementedError

    def close(self) -> None:
        raise NotImplementedError
