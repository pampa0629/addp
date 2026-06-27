package glb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	glbMagic           = "glTF"
	glbJSONChunkType   = 0x4E4F534A
	maxGLBJSONChunkLen = 16 * 1024 * 1024
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register GLB format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatGLB
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-glb",
		Format:   format.FormatGLB,
		I18nKey:  "format.glb",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".glb"},
			MimeTypes:         []string{"model/gltf-binary", "application/octet-stream"},
			ContentSignatures: []string{"hex:676c5446"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == glbMagic
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, version, length, err := readGLBJSON(input)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	modelInfo := buildModel3DInfo(doc)
	formatInfo := buildGLBFormatInfo(doc, version, length)
	return &format.Model3DDescribeResult{
		Model3D:    modelInfo,
		FormatInfo: formatInfo,
	}, nil
}

type glbDocument struct {
	Asset              glbAsset      `json:"asset"`
	Scene              *int          `json:"scene"`
	Scenes             []any         `json:"scenes"`
	Nodes              []any         `json:"nodes"`
	Meshes             []glbMesh     `json:"meshes"`
	Materials          []any         `json:"materials"`
	Textures           []any         `json:"textures"`
	Images             []any         `json:"images"`
	Animations         []any         `json:"animations"`
	Buffers            []any         `json:"buffers"`
	Accessors          []glbAccessor `json:"accessors"`
	ExtensionsUsed     []string      `json:"extensionsUsed"`
	ExtensionsRequired []string      `json:"extensionsRequired"`
}

type glbAsset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
	Copyright string `json:"copyright"`
}

type glbMesh struct {
	Primitives []glbPrimitive `json:"primitives"`
}

type glbPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
}

type glbAccessor struct {
	Count int64     `json:"count"`
	Type  string    `json:"type"`
	Min   []float64 `json:"min"`
	Max   []float64 `json:"max"`
}

func readGLBJSON(input io.Reader) (*glbDocument, uint32, uint32, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(input, header); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB header: %w", err)
	}
	if string(header[:4]) != glbMagic {
		return nil, 0, 0, fmt.Errorf("invalid GLB magic")
	}
	version := binary.LittleEndian.Uint32(header[4:8])
	length := binary.LittleEndian.Uint32(header[8:12])
	chunkHeader := make([]byte, 8)
	if _, err := io.ReadFull(input, chunkHeader); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB JSON chunk header: %w", err)
	}
	chunkLen := binary.LittleEndian.Uint32(chunkHeader[:4])
	chunkType := binary.LittleEndian.Uint32(chunkHeader[4:8])
	if chunkType != glbJSONChunkType {
		return nil, 0, 0, fmt.Errorf("GLB first chunk is not JSON")
	}
	if chunkLen > maxGLBJSONChunkLen {
		return nil, 0, 0, fmt.Errorf("GLB JSON chunk too large: %d", chunkLen)
	}
	chunk := make([]byte, int(chunkLen))
	if _, err := io.ReadFull(input, chunk); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB JSON chunk: %w", err)
	}
	chunk = bytes.TrimRight(chunk, " \t\r\n\x00")
	var doc glbDocument
	if err := json.Unmarshal(chunk, &doc); err != nil {
		return nil, 0, 0, fmt.Errorf("parse GLB JSON chunk: %w", err)
	}
	return &doc, version, length, nil
}

func buildModel3DInfo(doc *glbDocument) *datatype.Model3DInfo {
	if doc == nil {
		return nil
	}
	nodeCount := int64(len(doc.Nodes))
	meshCount := int64(len(doc.Meshes))
	materialCount := int64(len(doc.Materials))
	textureCount := int64(len(doc.Textures))
	animationCount := int64(len(doc.Animations))
	vertexCount, triangleCount := meshCounts(doc)
	return datatype.NormalizeModel3DInfo(&datatype.Model3DInfo{
		ModelKind:      datatype.Model3DKindMeshScene,
		NodeCount:      optionalInt64(nodeCount),
		MeshCount:      optionalInt64(meshCount),
		VertexCount:    optionalInt64(vertexCount),
		TriangleCount:  optionalInt64(triangleCount),
		MaterialCount:  optionalInt64(materialCount),
		TextureCount:   optionalInt64(textureCount),
		AnimationCount: optionalInt64(animationCount),
		Bounds3D:       glbBounds(doc),
	})
}

