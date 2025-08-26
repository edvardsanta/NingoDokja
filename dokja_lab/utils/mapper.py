from dataclasses import asdict, fields
from typing import Any, Dict, Type, TypeVar

T = TypeVar("T")


def from_dict(cls: Type[T], data: Dict[str, Any]) -> T:
    field_names = {f.name for f in fields(cls)}
    filtered_data = {k: v for k, v in data.items() if k in field_names}
    return cls(**filtered_data)


def to_dict(entity: T) -> Dict[str, Any]:
    return asdict(entity)
