package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type MetadataService struct {
	metadataRepo   *repository.MetadataRepository
	systemClient   *commonClient.SystemClient
	metaClient     *commonClient.MetaClient
	previews       *PreviewRegistry
	content        *ObjectContentRegistry
	metaServiceURL string
	httpClient     *http.Client
}

var ErrEngineAccessDenied = errors.New("engine not accessible for current tenant")

func NewMetadataService(metadataRepo *repository.MetadataRepository, systemClient *commonClient.SystemClient, metaClient *commonClient.MetaClient, previewRegistry *PreviewRegistry, contentRegistry *ObjectContentRegistry, metaServiceURL string) *MetadataService {
	pr := previewRegistry
	if pr == nil {
		pr = NewPreviewRegistry()
	}
	cr := contentRegistry
	if cr == nil {
		cr = NewObjectContentRegistry()
	}
	client := &http.Client{
		Timeout: 120 * time.Second,
	}
	return &MetadataService{
		metadataRepo:   metadataRepo,
		systemClient:   systemClient,
		metaClient:     metaClient,
		previews:       pr,
		content:        cr,
		metaServiceURL: strings.TrimRight(metaServiceURL, "/"),
		httpClient:     client,
	}
}

// PreviewRegistry 返回预览插件注册表
func (s *MetadataService) PreviewRegistry() *PreviewRegistry {
	return s.previews
}

// ContentRegistry 返回内容处理器注册表
func (s *MetadataService) ContentRegistry() *ObjectContentRegistry {
	return s.content
}

func (s *MetadataService) callMeta(ctx context.Context, method, path string, query url.Values, payload interface{}, authHeader string) ([]byte, error) {
	if strings.TrimSpace(s.metaServiceURL) == "" {
		return nil, fmt.Errorf("meta service url not configured")
	}
	if strings.TrimSpace(authHeader) == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	endpoint := s.metaServiceURL + path
	if len(query) > 0 {
		endpoint = endpoint + "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to build meta service request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call meta service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read meta service response: %w", err)
	}

	if resp.StatusCode >= http.StatusMultipleChoices {
		text := strings.TrimSpace(string(respBody))
		if text == "" {
			text = resp.Status
		}
		return nil, fmt.Errorf("meta service returned status %d: %s", resp.StatusCode, text)
	}

	return respBody, nil
}

func (s *MetadataService) ListScanTasks(ctx context.Context, engineID uint, authHeader string) ([]models.MetaScanTask, error) {
	body, err := s.callMeta(ctx, http.MethodGet, "/api/meta/scan/tasks", nil, nil, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []models.MetaScanTask `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse scan tasks: %w", err)
		}
	}

	tasks := make([]models.MetaScanTask, 0, len(resp.Data))
	for _, task := range resp.Data {
		if task.EngineID == engineID {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (s *MetadataService) CreateScanTask(ctx context.Context, engineID uint, req *models.MetaScanTaskRequest, authHeader string) (*models.MetaScanTask, error) {
	if req == nil {
		return nil, errors.New("scan task request cannot be nil")
	}
	payload := *req
	payload.EngineID = engineID

	body, err := s.callMeta(ctx, http.MethodPost, "/api/meta/scan/tasks", nil, payload, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data models.MetaScanTask `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse scan task response: %w", err)
		}
	}

	return &resp.Data, nil
}

func (s *MetadataService) UpdateScanTask(ctx context.Context, engineID, taskID uint, req *models.MetaScanTaskRequest, authHeader string) (*models.MetaScanTask, error) {
	if req == nil {
		return nil, errors.New("scan task request cannot be nil")
	}
	payload := *req
	payload.EngineID = engineID

	path := fmt.Sprintf("/api/meta/scan/tasks/%d", taskID)
	body, err := s.callMeta(ctx, http.MethodPut, path, nil, payload, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data models.MetaScanTask `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse scan task response: %w", err)
		}
	}

	return &resp.Data, nil
}

func (s *MetadataService) DeleteScanTask(ctx context.Context, taskID uint, authHeader string) error {
	path := fmt.Sprintf("/api/meta/scan/tasks/%d", taskID)
	_, err := s.callMeta(ctx, http.MethodDelete, path, nil, nil, authHeader)
	return err
}

func (s *MetadataService) TriggerScanTask(ctx context.Context, taskID uint, authHeader string) (*models.MetaScanTaskRun, error) {
	path := fmt.Sprintf("/api/meta/scan/tasks/%d/trigger", taskID)
	body, err := s.callMeta(ctx, http.MethodPost, path, nil, nil, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data models.MetaScanTaskRun `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse task trigger response: %w", err)
		}
	}

	return &resp.Data, nil
}

