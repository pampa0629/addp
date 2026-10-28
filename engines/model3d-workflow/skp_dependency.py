"""Prepare/verify the sole, source-pinned OpenSKP 1.3.0 repair.

Run prepare only while the owning Runtime is stopped. Lifecycle callers hold
ADDP's dependency lock. Unknown sources require reinstalling the pinned wheel.
"""
from __future__ import annotations

import argparse
import ast
import base64
import csv
import hashlib
import importlib.metadata
import io
import os
from pathlib import Path
import sys
import tempfile
import textwrap

ORIGINAL = {'_core.py': 'a20e702622e768b3cda6e73d46fb9a72ff45739f2588de3c93cd6cf4f177c818',
 'scene.py': '32f773d3c8261a8bd4d0f9b9e4953e78ac9c55696c542a0a2fc6145acbfd58a5',
 'instanced_scene.py': '433b441201f207e340773e5b926871b2628577593488bb3b3d5df8c9129e4299',
 '_face_groups.py': '1db19585c0f11c5351d9f5d0d52362d9dd49b49452a98361485b5c725736f6c4',
 'export/glb.py': '90f48de467fd7264baf1b00f7400dbfe0447279fe30776ca8b0395af5bc6f71f'}
REPAIRED = {'_core.py': '8a33e2faab8abf94845c0fe2bba7b0242aa097f771dde8b936329d6e5aea53ce',
 'scene.py': '4d4a6d7f567289f7d32d16039540c624a1155e003b9eb07bae43f72f7b1987ea',
 'instanced_scene.py': '7e009d59285fdcc434ede2259db3379415fe6079b02f08f9dac36900784e8643',
 '_face_groups.py': '188dff4be6f2d71f7161adaa0cbf993dd00397172432cfa40f9b182071450785',
 'export/glb.py': '817040409539c19339582d04e9299fcae52c9a2906ab861c514756170a28385b'}


def _digest(data):
    return hashlib.sha256(data).hexdigest()


def _replace_function(source, name, replacement):
    matches = [node for node in ast.walk(ast.parse(source))
               if isinstance(node, ast.FunctionDef) and node.name == name]
    if len(matches) != 1:
        raise ValueError(f"Expected one OpenSKP function {name}")
    node = matches[0]
    lines = source.splitlines(keepends=True)
    lines[node.lineno - 1:node.end_lineno] = [textwrap.indent(textwrap.dedent(replacement).strip(), " " * node.col_offset) + "\n"]
    return "".join(lines)


def repaired_source(name, data):
    if _digest(data) != ORIGINAL[name]:
        raise ValueError(f"Unrecognized OpenSKP 1.3.0 source: {name}")
    source = data.decode("utf-8")
    if name == "_core.py":
        source = _replace_function(source, "_extract_texture", """
            def _extract_texture(zf, xml_name, mat_elem, ns):
                from skp_materials import extract_texture
                return extract_texture(zf, xml_name, mat_elem, ns)
        """)
        old = "                mat_elem = root.find('.//mat:material', MAT_NS)"
        new = "                from skp_materials import material_element\n                mat_elem = material_element(root)"
        if source.count(old) != 1:
            raise ValueError("OpenSKP material hook drift")
        source = source.replace(old, new)
        old = '            except Exception:\n                logger.debug("Failed to parse material.xml %r", name, exc_info=True)'
        new = '            except Exception as error:\n                raise SkpParseError(f"Invalid SKP material {name!r}: {error}", stage="materials") from error'
        if source.count(old) != 1:
            raise ValueError("OpenSKP material failure hook drift")
        source = source.replace(old, new)
        # Reject duplicate material XML before ZipFile.read can select one.
        old = "            _validate_zip_entry_size(zf, name)\n            xml_data = zf.read(name)"
        new = '            if zf.namelist().count(name) != 1:\n                raise SkpParseError("Duplicate SKP material XML", stage="materials")\n' + old
        if source.count(old) != 1:
            raise ValueError("OpenSKP archive hook drift")
        source = source.replace(old, new)
    elif name in ("scene.py", "instanced_scene.py"):
        source = source.replace("texture_index_by_key: Dict[str, int] = {}", "texture_index_by_key: Dict[bytes, int] = {}")
        source = _replace_function(source, "texture_index_for", '''
            def texture_index_for(tex: Optional[Dict[str, Any]]) -> Optional[int]:
                if tex is None:
                    return None
                data = tex.get("data")
                if not isinstance(data, bytes) or not data:
                    raise ValueError("Missing declared SKP texture data")
                mime_type = _sniff_image_mime(data)
                if mime_type is None:
                    raise ValueError("Unsupported declared SKP texture MIME")
                # bytes keys compare full content, including on hash collision.
                hit = texture_index_by_key.get(data)
                if hit is not None:
                    return hit
                idx = len(textures)
                textures.append(SceneTexture(data=data, mime_type=mime_type, filename=tex.get("filename", "")))
                texture_index_by_key[data] = idx
                return idx
        ''')
    elif name == "_face_groups.py":
        old = "        if front_color == back_color:"
        new = ('        if (front_color == back_color and front_mat == back_mat\n'
               '                and f_data.get("uv_transform") == f_data.get("uv_transform_back")):')
        if source.count(old) != 1:
            raise ValueError("OpenSKP face-side material hook drift")
        source = source.replace(old, new)
    elif name == "export/glb.py":
        old = '            doubleSided=mat.get("doubleSided", False),'
        new = old + '\n            alphaMode=mat.get("alphaMode", "OPAQUE"),'
        if source.count(old) != 1:
            raise ValueError("OpenSKP material alpha hook drift")
        source = source.replace(old, new)
    compile(source, name, "exec")
    return source.encode("utf-8")


