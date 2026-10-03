"""Source preflight for the initial static DAE/3DS conversion scope."""
from __future__ import annotations

import math
import struct
import xml.etree.ElementTree as ET
from pathlib import Path, PurePosixPath
from urllib.parse import unquote, urlsplit

MAX_SUMMARY_BYTES = 64 << 20
NS = "{http://www.collada.org/2005/11/COLLADASchema}"


def validate_source(path: Path, source_format: str) -> list[str]:
    if path.stat().st_size > MAX_SUMMARY_BYTES:
        raise ValueError("source summary exceeds 64 MiB")
    data = path.read_bytes()
    refs = _dae_refs(data) if source_format == "dae" else _three_ds_refs(data)
    for ref in refs:
        text = unquote(ref) if source_format == "dae" else ref
        text = text.replace("\\", "/")
        if not text or urlsplit(text).scheme or urlsplit(text).netloc or "?" in text or "#" in text:
            raise ValueError(f"unsupported texture reference: {ref}")
        relative = PurePosixPath(text)
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError(f"texture reference escapes model directory: {ref}")
        candidate = path.parent
        # Compare directory entries rather than relying on host filesystem case behavior.
        for part in relative.parts:
            if not candidate.is_dir() or part not in {entry.name for entry in candidate.iterdir()}:
                raise ValueError(f"missing texture (exact case required): {ref}")
            candidate = candidate / part
        if not candidate.is_file() or not candidate.resolve().is_relative_to(path.parent.resolve()):
            raise ValueError(f"invalid local texture reference: {ref}")
        if candidate.suffix.lower() not in {".png", ".jpg", ".jpeg"}:
            raise ValueError(f"only PNG/JPEG textures are supported: {ref}")

    return refs


class _ColladaTreeBuilder(ET.TreeBuilder):
    def doctype(self, name: str, pubid: str | None, system: str | None) -> None:
        raise ValueError("DAE XML declarations are unsupported")


def _dae_refs(data: bytes) -> list[str]:
    try:
        root = ET.fromstring(data, parser=ET.XMLParser(target=_ColladaTreeBuilder()))
    except ET.ParseError as error:
        raise ValueError(f"invalid Collada XML: {error}") from error
    if root.tag != NS + "COLLADA" or root.get("version") not in {"1.4.0", "1.4.1"}:
        raise ValueError("only Collada 1.4.0 / 1.4.1 is supported")
    if root.find(NS + "library_animations") is not None or root.find(NS + "library_controllers") is not None:
        raise ValueError("DAE animations and controllers are outside initial support")
    for unit in root.findall(f"{NS}asset/{NS}unit"):
        if unit.get("meter") is not None:
            meter = float(unit.get("meter"))
            if meter <= 0 or not math.isfinite(meter):
                raise ValueError("invalid Collada unit")
    for axis in root.findall(f"{NS}asset/{NS}up_axis"):
        if (axis.text or "").strip() not in {"X_UP", "Y_UP", "Z_UP"}:
            raise ValueError("invalid Collada up_axis")
    diffuse_textures = {id(child) for diffuse in root.iter(NS + "diffuse") for child in diffuse}
    for element in root.iter():
        if element.tag.startswith(NS + "profile_") and element.tag != NS + "profile_COMMON":
            raise ValueError("only Collada profile_COMMON materials are supported")
        for key, value in element.attrib.items():
            if key in {"url", "source"} and value and not value.startswith("#"):
                raise ValueError("external DAE model references are unsupported")
        if element.tag == NS + "texture":
            # Only a texture directly under a diffuse channel is in scope.
            if id(element) not in diffuse_textures:
                raise ValueError("only DAE diffuse textures are supported")
    images = root.findall(f"{NS}library_images/{NS}image")
    refs = []
    for image in images:
        initial = image.find(NS + "init_from")
        if initial is None or list(initial) or not (initial.text or "").strip():
            raise ValueError("DAE embedded/empty images are unsupported")
        refs.append(initial.text.strip())
    return refs


def _three_ds_refs(data: bytes) -> list[str]:
    if len(data) < 6 or struct.unpack_from("<HI", data) != (0x4D4D, len(data)):
        raise ValueError("invalid 3DS main chunk")
    refs = []
    maps = {0xA200, 0xA204, 0xA210, 0xA220, 0xA230, 0xA33A, 0xA33C, 0xA33D, 0xA33E,
            0xA340, 0xA342, 0xA344, 0xA346, 0xA348, 0xA34A}
    containers = {0x3D3D, 0x4100, 0xAFFF, 0xB000, 0xB002, 0xB003, 0xB004, 0xB005, 0xB006, 0xB007}

    def scan(start: int, end: int, depth: int = 0, texture_map: int = 0) -> None:
        if depth > 64:
            raise ValueError("3DS chunk nesting exceeds limit")
        while start < end:
            if end - start < 6:
                raise ValueError("truncated 3DS chunk header")
            kind, size = struct.unpack_from("<HI", data, start)
            if size < 6 or size > end - start:
                raise ValueError("invalid 3DS chunk length")
            body, stop = start + 6, start + size
            if kind == 0x4000:
                zero = data.find(b"\x00", body, stop)
                if zero < 0:
                    raise ValueError("unterminated 3DS object name")
                scan(zero + 1, stop, depth + 1, texture_map)
            elif kind == 0x4120:
                if stop - body < 2:
                    raise ValueError("truncated 3DS faces")
                count = struct.unpack_from("<H", data, body)[0]
                children = body + 2 + count * 8
                if children > stop:
                    raise ValueError("invalid 3DS face count")
                scan(children, stop, depth + 1, texture_map)
            elif kind == 0x4110:
                if stop - body < 2 or stop - body != 2 + struct.unpack_from("<H", data, body)[0] * 12:
                    raise ValueError("invalid 3DS vertex count")
            elif kind in containers or kind in maps:
                scan(body, stop, depth + 1, kind if kind in maps else texture_map)
            elif kind == 0xA300:
                payload = data[body:stop]
                if not payload.endswith(b"\x00") or b"\x00" in payload[:-1]:
                    raise ValueError("invalid 3DS texture filename")
                try:
                    ref = payload[:-1].decode("utf-8")
                except UnicodeError as error:
                    raise ValueError("3DS texture names must be UTF-8") from error
                if ref:
                    if texture_map != 0xA200:
                        raise ValueError("only 3DS diffuse textures are supported")
                    refs.append(ref)
            elif 0xB020 <= kind <= 0xB029:
                if stop - body < 14:
                    raise ValueError("truncated 3DS keyframe track")
                if struct.unpack_from("<I", data, body + 10)[0] > 1:
                    raise ValueError("3DS animation is outside initial support")
            start = stop
    scan(6, len(data))
    return refs
