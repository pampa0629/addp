import math


WEB_MERCATOR_HALF_WORLD = 20037508.342789244


def web_mercator_tile_bounds(z: int, x: int, y: int) -> tuple[float, float, float, float]:
    if z < 0:
        raise ValueError("z must be greater than or equal to 0")
    tiles = 1 << z
    if x < 0 or x >= tiles or y < 0 or y >= tiles:
        raise ValueError("x/y out of range for z")
    tile_size = (WEB_MERCATOR_HALF_WORLD * 2.0) / float(tiles)
    minx = -WEB_MERCATOR_HALF_WORLD + x * tile_size
    maxx = minx + tile_size
    maxy = WEB_MERCATOR_HALF_WORLD - y * tile_size
    miny = maxy - tile_size
    return (minx, miny, maxx, maxy)


def int_param(value, name: str, minimum: int | None = None, default: int | None = None) -> int:
    if value is None:
        if default is None:
            raise ValueError(f"{name} is required")
        return default
    try:
        parsed = int(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{name} must be an integer") from exc
    if minimum is not None and parsed < minimum:
        raise ValueError(f"{name} must be greater than or equal to {minimum}")
    return parsed


def clamp_tile_size(value: int) -> int:
    if value not in (256, 512):
        raise ValueError("tile_size must be 256 or 512")
    return value


def is_finite_bounds(bounds: tuple[float, float, float, float]) -> bool:
    return all(math.isfinite(value) for value in bounds)
