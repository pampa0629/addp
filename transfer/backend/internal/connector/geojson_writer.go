package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// GeoJSONWriter GeoJSON 文件写入器
type GeoJSONWriter struct {
	filePath      string
	geometryField string
	file          *os.File
	encoder       *json.Encoder
	isFirstFeature bool
}

// GeoJSONWriterConfig GeoJSON Writer 配置
type GeoJSONWriterConfig struct {
	FilePath      string `json:"file_path"`       // .geojson 文件路径
	GeometryField string `json:"geometry_field"`  // 几何字段名（默认 "geometry"）
	Pretty        bool   `json:"pretty"`          // 是否格式化输出
}

// NewGeoJSONWriter 创建 GeoJSON Writer
func NewGeoJSONWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var writerConfig GeoJSONWriterConfig
	if err := mapToStruct(config.Config, &writerConfig); err != nil {
		return nil, fmt.Errorf("invalid geojson config: %w", err)
	}

	if writerConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if writerConfig.GeometryField == "" {
		writerConfig.GeometryField = "geometry"
	}

	return &GeoJSONWriter{
		filePath:      writerConfig.FilePath,
		geometryField: writerConfig.GeometryField,
		isFirstFeature: true,
	}, nil
}

// Open 打开 GeoJSON 文件写入
func (w *GeoJSONWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var writerConfig GeoJSONWriterConfig
	if err := mapToStruct(config.Config, &writerConfig); err != nil {
		return err
	}

	// 创建文件
	file, err := os.Create(w.filePath)
	if err != nil {
		return fmt.Errorf("failed to create geojson file: %w", err)
	}

	w.file = file
	w.encoder = json.NewEncoder(file)

	if writerConfig.Pretty {
		w.encoder.SetIndent("", "  ")
	}

	// 写入 FeatureCollection 头部
	file.WriteString(`{"type":"FeatureCollection","features":[`)

	return nil
}

// Write 写入数据批次
func (w *GeoJSONWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch.IsEmpty() {
		return nil
	}

	for _, row := range batch.Rows {
		feature := geojson.Feature{
			Properties: make(map[string]interface{}),
		}

		// 提取几何字段
		for key, value := range row {
			if key == w.geometryField {
				// 几何数据
				if value != nil {
					if geomMap, ok := value.(map[string]interface{}); ok {
						// 已经是 GeoJSON 格式
						geomJSON, _ := json.Marshal(geomMap)
						var geom interface{} // Use interface{} to hold geometry temporarily
						if err := json.Unmarshal(geomJSON, &geom); err == nil {
							// geojson.Feature.Geometry is geom.T, so we need to parse properly
							var feat geojson.Feature
							featJSON := map[string]interface{}{
								"type":     "Feature",
								"geometry": geomMap,
							}
							featBytes, _ := json.Marshal(featJSON)
							if err := json.Unmarshal(featBytes, &feat); err == nil {
								feature.Geometry = feat.Geometry
							}
						}
					}
				}
			} else if key == "id" {
				// ID 字段
				if idStr, ok := value.(string); ok {
					feature.ID = idStr
				} else if idNum, ok := value.(int); ok {
					feature.ID = fmt.Sprintf("%d", idNum)
				} else if value != nil {
					feature.ID = fmt.Sprintf("%v", value)
				}
			} else {
				// 属性字段
				feature.Properties[key] = value
			}
		}

		// 写入逗号（如果不是第一个 Feature）
		if !w.isFirstFeature {
			w.file.WriteString(",")
		} else {
			w.isFirstFeature = false
		}

		// 写入 Feature
		if err := w.encoder.Encode(feature); err != nil {
			return fmt.Errorf("failed to encode feature: %w", err)
		}
	}

	return nil
}

// Flush 刷新缓冲区
func (w *GeoJSONWriter) Flush(ctx context.Context) error {
	if w.file != nil {
		return w.file.Sync()
	}
	return nil
}

// Close 关闭连接
func (w *GeoJSONWriter) Close() error {
	if w.file != nil {
		// 写入 FeatureCollection 尾部
		w.file.WriteString("]}")
		return w.file.Close()
	}
	return nil
}
