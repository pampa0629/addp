import os
from contextlib import contextmanager
from typing import Any


def import_gdal():
    try:
        from osgeo import gdal
    except Exception as exc:
        raise RuntimeError("GDAL Python bindings are required") from exc
    gdal.UseExceptions()
    return gdal


@contextmanager
def gdal_config_env(values: dict[str, Any] | None):
    gdal = import_gdal()
    env = {
        str(key): str(value)
        for key, value in (values or {}).items()
        if key and value is not None
    }
    previous_env = {key: os.environ.get(key) for key in env}
    previous_config = {key: gdal.GetConfigOption(key) for key in env}
    try:
        for key, value in env.items():
            os.environ[key] = value
            gdal.SetConfigOption(key, value)
        yield gdal
    finally:
        for key, value in previous_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        for key, value in previous_config.items():
            gdal.SetConfigOption(key, value)


def join_gdal_path(root: str, ref: str) -> str:
    base = str(root or "").rstrip("/")
    child = str(ref or "").strip("/")
    if not base:
        return child
    if not child:
        return base
    return f"{base}/{child}"
