"""Bounded Binary 7400 static, untextured FBX source proof.

The Common plugin owns the same profile; both consume the shared case corpus.
This reader does not load related files or replace the native mesh converter.
"""
from __future__ import annotations

from dataclasses import dataclass
import math
import os
from pathlib import Path
import stat
import struct
import zlib

PROFILE = 'binary7400_static_untextured'
MAX_BYTES = 64 << 20
MAGIC = b'Kaydara FBX Binary  \x00\x1a\x00'
END_MAGIC = bytes.fromhex('f85a8c6adef5d97eece90ce3758f290b')
LAYERS = {
    'LayerElementNormal': ('Normals', 'NormalsIndex', 3),
    'LayerElementBinormal': ('Binormals', 'BinormalsIndex', 3),
    'LayerElementTangent': ('Tangents', 'TangentsIndex', 3),
    'LayerElementColor': ('Colors', 'ColorIndex', 4),
    'LayerElementUV': ('UV', 'UVIndex', 2),
    'LayerElementMaterial': ('Materials', '', 1),
    'LayerElementSmoothing': ('Smoothing', '', 1),
}


def require(condition, reason):
    if not condition:
        raise ValueError(f'FBX is outside {PROFILE}: {reason}')


@dataclass
class Array:
    kind: str
    count: int
    data: bytes


@dataclass
class Node:
    name: str
    values: list
    children: list


def one(nodes, name):
    found = [n for n in nodes if n.name == name]
    return found[0] if len(found) == 1 else None


def only(node, names):
    require(all(c.name in names for c in node.children), f'unsupported child of {node.name}')


def unique(node):
    require(len({c.name for c in node.children}) == len(node.children), 'duplicate field of ' + node.name)


def integer(node, value):
    return node is not None and not node.children and node.values == [value] and type(node.values[0]) is int


class Reader:
    def __init__(self, raw):
        self.raw = raw
        self.nodes = self.properties = self.expanded = 0

    def budget(self, size):
        self.expanded += size
        require(self.expanded <= MAX_BYTES, 'expanded declarations exceed 64 MiB')

    def property(self, at, limit):
        require(at < limit, 'short property')
        kind = chr(self.raw[at]); at += 1
        scalar = {'Y': ('h', 2), 'C': ('B', 1), 'I': ('i', 4), 'L': ('q', 8), 'F': ('f', 4), 'D': ('d', 8)}
        if kind in scalar:
            fmt, size = scalar[kind]
            require(at + size <= limit, 'short scalar')
            value = struct.unpack_from('<' + fmt, self.raw, at)[0]
            require(kind != 'C' or value in (0, 1), 'invalid boolean')
            require(not isinstance(value, float) or math.isfinite(value), 'nonfinite scalar')
            self.budget(size)
            return value, at + size
        if kind in ('S', 'R'):
            require(at + 4 <= limit, 'short string header')
            length = struct.unpack_from('<I', self.raw, at)[0]; at += 4
            require(at + length <= limit, 'short string')
            self.budget(length)
            data = self.raw[at:at + length]
            return data.decode('utf8') if kind == 'S' else data, at + length
        widths = {'f': 4, 'd': 8, 'i': 4, 'l': 8, 'b': 1, 'c': 1}
        require(kind in widths and at + 12 <= limit, 'unsupported array type or short header')
        count, encoding, length = struct.unpack_from('<III', self.raw, at); at += 12
        expected = count * widths[kind]
        require(at + length <= limit and expected <= MAX_BYTES, 'array length exceeds its frame/budget')
        self.budget(expected)
        data = self.raw[at:at + length]
        if encoding == 1:
            decoder = zlib.decompressobj()
            try:
                data = decoder.decompress(data, expected + 1)
            except zlib.error as error:
                raise ValueError('invalid FBX compressed array') from error
            require(len(data) == expected and decoder.eof and not decoder.unused_data and not decoder.unconsumed_tail,
                    'invalid compressed stream or expanded array length')
        else:
            require(encoding == 0 and len(data) == expected, 'invalid uncompressed array')
        if kind in ('f', 'd'):
            require(all(math.isfinite(v[0]) for v in struct.iter_unpack('<' + kind, data)), 'nonfinite array')
        return Array(kind, count, data), at + length

    def record(self, at, limit, depth):
        self.nodes += 1
        require(depth < 64 and self.nodes <= 200000 and at + 13 <= limit, 'record/depth/node budget')
        end, count, length, name_length = struct.unpack_from('<IIIB', self.raw, at)
        start = at + 13 + name_length; prop_end = start + length
        require(name_length > 0 and at < end <= limit and prop_end <= end
                and count <= 1000000 - self.properties, 'invalid record frame or property budget')
        self.properties += count
        name = self.raw[at + 13:start]
        require(all(33 <= b <= 126 for b in name), 'invalid record name')
        self.budget(len(name))
        node = Node(name.decode('ascii'), [], [])
        at = start
        for _ in range(count):
            value, at = self.property(at, prop_end); node.values.append(value)
        require(at == prop_end, 'property frame has trailing bytes')
        while at < end:
            require(at + 13 <= end, 'short child record')
            if self.raw[at:at + 13] == bytes(13):
                require(at + 13 == end, 'child terminator has trailing declarations')
                at += 13; break
            child, at = self.record(at, end - 13, depth + 1); node.children.append(child)
        require(at == end, 'record end mismatch')
        return node, at


