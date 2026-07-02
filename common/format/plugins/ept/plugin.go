package ept

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	ManifestFileName = "ept.json"
	maxManifestBytes = 16 * 1024 * 1024
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register EPT format plugin: %v", err))
	}
}

func (p *Plugin) DescribePointCloudScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("EPT scope reader is required")
	}
	manifestPath := path.Join(strings.Trim(scope.Path, "/"), ManifestFileName)
	if strings.Trim(manifestPath, "/") == "" {
		manifestPath = ManifestFileName
	}
	rc, err := reader.Open(ctx, contentio.NewRef(manifestPath, contentio.RoleManifest))
	if err != nil {
		return nil, fmt.Errorf("open EPT manifest: %w", err)
	}
	defer rc.Close()
	return p.describeManifest(ctx, rc)
}

func (p *Plugin) describeManifest(ctx context.Context, input io.Reader) (*format.PointCloudDescribeResult, error) {
	doc, err := decodeManifest(input, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return describeEPT(doc), nil
}

type manifestDocument struct {
	Version          string                 `json:"version"`
	DataType         string                 `json:"dataType"`
	Points           int64                  `json:"points"`
	Bounds           []float64              `json:"bounds"`
	BoundsConforming []float64              `json:"boundsConforming"`
	Schema           []schemaDimension      `json:"schema"`
	Span             int64                  `json:"span"`
	HierarchyType    string                 `json:"hierarchyType"`
	SRS              manifestSRS            `json:"srs"`
	Extra            map[string]interface{} `json:"-"`
}

type schemaDimension struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Size   int         `json:"size"`
	Scale  interface{} `json:"scale"`
	Offset interface{} `json:"offset"`
}

type manifestSRS struct {
	Authority  string                 `json:"authority"`
	Horizontal string                 `json:"horizontal"`
	WKT        string                 `json:"wkt"`
	Extra      map[string]interface{} `json:"-"`
}

func decodeManifest(input io.Reader, maxBytes int64) (*manifestDocument, error) {
	if maxBytes <= 0 {
		maxBytes = maxManifestBytes
	}
	limited := io.LimitReader(input, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read EPT manifest: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("EPT manifest too large: %d", len(data))
	}
	var doc manifestDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse EPT manifest: %w", err)
	}
	if strings.TrimSpace(doc.Version) == "" {
		return nil, fmt.Errorf("invalid EPT manifest: missing version")
	}
	if len(doc.Schema) == 0 {
		return nil, fmt.Errorf("invalid EPT manifest: missing schema")
	}
	return &doc, nil
}

func describeEPT(doc *manifestDocument) *format.PointCloudDescribeResult {
	if doc == nil {
		return nil
	}
	dimensions := schemaDimensionNames(doc.Schema)
	dimensionCount := len(dimensions)
	pointFormat := strings.TrimSpace(doc.DataType)
	if pointFormat == "" {
		pointFormat = "ept"
	}
	info := datatype.NormalizePointCloudInfo(&datatype.PointCloudInfo{
		PointCloudKind:    datatype.PointCloudKindTiledPointCloud,
		PointCount:        positiveInt64(doc.Points),
		PointFormat:       pointFormat,
		DimensionCount:    positiveInt(dimensionCount),
		Dimensions:        dimensions,
		Bounds3D:          eptBounds(doc),
		HasColor:          optionalBool(hasSchemaAny(doc.Schema, "Red", "Green", "Blue")),
		HasIntensity:      optionalBool(hasSchemaAny(doc.Schema, "Intensity")),
		HasClassification: optionalBool(hasSchemaAny(doc.Schema, "Classification")),
	})
	return &format.PointCloudDescribeResult{
		PointCloud: info,
		Spatial:    eptSpatialInfo(doc),
		FormatInfo: eptFormatInfo(doc),
	}
}

