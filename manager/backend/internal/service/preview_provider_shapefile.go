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
	"github.com/minio/minio-go/v7/pkg/credentials"
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
	if req == nil || req.Resource == nil {
		return false
	}

	if !isObjectStorageType(req.Resource.EngineType) {
		return false
	}

	if req.Table == "" {
		return false
	}

	formatType := format.DetectFormat(req.Table, nil)
	return formatType == format.FormatShapefile
}

func (p *shapefilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	minioClient, bucket, err := p.createMinioClient(req.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 如果 connection_info 中没有指定 bucket,使用 schema 参数作为 bucket
	if bucket == "" {
		bucket = req.Schema
	}

	// 验证 bucket 不为空
	if bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
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
				"type": p.mapShapeType(int32(shapeType)),
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

func (p *shapefilePreviewProvider) createMinioClient(resource *models.Resource) (*minio.Client, string, error) {
	connInfo := resource.ConnectionInfo

	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL, _ := connInfo["use_ssl"].(bool)
	bucket, _ := connInfo["bucket"].(string)

	// endpoint, accessKey, secretKey 是必需的
	// bucket 可以为空(从 schema 参数获取)
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, "", fmt.Errorf("missing required connection info")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, "", err
	}

	return client, bucket, nil
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

func (p *shapefilePreviewProvider) mapShapeType(shapeType int32) string {
	switch shapeType {
	case 0:
		return "Null"
	case 1:
		return "Point"
	case 3:
		return "Polyline"
	case 5:
		return "Polygon"
	case 8:
		return "MultiPoint"
	case 11:
		return "PointZ"
	case 13:
		return "PolylineZ"
	case 15:
		return "PolygonZ"
	case 18:
		return "MultiPointZ"
	case 21:
		return "PointM"
	case 23:
		return "PolylineM"
	case 25:
		return "PolygonM"
	case 28:
		return "MultiPointM"
	case 31:
		return "MultiPatch"
	default:
		return fmt.Sprintf("Unknown(%d)", shapeType)
	}
}
