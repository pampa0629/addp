package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/addp/common/format"
	"github.com/addp/common/geo/shapefile"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type shapefilePreviewProvider struct {
	priority int
}

func NewShapefilePreviewProvider() PreviewProvider {
	return &shapefilePreviewProvider{
		priority: 95, // 高优先级，但低于对象存储通用处理器
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

	// 只支持对象存储中的Shapefile
	if !isObjectStorageType(req.Resource.ResourceType) {
		return false
	}

	// 必须有table参数（对象路径）
	if req.Table == "" {
		return false
	}

	// 检查文件扩展名
	formatType := format.DetectFormat(req.Table, nil)
	return formatType == format.FormatShapefile
}

func (p *shapefilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 连接到对象存储
	minioClient, bucket, err := p.createMinioClient(req.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 2. 下载.shp文件到临时目录
	tempFile, err := p.downloadShapefile(ctx, minioClient, bucket, req.Schema, req.Table)
	if err != nil {
		return nil, fmt.Errorf("failed to download shapefile: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(tempFile)) // 删除整个临时目录

	// 3. 打开Shapefile
	reader, err := shapefile.Open(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	// 4. 获取Schema
	schema := reader.GetSchema()
	columns := make([]string, 0, len(schema)+1)

	// 添加几何字段
	columns = append(columns, "geometry")

	// 添加属性字段
	for _, field := range schema {
		columns = append(columns, field.Name)
	}

	// 5. 读取数据（根据分页参数）
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	// 读取所有features（或前N条）
	maxFeatures := pageSize * (req.Page + 1) // 读取到当前页为止的所有数据
	features, err := reader.ReadAllFeatures(maxFeatures)
	if err != nil {
		return nil, fmt.Errorf("failed to read features: %w", err)
	}

	// 6. 分页处理
	totalCount := reader.Reader.AttributeCount()
	startIdx := req.Page * pageSize
	endIdx := startIdx + pageSize

	if startIdx >= len(features) {
		// 超出范围，返回空数据
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

	// 7. 构建返回数据
	shapeType := reader.Reader.GeometryType
	rows := make([]map[string]interface{}, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		feature := features[i]
		row := make(map[string]interface{})

		// 添加几何数据（简化表示）
		if feature.Geometry != nil {
			row["geometry"] = map[string]interface{}{
				"type": p.mapShapeType(int32(shapeType)),
			}
		}

		// 添加属性数据
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
	}, nil
}

// createMinioClient 创建MinIO客户端并返回bucket名称
func (p *shapefilePreviewProvider) createMinioClient(resource *models.Resource) (*minio.Client, string, error) {
	connInfo := resource.ConnectionInfo

	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL, _ := connInfo["use_ssl"].(bool)
	bucket, _ := connInfo["bucket"].(string)

	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
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

// downloadShapefile 下载Shapefile到临时目录
func (p *shapefilePreviewProvider) downloadShapefile(ctx context.Context, client *minio.Client, bucket, schema, objectKey string) (string, error) {
	// 构造完整的对象路径
	fullPath := objectKey
	if schema != "" && schema != bucket {
		fullPath = filepath.Join(schema, objectKey)
	}

	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "shapefile-*")
	if err != nil {
		return "", err
	}

	// 下载.shp文件
	shpFile := filepath.Join(tempDir, filepath.Base(objectKey))
	if err := p.downloadFile(ctx, client, bucket, fullPath, shpFile); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to download .shp: %w", err)
	}

	// 下载关联文件（.shx, .dbf, .prj等）
	basePath := fullPath[:len(fullPath)-4] // 移除.shp扩展名
	localBasePath := shpFile[:len(shpFile)-4]

	extensions := []string{".shx", ".dbf", ".prj", ".cpg"}
	for _, ext := range extensions {
		remotePath := basePath + ext
		localPath := localBasePath + ext

		// 尝试下载，如果不存在也不报错（某些文件可选）
		_ = p.downloadFile(ctx, client, bucket, remotePath, localPath)
	}

	return shpFile, nil
}

// downloadFile 下载单个文件
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

// mapShapeType 将Shapefile的ShapeType映射到几何类型字符串
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
