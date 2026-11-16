"""SDK-free platform object contracts; providers are loaded only at startup."""
from .area import Area, ObjectStat
from .ref import Ref

__all__ = ["Area", "ObjectStat", "Ref"]
