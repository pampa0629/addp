"""Strict static glTF source profile; no network or directory discovery."""
from __future__ import annotations

import base64
import copy
import io
import json
import math
import re
import struct
import unicodedata
import warnings
from dataclasses import dataclass
from decimal import Decimal
from pathlib import Path
from urllib.parse import unquote_to_bytes, urlsplit

from PIL import Image

from glb_validation import _unsigned_values, _validate_primitive_indices, _validate_draw_count, _validate_texture_coordinates

MAX_MANIFEST = 16 * 1024 * 1024
MAX_SOURCE_BYTES = 64 * 1024 * 1024
MAX_IMAGE_PIXELS = 16 * 1024 * 1024
MAX_TOTAL_IMAGE_PIXELS = 64 * 1024 * 1024
MAX_EXTERNAL_FILES = 199
INT64_MAX = (1 << 63) - 1
COMPONENT_BYTES = {5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}
DIMENSIONS = {'SCALAR': 1, 'VEC2': 2, 'VEC3': 3, 'VEC4': 4, 'MAT2': 4, 'MAT3': 9, 'MAT4': 16}


@dataclass
class GLTFComposition:
    document: dict
    resources: list[dict]
    inline_buffers: list[bytes | None]
    verified_buffers: list[bytes] | None = None
    verified_images: list[tuple[bytes, str]] | None = None

    def paths(self) -> list[str]:
        return list(dict.fromkeys(resource['path'] for resource in self.resources))


def _number(raw: str):
    if len(raw) > 64 or ('e' in raw.lower() and abs(int(raw.lower().split('e')[1])) > 64):
        raise ValueError('glTF number outside representation budget')
    number = Decimal(raw)
    return int(number) if number == number.to_integral_value() else float(number)


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate glTF JSON member')
        result[key] = value
    return result


def _constant(_):
    raise ValueError('nonfinite glTF JSON number')


def _depth(raw: bytes):
    depth = 0
    quoted = escaped = False
    for value in raw:
        if quoted:
            if escaped:
                escaped = False
            elif value == 92:
                escaped = True
            elif value == 34:
                quoted = False
        elif value == 34:
            quoted = True
        elif value in (91, 123):
            depth += 1
            if depth > 65:
                raise ValueError('glTF JSON nesting outside budget')
        elif value in (93, 125):
            depth -= 1


def _profile(value, location='', depth=0):
    if depth > 64:
        raise ValueError('glTF JSON value depth outside budget')
    if isinstance(value, str):
        value.encode('utf-8', 'strict')
    elif isinstance(value, dict):
        for key, child in value.items():
            key.encode('utf-8', 'strict')
            if key == 'extensions' and (not isinstance(child, dict) or child):
                raise ValueError('glTF extensions unsupported')
            if key in ('extensionsUsed', 'extensionsRequired', 'targets', 'weights') or (not location and key in ('animations', 'skins')):
                if not isinstance(child, list) or child:
                    raise ValueError('animated, skinned, morph or extended glTF unsupported')
            if key == 'skin':
                raise ValueError('glTF skin unsupported')
            if key == 'uri':
                parts = location.split('/')
                if len(parts) != 3 or parts[1] not in ('buffers', 'images'):
                    raise ValueError('glTF URI outside core resource declaration')
            _profile(child, location + '/' + key.replace('~', '~0').replace('/', '~1'), depth + 1)
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _profile(child, location + '/' + str(index), depth + 1)


def _objects(root, key):
    value = root.get(key, [])
    if not isinstance(value, list) or any(not isinstance(item, dict) for item in value):
        raise ValueError('invalid glTF object array')
    return value


def _integer(obj, key, minimum=0, maximum=INT64_MAX, optional=False):
    if key not in obj and optional:
        return 0
    value = obj.get(key)
    if type(value) is not int or not minimum <= value <= maximum:
        raise ValueError('invalid glTF integer or index')
    return value


