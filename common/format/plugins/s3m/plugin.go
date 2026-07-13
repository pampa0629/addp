package s3m

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const maxSCPBytes = 16 * 1024 * 1024
const ManifestFileName = "scene.scp"

type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register S3M format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType { return format.FormatS3M }

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-s3m",
		Format:   format.FormatS3M,
		I18nKey:  "format.s3m",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			RelativePaths: []string{"config/" + ManifestFileName},
			MimeTypes: []string{
				"application/vnd.supermap.s3m-config",
			},
		},
	}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, _ *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, err := decodeSCP(input, maxSCPBytes)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return describeSCP(doc), nil
}

func (p *Plugin) DescribeModel3DScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("S3M scope reader is required")
	}
	lister, ok := reader.(contentio.Lister)
	if !ok {
		return nil, fmt.Errorf("S3M scope reader must support listing")
	}
	refs, err := lister.List(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("list S3M scope: %w", err)
	}
	manifestRef := ""
	for _, ref := range refs {
		if strings.EqualFold(path.Base(ref.Path), ManifestFileName) {
			if manifestRef != "" {
				return nil, fmt.Errorf("S3M scope contains multiple SCP manifests")
			}
			manifestRef = strings.Trim(ref.Path, "/")
		}
	}
	if manifestRef == "" {
		return nil, fmt.Errorf("S3M scope does not contain %s", ManifestFileName)
	}
	manifestPath := manifestRef
	if scopePath := strings.Trim(scope.Path, "/"); scopePath != "" && !strings.HasPrefix(manifestPath, scopePath+"/") {
		manifestPath = path.Join(scopePath, manifestPath)
	}
	rc, err := reader.Open(ctx, contentio.NewRef(manifestPath, contentio.RoleMain))
	if err != nil {
		return nil, fmt.Errorf("open S3M manifest: %w", err)
	}
	defer rc.Close()
	result, err := p.DescribeModel3D(ctx, rc, options)
	if err == nil && result != nil {
		result.FormatInfo["manifest_ref"] = strings.TrimPrefix(strings.Trim(manifestPath, "/"), strings.Trim(scope.Path, "/")+"/")
	}
	return result, err
}

type scpDocument struct {
	Version          string
	FileType         string
	Position         []float64
	Bounds           []float64
	RootTileCount    int64
	TileExtension    string
	ManifestEncoding string
}

type xmlSCP struct {
	XMLName  xml.Name `xml:"SuperMapCache"`
	Version  string   `xml:"Version"`
	FileType string   `xml:"FileType"`
	Position struct {
		X float64 `xml:"X"`
		Y float64 `xml:"Y"`
		Z float64 `xml:"Z"`
	} `xml:"Position"`
	BoundingBox struct {
		MinX float64 `xml:"MinX"`
		MinY float64 `xml:"MinY"`
		MinZ float64 `xml:"MinZ"`
		MaxX float64 `xml:"MaxX"`
		MaxY float64 `xml:"MaxY"`
		MaxZ float64 `xml:"MaxZ"`
	} `xml:"BoundingBox"`
	OSGFiles struct {
		Files []struct {
			FileName string `xml:"FileName"`
		} `xml:"Files"`
		FileNames []string `xml:"FileName"`
	} `xml:"OSGFiles"`
}

