"""SQLAlchemy field types for platform records shared by the Python consumers."""
from datetime import timezone

from sqlalchemy import DateTime, JSON
from sqlalchemy.dialects.mysql import DATETIME
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.types import TypeDecorator

PlatformJSON = JSON().with_variant(JSONB(), "postgresql")


class Instant(TypeDecorator):
    """An absolute instant. MySQL DATETIME stores UTC at microsecond precision."""
    impl = DateTime(timezone=True)
    cache_ok = True

    def load_dialect_impl(self, dialect):
        if dialect.name == "mysql":
            return dialect.type_descriptor(DATETIME(fsp=6))
        return dialect.type_descriptor(self.impl)

    def process_bind_param(self, value, dialect):
        if value is not None and dialect.name == "mysql":
            if value.tzinfo is None:
                raise ValueError("expected a timezone-aware instant")
            return value.astimezone(timezone.utc).replace(tzinfo=None)
        return value

    def process_result_value(self, value, dialect):
        if value is not None and dialect.name == "mysql":
            return value.replace(tzinfo=timezone.utc)
        return value
