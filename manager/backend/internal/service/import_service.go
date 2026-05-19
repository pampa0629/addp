package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

type importSystemClient interface {
	GetEngine(engineID uint) (*commonModels.Engine, error)
	ListObjectStorages(tenantID uint) ([]commonModels.Engine, error)
}

// ImportService 数据导入服务
// 职责：接收上传文件 → 存储到 MinIO temp 前缀 → 调用 Transfer API 创建并触发任务
type ImportService struct {
	minioClient          *minio.Client
	minioBucket          string
	minioEndpoint        string
	minioAccessKey       string
	minioSecretKey       string
	sourceEngineID       uint
	sourceEngineExplicit bool
	systemClient         importSystemClient
	transferClient       *client.TransferClient
}

// NewImportService 创建导入服务
func NewImportService(
	minioClient *minio.Client,
	minioBucket string,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	sourceEngineID uint,
	sourceEngineExplicit bool,
	systemClient importSystemClient,
	transferClient *client.TransferClient,
) *ImportService {
	return &ImportService{
		minioClient:          minioClient,
		minioBucket:          minioBucket,
		minioEndpoint:        minioEndpoint,
		minioAccessKey:       minioAccessKey,
		minioSecretKey:       minioSecretKey,
		sourceEngineID:       sourceEngineID,
		sourceEngineExplicit: sourceEngineExplicit,
		systemClient:         systemClient,
		transferClient:       transferClient,
	}
}