type jsonSCP struct {
	Asset      interface{}            `json:"asset"`
	Version    interface{}            `json:"version"`
	Extensions map[string]interface{} `json:"extensions"`
	Position   struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"position"`
	Tiles []struct {
		URL string `json:"url"`
	} `json:"tiles"`
	RootTiles []struct {
		URL string `json:"url"`
	} `json:"rootTiles"`
}

func decodeSCP(input io.Reader, maxBytes int64) (*scpDocument, error) {
	data, err := io.ReadAll(io.LimitReader(input, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read S3M manifest: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("S3M manifest too large: %d", len(data))
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("S3M manifest is empty")
	}
	if trimmed[0] == '<' {
		return decodeXMLSCP(trimmed)
	}
	return decodeJSONSCP(trimmed)
}

func decodeXMLSCP(data []byte) (*scpDocument, error) {
	var source xmlSCP
	if err := xml.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("parse S3M XML manifest: %w", err)
	}
	if source.XMLName.Local != "SuperMapCache" || strings.TrimSpace(source.FileType) == "" {
		return nil, fmt.Errorf("invalid S3M XML manifest")
	}
	names := append([]string{}, source.OSGFiles.FileNames...)
	for _, item := range source.OSGFiles.Files {
		names = append(names, item.FileName)
	}
	extension := ".s3m"
	if len(names) > 0 {
		extension = strings.ToLower(path.Ext(strings.ReplaceAll(names[0], ".osgb", ".s3m")))
	}
	return &scpDocument{
		Version: strings.TrimSpace(source.Version), FileType: strings.TrimSpace(source.FileType),
		Position:      []float64{source.Position.X, source.Position.Y, source.Position.Z},
		Bounds:        []float64{source.BoundingBox.MinX, source.BoundingBox.MinY, source.BoundingBox.MinZ, source.BoundingBox.MaxX, source.BoundingBox.MaxY, source.BoundingBox.MaxZ},
		RootTileCount: int64(len(names)), TileExtension: extension, ManifestEncoding: "xml",
	}, nil
}

func decodeJSONSCP(data []byte) (*scpDocument, error) {
	var source jsonSCP
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("parse S3M JSON manifest: %w", err)
	}
	rootTiles := source.Tiles
	if len(rootTiles) == 0 {
		rootTiles = source.RootTiles
	}
	if !strings.EqualFold(interfaceString(source.Asset), "SuperMap") || len(rootTiles) == 0 {
		return nil, fmt.Errorf("invalid S3M JSON manifest")
	}
	fileType := interfaceString(source.Extensions["s3m:FileType"])
	return &scpDocument{
		Version: interfaceString(source.Version), FileType: fileType,
		Position:      []float64{source.Position.X, source.Position.Y, source.Position.Z},
		RootTileCount: int64(len(rootTiles)), TileExtension: strings.ToLower(path.Ext(rootTiles[0].URL)), ManifestEncoding: "json",
	}, nil
}

func describeSCP(doc *scpDocument) *format.Model3DDescribeResult {
	info := map[string]interface{}{
		"manifest_encoding": doc.ManifestEncoding,
		"root_tile_count":   doc.RootTileCount,
		"tile_extension":    doc.TileExtension,
	}
	if doc.Version != "" {
		info["version"] = doc.Version
	}
	if doc.FileType != "" {
		info["file_type"] = doc.FileType
	}
	if len(doc.Position) == 3 {
		info["position"] = append([]float64(nil), doc.Position...)
	}
	model := datatype.NormalizeModel3DInfo(&datatype.Model3DInfo{
		ModelKind: datatype.Model3DKindTiledScene,
		Bounds3D:  s3mBounds(doc.Bounds),
	})
	var spatial *datatype.SpatialInfo
	if len(doc.Position) == 3 && doc.Position[0] >= -180 && doc.Position[0] <= 180 && doc.Position[1] >= -90 && doc.Position[1] <= 90 {
		srid := 4326
		spatial = &datatype.SpatialInfo{SRID: &srid, CRSRef: datatype.EPSGCRSRef(srid)}
	}
	return &format.Model3DDescribeResult{Model3D: model, Spatial: spatial, FormatInfo: info}
}

func s3mBounds(values []float64) *datatype.Bounds3D {
	if len(values) != 6 {
		return nil
	}
	minX, minY, minZ, maxX, maxY, maxZ := values[0], values[1], values[2], values[3], values[4], values[5]
	if minX == 0 && minY == 0 && minZ == 0 && maxX == 0 && maxY == 0 && maxZ == 0 {
		return nil
	}
	return &datatype.Bounds3D{MinX: &minX, MinY: &minY, MinZ: &minZ, MaxX: &maxX, MaxY: &maxY, MaxZ: &maxZ}
}

func interfaceString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
