package readers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/addp/common/format"
	geojson "github.com/addp/common/format/plugins/json"
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
	plugin := geojson.NewPlugin(parseOpts)

	if err := r.resetReader(); err != nil {
		return err
	}
	tableInfo, err := plugin.DescribeTable(ctx, r.file, parseOpts)
	if err != nil {
		return fmt.Errorf("failed to parse geojson schema: %w", err)
	}
	r.schema = convertSchemaFromTableInfo(tableInfo)

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

func convertSchemaFromTableInfo(tableInfo *format.TableInfo) *pipeline.Schema {
	if tableInfo == nil {
		return nil
	}

	fields := make([]pipeline.Field, 0, len(tableInfo.Fields))
	var spatialType string
	var srid int
	var geometryField string

	if spatialInfo := tableInfo.SpatialInfo; spatialInfo != nil {
		spatialType = spatialInfo.GeometryType
		srid = spatialInfo.SRID
		geometryField = spatialInfo.GeometryColumn
	}

	for _, field := range tableInfo.Fields {
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
	if geometryField != "" {
		metadata["geometry_field"] = geometryField
	}
	if spatialType != "" {
		metadata["geometry_type"] = spatialType
	}
	if srid != 0 {
		metadata["spatial_ref_sys"] = fmt.Sprintf("EPSG:%d", srid)
	}
	if tableInfo.RowCount != nil {
		metadata["record_count"] = *tableInfo.RowCount
	}

	return &pipeline.Schema{
		Fields:   fields,
		Metadata: metadata,
	}
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
