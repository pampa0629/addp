package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// ImportService 数据导入服务
// 职责：接收上传文件 → 存储到 MinIO temp 前缀 → 调用 Transfer API 创建并触发任务
type ImportService struct {
	minioClient    *minio.Client
	minioBucket    string
	minioEndpoint  string
	minioAccessKey string
	minioSecretKey string
	transferClient *client.TransferClient
}

// NewImportService 创建导入服务
func NewImportService(
	minioClient *minio.Client,
	minioBucket string,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	transferClient *client.TransferClient,
) *ImportService {
	return &ImportService{
		minioClient:    minioClient,
		minioBucket:    minioBucket,
		minioEndpoint:  minioEndpoint,
		minioAccessKey: minioAccessKey,
		minioSecretKey: minioSecretKey,
		transferClient: transferClient,
	}
}

// ImportShapefileRequest 导入 Shapefile 请求
type ImportShapefileRequest struct {
	FileContent     []byte // 文件内容（.zip 或 .shp）
	FileName        string // 原始文件名
	TargetEngineID  uint   // 目标数据库引擎 ID
	TargetSchema    string // 目标 schema（默认 public）
	TargetTable     string // 目标表名（可选，默认使用文件名）
	Encoding        string // DBF 编码（默认 UTF-8）
	TenantID        uint   // 租户 ID
}

// ImportResult 导入结果
type ImportResult struct {
	UploadUUID          string `json:"upload_uuid"`
	TransferTaskID      uint   `json:"transfer_task_id"`
	TransferExecutionID uint   `json:"transfer_execution_id"`
	Status              string `json:"status"`
}

// ImportShapefile 导入 Shapefile 文件
func (s *ImportService) ImportShapefile(ctx context.Context, req *ImportShapefileRequest) (*ImportResult, error) {
	log := logger.L()

	// 1. 自动推断目标表名（使用文件名去掉扩展名）
	tableName := req.TargetTable
	if tableName == "" {
		tableName = inferTableName(req.FileName)
	}
	if tableName == "" {
		return nil, fmt.Errorf("cannot infer table name from filename: %s", req.FileName)
	}

	// 2. 生成上传 UUID（用于 MinIO 路径）
	uploadUUID := uuid.New().String()
	prefix := fmt.Sprintf("temp/%s/", uploadUUID)

	// 3. 提取文件并上传到 MinIO
	log.Info("开始上传 Shapefile 到 MinIO", "upload_uuid", uploadUUID, "filename", req.FileName)

	ext := strings.ToLower(filepath.Ext(req.FileName))
	var files map[string][]byte

	switch ext {
	case ".zip":
		// 解压 ZIP 包
		var err error
		files, err = extractShapefileZip(req.FileContent)
		if err != nil {
			return nil, fmt.Errorf("failed to extract shapefile zip: %w", err)
		}
	case ".shp":
		// 单个 .shp 文件（不支持，需要 ZIP 包）
		return nil, fmt.Errorf("please upload a ZIP package containing .shp/.dbf/.shx files")
	default:
		return nil, fmt.Errorf("unsupported file format: %s (supported: .zip)", ext)
	}

	// 4. 上传所有文件到 MinIO
	for filename, content := range files {
		objectKey := prefix + filename
		_, err := s.minioClient.PutObject(ctx, s.minioBucket, objectKey,
			bytes.NewReader(content), int64(len(content)),
			minio.PutObjectOptions{ContentType: "application/octet-stream"},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to upload %s to MinIO: %w", filename, err)
		}
		log.Info("文件上传成功", "object_key", objectKey, "size", len(content))
	}

	// 5. 构建 MinIO 端点（Transfer 需要完整 URL）
	minioScheme := "http"
	minioEndpointURL := fmt.Sprintf("%s://%s", minioScheme, s.minioEndpoint)

	// 6. 创建 Transfer 任务
	taskName := fmt.Sprintf("import_%s_%s", tableName, time.Now().Format("20060102_150405"))

	targetSchema := req.TargetSchema
	if targetSchema == "" {
		targetSchema = "public"
	}

	sourceConfig := map[string]interface{}{
		"type":       "s3_shapefile",
		"endpoint":   minioEndpointURL,
		"access_key": s.minioAccessKey,
		"secret_key": s.minioSecretKey,
		"bucket":     s.minioBucket,
		"prefix":     prefix,
	}
	if req.Encoding != "" {
		sourceConfig["encoding"] = req.Encoding
	}

	autoScanMetadata := true
	createReq := &client.CreateTransferTaskRequest{
		Name:     taskName,
		TaskType: "import",
		Config: map[string]interface{}{
			"source": sourceConfig,
			"target": map[string]interface{}{
				"scope":     "system",
				"engine_id": req.TargetEngineID,
				"schema":    targetSchema,
				"table":     tableName,
			},
		},
		AutoScanMetadata: &autoScanMetadata,
		BatchSize:        1000,
		TenantID:         req.TenantID,
	}

	log.Info("创建 Transfer 任务", "task_name", taskName, "engine_id", req.TargetEngineID, "table", tableName)

	taskResp, err := s.transferClient.CreateTask(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create transfer task: %w", err)
	}

	log.Info("Transfer 任务创建成功", "task_id", taskResp.ID)

	// 7. 触发任务执行
	triggerResp, err := s.transferClient.TriggerTask(taskResp.ID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger transfer task %d: %w", taskResp.ID, err)
	}

	log.Info("Transfer 任务已触发", "task_id", taskResp.ID, "execution_id", triggerResp.ExecutionID)

	return &ImportResult{
		UploadUUID:          uploadUUID,
		TransferTaskID:      taskResp.ID,
		TransferExecutionID: triggerResp.ExecutionID,
		Status:              "pending",
	}, nil
}

