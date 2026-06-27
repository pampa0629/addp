package tiles3d

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	TilesetFileName       = "tileset.json"
	maxTilesetJSONBytes   = 16 * 1024 * 1024
	radiansToDegreesScale = 180 / math.Pi
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register 3D Tiles format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.Format3DTiles
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-3dtiles",
		Format:   format.Format3DTiles,
		I18nKey:  "format.3dtiles",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			FileNames: []string{TilesetFileName},
			MimeTypes: []string{
				"application/vnd.ogc.3dtiles+json",
			},
		},
	}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, err := DecodeTileset(input, maxTilesetJSONBytes)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return describeTileset(doc), nil
}

func (p *Plugin) DescribeModel3DScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("3D Tiles scope reader is required")
	}
	manifestPath := path.Join(strings.Trim(scope.Path, "/"), TilesetFileName)
	if strings.Trim(manifestPath, "/") == "" {
		manifestPath = TilesetFileName
	}
	rc, err := reader.Open(ctx, contentio.NewRef(manifestPath, contentio.RoleMain))
	if err != nil {
		return nil, fmt.Errorf("open 3D Tiles manifest: %w", err)
	}
	defer rc.Close()
	return p.DescribeModel3D(ctx, rc, options)
}

type TilesetDocument struct {
	Asset              tilesetAsset           `json:"asset"`
	GeometricError     *float64               `json:"geometricError"`
	Root               *tilesetTile           `json:"root"`
	ExtensionsUsed     []string               `json:"extensionsUsed"`
	ExtensionsRequired []string               `json:"extensionsRequired"`
	Properties         map[string]interface{} `json:"properties"`
}

type tilesetAsset struct {
	Version        string `json:"version"`
	TilesetVersion string `json:"tilesetVersion"`
	Generator      string `json:"generator"`
	Copyright      string `json:"copyright"`
}

type tilesetTile struct {
	BoundingVolume *tilesetBoundingVolume `json:"boundingVolume"`
	GeometricError *float64               `json:"geometricError"`
	Refine         string                 `json:"refine"`
	Content        *tilesetContent        `json:"content"`
	Contents       []tilesetContent       `json:"contents"`
	Children       []tilesetTile          `json:"children"`
}

type tilesetContent struct {
	URI string `json:"uri"`
	URL string `json:"url"`
}

type tilesetBoundingVolume struct {
	Box    []float64 `json:"box"`
	Region []float64 `json:"region"`
	Sphere []float64 `json:"sphere"`
}

type tilesetStats struct {
	tileCount     int64
	contentCount  int64
	leafTileCount int64
	maxDepth      int64
	bounds        *datatype.Bounds3D
	spatialExtent *datatype.BoundingBox
}

func DecodeTileset(input io.Reader, maxBytes int64) (*TilesetDocument, error) {
	if maxBytes <= 0 {
		maxBytes = maxTilesetJSONBytes
	}
	limited := io.LimitReader(input, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read 3D Tiles manifest: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("3D Tiles manifest too large: %d", len(data))
	}
	var doc TilesetDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse 3D Tiles manifest: %w", err)
	}
	if strings.TrimSpace(doc.Asset.Version) == "" {
		return nil, fmt.Errorf("invalid 3D Tiles manifest: missing asset.version")
	}
	if doc.Root == nil {
		return nil, fmt.Errorf("invalid 3D Tiles manifest: missing root")
	}
	return &doc, nil
}

func describeTileset(doc *TilesetDocument) *format.Model3DDescribeResult {
	if doc == nil {
		return nil
	}
	stats := collectTilesetStats(doc.Root)
	lodCount := stats.maxDepth + 1
	modelInfo := datatype.NormalizeModel3DInfo(&datatype.Model3DInfo{
		ModelKind: datatype.Model3DKindTiledScene,
		LODCount:  positiveInt64(lodCount),
		Bounds3D:  stats.bounds,
	})
	spatialInfo := tilesetSpatialInfo(stats.spatialExtent)
	return &format.Model3DDescribeResult{
		Model3D:    modelInfo,
		Spatial:    spatialInfo,
		FormatInfo: build3DTilesFormatInfo(doc, stats),
	}
}