def _files():
    distribution = importlib.metadata.distribution("openskp")
    if distribution.version != "1.3.0":
        raise ValueError("Expected fixed OpenSKP 1.3.0")
    result = {}
    for name in ORIGINAL:
        path = Path(distribution.locate_file("openskp/" + name))
        if path.is_symlink() or not path.resolve().is_relative_to(Path(sys.prefix).resolve()):
            raise ValueError("OpenSKP repair requires an owned installation")
        result[name] = path
    records = [entry for entry in distribution.files or () if str(entry).endswith(".dist-info/RECORD")]
    if len(records) != 1:
        raise ValueError("Missing unique OpenSKP RECORD")
    record = Path(distribution.locate_file(records[0]))
    if record.is_symlink() or not record.resolve().is_relative_to(Path(sys.prefix).resolve()):
        raise ValueError("OpenSKP RECORD is outside owned installation")
    return result, record


def verify():
    files, record = _files()
    rows = list(csv.reader(io.StringIO(record.read_text())))
    for name, path in files.items():
        data = path.read_bytes()
        if _digest(data) != REPAIRED[name]:
            raise ValueError(f"OpenSKP repair missing or changed: {name}; stop Runtime and prepare dependencies")
        matches = [row for row in rows if row and row[0] == "openskp/" + name]
        expected = "sha256=" + base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip("=")
        if matches != [["openskp/" + name, expected, str(len(data))]]:
            raise ValueError(f"OpenSKP RECORD mismatch: {name}")


def _atomic(path, data):
    descriptor, temporary = tempfile.mkstemp(prefix=".addp-skp-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
        os.chmod(temporary, path.stat().st_mode & 0o777)
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def prepare():
    files, record = _files()
    original = {path: path.read_bytes() for path in [*files.values(), record]}
    replacements = {}
    for name, path in files.items():
        data = original[path]
        if _digest(data) == REPAIRED[name]:
            replacements[path] = data
        else:
            replacements[path] = repaired_source(name, data)
        if _digest(replacements[path]) != REPAIRED[name]:
            raise ValueError(f"OpenSKP repair output mismatch: {name}")
    rows = list(csv.reader(io.StringIO(original[record].decode())))
    for name, path in files.items():
        matches = [row for row in rows if row and row[0] == "openskp/" + name]
        if len(matches) != 1:
            raise ValueError(f"Missing unique OpenSKP RECORD entry: {name}")
        data = replacements[path]
        matches[0][1:] = ["sha256=" + base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip("="), str(len(data))]
    output = io.StringIO(newline="")
    csv.writer(output, lineterminator="\n").writerows(rows)
    replacements[record] = output.getvalue().encode()
    try:
        for path, data in replacements.items():
            if data != original[path]:
                _atomic(path, data)
                # Never leave a valid timestamp-based bytecode of old source.
                if path.suffix == ".py":
                    for cache in (path.parent / "__pycache__").glob(path.stem + ".*.pyc"):
                        cache.unlink()
        verify()
    except BaseException:
        for path, data in original.items():
            _atomic(path, data)
        raise


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("prepare", "verify"))
    arguments = parser.parse_args()
    {"prepare": prepare, "verify": verify}[arguments.operation]()
