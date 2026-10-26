package fbx

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"github.com/addp/common/format"
)

// Profile is the single admitted FBX composition and conversion boundary.
const Profile = "binary7400_static_untextured"

const MaxCompositionBytes int64 = 64 << 20

const (
	maxDepth      = 64
	maxNodes      = 200000
	maxProperties = 1000000
)

type arrayValue struct {
	kind  byte
	count uint32
	data  []byte
}
type record struct {
	name     string
	values   []interface{}
	children []*record
}
type structure struct {
	ctx               context.Context
	raw               []byte
	nodes, properties int
	expanded          int64
}
type modelFacts struct{ meshes int64 }

var _ format.Model3DCompositionProvider = (*Plugin)(nil)

func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if _, err := readStructure(ctx, input, size); err != nil {
		return nil, err
	}
	return []string{}, nil
}

func readStructure(ctx context.Context, input io.ReaderAt, size int64) (*modelFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size < 40 || size > MaxCompositionBytes {
		return nil, fmt.Errorf("FBX composition requires a primary of 40 bytes to 64 MiB")
	}
	raw := make([]byte, int(size))
	if n, err := input.ReadAt(raw, 0); int64(n) != size || err != nil && err != io.EOF {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("read complete FBX structure: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, fbxBinaryMagic) || binary.LittleEndian.Uint32(raw[23:27]) != 7400 {
		return nil, fmt.Errorf("only Binary FBX 7400 is supported")
	}
	p := &structure{ctx: ctx, raw: raw}
	roots := []*record{}
	offset := 27
	for {
		if offset+13 > len(raw) {
			return nil, fmt.Errorf("FBX root terminator is missing")
		}
		if bytes.Equal(raw[offset:offset+13], make([]byte, 13)) {
			offset += 13
			break
		}
		r, next, err := p.readRecord(offset, len(raw), 0)
		if err != nil {
			return nil, err
		}
		roots = append(roots, r)
		offset = next
	}
	// The trailer's first sixteen bytes are a producer timestamp signature,
	// not resource declarations. Reserved padding, version and end magic are strict.
	tail := raw[offset:]
	gap := len(tail) - 156
	if gap < 4 || gap > 20 || !bytes.Equal(tail[16:16+gap], make([]byte, gap)) || binary.LittleEndian.Uint32(tail[16+gap:20+gap]) != 7400 ||
		!bytes.Equal(tail[20+gap:140+gap], make([]byte, 120)) || !bytes.Equal(tail[140+gap:], []byte{0xf8, 0x5a, 0x8c, 0x6a, 0xde, 0xf5, 0xd9, 0x7e, 0xec, 0xe9, 0x0c, 0xe3, 0x75, 0x8f, 0x29, 0x0b}) {
		return nil, fmt.Errorf("invalid or trailing Binary FBX footer")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	facts, err := validateScene(roots)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func (p *structure) budget(size int64) error {
	p.expanded += size
	if p.expanded > MaxCompositionBytes {
		return fmt.Errorf("FBX expanded declarations exceed 64 MiB")
	}
	return p.ctx.Err()
}

func (p *structure) readRecord(offset, limit, depth int) (*record, int, error) {
	fail := func() (*record, int, error) { return nil, 0, fmt.Errorf("invalid or unsupported Binary FBX record") }
	if err := p.ctx.Err(); err != nil {
		return nil, 0, err
	}
	p.nodes++
	if depth >= maxDepth || p.nodes > maxNodes || offset+13 > limit {
		return fail()
	}
	head := p.raw[offset : offset+13]
	end := int64(binary.LittleEndian.Uint32(head))
	count := int64(binary.LittleEndian.Uint32(head[4:]))
	length := int64(binary.LittleEndian.Uint32(head[8:]))
	nameLength := int(head[12])
	start := int64(offset + 13 + nameLength)
	propEnd := start + length
	if nameLength == 0 || end <= int64(offset) || end > int64(limit) || propEnd > end || count > maxProperties-int64(p.properties) {
		return fail()
	}
	p.properties += int(count)
	name := p.raw[offset+13 : start]
	for _, b := range name {
		if b < 33 || b > 126 {
			return fail()
		}
	}
	if err := p.budget(int64(len(name))); err != nil {
		return nil, 0, err
	}
	r := &record{name: string(name)}
	at := int(start)
	for range int(count) {
		value, next, err := p.readProperty(at, int(propEnd))
		if err != nil {
			return nil, 0, err
		}
		r.values = append(r.values, value)
		at = next
	}
	if at != int(propEnd) {
		return fail()
	}
	for at < int(end) {
		if at+13 > int(end) {
			return fail()
		}
		if bytes.Equal(p.raw[at:at+13], make([]byte, 13)) {
			if at+13 != int(end) {
				return fail()
			}
			at += 13
			break
		}
		child, next, err := p.readRecord(at, int(end)-13, depth+1)
		if err != nil {
			return nil, 0, err
		}
		r.children = append(r.children, child)
		at = next
	}
	if at != int(end) {
		return fail()
	}
	return r, at, nil
}

func (p *structure) readProperty(offset, limit int) (interface{}, int, error) {
	fail := func() (interface{}, int, error) { return nil, 0, fmt.Errorf("invalid or unsupported FBX property") }
	if offset >= limit {
		return fail()
	}
	kind := p.raw[offset]
	offset++
	sizes := map[byte]int{'Y': 2, 'C': 1, 'I': 4, 'L': 8, 'F': 4, 'D': 8}
	if size, ok := sizes[kind]; ok {
		if offset+size > limit {
			return fail()
		}
		data := p.raw[offset : offset+size]
		var value interface{}
		switch kind {
		case 'Y':
			value = int64(int16(binary.LittleEndian.Uint16(data)))
		case 'C':
			if data[0] > 1 {
				return fail()
			}
			value = int64(data[0])
		case 'I':
			value = int64(int32(binary.LittleEndian.Uint32(data)))
		case 'L':
			value = int64(binary.LittleEndian.Uint64(data))
		case 'F':
			value = float64(math.Float32frombits(binary.LittleEndian.Uint32(data)))
		case 'D':
			value = math.Float64frombits(binary.LittleEndian.Uint64(data))
		}
		if f, ok := value.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
			return fail()
		}
		if err := p.budget(int64(size)); err != nil {
			return nil, 0, err
		}
		return value, offset + size, nil
	}
	if kind == 'S' || kind == 'R' {
		if offset+4 > limit {
			return fail()
		}
		length := int64(binary.LittleEndian.Uint32(p.raw[offset:]))
		offset += 4
		if length > int64(limit-offset) {
			return fail()
		}
		data := p.raw[offset : int64(offset)+length]
		if err := p.budget(length); err != nil {
			return nil, 0, err
		}
		if kind == 'R' {
			return data, offset + int(length), nil
		}
		if !utf8.Valid(data) {
			return fail()
		}
		return string(data), offset + int(length), nil
	}
	width, ok := map[byte]int64{'f': 4, 'd': 8, 'i': 4, 'l': 8, 'b': 1, 'c': 1}[kind]
	if !ok || offset+12 > limit {
		return fail()
	}
	count := binary.LittleEndian.Uint32(p.raw[offset:])
	encoding := binary.LittleEndian.Uint32(p.raw[offset+4:])
	length := int64(binary.LittleEndian.Uint32(p.raw[offset+8:]))
	offset += 12
	expected := int64(count) * width
	if length > int64(limit-offset) || expected > MaxCompositionBytes {
		return fail()
	}
	if err := p.budget(expected); err != nil {
		return nil, 0, err
	}
	data := p.raw[offset : int64(offset)+length]
	if encoding == 1 {
		compressed := bytes.NewReader(data)
		stream, err := zlib.NewReader(compressed)
		if err != nil {
			return fail()
		}
		decoded, readErr := io.ReadAll(io.LimitReader(stream, expected+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || int64(len(decoded)) != expected || compressed.Len() != 0 {
			return fail()
		}
		data = decoded
	} else if encoding != 0 || length != expected {
		return fail()
	}
	if kind == 'f' || kind == 'd' {
		for at := 0; at < len(data); at += int(width) {
			var value float64
			if kind == 'f' {
				value = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[at:])))
			} else {
				value = math.Float64frombits(binary.LittleEndian.Uint64(data[at:]))
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fail()
			}
		}
	}
	return arrayValue{kind, count, data}, offset + int(length), nil
}

func onlyChildren(node *record, names ...string) bool {
	for _, child := range node.children {
		found := false
		for _, name := range names {
			if child.name == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func one(nodes []*record, name string) *record {
	var hit *record
	for _, n := range nodes {
		if n.name == name {
			if hit != nil {
				return nil
			}
			hit = n
		}
	}
	return hit
}

func uniqueChildren(node *record) bool {
	seen := map[string]bool{}
	for _, child := range node.children {
		if seen[child.name] {
			return false
		}
		seen[child.name] = true
	}
	return true
}
func integer(n *record, want int64) bool {
	return n != nil && len(n.children) == 0 && len(n.values) == 1 && n.values[0] == want
}
func scalarLeaf(n *record) bool {
	if len(n.children) != 0 {
		return false
	}
	for _, v := range n.values {
		switch v.(type) {
		case string, int64, float64:
		default:
			return false
		}
	}
	return true
}

func validateScene(roots []*record) (*modelFacts, error) {
	stage := "top-level sections"
	fail := func() (*modelFacts, error) { return nil, fmt.Errorf("FBX is outside %s: %s", Profile, stage) }
	root := &record{children: roots}
	if !onlyChildren(root, "FBXHeaderExtension", "FileId", "CreationTime", "Creator", "GlobalSettings", "Documents", "References", "Definitions", "Objects", "Connections", "Takes") {
		return fail()
	}
	seen := map[string]bool{}
	for _, n := range roots {
		if seen[n.name] {
			return fail()
		}
		seen[n.name] = true
	}
	stage = "header"
	header := one(roots, "FBXHeaderExtension")
	if header == nil || !integer(one(header.children, "FBXVersion"), 7400) || !onlyChildren(header, "FBXHeaderVersion", "FBXVersion", "EncryptionType", "CreationTimeStamp", "Creator", "SceneInfo") {
		return fail()
	}
	seen = map[string]bool{}
	for _, n := range header.children {
		if seen[n.name] {
			return fail()
		}
		seen[n.name] = true
	}
	if encryption := one(header.children, "EncryptionType"); encryption != nil && !integer(encryption, 0) {
		return fail()
	}
	if references := one(roots, "References"); references != nil && (len(references.values) != 0 || len(references.children) != 0) {
		return fail()
	}
	stage = "objects"
	objects, connections := one(roots, "Objects"), one(roots, "Connections")
	if objects == nil || connections == nil || len(objects.values) != 0 || len(connections.values) != 0 || !onlyChildren(objects, "Geometry", "Model", "Material") || !onlyChildren(connections, "C") {
		return fail()
	}
	byID := map[int64]*record{}
	meshes := int64(0)
	for _, object := range objects.children {
		if len(object.values) != 3 {
			return fail()
		}
		id, ok := object.values[0].(int64)
		if !ok || id <= 0 || byID[id] != nil {
			return fail()
		}
		if _, ok := object.values[1].(string); !ok {
			return fail()
		}
		subtype, ok := object.values[2].(string)
		if !ok {
			return fail()
		}
		byID[id] = object
		switch object.name {
		case "Geometry":
			stage = "mesh geometry and layer indices"
			if subtype != "Mesh" || !validMesh(object) {
				return fail()
			}
			meshes++
		case "Model":
			stage = "model fields"
			if subtype != "Mesh" && subtype != "Null" || !onlyChildren(object, "Version", "Properties70", "MultiLayer", "MultiTake", "Shading", "Culling") || !uniqueChildren(object) {
				return fail()
			}
			for _, name := range []string{"MultiLayer", "MultiTake"} {
				if flag := one(object.children, name); flag != nil && !integer(flag, 0) {
					return fail()
				}
			}
		case "Material":
			stage = "material fields"
			if subtype != "" || !onlyChildren(object, "Version", "ShadingModel", "MultiLayer", "Properties70") {
				return fail()
			}
			if m := one(object.children, "MultiLayer"); m != nil && !integer(m, 0) {
				return fail()
			}
			if !uniqueChildren(object) {
				return fail()
			}
		}
	}
	if meshes == 0 {
		return fail()
	}
	stage = "object connections"
	parents := map[int64]int64{}
	attached := map[int64]bool{}
	edges := map[[2]int64]bool{}
	for _, edge := range connections.children {
		if len(edge.values) != 3 || len(edge.children) != 0 || edge.values[0] != "OO" {
			return fail()
		}
		from, ok := edge.values[1].(int64)
		if !ok {
			return fail()
		}
		to, ok := edge.values[2].(int64)
		if !ok || byID[from] == nil || edges[[2]int64{from, to}] {
			return fail()
		}
		edges[[2]int64{from, to}] = true
		source, target := byID[from], byID[to]
		if source.name == "Model" {
			if to != 0 && (target == nil || target.name != "Model") {
				return fail()
			}
			if _, exists := parents[from]; exists {
				return fail()
			}
			parents[from] = to
		} else {
			if target == nil || target.name != "Model" || target.values[2] != "Mesh" {
				return fail()
			}
			attached[from] = true
		}
	}
	resolved := map[int64]bool{0: true}
	for id, node := range byID {
		if node.name == "Geometry" && !attached[id] {
			return fail()
		}
		if node.name == "Model" {
			seen := map[int64]bool{}
			for at := id; !resolved[at]; {
				if seen[at] {
					return fail()
				}
				seen[at] = true
				parent, ok := parents[at]
				if !ok {
					return fail()
				}
				at = parent
			}
			for at := range seen {
				resolved[at] = true
			}
		}
	}
	stage = "object definitions"
	if definitions := one(roots, "Definitions"); definitions != nil {
		if !onlyChildren(definitions, "Version", "Count", "ObjectType") {
			return fail()
		}
		seen := map[string]bool{}
		for _, n := range definitions.children {
			if n.name != "ObjectType" {
				continue
			}
			if len(n.values) != 1 {
				return fail()
			}
			kind, ok := n.values[0].(string)
			if !ok || seen[kind] || !onlyChildren(n, "Count", "PropertyTemplate") {
				return fail()
			}
			seen[kind] = true
			if kind != "GlobalSettings" && kind != "Geometry" && kind != "Model" && kind != "Material" {
				return fail()
			}
		}
	}
	stage = "takes"
	if takes := one(roots, "Takes"); takes != nil {
		if !onlyChildren(takes, "Current") {
			return fail()
		}
		for _, n := range takes.children {
			if len(n.values) != 1 || n.values[0] != "" || len(n.children) != 0 {
				return fail()
			}
		}
	}
	stage = "closed declaration grammar"
	if !validDeclarations(root) {
		return fail()
	}
	return &modelFacts{meshes: meshes}, nil
}

func validMesh(node *record) bool {
	if !onlyChildren(node, "GeometryVersion", "Properties70", "Vertices", "PolygonVertexIndex", "Edges", "Layer", "LayerElementNormal", "LayerElementBinormal", "LayerElementTangent", "LayerElementColor", "LayerElementUV", "LayerElementMaterial", "LayerElementSmoothing") {
		return false
	}
	vertices, indices := one(node.children, "Vertices"), one(node.children, "PolygonVertexIndex")
	if vertices == nil || indices == nil || len(vertices.values) != 1 || len(indices.values) != 1 {
		return false
	}
	v, ok := vertices.values[0].(arrayValue)
	if !ok || (v.kind != 'd' && v.kind != 'f') || v.count < 9 || v.count%3 != 0 {
		return false
	}
	i, ok := indices.values[0].(arrayValue)
	if !ok || i.kind != 'i' || i.count < 3 {
		return false
	}
	polygon, polygons := 0, 0
	for at := 0; at < len(i.data); at += 4 {
		index := int64(int32(binary.LittleEndian.Uint32(i.data[at:])))
		polygon++
		last := index < 0
		if last {
			index = -index - 1
		}
		if index >= int64(v.count/3) {
			return false
		}
		if last {
			if polygon < 3 {
				return false
			}
			polygon = 0
			polygons++
		}
	}
	if polygon != 0 {
		return false
	}
	layers := map[string]map[int64]bool{}
	for _, layer := range node.children {
		if !isLayer(layer.name) {
			continue
		}
		if len(layer.values) != 1 {
			return false
		}
		index, ok := layer.values[0].(int64)
		if !ok || index < 0 {
			return false
		}
		if layers[layer.name] == nil {
			layers[layer.name] = map[int64]bool{}
		}
		if layers[layer.name][index] || !validLayer(layer, int(v.count/3), int(i.count), polygons) {
			return false
		}
		layers[layer.name][index] = true
	}
	for _, layer := range node.children {
		if layer.name != "Layer" {
			continue
		}
		for _, entry := range layer.children {
			if entry.name != "LayerElement" {
				continue
			}
			kind, id := one(entry.children, "Type"), one(entry.children, "TypedIndex")
			if kind == nil || id == nil || len(kind.values) != 1 || len(id.values) != 1 {
				return false
			}
			name, ok := kind.values[0].(string)
			if !ok {
				return false
			}
			index, ok := id.values[0].(int64)
			if !ok || !layers[name][index] {
				return false
			}
		}
	}
	return true
}

func validLayer(layer *record, vertices, corners, polygons int) bool {
	if !uniqueChildren(layer) {
		return false
	}
	mapping, reference := one(layer.children, "MappingInformationType"), one(layer.children, "ReferenceInformationType")
	if mapping == nil || reference == nil || len(mapping.values) != 1 || len(reference.values) != 1 {
		return false
	}
	name, ok := mapping.values[0].(string)
	if !ok {
		return false
	}
	expected := map[string]int{"ByVertice": vertices, "ByPolygonVertex": corners, "ByPolygon": polygons, "AllSame": 1}[name]
	if expected == 0 {
		return false
	}
	fields := map[string]struct {
		name, index string
		width       int
	}{
		"LayerElementNormal": {"Normals", "NormalsIndex", 3}, "LayerElementBinormal": {"Binormals", "BinormalsIndex", 3},
		"LayerElementTangent": {"Tangents", "TangentsIndex", 3}, "LayerElementColor": {"Colors", "ColorIndex", 4},
		"LayerElementUV": {"UV", "UVIndex", 2}, "LayerElementMaterial": {"Materials", "", 1}, "LayerElementSmoothing": {"Smoothing", "", 1},
	}
	f := fields[layer.name]
	values := one(layer.children, f.name)
	if values == nil || len(values.values) != 1 {
		return false
	}
	data, ok := values.values[0].(arrayValue)
	if !ok || data.count == 0 || int(data.count)%f.width != 0 {
		return false
	}
	allowed := []string{"Version", "Name", "MappingInformationType", "ReferenceInformationType", f.name}
	if f.index != "" {
		allowed = append(allowed, f.index)
	}
	if layer.name == "LayerElementNormal" {
		allowed = append(allowed, "NormalsW")
	}
	if layer.name == "LayerElementBinormal" {
		allowed = append(allowed, "BinormalsW")
	}
	if layer.name == "LayerElementTangent" {
		allowed = append(allowed, "TangentsW")
	}
	if !onlyChildren(layer, allowed...) {
		return false
	}
	if layer.name == "LayerElementMaterial" || layer.name == "LayerElementSmoothing" {
		return (data.kind == 'i' || layer.name == "LayerElementSmoothing" && data.kind == 'b') && int(data.count) == expected && (reference.values[0] == "Direct" || reference.values[0] == "IndexToDirect")
	}
	if data.kind != 'f' && data.kind != 'd' {
		return false
	}
	if reference.values[0] == "Direct" {
		return one(layer.children, f.index) == nil && int(data.count)/f.width == expected
	}
	if reference.values[0] != "IndexToDirect" {
		return false
	}
	indices := one(layer.children, f.index)
	if indices == nil || len(indices.values) != 1 {
		return false
	}
	indexes, ok := indices.values[0].(arrayValue)
	if !ok || indexes.kind != 'i' || int(indexes.count) != expected {
		return false
	}
	for at := 0; at < len(indexes.data); at += 4 {
		index := int64(int32(binary.LittleEndian.Uint32(indexes.data[at:])))
		if index < 0 || index >= int64(data.count)/int64(f.width) {
			return false
		}
	}
	return true
}

// Closed child grammars prevent a new resource-bearing record from being
// silently skipped, including inside templates and otherwise unused objects.
func validDeclarations(n *record) bool {
	switch n.name {
	case "Properties70":
		if len(n.values) != 0 || !onlyChildren(n, "P") {
			return false
		}
	case "P":
		if len(n.values) < 4 || !scalarLeaf(n) {
			return false
		}
		for _, v := range n.values[:4] {
			if _, ok := v.(string); !ok {
				return false
			}
		}
		if n.values[0] == "Visibility" && len(n.values) > 4 && n.values[4] != int64(1) && n.values[4] != float64(1) {
			return false
		}
	case "CreationTimeStamp":
		if !onlyChildren(n, "Version", "Year", "Month", "Day", "Hour", "Minute", "Second", "Millisecond") {
			return false
		}
	case "SceneInfo":
		if !onlyChildren(n, "Type", "Version", "MetaData", "Properties70") {
			return false
		}
	case "MetaData":
		if !onlyChildren(n, "Version", "Title", "Subject", "Author", "Keywords", "Revision", "Comment") {
			return false
		}
	case "GlobalSettings":
		if !onlyChildren(n, "Version", "Properties70") {
			return false
		}
	case "Documents":
		if !onlyChildren(n, "Count", "Document") {
			return false
		}
	case "Document":
		if len(n.values) != 3 || n.values[2] != "Scene" || !onlyChildren(n, "Properties70", "RootNode") {
			return false
		}
	case "PropertyTemplate":
		if len(n.values) != 1 || !onlyChildren(n, "Properties70") {
			return false
		}
	case "Layer":
		if !onlyChildren(n, "Version", "LayerElement") {
			return false
		}
	case "LayerElement":
		if !onlyChildren(n, "Type", "TypedIndex") {
			return false
		}
		t := one(n.children, "Type")
		if t == nil || len(t.values) != 1 {
			return false
		}
		s, ok := t.values[0].(string)
		if !ok || !isLayer(s) {
			return false
		}
	case "LayerElementNormal", "LayerElementBinormal", "LayerElementTangent", "LayerElementColor", "LayerElementUV", "LayerElementMaterial", "LayerElementSmoothing":
		if !onlyChildren(n, "Version", "Name", "MappingInformationType", "ReferenceInformationType", "Normals", "NormalsW", "NormalsIndex", "Binormals", "BinormalsW", "BinormalsIndex", "Tangents", "TangentsW", "TangentsIndex", "Colors", "ColorIndex", "UV", "UVIndex", "Materials", "Smoothing") {
			return false
		}
	case "FileId":
		if len(n.values) != 1 || len(n.children) != 0 {
			return false
		}
		data, ok := n.values[0].([]byte)
		if !ok || len(data) != 16 {
			return false
		}
	case "FBXHeaderExtension", "Definitions", "ObjectType", "Objects", "Geometry", "Model", "Material", "Connections", "Takes", "References", "": // Validated in the scene-specific rules above.
	default:
		if len(n.children) != 0 {
			return false
		}
		for _, v := range n.values {
			switch v.(type) {
			case string, int64, float64, arrayValue:
			default:
				return false
			}
		}
	}
	for _, child := range n.children {
		if !validDeclarations(child) {
			return false
		}
	}
	return true
}

func isLayer(name string) bool {
	return name == "LayerElementNormal" || name == "LayerElementBinormal" || name == "LayerElementTangent" || name == "LayerElementColor" || name == "LayerElementUV" || name == "LayerElementMaterial" || name == "LayerElementSmoothing"
}