def _control(character):
    return unicodedata.category(character) == 'Cc'


def _unescape(raw):
    if re.search(r'%(?![0-9a-fA-F]{2})', raw):
        raise ValueError('invalid glTF URI escape')
    return unquote_to_bytes(raw)


def _resource_uri(raw):
    if not isinstance(raw, str) or not raw or any(_control(c) for c in raw):
        raise ValueError('invalid glTF resource URI')
    if raw.lower().startswith('data:'):
        header, separator, encoded = raw[5:].partition(',')
        if not separator:
            raise ValueError('invalid glTF data URI')
        header = header.lower()
        is_base64 = header.endswith(';base64')
        mime = header[:-7] if is_base64 else header
        if mime not in ('application/octet-stream', 'application/gltf-buffer', 'image/png', 'image/jpeg'):
            raise ValueError('unsupported glTF data MIME')
        payload = _unescape(encoded)
        if is_base64:
            decoded = base64.b64decode(payload, validate=True)
            if base64.b64encode(decoded) != payload:
                raise ValueError('noncanonical glTF base64')
            payload = decoded
        if not payload:
            raise ValueError('empty glTF data URI')
        return '', payload, mime
    if len(raw.encode('utf-8')) > 4096 or any(c in raw for c in '\\?#') or any(c.isspace() for c in raw):
        raise ValueError('unsupported glTF relative URI')
    parsed = urlsplit(raw)
    if parsed.scheme or parsed.netloc or parsed.query or parsed.fragment or re.search(r'%(?:2f|5c)', raw, re.I):
        raise ValueError('unsafe glTF relative URI')
    path = _unescape(parsed.path).decode('utf-8', 'strict')
    if not path or path.startswith('/') or any(c in path for c in '\\:') or any(_control(c) for c in path):
        raise ValueError('unsafe glTF decoded path')
    if any(part in ('', '.', '..') for part in path.split('/')):
        raise ValueError('unsafe glTF path component')
    return path, None, ''


def _image(payload, mime, budget):
    try:
        return _decode_image(payload, mime, budget)
    except (OSError, ValueError, SyntaxError, Image.DecompressionBombWarning, Image.DecompressionBombError) as error:
        raise ValueError('invalid glTF image content or pixel budget') from error


def _decode_image(payload, mime, budget):
    with warnings.catch_warnings():
        warnings.simplefilter('error', Image.DecompressionBombWarning)
        with Image.open(io.BytesIO(payload)) as image:
            actual = {'PNG': 'image/png', 'JPEG': 'image/jpeg'}.get(image.format)
            pixels = image.width * image.height
            if actual is None or (mime and actual != mime) or not 0 < pixels <= min(MAX_IMAGE_PIXELS, budget[0]):
                raise ValueError('invalid glTF image MIME or pixel budget')
            image.verify()
        with Image.open(io.BytesIO(payload)) as image:
            image.load()
        budget[0] -= pixels
    return actual


