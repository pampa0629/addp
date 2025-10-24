package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// GeoJSONReader GeoJSON 文件读取器
type GeoJSONReader struct {
	filePath  string
	file      *os.File
	decoder   *json.Decoder
	batchSize int
	offset    int64
	schema    *pipeline.Schema
	mode      pipeline.ReaderMode
}

// GeoJSONConfig GeoJSON 配置
type GeoJSONConfig struct {
	FilePath      string `json:"file_path"`       // .geojson 文件路径
	GeometryField string `json:"geometry_field"`  // 几何字段名（默认 "geometry"）
}

// NewGeoJSONReader 创建 GeoJSON Reader
func NewGeoJSONReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var geojsonConfig GeoJSONConfig
	if err := mapToStruct(config.Config, &geojsonConfig); err != nil {
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
		filePath:  geojsonConfig.FilePath,
		batchSize: batchSize,
		mode:      pipeline.ModeBatch,
	}, nil
}

// Open 打开 GeoJSON 文件
func (r *GeoJSONReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	file, err := os.Open(r.filePath)
	if err != nil {
		return fmt.Errorf("failed to open geojson file: %w", err)
	}

	r.file = file
	r.decoder = json.NewDecoder(file)

	// 读取第一个 token 判断格式
	token, err := r.decoder.Token()
	if err != nil {
		return fmt.Errorf("failed to read geojson: %w", err)
	}

	// 检查是否是 FeatureCollection
	if delim, ok := token.(json.Delim); ok && delim == '{' {
		// 读取 "type" 字段
		for r.decoder.More() {
			t, _ := r.decoder.Token()
			if t == "type" {
				typeValue, _ := r.decoder.Token()
				if typeValue != "FeatureCollection" {
					return fmt.Errorf("only FeatureCollection format is supported")
				}
			} else if t == "features" {
				// 找到 features 数组
				featuresToken, _ := r.decoder.Token()
				if delim, ok := featuresToken.(json.Delim); !ok || delim != '[' {
					return fmt.Errorf("features must be an array")
				}
				break
			} else {
				// 跳过其他字段
				r.decoder.Token()
			}
		}
	} else {
		return fmt.Errorf("invalid geojson format")
	}

	// 推断 schema（简化版）
	r.schema = &pipeline.Schema{
		Fields: []pipeline.Field{
			{Name: "id", Type: "string", Nullable: true},
			{Name: "geometry", Type: "geometry", SpatialType: "Geometry", Nullable: false},
			{Name: "properties", Type: "json", Nullable: true},
		},
		Metadata: map[string]interface{}{
			"source_type": "geojson",
		},
	}

	return nil
}

// Read 读取一批数据
func (r *GeoJSONReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	var batchRows []map[string]interface{}

	// 读取批次数据
	for i := 0; i < r.batchSize && r.decoder.More(); i++ {
		var feature geojson.Feature
		if err := r.decoder.Decode(&feature); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode feature: %w", err)
		}

		row := make(map[string]interface{})

		// ID
		if feature.ID != "" {
			row["id"] = feature.ID
		}

		// Geometry (保持为 GeoJSON map 格式)
		if feature.Geometry != nil {
			geomJSON, _ := json.Marshal(feature.Geometry)
			var geomMap map[string]interface{}
			json.Unmarshal(geomJSON, &geomMap)
			row["geometry"] = geomMap
		}

		// Properties (展开到顶层)
		if feature.Properties != nil {
			for k, v := range feature.Properties {
				row[k] = v
			}
		}

		batchRows = append(batchRows, row)
		r.offset++
	}

	// 检查是否读完
	if len(batchRows) == 0 {
		return nil, io.EOF
	}

	return &pipeline.DataBatch{
		Rows:      batchRows,
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
	// GeoJSON 不支持随机访问，需要重新打开并跳过
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
