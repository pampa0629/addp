"""Resolve deployment selection once; no owner authorization belongs here."""
import os
import re
from .area import BoundArea


def resolve_bucket(area, raw):
    if area not in {"standard", "manager", "develop"}:
        raise ValueError("unsupported platform object area")
    bucket = area
    seen = set()
    for entry in raw.split(",") if raw else ():
        name, separator, target = entry.partition("=")
        if not separator or name not in {"standard", "develop"} or name in seen:
            raise ValueError("invalid object store binding")
        if not re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", target) or re.fullmatch(r"(?:[0-9]+\.){3}[0-9]+", target) or any(x in target for x in ("..", ".-", "-.")):
            raise ValueError("invalid object store bucket")
        seen.add(name)
        if name == area:
            bucket = target
    return bucket


def open_object_area(area) -> BoundArea:
    if os.environ.get("ADDP_INFRA_OBJECTSTORE_PROVIDER", "minio") != "minio":
        raise ValueError("unsupported platform object provider")
    bucket = resolve_bucket(area, os.environ.get("ADDP_INFRA_OBJECTSTORE_BINDINGS", ""))
    from .minio import bind
    return bind(bucket)