def parse_composition(raw: bytes) -> GLTFComposition:
    if not 0 < len(raw) <= MAX_MANIFEST:
        raise ValueError('glTF manifest outside size budget')
    _depth(raw)
    doc = json.loads(raw.decode('utf-8', 'strict'), object_pairs_hook=_object, parse_int=_number, parse_float=_number, parse_constant=_constant)
    if not isinstance(doc, dict):
        raise ValueError('glTF manifest must be an object')
    _profile(doc)
    asset = doc.get('asset')
    if not isinstance(asset, dict) or asset.get('version') != '2.0' or ('minVersion' in asset and asset['minVersion'] != '2.0'):
        raise ValueError('glTF requires asset version 2.0')
    arrays = {key: _objects(doc, key) for key in ('buffers', 'bufferViews', 'images', 'accessors', 'meshes', 'nodes', 'scenes', 'textures', 'materials', 'samplers', 'cameras')}
    refs, inline, lengths, roles = [], [], [], {}
    budget = [MAX_TOTAL_IMAGE_PIXELS]

    def reference(role, index, uri, path, length=0, mime=''):
        if path in roles and roles[path] != role:
            raise ValueError('glTF resource has conflicting roles')
        roles[path] = role
        if len(roles) > MAX_EXTERNAL_FILES:
            raise ValueError('glTF external-file budget exceeded')
        refs.append(dict(role=role, index=index, uri=uri, path=path, byteLength=length, mimeType=mime))

    for index, buffer in enumerate(arrays['buffers']):
        length = _integer(buffer, 'byteLength', 1)
        path, payload, mime = _resource_uri(buffer.get('uri'))
        if not path:
            if mime not in ('application/octet-stream', 'application/gltf-buffer') or len(payload) < length:
                raise ValueError('invalid or truncated glTF inline buffer')
        else:
            reference('buffer', index, buffer['uri'], path, length)
        lengths.append(length)
        inline.append(payload)
    for view in arrays['bufferViews']:
        buffer = _integer(view, 'buffer', maximum=len(lengths) - 1)
        offset, length = _integer(view, 'byteOffset', optional=True), _integer(view, 'byteLength', 1)
        if offset + length > lengths[buffer]:
            raise ValueError('glTF bufferView exceeds declared buffer')
        if 'byteStride' in view and _integer(view, 'byteStride', 4, 252) % 4:
            raise ValueError('invalid glTF byteStride')
    for index, picture in enumerate(arrays['images']):
        if ('uri' in picture) == ('bufferView' in picture):
            raise ValueError('glTF image requires one URI or bufferView')
        mime = picture.get('mimeType', '')
        if 'mimeType' in picture and mime not in ('image/png', 'image/jpeg'):
            raise ValueError('unsupported glTF image MIME')
        if 'uri' in picture:
            path, payload, inline_mime = _resource_uri(picture['uri'])
            if path:
                reference('image', index, picture['uri'], path, mime=mime)
            else:
                if mime and mime != inline_mime:
                    raise ValueError('glTF image MIME mismatch')
                _image(payload, inline_mime, budget)
        else:
            if not mime:
                raise ValueError('glTF bufferView image requires MIME')
            view = arrays['bufferViews'][_integer(picture, 'bufferView', maximum=len(arrays['bufferViews']) - 1)]
            data = inline[view['buffer']]
            if data is not None:
                start = view.get('byteOffset', 0)
                _image(data[start:start + view['byteLength']], mime, budget)
    _accessors(arrays)
    _scene(doc, arrays)
    return GLTFComposition(doc, refs, inline)