func meshCounts(doc *glbDocument) (int64, int64) {
	var vertices int64
	var triangles int64
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			if positionIndex, ok := primitive.Attributes["POSITION"]; ok && validAccessorIndex(doc.Accessors, positionIndex) {
				vertices += doc.Accessors[positionIndex].Count
			}
			if primitive.Indices != nil && validAccessorIndex(doc.Accessors, *primitive.Indices) {
				triangles += doc.Accessors[*primitive.Indices].Count / 3
			}
		}
	}
	return vertices, triangles
}

func glbBounds(doc *glbDocument) *datatype.Bounds3D {
	if doc == nil {
		return nil
	}
	var bounds *datatype.Bounds3D
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			positionIndex, ok := primitive.Attributes["POSITION"]
			if !ok || !validAccessorIndex(doc.Accessors, positionIndex) {
				continue
			}
			accessor := doc.Accessors[positionIndex]
			if len(accessor.Min) < 3 || len(accessor.Max) < 3 {
				continue
			}
			bounds = mergeBounds(bounds, accessor.Min[0], accessor.Min[1], accessor.Min[2], accessor.Max[0], accessor.Max[1], accessor.Max[2])
		}
	}
	return bounds
}

func buildGLBFormatInfo(doc *glbDocument, version, length uint32) map[string]interface{} {
	info := map[string]interface{}{
		"glb_version": version,
		"byte_length": length,
	}
	if doc == nil {
		return info
	}
	if value := strings.TrimSpace(doc.Asset.Version); value != "" {
		info["gltf_version"] = value
	}
	if value := strings.TrimSpace(doc.Asset.Generator); value != "" {
		info["generator"] = value
	}
	if value := strings.TrimSpace(doc.Asset.Copyright); value != "" {
		info["copyright"] = value
	}
	if doc.Scene != nil {
		info["default_scene"] = *doc.Scene
	}
	if len(doc.ExtensionsUsed) > 0 {
		info["extensions_used"] = doc.ExtensionsUsed
	}
	if len(doc.ExtensionsRequired) > 0 {
		info["extensions_required"] = doc.ExtensionsRequired
	}
	info["scene_count"] = len(doc.Scenes)
	info["buffer_count"] = len(doc.Buffers)
	info["image_count"] = len(doc.Images)
	info["accessor_count"] = len(doc.Accessors)
	return info
}

func validAccessorIndex(accessors []glbAccessor, index int) bool {
	return index >= 0 && index < len(accessors)
}

func optionalInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func mergeBounds(bounds *datatype.Bounds3D, minX, minY, minZ, maxX, maxY, maxZ float64) *datatype.Bounds3D {
	if bounds == nil {
		return &datatype.Bounds3D{
			MinX: &minX,
			MinY: &minY,
			MinZ: &minZ,
			MaxX: &maxX,
			MaxY: &maxY,
			MaxZ: &maxZ,
		}
	}
	if bounds.MinX == nil || minX < *bounds.MinX {
		bounds.MinX = &minX
	}
	if bounds.MinY == nil || minY < *bounds.MinY {
		bounds.MinY = &minY
	}
	if bounds.MinZ == nil || minZ < *bounds.MinZ {
		bounds.MinZ = &minZ
	}
	if bounds.MaxX == nil || maxX > *bounds.MaxX {
		bounds.MaxX = &maxX
	}
	if bounds.MaxY == nil || maxY > *bounds.MaxY {
		bounds.MaxY = &maxY
	}
	if bounds.MaxZ == nil || maxZ > *bounds.MaxZ {
		bounds.MaxZ = &maxZ
	}
	return bounds
}
