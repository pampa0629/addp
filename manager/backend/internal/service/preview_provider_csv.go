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

	// Use shared CSV parser (now using FileTableParser interface)
	parser := csvParser.NewParser(opts)

	// Parse TableInfo to get schema and row count
	tableInfo, err := parser.ParseTableInfo(ctx, object, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV table info: %w", err)
	}

	// Extract column names from TableInfo
	columns := make([]string, len(tableInfo.Fields))
	for i, field := range tableInfo.Fields {
		columns[i] = field.Name
	}

	// Get total count from TableInfo
	totalCount := int64(0)
	if tableInfo.RowCount != nil {
		totalCount = *tableInfo.RowCount
	}

	// Calculate pagination
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	offset := req.Page * pageSize

	// Reset stream for data reading
	object, err = minioClient.GetObject(ctx, bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// Read paginated records using new FileTableParser.ReadPreview
	records, err := parser.ReadPreview(ctx, object, int64(offset), int64(pageSize), opts)
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