func collectTilesetStats(root *tilesetTile) tilesetStats {
	stats := tilesetStats{}
	visitTilesetTile(root, 0, &stats)
	return stats
}

func visitTilesetTile(tile *tilesetTile, depth int64, stats *tilesetStats) {
	if tile == nil || stats == nil {
		return
	}
	stats.tileCount++
	if depth > stats.maxDepth {
		stats.maxDepth = depth
	}
	if tile.Content != nil && contentURI(*tile.Content) != "" {
		stats.contentCount++
	}
	for _, content := range tile.Contents {
		if contentURI(content) != "" {
			stats.contentCount++
		}
	}
	if tile.BoundingVolume != nil {
		stats.bounds = mergeBounds(stats.bounds, boundsFromVolume(tile.BoundingVolume))
		stats.spatialExtent = mergeExtent(stats.spatialExtent, spatialExtentFromRegion(tile.BoundingVolume.Region))
	}
	if len(tile.Children) == 0 {
		stats.leafTileCount++
		return
	}
	for i := range tile.Children {
		visitTilesetTile(&tile.Children[i], depth+1, stats)
	}
}

func contentURI(content tilesetContent) string {
	if value := strings.TrimSpace(content.URI); value != "" {
		return value
	}
	return strings.TrimSpace(content.URL)
}

func build3DTilesFormatInfo(doc *TilesetDocument, stats tilesetStats) map[string]interface{} {
	info := map[string]interface{}{
		"manifest_ref":    TilesetFileName,
		"asset_version":   strings.TrimSpace(doc.Asset.Version),
		"tile_count":      stats.tileCount,
		"content_count":   stats.contentCount,
		"leaf_tile_count": stats.leafTileCount,
		"max_depth":       stats.maxDepth,
	}
	if value := strings.TrimSpace(doc.Asset.TilesetVersion); value != "" {
		info["tileset_version"] = value
	}
	if value := strings.TrimSpace(doc.Asset.Generator); value != "" {
		info["generator"] = value
	}
	if value := strings.TrimSpace(doc.Asset.Copyright); value != "" {
		info["copyright"] = value
	}
	if doc.GeometricError != nil {
		info["geometric_error"] = *doc.GeometricError
	}
	if doc.Root != nil && strings.TrimSpace(doc.Root.Refine) != "" {
		info["root_refine"] = strings.TrimSpace(doc.Root.Refine)
	}
	if len(doc.ExtensionsUsed) > 0 {
		info["extensions_used"] = append([]string(nil), doc.ExtensionsUsed...)
	}
	if len(doc.ExtensionsRequired) > 0 {
		info["extensions_required"] = append([]string(nil), doc.ExtensionsRequired...)
	}
	if len(doc.Properties) > 0 {
		info["property_count"] = len(doc.Properties)
	}
	return info
}

func tilesetSpatialInfo(extent *datatype.BoundingBox) *datatype.SpatialInfo {
	if extent == nil {
		return nil
	}
	srid := 4326
	return &datatype.SpatialInfo{
		SRID:   &srid,
		CRSRef: datatype.EPSGCRSRef(srid),
		Extent: extent,
	}
}

