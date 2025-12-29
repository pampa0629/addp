package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	csvParser "github.com/addp/common/format/csv"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type csvPreviewProvider struct {
	priority int
}

func NewCSVPreviewProvider() PreviewProvider {
	return &csvPreviewProvider{
		priority: 90,
	}
}

func (p *csvPreviewProvider) Name() string {
	return "builtin:csv"
}

func (p *csvPreviewProvider) Priority() int {
	return p.priority
}

func (p *csvPreviewProvider) Supports(req *PreviewRequest) bool {
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
	return formatType == format.FormatCSV || formatType == format.FormatTSV
}

func (p *csvPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// Create MinIO client
	minioClient, bucket, err := p.createMinioClient(req.Engine)
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

	// Build full object path
	fullPath := req.Table
	if req.Schema != "" && req.Schema != bucket {
		fullPath = filepath.Join(req.Schema, req.Table)
	}

	// Get object from MinIO
	object, err := minioClient.GetObject(ctx, bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}
	defer object.Close()

	// Build parse options
	opts := &format.ParseOptions{
		Delimiter:  p.detectDelimiter(req.Table),
		HasHeader:  true,
		SampleSize: 100,
	}

	// Use shared CSV parser
	parser := csvParser.NewParser(opts)

	// Get total count first
	totalCount, err := parser.CountRecords(ctx, object)
	if err != nil {
		// If count fails, continue with best effort
		totalCount = 0
	}

	// Reset stream
	object, err = minioClient.GetObject(ctx, bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object: %w", err)
	}
	defer object.Close()

	// Parse schema to get column names
	schema, err := parser.ParseSchema(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV schema: %w", err)
	}

	// Extract column names
	columns := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		columns[i] = field.Name
	}

	// Calculate pagination
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	offset := req.Page * pageSize

	// Reset stream again for data reading
	object, err = minioClient.GetObject(ctx, bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// Read paginated records using shared parser
	records, err := parser.ReadRecords(ctx, object, int64(offset), int64(pageSize))
	if err != nil {
		// Partial read is OK
		if len(records) == 0 {
			return nil, fmt.Errorf("failed to read CSV records: %w", err)
		}
	}

	// If we couldn't get total count earlier, estimate it
	if totalCount == 0 {
		totalCount = int64(len(records))
	}

	return &models.TablePreview{
		Mode:     PreviewModeObject,
		Columns:  columns,
		Rows:     records,
		Total:    int(totalCount),
		Page:     req.Page,
		PageSize: pageSize,
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        req.Table,
			ContentType: "text/csv",
			Content: &models.ObjectPreviewContent{
				Kind: "csv",
			},
		},
	}, nil
}

func (p *csvPreviewProvider) createMinioClient(resource *models.Engine) (*minio.Client, string, error) {
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

func (p *csvPreviewProvider) detectDelimiter(filename string) rune {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".tsv":
		return '\t'
	case ".csv":
		return ','
	default:
		return ','
	}
}