// extractShapefileZip 从 ZIP 包中解压 Shapefile 相关文件
// 支持文件在根目录或一层子目录中的 ZIP 包
func extractShapefileZip(zipData []byte) (map[string][]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	// 从 ZIP 中提取所有 Shapefile 相关文件
	shapefileExts := map[string]bool{
		".shp": true,
		".dbf": true,
		".shx": true,
		".prj": true,
		".cpg": true, // 编码文件
		".qpj": true, // 投影信息
	}

	files := make(map[string][]byte)
	requiredExts := map[string]bool{".shp": false, ".dbf": false, ".shx": false}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if !shapefileExts[ext] {
			continue
		}

		// 使用文件名（不含路径），以扁平化结构
		baseName := filepath.Base(f.Name)

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", f.Name, err)
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", f.Name, err)
		}

		files[baseName] = content

		// 标记必需文件是否存在
		if _, ok := requiredExts[ext]; ok {
			requiredExts[ext] = true
		}
	}

	// 校验必需文件是否都存在
	for ext, found := range requiredExts {
		if !found {
			return nil, fmt.Errorf("missing required shapefile component: *%s", ext)
		}
	}

	return files, nil
}

// inferTableName 从文件名推断表名
// 例如: "railway_2024.zip" → "railway_2024"
func inferTableName(filename string) string {
	base := filepath.Base(filename)
	// 去掉扩展名
	name := strings.TrimSuffix(base, filepath.Ext(base))
	// 转换为小写，替换非字母数字字符为下划线
	var result strings.Builder
	for i, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			result.WriteRune(ch)
		} else if ch >= 'A' && ch <= 'Z' {
			result.WriteRune(ch + 32) // 转小写
		} else if i > 0 {
			result.WriteRune('_')
		}
	}
	// 清理连续下划线
	r := result.String()
	for strings.Contains(r, "__") {
		r = strings.ReplaceAll(r, "__", "_")
	}
	return strings.Trim(r, "_")
}