func schemaDimensionNames(schema []schemaDimension) []string {
	names := make([]string, 0, len(schema))
	for _, dim := range schema {
		name := strings.TrimSpace(dim.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func eptBounds(doc *manifestDocument) *datatype.Bounds3D {
	if doc == nil {
		return nil
	}
	if bounds := boundsFromArray(doc.BoundsConforming); bounds != nil {
		return bounds
	}
	return boundsFromArray(doc.Bounds)
}

func boundsFromArray(values []float64) *datatype.Bounds3D {
	if len(values) != 6 {
		return nil
	}
	return &datatype.Bounds3D{
		MinX: float64Ptr(values[0]),
		MinY: float64Ptr(values[1]),
		MinZ: float64Ptr(values[2]),
		MaxX: float64Ptr(values[3]),
		MaxY: float64Ptr(values[4]),
		MaxZ: float64Ptr(values[5]),
	}
}

func eptSpatialInfo(doc *manifestDocument) *datatype.SpatialInfo {
	if doc == nil {
		return nil
	}
	var spatial *datatype.SpatialInfo
	if bounds := eptBounds(doc); bounds != nil && bounds.MinX != nil && bounds.MinY != nil && bounds.MaxX != nil && bounds.MaxY != nil {
		extent := datatype.NewBoundingBox(*bounds.MinX, *bounds.MinY, *bounds.MaxX, *bounds.MaxY)
		spatial = &datatype.SpatialInfo{Extent: &extent}
	}
	if srid, ok := eptEPSG(doc.SRS); ok {
		if spatial == nil {
			spatial = &datatype.SpatialInfo{}
		}
		spatial.SRID = &srid
		spatial.CRSRef = datatype.EPSGCRSRef(srid)
	}
	return spatial
}

func eptEPSG(srs manifestSRS) (int, bool) {
	if !strings.EqualFold(strings.TrimSpace(srs.Authority), "EPSG") {
		return 0, false
	}
	value := strings.TrimSpace(srs.Horizontal)
	value = strings.TrimPrefix(strings.ToUpper(value), "EPSG:")
	srid, err := strconv.Atoi(value)
	if err != nil || srid <= 0 {
		return 0, false
	}
	return srid, true
}

func eptFormatInfo(doc *manifestDocument) map[string]interface{} {
	if doc == nil {
		return nil
	}
	info := map[string]interface{}{
		"manifest_ref": ManifestFileName,
		"version":      strings.TrimSpace(doc.Version),
		"schema":       schemaFormatInfo(doc.Schema),
	}
	if value := strings.TrimSpace(doc.DataType); value != "" {
		info["data_type"] = value
	}
	if doc.Points > 0 {
		info["points"] = doc.Points
	}
	if len(doc.Bounds) == 6 {
		info["bounds"] = append([]float64(nil), doc.Bounds...)
	}
	if len(doc.BoundsConforming) == 6 {
		info["bounds_conforming"] = append([]float64(nil), doc.BoundsConforming...)
	}
	if doc.Span > 0 {
		info["span"] = doc.Span
	}
	if value := strings.TrimSpace(doc.HierarchyType); value != "" {
		info["hierarchy_type"] = value
	}
	if srs := eptSRSInfo(doc.SRS); len(srs) > 0 {
		info["srs"] = srs
	}
	return info
}

func schemaFormatInfo(schema []schemaDimension) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(schema))
	for _, dim := range schema {
		item := map[string]interface{}{}
		if value := strings.TrimSpace(dim.Name); value != "" {
			item["name"] = value
		}
		if value := strings.TrimSpace(dim.Type); value != "" {
			item["type"] = value
		}
		if dim.Size > 0 {
			item["size"] = dim.Size
		}
		if value := scalarFormatValue(dim.Scale); value != nil {
			item["scale"] = value
		}
		if value := scalarFormatValue(dim.Offset); value != nil {
			item["offset"] = value
		}
		if len(item) > 0 {
			result = append(result, item)
		}
	}
	return result
}

func scalarFormatValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return trimmed
		}
	case float64:
		return typed
	case int:
		return typed
	case int64:
		return typed
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	}
	return nil
}

func eptSRSInfo(srs manifestSRS) map[string]interface{} {
	info := map[string]interface{}{}
	if value := strings.TrimSpace(srs.Authority); value != "" {
		info["authority"] = value
	}
	if value := strings.TrimSpace(srs.Horizontal); value != "" {
		info["horizontal"] = value
	}
	if value := strings.TrimSpace(srs.WKT); value != "" {
		info["wkt"] = value
	}
	return info
}

func hasSchemaAny(schema []schemaDimension, names ...string) bool {
	lookup := make(map[string]struct{}, len(names))
	for _, name := range names {
		lookup[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	for _, dim := range schema {
		if _, ok := lookup[strings.ToLower(strings.TrimSpace(dim.Name))]; ok {
			return true
		}
	}
	return false
}

func positiveInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func positiveInt(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func optionalBool(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatEPT
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-ept",
		Format:   format.FormatEPT,
		I18nKey:  "format.ept",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			FileNames: []string{ManifestFileName},
		},
	}
}
