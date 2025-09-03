import json
import sqlite3
from dataclasses import fields
from datetime import datetime
from typing import Any, Dict, Optional, Type, TypeVar, Union, get_args, get_origin

from infra.storage import BaseStorage

T = TypeVar("T")


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
        entity_dict = entity.__dict__.copy()

        for key, value in entity_dict.items():
            if isinstance(value, (list, dict)):
                entity_dict[key] = json.dumps(value)  # store as JSON string
            elif isinstance(value, bool):
                entity_dict[key] = int(value)  # bool -> int
            elif isinstance(value, datetime):
                # TODO: I am not sure if this is the best way to handle datetime
                entity_dict[key] = value.isoformat()  # datetime -> string

        placeholders = ", ".join("?" for _ in entity_dict)
        columns = ", ".join(entity_dict.keys())
        values = tuple(entity_dict.values())

        try:
            cursor.execute(
                f"INSERT INTO {self.table_name} ({columns}) VALUES ({placeholders})",
                values,
            )
            self.conn.commit()
            return True
        except sqlite3.IntegrityError:  # duplicate primary key
            return False

    def get_random(self) -> Optional[T]:
        col_names = [f.name for f in fields(self.entity_cls)]
        query = f"SELECT {', '.join(col_names)} FROM {self.table_name} ORDER BY RANDOM() LIMIT 1"

        with self.conn as conn:
            cursor = conn.cursor()
            cursor.execute(query)
            row = cursor.fetchone()

        if not row:
            return None

        row_dict = dict(zip(col_names, row))

        init_field_names = {f.name for f in fields(self.entity_cls) if f.init}
        init_kwargs = {k: v for k, v in row_dict.items() if k in init_field_names}
        non_init_fields = {
            k: v for k, v in row_dict.items() if k not in init_field_names
        }

        entity = self.entity_cls(**init_kwargs)
        for k, v in non_init_fields.items():
            setattr(entity, k, v)

        return entity

    from typing import Callable, List

    def get_filtered(self, **filters) -> List[T]:
        """
        Retrieve a list of entities from the database filtered by the given keyword arguments.

        This method dynamically constructs a SQL SELECT query based on the provided filters.
        Only columns that match the fields of the entity class are selected. The results
        are returned as instances of `self.entity_cls`, with all fields properly initialized.

        Parameters:
            **filters: Arbitrary keyword arguments where the key is the column/field name
                       and the value is the value to filter by. Multiple filters are combined
                       using AND in the SQL WHERE clause.
                       Example: get_filtered(name="Alice", age=30)

        Returns:
            List[T]: A list of entities of type `self.entity_cls` matching the filter criteria.

        Behavior:
            - Constructs a SELECT query for all columns of the entity.
            - Applies WHERE conditions based on filters if provided.
            - Executes the query and fetches all matching rows.
            - Converts each row to an instance of `self.entity_cls`.
              - Fields that are declared in `__init__` are passed as constructor arguments.
              - Fields not in `__init__` are set using `setattr`.
        """
        col_names = [f.name for f in fields(self.entity_cls)]
        conditions = [f"{k} = ?" for k in filters.keys()]
        query = f"SELECT {', '.join(col_names)} FROM {self.table_name}"
        if conditions:
            query += " WHERE " + " AND ".join(conditions)

        with self.conn as conn:
            cursor = conn.cursor()
            cursor.execute(query, tuple(filters.values()))
            rows = cursor.fetchall()

        result = []
        for row in rows:
            row_dict = dict(zip(col_names, row))
            init_field_names = {f.name for f in fields(self.entity_cls) if f.init}
            init_kwargs = {k: v for k, v in row_dict.items() if k in init_field_names}
            non_init_fields = {
                k: v for k, v in row_dict.items() if k not in init_field_names
            }

            entity = self.entity_cls(**init_kwargs)
            for k, v in non_init_fields.items():
                setattr(entity, k, v)

            result.append(entity)

        return result

    def get_by_id_hash(self, entity_hash: str) -> Optional[T]:
        cursor = self.conn.cursor()
        cursor.execute(
            f"SELECT * FROM {self.table_name} WHERE id_hash = ?", (entity_hash,)
        )
        row = cursor.fetchone()
        if not row:
            return None

        col_names = [f.name for f in fields(self.entity_cls)]
        row_dict = dict(zip(col_names, row))

        init_field_names = {f.name for f in fields(self.entity_cls) if f.init}
        init_kwargs = {k: v for k, v in row_dict.items() if k in init_field_names}
        non_init_fields = {
            k: v for k, v in row_dict.items() if k not in init_field_names
        }

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
        cursor.execute(
            f"UPDATE {self.table_name} SET {set_clause} WHERE {pk} = ?", values
        )
        self.conn.commit()