// ImportShapefileRequest 导入 Shapefile 请求
type ImportShapefileRequest struct {
	FileContent    []byte // Shapefile ZIP 包内容
	FileName       string // 原始文件名
	TargetEngineID uint   // 目标数据库引擎 ID
	TargetSchema   string // 目标 schema（默认 public）
	TargetTable    string // 目标表名（可选，默认使用文件名）
	Encoding       string // DBF 编码（默认 UTF-8）
	TenantID       uint   // 租户 ID
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

	sourceEngineID, err := s.resolveImportSourceEngine(req.TenantID)
	if err != nil {
		return nil, err
	}
	sourceObjectPath := prefix + primaryShapefileName(files)

	// 6. 创建 Transfer 任务
	taskName := fmt.Sprintf("import_%s_%s", tableName, time.Now().Format("20060102_150405"))

	targetSchema := req.TargetSchema
	if targetSchema == "" {
		targetSchema = "public"
	}

	autoScanMetadata := true
	createReq := &client.CreateTransferTaskRequest{
		Name:             taskName,
		TaskType:         "import",
		Config:           s.buildShapefileImportTaskConfig(sourceEngineID, sourceObjectPath, req, targetSchema, tableName),
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

func (s *ImportService) buildShapefileImportTaskConfig(sourceEngineID uint, sourceObjectPath string, req *ImportShapefileRequest, targetSchema string, tableName string) map[string]interface{} {
	options := map[string]interface{}{}
	if req != nil && strings.TrimSpace(req.Encoding) != "" {
		options["encoding"] = strings.TrimSpace(req.Encoding)
	}
	source := map[string]interface{}{
		"engine": map[string]interface{}{
			"scope": "system",
			"id":    sourceEngineID,
		},
		"resource": map[string]interface{}{
			"kind": "object",
			"path": map[string]interface{}{
				"bucket": s.minioBucket,
				"path":   sourceObjectPath,
			},
		},
		"data_type":      "table",
		"representation": "encoded",
		"format":         "shapefile",
	}
	if len(options) > 0 {
		source["options"] = options
	}
	return map[string]interface{}{
		"mode":   "batch",
		"source": source,
		"target": map[string]interface{}{
			"engine": map[string]interface{}{
				"scope": "system",
				"id":    req.TargetEngineID,
			},
			"resource": map[string]interface{}{
				"kind": "native_table",
				"path": map[string]interface{}{
					"schema": targetSchema,
					"table":  tableName,
				},
			},
			"data_type":      "table",
			"representation": "native",
			"policy": map[string]interface{}{
				"write_mode": "overwrite",
			},
		},
	}
}

func (s *ImportService) resolveImportSourceEngine(tenantID uint) (uint, error) {
	if s.sourceEngineExplicit {
		return s.resolveExplicitImportSourceEngine()
	}
	if s.systemClient == nil {
		return 0, fmt.Errorf("manager import source engine is not configured; set MANAGER_IMPORT_SOURCE_ENGINE_ID or enable System integration")
	}
	engines, err := s.systemClient.ListObjectStorages(tenantID)
	if err != nil {
		return 0, fmt.Errorf("list object storage engines for import source: %w", err)
	}
	matches := make([]commonModels.Engine, 0, 1)
	for _, engine := range engines {
		if !engine.IsActive {
			continue
		}
		if s.matchesConfiguredStagingObjectStore(engine) {
			matches = append(matches, engine)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		return 0, fmt.Errorf("no object storage engine matches manager import staging MinIO endpoint %q and bucket %q; set MANAGER_IMPORT_SOURCE_ENGINE_ID", s.minioEndpoint, s.minioBucket)
	default:
		sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
		ids := make([]string, 0, len(matches))
		for _, engine := range matches {
			ids = append(ids, fmt.Sprintf("%d", engine.ID))
		}
		return 0, fmt.Errorf("multiple object storage engines match manager import staging MinIO endpoint %q and bucket %q: %s; set MANAGER_IMPORT_SOURCE_ENGINE_ID", s.minioEndpoint, s.minioBucket, strings.Join(ids, ","))
	}
}

func (s *ImportService) resolveExplicitImportSourceEngine() (uint, error) {
	if s.sourceEngineID == 0 {
		return 0, fmt.Errorf("MANAGER_IMPORT_SOURCE_ENGINE_ID must be positive")
	}
	if s.systemClient == nil {
		return s.sourceEngineID, nil
	}
	engine, err := s.systemClient.GetEngine(s.sourceEngineID)
	if err != nil {
		return 0, fmt.Errorf("get manager import source engine %d: %w", s.sourceEngineID, err)
	}
	if engine == nil || !engine.IsActive {
		return 0, fmt.Errorf("manager import source engine %d is not active", s.sourceEngineID)
	}
	return engine.ID, nil
}

func (s *ImportService) matchesConfiguredStagingObjectStore(engine commonModels.Engine) bool {
	conn := engine.ConnectionInfo
	if conn == nil {
		return false
	}
	if !sameEndpoint(connectionString(conn, "endpoint"), s.minioEndpoint) {
		return false
	}
	if bucket := strings.TrimSpace(connectionString(conn, "bucket")); bucket != "" && bucket != strings.TrimSpace(s.minioBucket) {
		return false
	}
	accessKey := connectionString(conn, "access_key")
	if accessKey == "" {
		accessKey = connectionString(conn, "accessKey")
	}
	if accessKey != "" && accessKey != s.minioAccessKey {
		return false
	}
	return true
}

func connectionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func sameEndpoint(left, right string) bool {
	return normalizeEndpoint(left) == normalizeEndpoint(right)
}

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return strings.TrimRight(endpoint, "/")
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
	componentsByBase := map[string]map[string]bool{}

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

		base := strings.TrimSuffix(strings.ToLower(baseName), ext)
		if _, ok := componentsByBase[base]; !ok {
			componentsByBase[base] = map[string]bool{}
		}
		componentsByBase[base][ext] = true
	}

	if primaryShapefileName(files) == "" {
		return nil, fmt.Errorf("missing required shapefile component: *%s", ".shp")
	}
	if len(componentsByBase) > 1 {
		bases := make([]string, 0, len(componentsByBase))
		for base := range componentsByBase {
			bases = append(bases, base)
		}
		sort.Strings(bases)
		return nil, fmt.Errorf("shapefile components in one ZIP must have the same basename; found: %s", strings.Join(bases, ","))
	}
	requiredExts := []string{".shp", ".dbf", ".shx"}
	completeBases := make([]string, 0, 1)
	for base, exts := range componentsByBase {
		if hasAllExtensions(exts, requiredExts) {
			completeBases = append(completeBases, base)
		}
	}
	if len(completeBases) == 0 {
		return nil, fmt.Errorf("missing required shapefile component set: .shp/.dbf/.shx with the same basename")
	}
	return files, nil
}

func hasAllExtensions(exts map[string]bool, required []string) bool {
	for _, ext := range required {
		if !exts[ext] {
			return false
		}
	}
	return true
}

func primaryShapefileName(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.EqualFold(filepath.Ext(name), ".shp") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
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
