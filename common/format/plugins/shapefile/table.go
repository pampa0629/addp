package shapefile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
)

// Plugin 实现 Shapefile 格式插件
type Plugin struct {
	options *format.ParseOptions
}

// NewPlugin 创建 Shapefile 插件
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts}
}

func (plugin *Plugin) DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, options *format.ParseOptions) (*format.TableInfo, error) {
	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	opts := plugin.resolveMaterializedOptions(basePath, options)
	return plugin.describeTableInfoFromHeaders(basePath, refs, opts)
}

func (plugin *Plugin) SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	opts := plugin.resolveOptions(options)
	if rows, ok, err := plugin.sampleMultiTableIndexed(ctx, reader, refs, offset, limit, opts); ok {
		if err == nil {
			return rows, nil
		}
		if !isIndexedSampleFallbackError(err) {
			return nil, err
		}
	}

	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	applyMaterializedSidecarOptions(basePath, opts)
	return plugin.sampleTableFromPath(ctx, basePath+".shp", offset, limit, opts)
}

func (plugin *Plugin) describeTableInfoFromHeaders(basePath string, refs []format.RelatedRef, opts *format.ParseOptions) (*format.TableInfo, error) {
	shpHeader, err := readSHPHeader(basePath + ".shp")
	if err != nil {
		return nil, err
	}
	dbfHeader, err := readDBFHeader(basePath+".dbf", opts.Encoding)
	if err != nil {
		return nil, err
	}

	geometryField := plugin.getGeometryFieldName()
	fields := make([]format.FieldInfo, 0, len(dbfHeader.Fields)+1)
	geomType := determineShapefileGeometryType(shpHeader.ShapeType)
	fields = append(fields, format.FieldInfo{
		Name:         geometryField,
		Type:         format.FieldTypeGeometry,
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})
	for _, field := range dbfHeader.Fields {
		fields = append(fields, format.FieldInfo{
			Name:      field.Name,
			Type:      dbfNativeTypeToCommon(field.RawType),
			Nullable:  true,
			Size:      field.Size,
			Precision: field.Precision,
		})
	}

	rowCount := int64(dbfHeader.RecordCount)
	spatialInfo := &format.SpatialInfo{
		GeometryColumn: geometryField,
		GeometryType:   geomType,
		SRID:           0,
		BoundingBox:    &shpHeader.BBox,
		Dimension:      2,
	}
	if opts != nil && opts.SpatialRefSys != "" {
		if srid := commonSpatial.ParseSRID(opts.SpatialRefSys); srid > 0 {
			spatialInfo.SRID = srid
		}
	}
	shapefileInfo := &FormatInfo{
		Encoding:   "",
		ShapeType:  geomType,
		HasPRJ:     fileExists(basePath + ".prj"),
		HasCPG:     fileExists(basePath + ".cpg"),
		DBFVersion: dbfHeader.Version,
	}
	if opts != nil {
		shapefileInfo.Encoding = NormalizeDBFEncoding(opts.Encoding)
	}

	info := &Info{
		BaseName:      filepath.Base(basePath),
		RefExtensions: refExtensions(refs),
		HasPRJ:        shapefileInfo.HasPRJ,
		HasCPG:        shapefileInfo.HasCPG,
		ShapeType:     geomType,
		DBFVersion:    dbfHeader.Version,
		Encoding:      shapefileInfo.Encoding,
	}
	return &format.TableInfo{
		Name:        "shapefile_data",
		RowCount:    &rowCount,
		Fields:      fields,
		PrimaryKey:  []string{},
		SpatialInfo: spatialInfo,
		FormatInfo:  map[string]interface{}{"shapefile": mergeFormatInfo(shapefileInfo.FormatAttributes(), info.FormatAttributes())},
	}, nil
}

func mergeFormatInfo(values ...map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for _, value := range values {
		for k, v := range value {
			result[k] = v
		}
	}
	return result
}

func (plugin *Plugin) sampleTableFromPath(ctx context.Context, shpPath string, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	opts = plugin.resolveOptions(opts)
	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgEncoding := readCPGEncoding(strings.TrimSuffix(shpPath, filepath.Ext(shpPath))); cpgEncoding != "" {
			encodingName = cpgEncoding
		}
	}
	reader, err := openWithEncoding(shpPath, encodingName)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	fields := reader.Fields()
	geometryField := plugin.getGeometryFieldName()
	currentRecord := int64(0)
	for currentRecord < offset && reader.Next() {
		currentRecord++
	}

	maxRows := limit
	if limit < 0 {
		maxRows = 1<<31 - 1
	}
	records := make([]map[string]interface{}, 0)
	readCount := int64(0)
	recordIndex := int(offset)

	for readCount < maxRows && reader.Next() {
		select {
		case <-ctx.Done():
			return records, ctx.Err()
		default:
		}

		_, shape := reader.Shape()
		record := make(map[string]interface{}, len(fields)+1)
		for i, field := range fields {
			fieldName := reader.trimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(reader.readAttributeDecoded(recordIndex, i))
			if rawValue == "" {
				record[fieldName] = nil
			} else {
				record[fieldName] = parseDBFAttribute(field.Fieldtype, rawValue)
			}
		}
		if shape != nil {
			geometryValue, err := shapeToRowValue(shape, opts, sridFromParseOptions(opts))
			if err == nil {
				record[geometryField] = geometryValue
			} else {
				record[geometryField] = nil
			}
		} else {
			record[geometryField] = nil
		}
		records = append(records, record)
		readCount++
		recordIndex++
	}
	return records, nil
}

func (plugin *Plugin) resolveOptions(options *format.ParseOptions) *format.ParseOptions {
	if options != nil {
		copied := *options
		return &copied
	}
	if plugin != nil && plugin.options != nil {
		copied := *plugin.options
		return &copied
	}
	return format.DefaultParseOptions()
}

func (plugin *Plugin) resolveMaterializedOptions(basePath string, options *format.ParseOptions) *format.ParseOptions {
	opts := plugin.resolveOptions(options)
	applyMaterializedSidecarOptions(basePath, opts)
	return opts
}

func applyMaterializedSidecarOptions(basePath string, opts *format.ParseOptions) {
	if opts == nil {
		return
	}
	if opts.SpatialRefSys == "" {
		if prjBytes, readErr := os.ReadFile(basePath + ".prj"); readErr == nil {
			opts.SpatialRefSys = strings.TrimSpace(string(prjBytes))
		}
	}
	if opts.Encoding == "" || NormalizeDBFEncoding(opts.Encoding) == "utf-8" {
		if cpgEncoding := readCPGEncoding(basePath); cpgEncoding != "" {
			opts.Encoding = cpgEncoding
		}
	}
}

// getGeometryFieldName 获取几何字段名称
func (plugin *Plugin) getGeometryFieldName() string {
	if plugin != nil && plugin.options != nil && plugin.options.ExtraParams != nil {
		if name, ok := plugin.options.ExtraParams["geometry_field"].(string); ok && name != "" {
			return name
		}
	}
	return "geometry" // 默认几何字段名
}

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