func (s *MetadataService) ListScanRuns(ctx context.Context, engineID uint, taskID *uint, status, storageType string, limit, offset int, authHeader string) ([]models.MetaScanTaskRun, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	if taskID != nil && *taskID > 0 {
		query.Set("task_id", strconv.Itoa(int(*taskID)))
	}
	if trimmed := strings.TrimSpace(status); trimmed != "" {
		query.Set("status", trimmed)
	}
	if trimmedStorage := strings.TrimSpace(storageType); trimmedStorage != "" {
		query.Set("storage_type", trimmedStorage)
	}

	body, err := s.callMeta(ctx, http.MethodGet, "/api/meta/scan/runs", query, nil, authHeader)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Data  []models.MetaScanTaskRun `json:"data"`
		Total int64                    `json:"total"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, 0, fmt.Errorf("failed to parse scan runs: %w", err)
		}
	}

	runs := make([]models.MetaScanTaskRun, 0, len(resp.Data))
	for _, run := range resp.Data {
		if run.EngineID == engineID {
			runs = append(runs, run)
		}
	}

	return runs, int64(len(runs)), nil
}

func (s *MetadataService) GetScanRun(ctx context.Context, engineID, runID uint, authHeader string) (*models.MetaScanTaskRun, error) {
	path := fmt.Sprintf("/api/meta/scan/runs/%d", runID)
	body, err := s.callMeta(ctx, http.MethodGet, path, nil, nil, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data models.MetaScanTaskRun `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse scan run: %w", err)
		}
	}

	if resp.Data.ID == 0 {
		return nil, fmt.Errorf("scan run not found")
	}
	if resp.Data.EngineID != engineID {
		return nil, fmt.Errorf("scan run not found for resource")
	}

	return &resp.Data, nil
}

