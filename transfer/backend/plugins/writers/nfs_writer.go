package writers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	nfsplugin "github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	parquetgo "github.com/parquet-go/parquet-go"
)

// NFSWriter NFS 文件系统写入器，支持 shapefile/geojson/csv/parquet 格式
// - geojson/csv/parquet：直接写入 NFS，无临时文件
// - shapefile：写入临时目录后逐文件上传（go-shp 不支持自定义 io.Writer）
type NFSWriter struct {
	nfs      *nfsplugin.NFSPlugin
	connInfo plugin.ConnectionInfo

	path     string // NFS 上的目标目录
	fileName string // 输出文件名（不含扩展名）
	fileType string // geojson / csv / shapefile / parquet

	geometryField string

	fileWriter pipeline.Writer // 委托的格式写入器
	tempDir    string          // shapefile 临时目录
}

// NewNFSWriter 创建 NFS 写入器工厂函数
func NewNFSWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	return &NFSWriter{
		nfs: &nfsplugin.NFSPlugin{},
	}, nil
}

// Open 初始化 NFS 连接并创建格式写入器
func (w *NFSWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	w.connInfo = plugin.ConnectionInfo{
		"server":      utils.GetStringConfig(config, "server", ""),
		"export_path": utils.GetStringConfig(config, "export_path", ""),
	}
	if w.connInfo["server"] == "" || w.connInfo["export_path"] == "" {
		return fmt.Errorf("NFS writer requires server and export_path")
	}

	w.path = utils.GetStringConfig(config, "path", "")
	w.fileName = utils.GetStringConfig(config, "file_name", "output")
	w.fileType = strings.ToLower(utils.GetStringConfig(config, "file_type", "geojson"))
	w.geometryField = utils.GetStringConfig(config, "geometry_field", "")
	if w.geometryField == "" {
		w.geometryField = "geometry"
	}

	// 确保目标目录存在
	if w.path != "" {
		if err := w.nfs.MkdirAll(ctx, w.connInfo, w.path); err != nil {
			return fmt.Errorf("failed to create NFS directory: %w", err)
		}
	}

	switch w.fileType {
	case "geojson":
		return w.openGeoJSONWriter(ctx, config)
	case "csv":
		return w.openCSVWriter(ctx, config)
	case "shapefile":
		return w.openShapefileWriter(ctx, config)
	case "parquet":
		return w.openParquetWriter(ctx, config)
	default:
		return fmt.Errorf("unsupported NFS file type: %s", w.fileType)
	}
}

func (w *NFSWriter) openGeoJSONWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	nfsPath := w.nfsFilePath(w.fileName + ".geojson")
	file, err := w.nfs.OpenFileForWrite(ctx, w.connInfo, nfsPath)
	if err != nil {
		return fmt.Errorf("failed to open NFS file for geojson: %w", err)
	}
	gw, err := newGeoJSONWriterWithFile(file, w.geometryField)
	if err != nil {
		file.Close()
		return err
	}
	w.fileWriter = gw
	return nil
}

func (w *NFSWriter) openCSVWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	nfsPath := w.nfsFilePath(w.fileName + ".csv")
	file, err := w.nfs.OpenFileForWrite(ctx, w.connInfo, nfsPath)
	if err != nil {
		return fmt.Errorf("failed to open NFS file for csv: %w", err)
	}
	delimiter := utils.GetStringConfig(config, "delimiter", ",")
	w.fileWriter = newCSVWriterWithFile(file, delimiter, true)
	return nil
}

func (w *NFSWriter) openShapefileWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	// go-shp 只能写本地文件系统，先写临时目录再上传
	tempDir, err := os.MkdirTemp("", "nfs_writer_shapefile_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	w.tempDir = tempDir

	baseName := w.fileName
	if baseName == "" {
		baseName = "dataset"
	}
	shapefilePath := filepath.Join(tempDir, baseName+".shp")

	fileConfig := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path":      shapefilePath,
			"geometry_field": w.geometryField,
		},
		BatchSize: config.BatchSize,
	}

	writer, err := NewShapefileWriter(fileConfig)
	if err != nil {
		os.RemoveAll(tempDir)
		return err
	}
	if err := writer.Open(ctx, fileConfig); err != nil {
		os.RemoveAll(tempDir)
		return err
	}
	w.fileWriter = writer
	return nil
}

