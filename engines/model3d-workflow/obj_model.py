"""Bounded structural preflight for the admitted OBJ/MTL source composition.

Names remain literal. The staged directory is the only filesystem root, and
all dependency opens use directory descriptors without following symlinks.
"""
from __future__ import annotations

from contextlib import contextmanager
import math
import os
from pathlib import Path
import re
import stat
import unicodedata

MAX_COMPOSITION_BYTES = 64 << 20
MAX_COMPOSITION_REFS = 200
MAX_NAME_BYTES = 4096
MAX_LINE_BYTES = 1 << 20
TEXTURE_KEYWORDS = frozenset({
    "map_ka", "map_kd", "map_ks", "map_d", "map_ke", "map_emissive",
    "map_bump", "bump", "map_kn", "norm", "map_disp", "disp", "map_ns",
    "map_pr", "map_pm", "map_ps",
})
VALUE_KEYWORDS = frozenset({
    "newmtl", "ka", "kd", "ks", "ke", "ns", "ni", "d", "tr", "tf", "illum",
    "sharpness", "pr", "pm", "ps", "pc", "pcr", "aniso", "anisor",
})


def _token(line: str) -> tuple[str, str]:
    match = re.search(r"[ \t]", line)
    return (line[:match.start()], line[match.end():].strip(" \t")) if match else (line, "")


def _option_value(option: str, value: str) -> bool:
    if option in {"-blendu", "-blendv", "-clamp"}:
        return value in {"on", "off"}
    if option == "-imfchan":
        return len(value) == 1 and value in "rgbmlz"
    if option == "-texres":
        return bool(re.fullmatch(r"[+]?[0-9]+", value)) and 0 < int(value) < 2**31
    if not value.isascii() or value.strip() != value:
        return False
    try:
        number = float.fromhex(value) if "0x" in value.lower() else float(value)
        return math.isfinite(number)
    except ValueError:
        return False


def _texture_name(remainder: str) -> str:
    while remainder.startswith("-"):
        option, rest = _token(remainder)
        option = option.lower()
        if option in {"-o", "-s", "-t"}:
            count = 3
        elif option == "-mm":
            count = 2
        elif option in {"-blendu", "-blendv", "-clamp", "-boost", "-texres", "-bm", "-imfchan"}:
            count = 1
        else:
            raise ValueError("unsupported MTL texture option")
        for _ in range(count):
            value, rest = _token(rest)
            if not value or not _option_value(option, value):
                raise ValueError("invalid MTL texture option argument")
        remainder = rest
    return remainder


def parse_composition(data: bytes, *, material: bool = False) -> list[str]:
    """Parse one complete caller-supplied document; never open related files."""
    if len(data) > MAX_COMPOSITION_BYTES or not material and not data:
        raise ValueError("OBJ/MTL composition size exceeds its budget")
    refs, seen, declarations = [], set(), 0
    # Match the Go scanner's LF and optional CRLF boundaries, not splitlines'
    # additional Unicode/control-character line separators.
    for raw in data.split(b"\n"):
        if raw.endswith(b"\r"):
            raw = raw[:-1]
        if len(raw) > MAX_LINE_BYTES:
            raise ValueError("OBJ/MTL composition line exceeds its budget")
        line = raw.decode("utf-8", errors="strict")
        if any(ord(c) < 32 and c != "\t" or ord(c) == 127 or c == "\ufeff" for c in line):
            raise ValueError("unsupported OBJ/MTL composition text")
        trailing_space = line.endswith((" ", "\t"))
        line = line.strip(" \t")
        if not line or line.startswith("#"):
            continue
        if line.endswith("\\"):
            raise ValueError("OBJ/MTL continuation is unsupported")
        keyword, remainder = _token(line)
        lower, ref = keyword.lower(), ""
        if material:
            if lower in TEXTURE_KEYWORDS:
                ref = _texture_name(remainder)
            elif lower not in VALUE_KEYWORDS:
                raise ValueError("unsupported MTL composition statement")
        elif keyword == "mtllib":
            ref = remainder
        elif lower.startswith("mtllib") or lower in {"maplib", "usemap", "call", "csh", "shadow_obj", "trace_obj"}:
            raise ValueError("unsupported OBJ dependency statement")
        if not ref and (keyword == "mtllib" or material and lower in TEXTURE_KEYWORDS):
            raise ValueError("missing OBJ/MTL composition reference")
        if not ref:
            continue
        declarations += 1
        if (trailing_space or declarations > MAX_COMPOSITION_REFS or len(ref.encode("utf-8")) > MAX_NAME_BYTES
                or any(c in ref for c in "#\\\"'")):
            raise ValueError("unsupported OBJ/MTL composition reference")
        if ref not in seen:
            refs.append(ref)
            seen.add(ref)
    return refs