func (s *MetadataService) CreateManualScanRun(ctx context.Context, engineID uint, req *models.MetaManualScanRequest, authHeader string) (*models.MetaScanTaskRun, error) {
	payload := map[string]interface{}{
		"engine_id": engineID,
	}

	if req != nil {
		if len(req.Namespaces) > 0 {
			payload["namespaces"] = req.Namespaces
		}
		if len(req.ObjectPaths) > 0 {
			payload["object_paths"] = req.ObjectPaths
		}
		if depth := strings.TrimSpace(req.ScanDepth); depth != "" {
			payload["scan_depth"] = depth
		}
		if scanType := strings.TrimSpace(req.ScanType); scanType != "" {
			payload["scan_type"] = scanType
		}
	}

	if _, ok := payload["scan_depth"]; !ok {
		payload["scan_depth"] = "deep"
	}
	if _, ok := payload["scan_type"]; !ok {
		payload["scan_type"] = "manual"
	}

	body, err := s.callMeta(ctx, http.MethodPost, "/api/meta/scan/run/manual", nil, payload, authHeader)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data models.MetaScanTaskRun `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse manual run response: %w", err)
		}
	}

	return &resp.Data, nil
}

func resourceAccessible(resource *models.Engine, tenantID *uint) bool {
	if resource == nil || !resource.IsActive {
		return false
	}
	if tenantID == nil {
		return true
	}
	if resource.TenantID == nil {
		return false
	}
	return *resource.TenantID == *tenantID
}

// getResource 通过 System 服务获取解密后的资源信息
func (s *MetadataService) getResource(engineID uint) (*models.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	sysResource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	return convertResource(sysResource), nil
}

func (s *MetadataService) getResourceForTenant(engineID uint, tenantID *uint) (*models.Engine, error) {
	resource, err := s.getResource(engineID)
	if err != nil {
		return nil, err
	}
	if !resourceAccessible(resource, tenantID) {
		return nil, ErrEngineAccessDenied
	}
	return resource, nil
}

func convertResource(src *commonModels.Engine) *models.Engine {
	if src == nil {
		return nil
	}

	var tenantIDPtr *uint
	if src.TenantID != nil && *src.TenantID != 0 {
		tenantID := *src.TenantID
		tenantIDPtr = &tenantID
	}

	connInfo := make(models.ConnectionInfo, len(src.ConnectionInfo))
	for k, v := range src.ConnectionInfo {
		connInfo[k] = v
	}

	return &models.Engine{
		ID:             src.ID,
		Name:           src.Name,
		EngineType:     src.EngineType,
		ConnectionInfo: connInfo,
		Description:    src.Description,
		CreatedBy:      src.CreatedBy,
		TenantID:       tenantIDPtr,
		IsActive:       src.IsActive,
	}
}

// StreamVideo 视频流式传输
// 支持HTTP Range请求，用于视频播放器的seek功能
func (s *MetadataService) StreamVideo(
	ctx context.Context,
	resourceID uint,
	objectKey string,
	rangeHeader string,
	tenantID *uint,
) (io.ReadCloser, int64, string, string, error) {
	// 获取resource信息
	resource, err := s.getResourceForTenant(resourceID, tenantID)
	if err != nil {
		return nil, 0, "", "", ErrEngineAccessDenied
	}

	// 检查是否为对象存储类型
	resourceType := strings.ToLower(resource.EngineType)
	if resourceType != "minio" && resourceType != "s3" && resourceType != "oss" {
		return nil, 0, "", "", fmt.Errorf("resource type %s does not support video streaming", resource.EngineType)
	}

	pl, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	metadataProvider, _ := pl.(plugin.ItemMetadataProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	rangeReader, _ := pl.(plugin.RangeReadableProvider)
	if contentReader == nil {
		return nil, 0, "", "", fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	// 解析objectKey（格式：bucket/path/to/file.mp4）
	parts := strings.SplitN(objectKey, "/", 2)
	if len(parts) != 2 {
		return nil, 0, "", "", fmt.Errorf("invalid object key format: %s", objectKey)
	}
	bucket := parts[0]
	objectPath := parts[1]
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	// 获取对象信息
	meta, err := getObjectPreviewMetadata(ctx, metadataProvider, connInfo, resource.ID, bucket, objectPath)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to stat object: %w", err)
	}

	// 推断Content-Type
	contentType := meta.ContentType
	if contentType == "" {
		// 根据扩展名推断
		ext := strings.ToLower(filepath.Ext(objectPath))
		switch ext {
		case ".mp4":
			contentType = "video/mp4"
		case ".avi":
			contentType = "video/x-msvideo"
		case ".mkv":
			contentType = "video/x-matroska"
		case ".mov":
			contentType = "video/quicktime"
		case ".webm":
			contentType = "video/webm"
		case ".flv":
			contentType = "video/x-flv"
		case ".wmv":
			contentType = "video/x-ms-wmv"
		default:
			contentType = "application/octet-stream"
		}
	}

	var contentLength int64
	var contentRange string
	var readOptions plugin.ReadOptions

	if rangeHeader != "" {
		// 解析Range header (格式: "bytes=start-end")
		rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
		rangeParts := strings.Split(rangeHeader, "-")

		if len(rangeParts) == 2 {
			start, err := strconv.ParseInt(rangeParts[0], 10, 64)
			if err != nil {
				start = 0
			}

			var end int64
			if rangeParts[1] != "" {
				end, err = strconv.ParseInt(rangeParts[1], 10, 64)
				if err != nil {
					end = meta.Size - 1
				}
			} else {
				end = meta.Size - 1
			}

			// 确保范围有效
			if start < 0 {
				start = 0
			}
			if end >= meta.Size {
				end = meta.Size - 1
			}
			if start > end {
				return nil, 0, "", "", fmt.Errorf("invalid range: start > end")
			}

			contentLength = end - start + 1
			contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, meta.Size)
			readOptions = plugin.ReadOptions{Offset: start, Length: contentLength}
		}
	} else {
		// 没有Range，返回完整内容
		contentLength = meta.Size
		contentRange = ""
	}

	// 获取对象流
	var reader io.ReadCloser
	if readOptions.Length > 0 && rangeReader != nil {
		reader, err = rangeReader.OpenRange(ctx, connInfo, objectStorageObjectCatalogPath(resource.ID, bucket, objectPath), readOptions)
	} else {
		reader, err = contentReader.OpenContent(ctx, connInfo, objectStorageObjectCatalogPath(resource.ID, bucket, objectPath), readOptions)
	}
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to get object: %w", err)
	}

	return reader, contentLength, contentRange, contentType, nil
}
