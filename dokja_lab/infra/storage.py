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


class SQLiteStorage(BaseStorage[T]):
    def __init__(self, db_file: str, entity_cls: Type[T]):
        super().__init__(entity_cls)
        self.conn = sqlite3.connect(db_file, check_same_thread=False)
        self.table_name = entity_cls.__name__.lower()
        self._ensure_table()

    def _ensure_table(self):
        cols = []
        for f in fields(self.entity_cls):
            typ = f.type
            # Handle Optional types
            if get_origin(typ) is Union:
                args = [a for a in get_args(typ) if a is not type(None)]
                if args:
                    typ = args[0]

            if typ in (int, bool):
                col_type = "INTEGER"
            elif typ == float:
                col_type = "REAL"
            else:
                col_type = "TEXT"

            cols.append(f"{f.name} {col_type}")

        if cols:
            first_field_type = fields(self.entity_cls)[0].type
            if first_field_type == int:
                cols[0] += " PRIMARY KEY AUTOINCREMENT"
            else:
                cols[0] += " PRIMARY KEY"

        columns_sql = ", ".join(cols)
        cursor = self.conn.cursor()
        cursor.execute(f"CREATE TABLE IF NOT EXISTS {self.table_name} ({columns_sql})")
        self.conn.commit()

    def add(self, entity: T) -> bool:
        cursor = self.conn.cursor()
        entity_dict = entity.__dict__.copy()  # make a copy to modify

        for key, value in entity_dict.items():
            if isinstance(value, (list, dict)):
                entity_dict[key] = json.dumps(value)  # store as JSON string
            elif isinstance(value, bool):
                entity_dict[key] = int(value)  # bool -> int
            elif isinstance(value, datetime):
                entity_dict[key] = value.isoformat()  # datetime -> string

        placeholders = ", ".join("?" for _ in entity_dict)
        columns = ", ".join(entity_dict.keys())
        values = tuple(entity_dict.values())

        try:
            cursor.execute(f"INSERT INTO {self.table_name} ({columns}) VALUES ({placeholders})", values)
            self.conn.commit()
            return True
        except sqlite3.IntegrityError:  # duplicate primary key
            return False

    def get_by_id_hash(self, entity_hash: str) -> Optional[T]:
        cursor = self.conn.cursor()
        cursor.execute(
            f"SELECT * FROM {self.table_name} WHERE id_hash = ?",
            (entity_hash,)
        )
        row = cursor.fetchone()
        if not row:
            return None

        col_names = [f.name for f in fields(self.entity_cls)]
        row_dict = dict(zip(col_names, row))

        init_field_names = {f.name for f in fields(self.entity_cls) if f.init}
        init_kwargs = {k: v for k, v in row_dict.items() if k in init_field_names}
        non_init_fields = {k: v for k, v in row_dict.items() if k not in init_field_names}

        entity = self.entity_cls(**init_kwargs)

        for k, v in non_init_fields.items():
            setattr(entity, k, v)

        return entity

    def get_all(self) -> List[T]:
        cursor = self.conn.cursor()
        cursor.execute(f"SELECT * FROM {self.table_name}")
        rows = cursor.fetchall()
        results = []
        for row in rows:
            row_dict = {f.name: row[i] for i, f in enumerate(fields(self.entity_cls))}
            results.append(self.entity_cls(**row_dict))
        return results

    def get_by_id(self, entity_id: Any) -> T:
        pass

    def exists(self, entity_id: Any) -> bool:
        pk = fields(self.entity_cls)[0].name
        cursor = self.conn.cursor()
        cursor.execute(f"SELECT 1 FROM {self.table_name} WHERE {pk} = ?", (entity_id,))
        return cursor.fetchone() is not None

    def update(self, entity_id: Any, updates: Dict[str, Any]) -> None:
        pk = fields(self.entity_cls)[0].name
        set_clause = ", ".join(f"{k} = ?" for k in updates.keys())
        values = tuple(updates.values()) + (entity_id,)
        cursor = self.conn.cursor()
        cursor.execute(f"UPDATE {self.table_name} SET {set_clause} WHERE {pk} = ?", values)
        self.conn.commit()
