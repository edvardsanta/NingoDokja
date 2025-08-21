import json
import sqlite3
from dataclasses import is_dataclass, fields
from abc import ABC, abstractmethod
from datetime import datetime
from typing import Any, Dict, List, Generic, TypeVar, Type, get_origin, Union, get_args, Optional
import redis

T = TypeVar("T")

class BaseStorage(ABC, Generic[T]):
    def __init__(self, entity_cls: Type[T]):
        if not is_dataclass(entity_cls):
            raise ValueError("Entity class must be a dataclass")
        self.entity_cls = entity_cls

    @abstractmethod
    def add(self, entity: T) -> bool:
        pass

    @abstractmethod
    def get_all(self) -> List[T]:
        pass

    @abstractmethod
    def get_by_id(self, entity_id: Any) -> T:
        pass

    @abstractmethod
    def get_by_id_hash(self, entity_hash: str) -> Optional[T]:
        pass

    @abstractmethod
    def get_random(self) -> Optional[T]:
        pass

    @abstractmethod
    def get_filtered(self, **filters) -> List[T]:
        """Get entities filtered by SQL conditions."""
        pass

    @abstractmethod
    def exists(self, entity_id: Any) -> bool:
        pass

    @abstractmethod
    def update(self, entity_id: Any, updates: Dict[str, Any]) -> None:
        pass


class RedisStorage(BaseStorage[T]):

    # TODO: Implement BaseStorage correctly for RedisStorage
    def __init__(self, entity_cls: Type[T], host="localhost", port=6379, db=0):
        super().__init__(entity_cls)
        self.r = redis.Redis(host=host, port=port, db=db)
        self.set_key = f"{self.entity_cls.__name__.lower()}:set"
        self.data_key = f"{self.entity_cls.__name__.lower()}:data"

    def _get_entity_id(self, entity: T) -> Any:
        # assume first dataclass field is the primary key
        return getattr(entity, fields(self.entity_cls)[0].name)

    def add(self, entity: T) -> bool:
        entity_id = self._get_entity_id(entity)
        if self.r.sismember(self.set_key, entity_id):
            return False
        self.r.sadd(self.set_key, entity_id)
        # store as JSON string
        self.r.hset(self.data_key, entity_id, json.dumps(entity.__dict__))
        return True

    def get_all(self) -> List[T]:
        all_data = self.r.hgetall(self.data_key).values()
        result = []
        for item in all_data:
            data = json.loads(item)
            result.append(self.entity_cls(**data))
        return result

    def exists(self, entity_id: Any) -> bool:
        return self.r.sismember(self.set_key, entity_id) is not None

    def update(self, entity_id: Any, updates: Dict[str, Any]) -> None:
        entity_data = self.r.hget(self.data_key, entity_id)
        if entity_data:
            data = json.loads(entity_data)
            data.update(updates)
            self.r.hset(self.data_key, entity_id, json.dumps(data))


