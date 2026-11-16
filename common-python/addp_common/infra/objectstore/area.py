from dataclasses import dataclass
from typing import BinaryIO, Protocol


@dataclass(frozen=True)
class ObjectStat:
    size: int
    media_type: str


class Area(Protocol):
    def put(self, key: str, body: BinaryIO, size: int, media_type: str) -> None: ...
    def stat(self, key: str) -> ObjectStat: ...
    def open(self, key: str) -> BinaryIO: ...
    def delete(self, key: str) -> None: ...


class BoundArea(Area, Protocol):
    """Binding readiness is independent of individual object operations."""
    def check_ready(self) -> None: ...