func (w *NFSWriter) openParquetWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	nfsPath := w.nfsFilePath(w.fileName + ".parquet")
	file, err := w.nfs.OpenFileForWrite(ctx, w.connInfo, nfsPath)
	if err != nil {
		return fmt.Errorf("failed to open NFS file for parquet: %w", err)
	}
	pw := &nfsParquetWriter{file: file}
	w.fileWriter = pw
	return nil
}

// Write 写入数据批次
func (w *NFSWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	return w.fileWriter.Write(ctx, batch)
}

// Flush 刷新缓冲区
func (w *NFSWriter) Flush(ctx context.Context) error {
	return w.fileWriter.Flush(ctx)
}

// Close 关闭写入器，shapefile 需要上传到 NFS
func (w *NFSWriter) Close() error {
	defer func() {
		if w.tempDir != "" {
			os.RemoveAll(w.tempDir)
		}
	}()

	if w.fileWriter != nil {
		if err := w.fileWriter.Close(); err != nil {
			return err
		}
	}

	if w.fileType == "shapefile" && w.tempDir != "" {
		return w.uploadShapefileToNFS()
	}
	return nil
}

// uploadShapefileToNFS 将临时目录中的 shapefile 组件逐一上传到 NFS
func (w *NFSWriter) uploadShapefileToNFS() error {
	baseName := w.fileName
	if baseName == "" {
		baseName = "dataset"
	}
	baseLocal := filepath.Join(w.tempDir, baseName)

	for _, ext := range []string{"shp", "shx", "dbf", "prj", "cpg"} {
		localPath := baseLocal + "." + ext
		if _, err := os.Stat(localPath); err != nil {
			continue
		}
		f, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("failed to open shapefile component %s: %w", ext, err)
		}
		nfsPath := w.nfsFilePath(baseName + "." + ext)
		uploadErr := w.nfs.WriteFile(context.Background(), w.connInfo, nfsPath, f)
		f.Close()
		if uploadErr != nil {
			return fmt.Errorf("failed to upload %s to NFS: %w", ext, uploadErr)
		}
	}
	return nil
}

// nfsFilePath 拼接 NFS 目标路径
func (w *NFSWriter) nfsFilePath(name string) string {
	if w.path == "" {
		return "/" + name
	}
	p := strings.TrimSuffix(w.path, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p + "/" + name
}

// nfsParquetWriter 直接写入 NFS 的 Parquet 写入器
type nfsParquetWriter struct {
	file        io.WriteCloser
	pw          *parquetgo.GenericWriter[map[string]any]
	schema      *parquetgo.Schema
	fieldNames  []string
	initialized bool
}

func (w *nfsParquetWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	return nil // 已在 NFSWriter.openParquetWriter 中打开文件
}

func (w *nfsParquetWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if !w.initialized {
		if err := w.init(batch); err != nil {
			return err
		}
	}
	rows := make([]map[string]any, 0, len(batch.Rows))
	for _, row := range batch.Rows {
		normalized := make(map[string]any, len(w.fieldNames))
		for _, name := range w.fieldNames {
			normalized[name] = normalizeParquetValue(row[name])
		}
		rows = append(rows, normalized)
	}
	if _, err := w.pw.Write(rows); err != nil {
		return fmt.Errorf("failed to write parquet rows: %w", err)
	}
	return nil
}

func (w *nfsParquetWriter) init(batch *pipeline.DataBatch) error {
	if batch.Schema != nil && len(batch.Schema.Fields) > 0 {
		w.fieldNames = make([]string, 0, len(batch.Schema.Fields))
		for _, f := range batch.Schema.Fields {
			w.fieldNames = append(w.fieldNames, f.Name)
		}
		w.schema = buildSchemaFromPipelineFields(batch.Schema.Fields)
	} else if len(batch.Rows) > 0 {
		w.fieldNames, w.schema = inferSchemaFromRow(batch.Rows[0])
	} else {
		return fmt.Errorf("cannot infer parquet schema: no schema and no rows")
	}
	w.pw = parquetgo.NewGenericWriter[map[string]any](w.file, w.schema)
	w.initialized = true
	return nil
}

func (w *nfsParquetWriter) Flush(ctx context.Context) error {
	if w.pw != nil {
		return w.pw.Flush()
	}
	return nil
}

func (w *nfsParquetWriter) Close() error {
	if w.pw != nil {
		if err := w.pw.Close(); err != nil {
			return err
		}
	}
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
