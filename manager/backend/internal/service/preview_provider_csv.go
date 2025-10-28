package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type csvPreviewProvider struct {
	priority int
}

// NewCSVPreviewProvider 创建 CSV 预览插件实例
func NewCSVPreviewProvider() PreviewProvider {
	return &csvPreviewProvider{
		priority: 90, // 高优先级
	}
}

func (p *csvPreviewProvider) Name() string {
	return "builtin:csv"
}

func (p *csvPreviewProvider) Priority() int {
	return p.priority
}

func (p *csvPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}

	// 只支持对象存储中的CSV
	if !isObjectStorageType(req.Resource.ResourceType) {
		return false
	}

	// 必须有table参数（对象路径）
	if req.Table == "" {
		return false
	}

	// 检查文件扩展名
	formatType := format.DetectFormat(req.Table, nil)
	return formatType == format.FormatCSV || formatType == format.FormatTSV
}

func (p *csvPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 连接到对象存储
	minioClient, bucket, err := p.createMinioClient(req.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 2. 构造完整的对象路径
	fullPath := req.Table
	if req.Schema != "" && req.Schema != bucket {
		fullPath = filepath.Join(req.Schema, req.Table)
	}

	// 3. 获取对象
	object, err := minioClient.GetObject(ctx, bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}
	defer object.Close()

	// 4. 创建CSV reader
	reader := csv.NewReader(object)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true

	// 检测分隔符
	delimiter := p.detectDelimiter(req.Table)
	reader.Comma = delimiter

	// 5. 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 清理表头
	columns := make([]string, len(headers))
	for i, h := range headers {
		columns[i] = strings.TrimSpace(h)
		if columns[i] == "" {
			columns[i] = fmt.Sprintf("Column%d", i+1)
		}
	}

	// 6. 读取所有数据（用于计数和分页）
	allRows := make([][]string, 0)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错处理：跳过损坏的行
			continue
		}
		allRows = append(allRows, record)
	}

	totalCount := len(allRows)

	// 7. 分页处理
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	startIdx := req.Page * pageSize
	endIdx := startIdx + pageSize

	if startIdx >= totalCount {
		// 超出范围，返回空数据
		return &models.TablePreview{
			Mode:     PreviewModeObject,
			Columns:  columns,
			Rows:     []map[string]interface{}{},
			Total:    totalCount,
			Page:     req.Page,
			PageSize: pageSize,
		}, nil
	}

	if endIdx > totalCount {
		endIdx = totalCount
	}

	// 8. 构建返回数据
	rows := make([]map[string]interface{}, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		record := allRows[i]
		row := make(map[string]interface{})

		for j, value := range record {
			if j < len(columns) {
				row[columns[j]] = value
			} else {
				// 处理列数不匹配的情况
				row[fmt.Sprintf("Extra%d", j-len(columns)+1)] = value
			}
		}

		rows = append(rows, row)
	}

	return &models.TablePreview{
		Mode:     PreviewModeObject,
		Columns:  columns,
		Rows:     rows,
		Total:    totalCount,
		Page:     req.Page,
		PageSize: pageSize,
	}, nil
}

// createMinioClient 创建MinIO客户端并返回bucket名称
func (p *csvPreviewProvider) createMinioClient(resource *models.Resource) (*minio.Client, string, error) {
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

// detectDelimiter 检测CSV分隔符
func (p *csvPreviewProvider) detectDelimiter(filename string) rune {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".tsv":
		return '\t'
	case ".csv":
		return ','
	default:
		// 默认使用逗号
		return ','
	}
}
