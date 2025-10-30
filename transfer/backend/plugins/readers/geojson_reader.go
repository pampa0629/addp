package readers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/format"
	"github.com/addp/common/format/geojson"
	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
)

// GeoJSONReader GeoJSON 文件读取器
type GeoJSONReader struct {
	filePath      string
	file          *os.File
	iterator      *geojson.Iterator
	batchSize     int
	offset        int64
	schema        *pipeline.Schema
	geometryField string
	mode          pipeline.ReaderMode
}

// GeoJSONConfig GeoJSON 配置
type GeoJSONConfig struct {
	FilePath      string `json:"file_path"`
	GeometryField string `json:"geometry_field"`
}

// NewGeoJSONReader 创建 GeoJSON Reader
func NewGeoJSONReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var geojsonConfig GeoJSONConfig
	if err := utils.MapToStruct(config.Config, &geojsonConfig); err != nil {
		return nil, fmt.Errorf("invalid geojson config: %w", err)
	}

	if geojsonConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if geojsonConfig.GeometryField == "" {
		geojsonConfig.GeometryField = "geometry"
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &GeoJSONReader{
		filePath:      geojsonConfig.FilePath,
		batchSize:     batchSize,
		geometryField: geojsonConfig.GeometryField,
		mode:          pipeline.ModeBatch,
	}, nil
}

// Open 打开 GeoJSON 文件
func (r *GeoJSONReader) Open(ctx context.Context, _ pipeline.ConnectorConfig) error {
	file, err := os.Open(r.filePath)
	if err != nil {
		return fmt.Errorf("failed to open geojson file: %w", err)
	}
	r.file = file

	parseOpts := format.DefaultParseOptions()
	if parseOpts.ExtraParams == nil {
		parseOpts.ExtraParams = make(map[string]interface{})
	}
	parseOpts.ExtraParams["geometry_field"] = r.geometryField
	parser := geojson.NewParser(parseOpts)

	if err := r.resetReader(); err != nil {
		return err
	}
	formatSchema, err := parser.ParseSchema(ctx, r.file)
	if err != nil {
		return fmt.Errorf("failed to parse geojson schema: %w", err)
	}
	r.schema = convertSchema(formatSchema)

	if err := r.resetReader(); err != nil {
		return err
	}

	iter, err := geojson.NewFeatureIterator(r.file)
	if err != nil {
		return fmt.Errorf("failed to initialize geojson iterator: %w", err)
	}
	r.iterator = iter

	return nil
}

// Read 读取一批数据
func (r *GeoJSONReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	if r.iterator == nil {
		return nil, fmt.Errorf("geojson reader not initialized")
	}

	rows := make([]map[string]interface{}, 0, r.batchSize)

	for i := 0; i < r.batchSize; i++ {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}

		feature, err := r.iterator.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read geojson feature: %w", err)
		}

		rows = append(rows, feature.ToRecord(r.geometryField))
		r.offset++
	}

	if len(rows) == 0 {
		return nil, io.EOF
	}

	return &pipeline.DataBatch{
		Rows:      rows,
		Schema:    r.schema,
		Offset:    r.offset,
		Timestamp: time.Now(),
	}, nil
}

// Schema 返回数据 schema
func (r *GeoJSONReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo 跳转到指定偏移量
func (r *GeoJSONReader) SeekTo(offset int64) error {
	// GeoJSON Reader 当前不支持随机定位
	return fmt.Errorf("seek not supported for geojson reader")
}

// Close 关闭连接
func (r *GeoJSONReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// Mode 返回读取模式
func (r *GeoJSONReader) Mode() pipeline.ReaderMode {
	return r.mode
}

func (r *GeoJSONReader) resetReader() error {
	if r.file == nil {
		return fmt.Errorf("geojson reader: file not opened")
	}
	_, err := r.file.Seek(0, io.SeekStart)
	return err
}

func convertSchema(schema *format.Schema) *pipeline.Schema {
	if schema == nil {
		return nil
	}

	fields := make([]pipeline.Field, 0, len(schema.Fields))
	var spatialType string
	if schema.GeometryType != nil {
		spatialType = *schema.GeometryType
	}
	srid := parseSRID(schema.SpatialRefSys)

	for _, field := range schema.Fields {
		pField := pipeline.Field{
			Name:     field.Name,
			Type:     string(field.Type),
			Nullable: field.Nullable,
		}

		if field.Type == format.FieldTypeGeometry {
			pField.SpatialType = spatialType
			pField.SRID = srid
		}

		fields = append(fields, pField)
	}

	metadata := make(map[string]interface{})
	if schema.GeometryField != nil {
		metadata["geometry_field"] = *schema.GeometryField
	}
	if schema.GeometryType != nil {
		metadata["geometry_type"] = *schema.GeometryType
	}
	if schema.SpatialRefSys != nil {
		metadata["spatial_ref_sys"] = *schema.SpatialRefSys
	}
	if schema.RecordCount != nil {
		metadata["record_count"] = *schema.RecordCount
	}

	return &pipeline.Schema{
		Fields:   fields,
		Metadata: metadata,
	}
}

func parseSRID(spatialRef *string) int {
	if spatialRef == nil || *spatialRef == "" {
		return 0
	}
	value := strings.TrimSpace(*spatialRef)
	if strings.HasPrefix(strings.ToUpper(value), "EPSG:") {
		num := strings.TrimPrefix(strings.ToUpper(value), "EPSG:")
		if srid, err := strconv.Atoi(num); err == nil {
			return srid
		}
	}
	if srid, err := strconv.Atoi(value); err == nil {
		return srid
	}
	return 0
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
