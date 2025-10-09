from dataclasses import asdict, fields
from typing import Any, Dict, Type, TypeVar

T = TypeVar("T")


def from_dict(cls: Type[T], data: Dict[str, Any]) -> T:
    field_names = {f.name for f in fields(cls)}
    filtered_data = {k: v for k, v in data.items() if k in field_names}
    try:
        return cls(**filtered_data)
    except TypeError as e:
        missing = [
            f.name for f in fields(cls) if f.init and f.name not in filtered_data
        ]
        raise TypeError(
            f"Error creating {cls.__name__} from dict: {e}. Missing fields: {missing}"
        )
    except ValueError as e:
        raise ValueError(f"Error creating {cls.__name__} from dict: {e}")


def to_dict(entity: T) -> Dict[str, Any]:
    return asdict(entity)