def _element(accessor):
    kind = accessor['type']
    size = COMPONENT_BYTES[accessor['componentType']]
    if kind.startswith('MAT'):
        columns = int(kind[-1])
        return columns * ((columns * size + 3) // 4 * 4)
    return DIMENSIONS[kind] * size


def _accessors(arrays):
    views = arrays['bufferViews']
    remaining = MAX_SOURCE_BYTES
    for accessor in arrays['accessors']:
        count = _integer(accessor, 'count', 1)
        component = _integer(accessor, 'componentType', 5120, 5126)
        if component not in COMPONENT_BYTES or accessor.get('type') not in DIMENSIONS:
            raise ValueError('unsupported glTF accessor format')
        width, element = COMPONENT_BYTES[component], _element(accessor)
        remaining -= count * element
        if remaining < 0:
            raise ValueError('glTF addressed accessor bytes outside budget')
        offset = _integer(accessor, 'byteOffset', optional=True)
        if offset % width:
            raise ValueError('unaligned glTF accessor')
        if 'bufferView' in accessor:
            view = views[_integer(accessor, 'bufferView', maximum=len(views) - 1)]
            stride = view.get('byteStride', element)
            if stride < element or (view.get('byteOffset', 0) + offset) % width or offset + element + (count - 1) * stride > view['byteLength']:
                raise ValueError('glTF accessor exceeds bufferView')
        if 'sparse' in accessor:
            sparse = accessor['sparse']
            if not isinstance(sparse, dict):
                raise ValueError('invalid glTF sparse accessor')
            sparse_count = _integer(sparse, 'count', 1, count)
            for key in ('indices', 'values'):
                part = sparse.get(key)
                if not isinstance(part, dict):
                    raise ValueError('invalid glTF sparse part')
                view = views[_integer(part, 'bufferView', maximum=len(views) - 1)]
                part_offset = _integer(part, 'byteOffset', optional=True)
                size, alignment = element, width
                if key == 'indices':
                    component = _integer(part, 'componentType', 5121, 5125)
                    size = {5121: 1, 5123: 2, 5125: 4}.get(component, 0)
                    alignment = size
                if not size or 'byteStride' in view or part_offset % alignment or (view.get('byteOffset', 0) + part_offset) % alignment or part_offset + sparse_count * size > view['byteLength']:
                    raise ValueError('invalid glTF sparse storage')


def _scene(doc, arrays):
    for texture in arrays['textures']:
        _integer(texture, 'source', maximum=len(arrays['images']) - 1)
        if 'sampler' in texture:
            _integer(texture, 'sampler', maximum=len(arrays['samplers']) - 1)
    for mesh in arrays['meshes']:
        primitives = _objects(mesh, 'primitives')
        if not primitives:
            raise ValueError('glTF mesh requires primitives')
        for primitive in primitives:
            attrs = primitive.get('attributes')
            if not isinstance(attrs, dict) or 'POSITION' not in attrs:
                raise ValueError('glTF primitive requires POSITION')
            for key in attrs:
                _integer(attrs, key, maximum=len(arrays['accessors']) - 1)
            for key, maximum in (('indices', len(arrays['accessors']) - 1), ('material', len(arrays['materials']) - 1), ('mode', 6)):
                if key in primitive:
                    _integer(primitive, key, maximum=maximum)
    parents, children = [0] * len(arrays['nodes']), []
    for index, node in enumerate(arrays['nodes']):
        for key, maximum in (('mesh', len(arrays['meshes']) - 1), ('camera', len(arrays['cameras']) - 1)):
            if key in node:
                _integer(node, key, maximum=maximum)
        for key, size in (('matrix', 16), ('translation', 3), ('rotation', 4), ('scale', 3)):
            if key in node:
                values = node[key]
                if ('matrix' in node and key != 'matrix') or not isinstance(values, list) or len(values) != size or not all(type(v) in (int, float) and math.isfinite(v) for v in values):
                    raise ValueError('invalid glTF node transform')
                if key == 'rotation' and abs(sum(v * v for v in values) - 1) > 1e-5:
                    raise ValueError('invalid glTF quaternion')
        descendants = node.get('children', [])
        if not isinstance(descendants, list):
            raise ValueError('invalid glTF children')
        seen = set()
        for child in descendants:
            _integer({'index': child}, 'index', maximum=len(parents) - 1)
            if child == index or child in seen:
                raise ValueError('invalid glTF child')
            seen.add(child)
            parents[child] += 1
            if parents[child] > 1:
                raise ValueError('glTF node has multiple parents')
        children.append(descendants)
    state = [0] * len(parents)
    heights = [0] * len(parents)

    def visit(index, depth=0):
        if depth > 64 or state[index] == 1:
            raise ValueError('glTF cycle or scene depth outside budget')
        if state[index] == 2:
            if depth + heights[index] > 64:
                raise ValueError('glTF scene depth outside budget')
            return
        state[index] = 1
        for child in children[index]:
            visit(child, depth + 1)
            heights[index] = max(heights[index], heights[child] + 1)
        state[index] = 2

    for index in range(len(parents)):
        visit(index)
    for scene in arrays['scenes']:
        roots = scene.get('nodes', [])
        if not isinstance(roots, list):
            raise ValueError('invalid glTF scene roots')
        seen = set()
        for index in roots:
            _integer({'index': index}, 'index', maximum=len(parents) - 1)
            if index in seen or parents[index]:
                raise ValueError('invalid glTF scene root')
            seen.add(index)
    if 'scene' in doc:
        _integer(doc, 'scene', maximum=len(arrays['scenes']) - 1)


def _texture_infos(material):
    if not isinstance(material, dict) or not isinstance(material.get('pbrMetallicRoughness', {}), dict):
        raise ValueError('invalid glTF material')
    return [material.get(key) for key in ('normalTexture', 'occlusionTexture', 'emissiveTexture')] + [material.get('pbrMetallicRoughness', {}).get(key) for key in ('baseColorTexture', 'metallicRoughnessTexture')]


def validate_source(primary: Path) -> GLTFComposition:
    """Check only declared actual members, relative to the same primary file."""
    if primary.is_symlink():
        raise ValueError('glTF primary cannot be a symlink')
    with primary.open('rb') as stream:
        raw = stream.read(MAX_MANIFEST + 1)
    composition = parse_composition(raw)
    root = primary.parent.resolve()
    remaining = MAX_SOURCE_BYTES - len(raw)
    resources = {}
    for relative in composition.paths():
        target = root
        for part in relative.split('/'):
            target = target / part
            if target.is_symlink():
                raise ValueError('glTF dependency cannot be a symlink')
        if target.resolve() == primary.resolve() or not target.resolve().is_relative_to(root) or not target.is_file():
            raise ValueError('glTF dependency missing or outside model root')
        with target.open('rb') as stream:
            payload = stream.read(remaining + 1)
        remaining -= len(payload)
        if remaining < 0:
            raise ValueError('glTF source bytes outside budget')
        resources[relative] = payload
    doc = composition.document
    buffers, packed, bases = [], bytearray(), []
    for index, buffer in enumerate(doc.get('buffers', [])):
        path, inline, _ = _resource_uri(buffer['uri'])
        payload = resources[path] if path else inline
        length = buffer['byteLength']
        if len(payload) < length:
            raise ValueError('glTF dependency buffer is truncated')
        buffers.append(payload[:length])
        packed.extend(b'\0' * (-len(packed) % 4))
        bases.append(len(packed))
        if len(packed) + length > MAX_SOURCE_BYTES:
            raise ValueError('glTF addressed buffer bytes outside budget')
        packed.extend(payload[:length])
    views = copy.deepcopy(doc.get('bufferViews', []))
    for view in views:
        view['byteOffset'] = view.get('byteOffset', 0) + bases[view['buffer']]
        view['buffer'] = 0
    budget = [MAX_TOTAL_IMAGE_PIXELS]
    pictures = []
    for picture in doc.get('images', []):
        if 'uri' in picture:
            path, payload, mime = _resource_uri(picture['uri'])
            payload = resources[path] if path else payload
            mime = picture.get('mimeType', mime)
        else:
            view = doc['bufferViews'][picture['bufferView']]
            start = view.get('byteOffset', 0)
            payload = buffers[view['buffer']][start:start + view['byteLength']]
            mime = picture['mimeType']
        actual_mime = _image(payload, mime, budget)
        pictures.append((payload, actual_mime))
    if sum(len(data) for data in buffers) + sum(len(data) for data, _ in pictures) > MAX_SOURCE_BYTES:
        raise ValueError('glTF expanded conversion bytes outside budget')
    accessors, textures, materials = doc.get('accessors', []), doc.get('textures', []), doc.get('materials', [])
    _finite_accessor_payloads(accessors, views, packed)
    for material in materials:
        for info in _texture_infos(material):
            if info is not None:
                if not isinstance(info, dict):
                    raise ValueError('invalid glTF texture info')
                _integer(info, 'index', maximum=len(textures) - 1)
    for mesh in doc.get('meshes', []):
        for primitive in mesh['primitives']:
            position = accessors[primitive['attributes']['POSITION']]
            if position['type'] != 'VEC3' or position['componentType'] != 5126:
                raise ValueError('glTF POSITION must be float VEC3')
            count = _validate_primitive_indices(primitive, accessors, views, packed, position['count'])
            _validate_draw_count(primitive, count)
            if 'material' in primitive:
                for info in _texture_infos(materials[primitive['material']]):
                    if info is not None:
                        _validate_texture_coordinates(primitive, info, accessors, views, position['count'])
    composition.verified_buffers = buffers
    composition.verified_images = pictures
    return composition


def _finite_accessor_payloads(accessors, views, binary):
    for accessor in accessors:
        sparse = accessor.get('sparse')
        positions = iter(())
        sparse_start = 0
        if sparse:
            count = sparse['count']
            binding = sparse['indices']
            positions = _unsigned_values(binding, binding['componentType'], count, views, binary, sparse=True)
            value_binding = sparse['values']
            sparse_start = views[value_binding['bufferView']].get('byteOffset', 0) + value_binding.get('byteOffset', 0)
        if accessor['componentType'] != 5126:
            previous = -1
            for position in positions:
                if not previous < position < accessor['count']:
                    raise ValueError('invalid glTF sparse index order or range')
                previous = position
            continue
        width = DIMENSIONS[accessor['type']]
        element = width * 4
        view = views[accessor['bufferView']] if 'bufferView' in accessor else None
        start = (view.get('byteOffset', 0) + accessor.get('byteOffset', 0)) if view else 0
        stride = view.get('byteStride', element) if view else element
        replacement = next(positions, None)
        previous, sparse_index = -1, 0
        for index in range(accessor['count']):
            if replacement is not None and not previous < replacement < accessor['count']:
                raise ValueError('invalid glTF sparse index order or range')
            if replacement == index:
                offset = sparse_start + sparse_index * element
                previous = replacement
                replacement = next(positions, None)
                sparse_index += 1
            elif view:
                offset = start + index * stride
            else:
                continue  # Unspecified base storage is zero.
            if not all(math.isfinite(value) for value in struct.unpack_from('<' + 'f' * width, binary, offset)):
                raise ValueError('nonfinite glTF accessor payload')


def prepare_converter_source(composition: GLTFComposition, work_dir: Path) -> Path:
    """Adapt verified bytes to standard base64 glTF in private scratch space.

    The source snapshot and its names are untouched. This is an input adapter,
    never a published model or an alternative conversion route.
    """
    if composition.verified_buffers is None or composition.verified_images is None:
        raise ValueError('glTF converter input requires verified actual source bytes')
    root = work_dir / 'gltf-input'
    root.mkdir()
    doc = copy.deepcopy(composition.document)
    for index, payload in enumerate(composition.verified_buffers):
        doc['buffers'][index]['uri'] = 'data:application/octet-stream;base64,' + base64.b64encode(payload).decode('ascii')
    for index, (payload, mime) in enumerate(composition.verified_images):
        picture = doc['images'][index]
        picture.pop('bufferView', None)
        picture['uri'] = 'data:' + mime + ';base64,' + base64.b64encode(payload).decode('ascii')
        picture['mimeType'] = mime
    primary = root / 'model.gltf'
    primary.write_text(json.dumps(doc, ensure_ascii=False, allow_nan=False, separators=(',', ':')), encoding='utf-8')
    return primary


def has_used_textures(composition: GLTFComposition) -> bool:
    doc = composition.document
    return any(info is not None for mesh in doc.get('meshes', []) for primitive in mesh['primitives'] if 'material' in primitive for info in _texture_infos(doc['materials'][primitive['material']]))