func boundsFromVolume(volume *tilesetBoundingVolume) *datatype.Bounds3D {
	if volume == nil {
		return nil
	}
	if len(volume.Box) == 12 {
		cx, cy, cz := volume.Box[0], volume.Box[1], volume.Box[2]
		ex := math.Abs(volume.Box[3]) + math.Abs(volume.Box[6]) + math.Abs(volume.Box[9])
		ey := math.Abs(volume.Box[4]) + math.Abs(volume.Box[7]) + math.Abs(volume.Box[10])
		ez := math.Abs(volume.Box[5]) + math.Abs(volume.Box[8]) + math.Abs(volume.Box[11])
		return newBounds3D(cx-ex, cy-ey, cz-ez, cx+ex, cy+ey, cz+ez)
	}
	if len(volume.Sphere) == 4 {
		cx, cy, cz, radius := volume.Sphere[0], volume.Sphere[1], volume.Sphere[2], math.Abs(volume.Sphere[3])
		return newBounds3D(cx-radius, cy-radius, cz-radius, cx+radius, cy+radius, cz+radius)
	}
	if len(volume.Region) == 6 {
		minX := volume.Region[0] * radiansToDegreesScale
		minY := volume.Region[1] * radiansToDegreesScale
		maxX := volume.Region[2] * radiansToDegreesScale
		maxY := volume.Region[3] * radiansToDegreesScale
		minZ := volume.Region[4]
		maxZ := volume.Region[5]
		return newBounds3D(minX, minY, minZ, maxX, maxY, maxZ)
	}
	return nil
}

func spatialExtentFromRegion(region []float64) *datatype.BoundingBox {
	if len(region) != 6 {
		return nil
	}
	extent := datatype.NewBoundingBox(
		region[0]*radiansToDegreesScale,
		region[1]*radiansToDegreesScale,
		region[2]*radiansToDegreesScale,
		region[3]*radiansToDegreesScale,
	)
	return &extent
}

func newBounds3D(minX, minY, minZ, maxX, maxY, maxZ float64) *datatype.Bounds3D {
	return &datatype.Bounds3D{
		MinX: &minX,
		MinY: &minY,
		MinZ: &minZ,
		MaxX: &maxX,
		MaxY: &maxY,
		MaxZ: &maxZ,
	}
}

func mergeBounds(left, right *datatype.Bounds3D) *datatype.Bounds3D {
	if left == nil {
		return right.Clone()
	}
	if right == nil {
		return left
	}
	left = minBound(left, right.MinX, "x")
	left = minBound(left, right.MinY, "y")
	left = minBound(left, right.MinZ, "z")
	left = maxBound(left, right.MaxX, "x")
	left = maxBound(left, right.MaxY, "y")
	left = maxBound(left, right.MaxZ, "z")
	return left
}

func minBound(bounds *datatype.Bounds3D, value *float64, axis string) *datatype.Bounds3D {
	if bounds == nil || value == nil {
		return bounds
	}
	switch axis {
	case "x":
		if bounds.MinX == nil || *value < *bounds.MinX {
			bounds.MinX = cloneFloat64(value)
		}
	case "y":
		if bounds.MinY == nil || *value < *bounds.MinY {
			bounds.MinY = cloneFloat64(value)
		}
	case "z":
		if bounds.MinZ == nil || *value < *bounds.MinZ {
			bounds.MinZ = cloneFloat64(value)
		}
	}
	return bounds
}

func maxBound(bounds *datatype.Bounds3D, value *float64, axis string) *datatype.Bounds3D {
	if bounds == nil || value == nil {
		return bounds
	}
	switch axis {
	case "x":
		if bounds.MaxX == nil || *value > *bounds.MaxX {
			bounds.MaxX = cloneFloat64(value)
		}
	case "y":
		if bounds.MaxY == nil || *value > *bounds.MaxY {
			bounds.MaxY = cloneFloat64(value)
		}
	case "z":
		if bounds.MaxZ == nil || *value > *bounds.MaxZ {
			bounds.MaxZ = cloneFloat64(value)
		}
	}
	return bounds
}

func mergeExtent(left, right *datatype.BoundingBox) *datatype.BoundingBox {
	if left == nil {
		if right == nil {
			return nil
		}
		clone := *right
		return &clone
	}
	if right == nil {
		return left
	}
	merged := *left
	if (*right)[0] < merged[0] {
		merged[0] = (*right)[0]
	}
	if (*right)[1] < merged[1] {
		merged[1] = (*right)[1]
	}
	if (*right)[2] > merged[2] {
		merged[2] = (*right)[2]
	}
	if (*right)[3] > merged[3] {
		merged[3] = (*right)[3]
	}
	return &merged
}

func positiveInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