def array(node):
    require(node is not None and len(node.values) == 1 and isinstance(node.values[0], Array), 'missing array')
    return node.values[0]


def validate_layer(layer, vertices, corners, polygons):
    unique(layer)
    mapping, reference = one(layer.children, 'MappingInformationType'), one(layer.children, 'ReferenceInformationType')
    require(mapping is not None and reference is not None and len(mapping.values) == len(reference.values) == 1,
            'missing layer mapping')
    require(isinstance(mapping.values[0], str), 'invalid layer mapping type')
    expected = {'ByVertice': vertices, 'ByPolygonVertex': corners, 'ByPolygon': polygons, 'AllSame': 1}.get(mapping.values[0], 0)
    require(expected > 0, 'unsupported layer mapping')
    field, index, width = LAYERS[layer.name]
    data = array(one(layer.children, field))
    require(data.count > 0 and data.count % width == 0, 'invalid direct layer array')
    allowed = {'Version', 'Name', 'MappingInformationType', 'ReferenceInformationType', field}
    if index: allowed.add(index)
    w = {'LayerElementNormal': 'NormalsW', 'LayerElementBinormal': 'BinormalsW', 'LayerElementTangent': 'TangentsW'}
    if layer.name in w: allowed.add(w[layer.name])
    only(layer, allowed)
    if layer.name in ('LayerElementMaterial', 'LayerElementSmoothing'):
        require((data.kind == 'i' or layer.name == 'LayerElementSmoothing' and data.kind == 'b')
                and data.count == expected and reference.values[0] in ('Direct', 'IndexToDirect'), 'invalid discrete layer')
        return
    require(data.kind in ('f', 'd'), 'layer requires real numeric data')
    if reference.values[0] == 'Direct':
        require(one(layer.children, index) is None and data.count // width == expected, 'invalid Direct layer')
        return
    require(reference.values[0] == 'IndexToDirect', 'unsupported reference mode')
    indices = array(one(layer.children, index))
    require(indices.kind == 'i' and indices.count == expected
            and all(0 <= i[0] < data.count // width for i in struct.iter_unpack('<i', indices.data)), 'missing or invalid layer indices')


def validate_mesh(node):
    only(node, {'GeometryVersion', 'Properties70', 'Vertices', 'PolygonVertexIndex', 'Edges', 'Layer', *LAYERS})
    vertices, indices = array(one(node.children, 'Vertices')), array(one(node.children, 'PolygonVertexIndex'))
    require(vertices.kind in ('f', 'd') and vertices.count >= 9 and vertices.count % 3 == 0
            and indices.kind == 'i' and indices.count >= 3, 'invalid mesh arrays')
    size = polygons = 0
    for (index,) in struct.iter_unpack('<i', indices.data):
        size += 1; last = index < 0
        if last: index = -index - 1
        require(index < vertices.count // 3, 'polygon vertex outside source vertices')
        if last:
            require(size >= 3, 'polygon has fewer than three vertices'); size = 0; polygons += 1
    require(size == 0, 'unterminated polygon')
    layers = set()
    for layer in node.children:
        if layer.name not in LAYERS: continue
        require(len(layer.values) == 1 and type(layer.values[0]) is int and layer.values[0] >= 0
                and (layer.name, layer.values[0]) not in layers, 'duplicate or invalid layer index')
        layers.add((layer.name, layer.values[0]))
        validate_layer(layer, vertices.count // 3, indices.count, polygons)
    for layer in (c for c in node.children if c.name == 'Layer'):
        for entry in (c for c in layer.children if c.name == 'LayerElement'):
            kind, index = one(entry.children, 'Type'), one(entry.children, 'TypedIndex')
            require(kind is not None and index is not None and len(kind.values) == len(index.values) == 1
                    and isinstance(kind.values[0], str) and type(index.values[0]) is int
                    and (kind.values[0], index.values[0]) in layers, 'dangling layer declaration')


def validate_declarations(node):
    name, values = node.name, node.values
    if name == 'Properties70':
        require(not values, 'Properties70 cannot carry values'); only(node, {'P'})
    elif name == 'P':
        require(len(values) >= 4 and not node.children and all(isinstance(v, str) for v in values[:4])
                and all(type(v) in (str, int, float) for v in values), 'invalid property declaration')
        if values[0] == 'Visibility' and len(values) > 4: require(values[4] == 1, 'hidden model outside profile')
    elif name == 'CreationTimeStamp': only(node, {'Version', 'Year', 'Month', 'Day', 'Hour', 'Minute', 'Second', 'Millisecond'})
    elif name == 'SceneInfo': only(node, {'Type', 'Version', 'MetaData', 'Properties70'})
    elif name == 'MetaData': only(node, {'Version', 'Title', 'Subject', 'Author', 'Keywords', 'Revision', 'Comment'})
    elif name == 'GlobalSettings': only(node, {'Version', 'Properties70'})
    elif name == 'Documents': only(node, {'Count', 'Document'})
    elif name == 'Document':
        require(len(values) == 3 and values[2] == 'Scene', 'unsupported document'); only(node, {'Properties70', 'RootNode'})
    elif name == 'PropertyTemplate':
        require(len(values) == 1, 'invalid property template'); only(node, {'Properties70'})
    elif name == 'Layer': only(node, {'Version', 'LayerElement'})
    elif name == 'LayerElement':
        only(node, {'Type', 'TypedIndex'}); kind = one(node.children, 'Type')
        require(kind is not None and len(kind.values) == 1 and isinstance(kind.values[0], str) and kind.values[0] in LAYERS,
                'unsupported layer element type')
    elif name in LAYERS:
        only(node, {'Version', 'Name', 'MappingInformationType', 'ReferenceInformationType', 'Normals', 'NormalsW', 'NormalsIndex',
                    'Binormals', 'BinormalsW', 'BinormalsIndex', 'Tangents', 'TangentsW', 'TangentsIndex', 'Colors', 'ColorIndex',
                    'UV', 'UVIndex', 'Materials', 'Smoothing'})
    elif name == 'FileId':
        require(not node.children and len(values) == 1 and type(values[0]) is bytes and len(values[0]) == 16, 'invalid FileId')
    elif name not in {'FBXHeaderExtension', 'Definitions', 'ObjectType', 'Objects', 'Geometry', 'Model', 'Material',
                       'Connections', 'Takes', 'References', ''}:
        require(not node.children and all(type(v) in (str, int, float, Array) for v in values), 'unsupported leaf declaration')
    for child in node.children: validate_declarations(child)


def validate_scene(nodes):
    root = Node('', [], nodes)
    only(root, {'FBXHeaderExtension', 'FileId', 'CreationTime', 'Creator', 'GlobalSettings', 'Documents', 'References',
                'Definitions', 'Objects', 'Connections', 'Takes'})
    require(len({n.name for n in nodes}) == len(nodes), 'duplicate root section')
    header = one(nodes, 'FBXHeaderExtension')
    require(header is not None and integer(one(header.children, 'FBXVersion'), 7400), 'invalid header/version')
    only(header, {'FBXHeaderVersion', 'FBXVersion', 'EncryptionType', 'CreationTimeStamp', 'Creator', 'SceneInfo'})
    require(len({n.name for n in header.children}) == len(header.children), 'duplicate header field')
    encryption = one(header.children, 'EncryptionType')
    require(encryption is None or integer(encryption, 0), 'encrypted input')
    refs = one(nodes, 'References')
    require(refs is None or not refs.values and not refs.children, 'external reference')
    objects, connections = one(nodes, 'Objects'), one(nodes, 'Connections')
    require(objects is not None and connections is not None and not objects.values and not connections.values, 'missing objects/connections')
    only(objects, {'Geometry', 'Model', 'Material'}); only(connections, {'C'})
    by_id = {}; meshes = 0
    for obj in objects.children:
        require(len(obj.values) == 3 and type(obj.values[0]) is int and obj.values[0] > 0 and obj.values[0] not in by_id
                and isinstance(obj.values[1], str) and isinstance(obj.values[2], str), 'invalid object identity')
        by_id[obj.values[0]] = obj
        if obj.name == 'Geometry':
            require(obj.values[2] == 'Mesh', 'non-mesh geometry'); validate_mesh(obj); meshes += 1
        elif obj.name == 'Model':
            require(obj.values[2] in ('Mesh', 'Null'), 'unsupported model subtype')
            only(obj, {'Version', 'Properties70', 'MultiLayer', 'MultiTake', 'Shading', 'Culling'}); unique(obj)
            for name in ('MultiLayer', 'MultiTake'):
                flag = one(obj.children, name)
                require(flag is None or integer(flag, 0), 'unsupported model flag ' + name)
        else:
            require(obj.values[2] == '', 'unsupported material subtype')
            only(obj, {'Version', 'ShadingModel', 'MultiLayer', 'Properties70'})
            unique(obj)
            multi = one(obj.children, 'MultiLayer'); require(multi is None or integer(multi, 0), 'multilayer material')
    require(meshes > 0, 'empty mesh scene')
    parents = {}; attached = set(); edges = set()
    for edge in connections.children:
        require(len(edge.values) == 3 and not edge.children and edge.values[0] == 'OO'
                and type(edge.values[1]) is int and type(edge.values[2]) is int and edge.values[1] in by_id, 'invalid connection')
        source, target = edge.values[1:]; obj = by_id[source]; parent = by_id.get(target)
        require((source, target) not in edges, 'duplicate object connection'); edges.add((source, target))
        if obj.name == 'Model':
            require((target == 0 or parent is not None and parent.name == 'Model') and source not in parents, 'invalid model parent')
            parents[source] = target
        else:
            require(parent is not None and parent.name == 'Model' and parent.values[2] == 'Mesh', 'invalid geometry/material attachment')
            attached.add(source)
    resolved = {0}
    for identifier, node in by_id.items():
        if node.name == 'Geometry': require(identifier in attached, 'unattached geometry')
        if node.name == 'Model':
            seen = set(); at = identifier
            while at not in resolved:
                require(at not in seen and at in parents, 'cycle or missing model hierarchy'); seen.add(at); at = parents[at]
            resolved.update(seen)
    definitions = one(nodes, 'Definitions')
    if definitions is not None:
        only(definitions, {'Version', 'Count', 'ObjectType'}); seen = set()
        for item in (c for c in definitions.children if c.name == 'ObjectType'):
            require(len(item.values) == 1 and isinstance(item.values[0], str) and item.values[0] not in seen
                    and item.values[0] in ('GlobalSettings', 'Geometry', 'Model', 'Material'), 'unsupported object definition')
            seen.add(item.values[0]); only(item, {'Count', 'PropertyTemplate'})
    takes = one(nodes, 'Takes')
    if takes is not None:
        only(takes, {'Current'}); require(all(n.values == [''] and not n.children for n in takes.children), 'animation takes')
    validate_declarations(root)
    return {'profile': PROFILE, 'version': 7400, 'encoding': 'binary', 'mesh_count': meshes}


def parse_fbx(raw: bytes):
    require(40 <= len(raw) <= MAX_BYTES and raw.startswith(MAGIC) and struct.unpack_from('<I', raw, 23)[0] == 7400,
            'only Binary 7400 with a complete primary up to 64 MiB is supported')
    reader = Reader(raw); nodes = []; at = 27
    while True:
        require(at + 13 <= len(raw), 'missing root terminator')
        if raw[at:at + 13] == bytes(13): at += 13; break
        node, at = reader.record(at, len(raw), 0); nodes.append(node)
    tail = raw[at:]; gap = len(tail) - 156
    require(4 <= gap <= 20 and tail[16:16 + gap] == bytes(gap)
            and struct.unpack_from('<I', tail, 16 + gap)[0] == 7400 and tail[20 + gap:140 + gap] == bytes(120)
            and tail[140 + gap:] == END_MAGIC, 'invalid or trailing Binary FBX footer')
    return validate_scene(nodes)


def stage_fbx_source(path: Path, directory: Path):
    """Validate one regular file, then give Assimp exactly those private bytes."""
    fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        facts = os.fstat(stream.fileno())
        require(stat.S_ISREG(facts.st_mode) and 40 <= facts.st_size <= MAX_BYTES, 'invalid source file/budget')
        raw = stream.read(MAX_BYTES + 1)
        require(len(raw) == facts.st_size, 'source changed size or was truncated')
    profile = parse_fbx(raw)
    target = directory / 'validated-source.fbx'
    with target.open('xb') as output: output.write(raw)
    return target, profile
