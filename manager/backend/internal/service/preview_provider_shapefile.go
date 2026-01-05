package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/addp/common/format"
	"github.com/addp/common/format/shapefile"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
)

type shapefilePreviewProvider struct {
	priority int
}

func NewShapefilePreviewProvider() PreviewProvider {
	return &shapefilePreviewProvider{
		priority: 95,
	}
}

func (p *shapefilePreviewProvider) Name() string {
	return "builtin:shapefile"
}

func (p *shapefilePreviewProvider) Priority() int {
	return p.priority
}

func (p *shapefilePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}

	if !isObjectStorageType(req.Engine.EngineType) {
		return false
	}

	if req.Table == "" {
		return false
	}

	formatType := format.DetectFormat(req.Table, nil)
	return formatType == format.FormatShapefile
}

func (p *shapefilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 使用统一的 MinIO 客户端创建函数
	minioClient, connBucket, err := createMinioClientFromEngine(req.Engine)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 使用统一的 bucket 解析函数
	bucket, err := resolveBucket(connBucket, req.Schema)
	if err != nil {
		return nil, err
	}

	tempFile, err := p.downloadShapefile(ctx, minioClient, bucket, req.Schema, req.Table)
	if err != nil {
		return nil, fmt.Errorf("failed to download shapefile: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(tempFile))

	reader, err := shapefile.Open(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	schema := reader.GetSchema()
	columns := make([]string, 0, len(schema)+1)
	columns = append(columns, "geometry")
	for _, field := range schema {
		columns = append(columns, field.Name)
	}

	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	maxFeatures := pageSize * (req.Page + 1)
	features, err := reader.ReadAllFeatures(maxFeatures)
	if err != nil {
		return nil, fmt.Errorf("failed to read features: %w", err)
	}

	totalCount := reader.Reader.AttributeCount()
	startIdx := req.Page * pageSize
	endIdx := startIdx + pageSize
	if startIdx >= len(features) {
		return &models.TablePreview{
			Mode:            PreviewModeObject,
			Columns:         columns,
			Rows:            []map[string]interface{}{},
			Total:           totalCount,
			Page:            req.Page,
			PageSize:        pageSize,
			GeometryColumns: []string{"geometry"},
		}, nil
	}
	if endIdx > len(features) {
		endIdx = len(features)
	}

	shapeType := reader.Reader.GeometryType
	rows := make([]map[string]interface{}, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		feature := features[i]
		row := make(map[string]interface{})

		if feature.Geometry != nil {
			row["geometry"] = map[string]interface{}{
				"type": shapefile.MapShapeType(shapeType),
			}
		}

		for k, v := range feature.Properties {
			row[k] = v
		}

		rows = append(rows, row)
	}

	return &models.TablePreview{
		Mode:            PreviewModeObject,
		Columns:         columns,
		Rows:            rows,
		Total:           totalCount,
		Page:            req.Page,
		PageSize:        pageSize,
		GeometryColumns: []string{"geometry"},
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        req.Table,
			ContentType: "application/x-esri-shapefile",
			Content: &models.ObjectPreviewContent{
				Kind: "shapefile",
			},
		},
	}, nil
}

func (p *shapefilePreviewProvider) downloadShapefile(ctx context.Context, client *minio.Client, bucket, schema, table string) (string, error) {
	// 处理路径:table 可能是 ".shp", ".dbf", ".shx" 等任何 shapefile 组件
	// 我们需要提取基础路径并确保下载所有组件
	fullPath := table
	if schema != "" && schema != bucket {
		fullPath = filepath.Join(schema, table)
	}

	// 如果 fullPath 以 .dbf, .shx, .prj 等结尾,去掉扩展名得到基础路径
	ext := filepath.Ext(fullPath)
	basePath := fullPath
	if ext != "" {
		basePath = fullPath[:len(fullPath)-len(ext)]
	}

	tempDir, err := os.MkdirTemp("", "shapefile-preview-*")
	if err != nil {
		return "", err
	}

	// 下载 .shp 主文件
	shpPath := basePath + ".shp"
	shpFile := filepath.Join(tempDir, filepath.Base(shpPath))
	object, err := client.GetObject(ctx, bucket, shpPath, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer object.Close()

	destFile, err := os.Create(shpFile)
	if err != nil {
		return "", err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, object); err != nil {
		return "", err
	}

	localBasePath := shpFile[:len(shpFile)-4]

	// 下载其他必需的组件文件
	extensions := []string{".shx", ".dbf", ".prj", ".cpg"}
	for _, extension := range extensions {
		remotePath := basePath + extension
		localPath := localBasePath + extension
		_ = p.downloadFile(ctx, client, bucket, remotePath, localPath)
	}

	return shpFile, nil
}

func (p *shapefilePreviewProvider) downloadFile(ctx context.Context, client *minio.Client, bucket, objectKey, destPath string) error {
	object, err := client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer object.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, object)
	return err
}
