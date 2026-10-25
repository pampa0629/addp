package format

import (
	"net/url"
	"strings"

	"github.com/addp/common/datatype"
)

const MaxGLTFManifestBytes int64 = 16 * 1024 * 1024

// GLTFDocument is the glTF JSON manifest subset needed by ADDP metadata.
type GLTFDocument struct {
	Asset              GLTFAsset        `json:"asset"`
	Scene              *int             `json:"scene"`
	Scenes             []any            `json:"scenes"`
	Nodes              []any            `json:"nodes"`
	Meshes             []GLTFMesh       `json:"meshes"`
	Materials          []any            `json:"materials"`
	Textures           []any            `json:"textures"`
	Images             []GLTFImage      `json:"images"`
	Buffers            []GLTFBuffer     `json:"buffers"`
	BufferViews        []GLTFBufferView `json:"bufferViews"`
	Animations         []any            `json:"animations"`
	Accessors          []GLTFAccessor   `json:"accessors"`
	ExtensionsUsed     []string         `json:"extensionsUsed"`
	ExtensionsRequired []string         `json:"extensionsRequired"`
}

type GLTFAsset struct {
	Version    string `json:"version"`
	MinVersion string `json:"minVersion"`
	Generator  string `json:"generator"`
	Copyright  string `json:"copyright"`
}

type GLTFMesh struct {
	Primitives []GLTFPrimitive `json:"primitives"`
}

type GLTFPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
}

type GLTFBuffer struct {
	URI        string `json:"uri"`
	ByteLength int64  `json:"byteLength"`
}

type GLTFBufferView struct {
	Buffer     int   `json:"buffer"`
	ByteOffset int64 `json:"byteOffset"`
	ByteLength int64 `json:"byteLength"`
	ByteStride int64 `json:"byteStride"`
}

type GLTFImage struct {
	URI        string `json:"uri"`
	BufferView *int   `json:"bufferView"`
	MimeType   string `json:"mimeType"`
}

type GLTFAccessor struct {
	Count int64     `json:"count"`
	Type  string    `json:"type"`
	Min   []float64 `json:"min"`
	Max   []float64 `json:"max"`
}

type GLTFResourceRef struct {
	Role       string
	Index      int
	URI        string
	Path       string
	ByteLength int64
	MimeType   string
}

func BuildGLTFModel3DInfo(doc *GLTFDocument) *datatype.Model3DInfo {
	if doc == nil {
		return nil
	}
	nodeCount := int64(len(doc.Nodes))
	meshCount := int64(len(doc.Meshes))
	materialCount := int64(len(doc.Materials))
	textureCount := int64(len(doc.Textures))
	animationCount := int64(len(doc.Animations))
	vertexCount, triangleCount := GLTFMeshCounts(doc)
	return datatype.NormalizeModel3DInfo(&datatype.Model3DInfo{
		ModelKind:      datatype.Model3DKindMeshScene,
		NodeCount:      optionalPositiveInt64(nodeCount),
		MeshCount:      optionalPositiveInt64(meshCount),
		VertexCount:    optionalPositiveInt64(vertexCount),
		TriangleCount:  optionalPositiveInt64(triangleCount),
		MaterialCount:  optionalPositiveInt64(materialCount),
		TextureCount:   optionalPositiveInt64(textureCount),
		AnimationCount: optionalPositiveInt64(animationCount),
		Bounds3D:       GLTFBounds(doc),
	})
}

func BuildGLTFFormatInfo(doc *GLTFDocument) map[string]interface{} {
	info := map[string]interface{}{}
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
		info["extensions_used"] = append([]string(nil), doc.ExtensionsUsed...)
	}
	if len(doc.ExtensionsRequired) > 0 {
		info["extensions_required"] = append([]string(nil), doc.ExtensionsRequired...)
	}
	info["scene_count"] = len(doc.Scenes)
	info["buffer_count"] = len(doc.Buffers)
	info["image_count"] = len(doc.Images)
	info["accessor_count"] = len(doc.Accessors)
	info["external_resource_count"] = gltfDeclaredLocalResourceCount(doc)
	return info
}

func GLTFMeshCounts(doc *GLTFDocument) (int64, int64) {
	var vertices int64
	var triangles int64
	if doc == nil {
		return 0, 0
	}
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			if positionIndex, ok := primitive.Attributes["POSITION"]; ok && validGLTFAccessorIndex(doc.Accessors, positionIndex) {
				vertices += doc.Accessors[positionIndex].Count
			}
			if primitive.Indices != nil && validGLTFAccessorIndex(doc.Accessors, *primitive.Indices) {
				triangles += doc.Accessors[*primitive.Indices].Count / 3
			}
		}
	}
	return vertices, triangles
}

func GLTFBounds(doc *GLTFDocument) *datatype.Bounds3D {
	if doc == nil {
		return nil
	}
	var bounds *datatype.Bounds3D
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			positionIndex, ok := primitive.Attributes["POSITION"]
			if !ok || !validGLTFAccessorIndex(doc.Accessors, positionIndex) {
				continue
			}
			accessor := doc.Accessors[positionIndex]
			if len(accessor.Min) < 3 || len(accessor.Max) < 3 {
				continue
			}
			bounds = mergeGLTFBounds(bounds, accessor.Min[0], accessor.Min[1], accessor.Min[2], accessor.Max[0], accessor.Max[1], accessor.Max[2])
		}
	}
	return bounds
}

// gltfDeclaredLocalResourceCount is metadata only, including for GLB. It never
// produces paths or proves resource completeness; consumers use ParseGLTFComposition.
func gltfDeclaredLocalResourceCount(doc *GLTFDocument) int {
	if doc == nil {
		return 0
	}
	count := 0
	for _, buffer := range doc.Buffers {
		if gltfDeclaredLocalURI(buffer.URI) {
			count++
		}
	}
	for _, image := range doc.Images {
		if gltfDeclaredLocalURI(image.URI) {
			count++
		}
	}
	return count
}

func gltfDeclaredLocalURI(rawURI string) bool {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" || strings.HasPrefix(strings.ToLower(rawURI), "data:") {
		return false
	}
	parsed, err := url.Parse(rawURI)
	return err == nil && !parsed.IsAbs() && parsed.Host == "" && strings.TrimSpace(parsed.Path) != "" && !strings.HasPrefix(parsed.Path, "/")
}

func validGLTFAccessorIndex(accessors []GLTFAccessor, index int) bool {
	return index >= 0 && index < len(accessors)
}

func optionalPositiveInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func mergeGLTFBounds(bounds *datatype.Bounds3D, minX, minY, minZ, maxX, maxY, maxZ float64) *datatype.Bounds3D {
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
