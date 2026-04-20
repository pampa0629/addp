package writers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/pkg/vfs"
	"github.com/addp/transfer/plugins/utils"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// GeoJSONWriter GeoJSON 文件写入器
type GeoJSONWriter struct {
	filePath       string
	geometryField  string
	file           io.WriteCloser
	encoder        *json.Encoder
	isFirstFeature bool
	vfs            vfs.VFS // nil 时使用本地文件系统
}

// GeoJSONWriterConfig GeoJSON Writer 配置
type GeoJSONWriterConfig struct {
	FilePath      string `json:"file_path"`      // .geojson 文件路径
	GeometryField string `json:"geometry_field"` // 几何字段名（默认 "geometry"）
	Pretty        bool   `json:"pretty"`         // 是否格式化输出
}

// NewGeoJSONWriter 创建 GeoJSON Writer
func NewGeoJSONWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var writerConfig GeoJSONWriterConfig
	if err := utils.MapToStruct(config.Config, &writerConfig); err != nil {
		return nil, fmt.Errorf("invalid geojson config: %w", err)
	}

	if writerConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if writerConfig.GeometryField == "" {
		writerConfig.GeometryField = "geometry"
	}

	return &GeoJSONWriter{
		filePath:       writerConfig.FilePath,
		geometryField:  writerConfig.GeometryField,
		isFirstFeature: true,
	}, nil
}

// Open 打开 GeoJSON 文件写入
func (w *GeoJSONWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var writerConfig GeoJSONWriterConfig
	if err := utils.MapToStruct(config.Config, &writerConfig); err != nil {
		return err
	}

	fs := w.vfs
	if fs == nil {
		fs = &vfs.LocalVFS{}
	}

	file, err := fs.Create(w.filePath)
	if err != nil {
		return fmt.Errorf("failed to create geojson file: %w", err)
	}

	w.file = file
	w.encoder = json.NewEncoder(file)

	if writerConfig.Pretty {
		w.encoder.SetIndent("", "  ")
	}

	// 写入 FeatureCollection 头部
	if _, err := io.WriteString(w.file, `{"type":"FeatureCollection","features":[`); err != nil {
		return fmt.Errorf("failed to write geojson header: %w", err)
	}

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
				if value != nil {
					if geomMap, ok := value.(map[string]interface{}); ok {
						featJSON := map[string]interface{}{
							"type":     "Feature",
							"geometry": geomMap,
						}
						featBytes, _ := json.Marshal(featJSON)
						var feat geojson.Feature
						if err := json.Unmarshal(featBytes, &feat); err == nil {
							feature.Geometry = feat.Geometry
						}
					}
				}
			} else if key == "id" {
				if idStr, ok := value.(string); ok {
					feature.ID = idStr
				} else if idNum, ok := value.(int); ok {
					feature.ID = fmt.Sprintf("%d", idNum)
				} else if value != nil {
					feature.ID = fmt.Sprintf("%v", value)
				}
			} else {
				feature.Properties[key] = value
			}
		}

		if !w.isFirstFeature {
			if _, err := io.WriteString(w.file, ","); err != nil {
				return fmt.Errorf("failed to write separator: %w", err)
			}
		} else {
			w.isFirstFeature = false
		}

		if err := w.encoder.Encode(feature); err != nil {
			return fmt.Errorf("failed to encode feature: %w", err)
		}
	}

	return nil
}

// Flush 刷新缓冲区
func (w *GeoJSONWriter) Flush(ctx context.Context) error {
	if w.file != nil {
		if syncer, ok := w.file.(interface{ Sync() error }); ok {
			return syncer.Sync()
		}
	}
	return nil
}

// Close 关闭连接
func (w *GeoJSONWriter) Close() error {
	if w.file != nil {
		if _, err := io.WriteString(w.file, "]}"); err != nil {
			_ = w.file.Close()
			return fmt.Errorf("failed to write geojson footer: %w", err)
		}
		return w.file.Close()
	}
	return nil
}

// newGeoJSONWriterWithVFS 创建带 VFS 的 GeoJSON Writer（供 NFSWriter 使用）
func newGeoJSONWriterWithVFS(filePath, geometryField string, fs vfs.VFS) *GeoJSONWriter {
	if geometryField == "" {
		geometryField = "geometry"
	}
	return &GeoJSONWriter{
		filePath:       filePath,
		geometryField:  geometryField,
		isFirstFeature: true,
		vfs:            fs,
	}
}

// newGeoJSONWriterWithFile 使用已打开的 io.WriteCloser 创建 GeoJSON Writer（供 NFSWriter 使用）
func newGeoJSONWriterWithFile(file io.WriteCloser, geometryField string) (*GeoJSONWriter, error) {
	if geometryField == "" {
		geometryField = "geometry"
	}
	w := &GeoJSONWriter{
		file:           file,
		geometryField:  geometryField,
		isFirstFeature: true,
	}
	w.encoder = json.NewEncoder(file)
	if _, err := io.WriteString(file, `{"type":"FeatureCollection","features":[`); err != nil {
		return nil, fmt.Errorf("failed to write geojson header: %w", err)
	}
	return w, nil
}