def _relative_parts(name: str) -> list[str]:
    if (not name or len(name.encode("utf-8")) > MAX_NAME_BYTES or name.startswith("/")
            or any(c in name for c in "\\:\x00\r\n\"'#")
            or any(unicodedata.category(c) == "Cc" for c in name)):
        raise ValueError("invalid OBJ relative dependency")
    parts = name.split("/")
    if any(part in {"", ".", ".."} for part in parts):
        raise ValueError("OBJ dependency escapes its exact relative path")
    return parts


@contextmanager
def _open_exact(root: Path, name: str):
    parts = _relative_parts(name)
    directories = []
    try:
        current = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        directories.append(current)
        for part in parts[:-1]:
            if part not in os.listdir(current):
                raise ValueError("missing OBJ dependency (exact case required)")
            current = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=current)
            directories.append(current)
        if parts[-1] not in os.listdir(current):
            raise ValueError("missing OBJ dependency (exact case required)")
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=current)
        with os.fdopen(descriptor, "rb") as source:
            if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                raise ValueError("OBJ dependencies must be ordinary files")
            yield source
    finally:
        for descriptor in reversed(directories):
            os.close(descriptor)


def _read_structure(root: Path, name: str, *, empty: bool = False, budget: int = MAX_COMPOSITION_BYTES) -> bytes:
    with _open_exact(root, name) as source:
        size = os.fstat(source.fileno()).st_size
        if size > budget or not empty and size == 0:
            raise ValueError("OBJ/MTL composition size exceeds its budget")
        data = source.read(size + 1)
        if len(data) != size:
            raise ValueError("OBJ/MTL composition changed while reading")
        return data


def validate_obj_source(path: Path) -> list[str]:
    if path.suffix.lower() != ".obj":
        raise ValueError("OBJ conversion requires an .obj primary entrypoint")
    root = path.parent.resolve(strict=True)
    data = _read_structure(root, path.name)
    libraries = parse_composition(data)
    names, textures, seen_textures = {path.name}, [], set()
    total_mtl = 0
    for name in libraries:
        parts = _relative_parts(name)
        if len(parts) != 1 or Path(name).suffix.lower() != ".mtl":
            raise ValueError("OBJ material libraries must be direct .mtl siblings")
        raw = _read_structure(root, name, empty=True, budget=MAX_COMPOSITION_BYTES - total_mtl)
        total_mtl += len(raw)
        if total_mtl > MAX_COMPOSITION_BYTES:
            raise ValueError("OBJ material library total exceeds 64 MiB")
        names.add(name)
        for ref in parse_composition(raw, material=True):
            _relative_parts(ref)
            if Path(ref).suffix.lower() not in {".png", ".jpg", ".jpeg"}:
                raise ValueError("OBJ textures must be PNG/JPEG")
            with _open_exact(root, ref) as texture:
                if os.fstat(texture.fileno()).st_size <= 0:
                    raise ValueError("OBJ textures must be nonempty")
            names.add(ref)
            if ref not in seen_textures:
                textures.append(ref)
                seen_textures.add(ref)
        if len(names) > MAX_COMPOSITION_REFS:
            raise ValueError("OBJ composition exceeds the exact reference budget")
    return textures
