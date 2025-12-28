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
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
)

type MetadataService struct {
	metadataRepo   *repository.MetadataRepository
	resourceRepo   *repository.ResourceRepository
	systemClient   *commonClient.SystemClient
	metaClient     *commonClient.MetaClient
	previews       *PreviewRegistry
	content        *ObjectContentRegistry
	metaServiceURL string
	httpClient     *http.Client
}

var ErrResourceAccessDenied = errors.New("resource not accessible for current tenant")

type ExplorerNodeRefreshRequest struct {
	NodeType     string `json:"node_type"`
	Schema       string `json:"schema"`
	Path         string `json:"path"`
	FullPath     string `json:"full_path"`
	FullName     string `json:"full_name"`
	Table        string `json:"table"`
	ResourceType string `json:"resource_type"`
}

func NewMetadataService(metadataRepo *repository.MetadataRepository, resourceRepo *repository.ResourceRepository, systemClient *commonClient.SystemClient, metaClient *commonClient.MetaClient, previewRegistry *PreviewRegistry, contentRegistry *ObjectContentRegistry, metaServiceURL string) *MetadataService {
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
		resourceRepo:   resourceRepo,
		systemClient:   systemClient,
		metaClient:     metaClient,
		previews:       pr,
		content:        cr,
		metaServiceURL: strings.TrimRight(metaServiceURL, "/"),
		httpClient:     client,
	}
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

// ScanResource 扫描资源的元数据（轻量级）
func (s *MetadataService) ScanResource(engineID uint) (*models.MetadataScanResult, error) {
	// 获取资源信息（优先从 System 服务获取解密后的连接信息）
	resource, err := s.getResource(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	var result models.MetadataScanResult

	switch resource.EngineType {
	case "postgresql":
		// 扫描数据库表
		tables, err := s.metadataRepo.ScanDatabaseTables(engineID, resource.ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to scan database tables: %w", err)
		}

		// 保存或更新表元数据
		if err := s.metadataRepo.SaveOrUpdateTables(tables); err != nil {
			return nil, fmt.Errorf("failed to save table metadata: %w", err)
		}

		// 获取更新后的列表
		allTables, err := s.metadataRepo.GetManagedTables(engineID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get tables: %w", err)
		}

		result.TotalItems = len(allTables)

		managedCount := 0
		items := make([]interface{}, len(allTables))
		for i, table := range allTables {
			if table.IsManaged {
				managedCount++
			}
			items[i] = table
		}

		result.ManagedItems = managedCount
		result.UnmanagedItems = result.TotalItems - managedCount
		result.Items = items

	case "minio":
		// TODO: 对象存储扫描逻辑
		return nil, fmt.Errorf("minio scanning not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource.EngineType)
	}

	return &result, nil
}

// RefreshExplorerNode 触发 Meta 服务对指定节点进行重新扫描
func (s *MetadataService) RefreshExplorerNode(ctx context.Context, engineID uint, tenantID *uint, req *ExplorerNodeRefreshRequest, authHeader string) error {
	if req == nil {
		return errors.New("refresh request payload is required")
	}
	if s.metaServiceURL == "" {
		return fmt.Errorf("meta service url not configured")
	}
	if strings.TrimSpace(authHeader) == "" {
		return fmt.Errorf("missing authorization header")
	}

	resource, err := s.getResourceForTenant(engineID, tenantID)
	if err != nil {
		return err
	}

	nodeType := strings.ToLower(strings.TrimSpace(req.NodeType))
	if nodeType == "" {
		nodeType = "resource"
	}

	resourceType := strings.ToLower(resource.EngineType)

	// 点击目录时使用shallow扫描（只列文件名，不做元数据提取）
	// 点击文件时使用deep扫描（提取详细元数据）
	scanDepth := "shallow"
	if req.NodeType == "object" {
		scanDepth = "deep"
	}

	payload := map[string]interface{}{
		"engine_id": engineID,
		"scan_depth":  scanDepth,
		"scan_type":   "manual",
	}

	var (
		schemaNames []string
		objectPaths []string
	)

	if isObjectStorageType(resourceType) {
		bucketPath := normalizeObjectPathCandidate(req.Schema)
		switch nodeType {
		case "resource":
			if bucketPath != "" {
				objectPaths = append(objectPaths, bucketPath)
			} else {
				return fmt.Errorf("missing bucket for object storage resource refresh")
			}
		case "bucket":
			if bucketPath == "" {
				return fmt.Errorf("missing bucket path for node type %s", nodeType)
			}
			objectPaths = append(objectPaths, bucketPath)
		default:
			targetPath := req.normalizedObjectPath()
			if targetPath == "" {
				return fmt.Errorf("missing object path for node type %s", nodeType)
			}
			objectPaths = append(objectPaths, targetPath)
		}
	} else {
		schema := strings.TrimSpace(req.Schema)
		if nodeType != "resource" {
			if schema == "" {
				return fmt.Errorf("missing schema for node type %s", nodeType)
			}
			schemaNames = append(schemaNames, schema)
		}
	}

	if len(schemaNames) > 0 {
		payload["schema_names"] = schemaNames
	}
	if len(objectPaths) > 0 {
		payload["object_paths"] = objectPaths
	}

	logger.L().Info("数据探查: 触发节点刷新",
		"engine_id", engineID,
		"resource_type", resource.EngineType,
		"node_type", nodeType,
		"schema", req.Schema,
		"path", req.Path,
		"full_path", req.FullPath,
		"target_schemas", schemaNames,
		"target_paths", objectPaths,
	)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode scan payload: %w", err)
	}

	endpoint := s.metaServiceURL + "/api/meta/scan/resource"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build meta scan request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", authHeader)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to call meta scan service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		text := strings.TrimSpace(string(msg))
		if text == "" {
			text = resp.Status
		}
		return fmt.Errorf("meta scan service returned status %d: %s", resp.StatusCode, text)
	}

	logger.L().Info("数据探查: 节点刷新完成",
		"engine_id", engineID,
		"node_type", nodeType,
	)

	return nil
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
		if len(req.SchemaNames) > 0 {
			payload["schema_names"] = req.SchemaNames
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

// GetTables 获取资源的表列表
func (s *MetadataService) GetTables(engineID uint, isManaged *bool) ([]models.ManagedTable, error) {
	return s.metadataRepo.GetManagedTables(engineID, isManaged)
}

// ManageTable 纳管表（提取详细元数据）
func (s *MetadataService) ManageTable(tableID uint) error {
	// 获取表信息  - 直接通过GetByID获取
	table, err := s.metadataRepo.GetManagedTableByID(tableID)
	if err != nil {
		return fmt.Errorf("table not found: %w", err)
	}

	// 获取资源连接信息
	resource, err := s.getResource(table.EngineID)
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// 标记为已纳管并提取详细元数据
	return s.metadataRepo.MarkTableAsManaged(tableID, resource.ConnectionInfo)
}

// UnmanageTable 取消纳管表
func (s *MetadataService) UnmanageTable(tableID uint) error {
	return s.metadataRepo.UnmarkTableAsManaged(tableID)
}

// ListExplorerResources 返回可用于数据探查的存储引擎列表
func (s *MetadataService) ListExplorerEngines(tenantID *uint) ([]models.ExplorerResource, error) {
	engines, err := s.listActiveEngines(tenantID)
	if err != nil {
		return nil, err
	}

	result := make([]models.ExplorerResource, 0, len(engines))
	for _, res := range engines {
		result = append(result, models.ExplorerResource{
			ID:           res.ID,
			Name:         res.Name,
			EngineType: res.EngineType,
			Description:  res.Description,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// GetResourceTree 获取单个资源的 Schema/目录树
func (s *MetadataService) GetResourceTree(ctx context.Context, engineID uint, tenantID *uint) (*models.DataExplorerResource, error) {
	resource, err := s.getResourceForTenant(engineID, tenantID)
	if err != nil {
		return nil, err
	}

	return s.buildResourceTree(ctx, resource)
}

// GetLegacyResourceTree 兼容旧接口的全量资源树
func (s *MetadataService) GetLegacyResourceTree(ctx context.Context, tenantID *uint) ([]models.DataExplorerResource, error) {
	engines, err := s.listActiveEngines(tenantID)
	if err != nil {
		return nil, err
	}

	result := make([]models.DataExplorerResource, 0, len(engines))
	for i := range engines {
		tree, err := s.buildResourceTree(ctx, &engines[i])
		if err != nil {
			return nil, err
		}
		if tree != nil {
			result = append(result, *tree)
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *MetadataService) buildResourceTree(ctx context.Context, resource *models.Resource) (*models.DataExplorerResource, error) {
	// 从 context 中提取 JWT token
	var metaClientForRequest *commonClient.MetaClient
	if jwtToken, ok := ctx.Value("jwt_token").(string); ok && jwtToken != "" {
		// 使用用户的 JWT token 创建临时 MetaClient
		metaClientForRequest = commonClient.NewMetaClient(s.metaServiceURL, jwtToken)
	} else {
		// 降级使用默认的 metaClient（使用内部 API Key）
		metaClientForRequest = s.metaClient
	}

	// 使用带 JWT token 的 MetaClient 查询元数据
	topNodes, childNodes, items, err := s.metadataRepo.ListScannedNodesAndItems(resource.ID, metaClientForRequest)
	if err != nil {
		if errors.Is(err, repository.ErrMetadataSchemaMissing) {
			logger.L().Warn("数据探查: metadata schema 尚未初始化，返回空树", "engine_id", resource.ID)
			return &models.DataExplorerResource{
				ID:           resource.ID,
				Name:         resource.Name,
				EngineType: resource.EngineType,
				Schemas:      []models.DataExplorerSchema{},
			}, nil
		}
		return nil, err
	}

	childrenByParent := make(map[uint][]*models.MetaNodeLite)
	for i := range childNodes {
		node := &childNodes[i]
		if node.ParentNodeID != nil {
			parentID := *node.ParentNodeID
			childrenByParent[parentID] = append(childrenByParent[parentID], node)
		}
	}

	itemsByNode := make(map[uint][]*models.MetaItemLite)
	for i := range items {
		item := &items[i]
		itemsByNode[item.NodeID] = append(itemsByNode[item.NodeID], item)
	}

	resourceType := strings.ToLower(resource.EngineType)
	var schemasForResource []models.DataExplorerSchema

	if isObjectStorageType(resourceType) {
		for i := range topNodes {
			bucket := &topNodes[i]
			children := buildObjectStorageTree(bucket, childrenByParent, itemsByNode)
			if len(children) == 0 {
				continue
			}
			schemasForResource = append(schemasForResource, models.DataExplorerSchema{
				Name:   bucket.Name,
				Tables: children,
			})
		}
	} else {
		for i := range topNodes {
			schemaNode := &topNodes[i]
			if strings.ToLower(schemaNode.NodeType) != "schema" {
				continue
			}
			itemList := itemsByNode[schemaNode.ID]
			if len(itemList) == 0 {
				continue
			}

			tables := make([]models.DataExplorerTable, 0, len(itemList))
			for _, item := range itemList {
				if strings.ToLower(item.ItemType) != "table" {
					continue
				}
				fullName := item.FullName
				if fullName == "" {
					fullName = fmt.Sprintf("%s.%s", schemaNode.Name, item.Name)
				}
				tables = append(tables, models.DataExplorerTable{
					ID:       item.ID,
					Name:     item.Name,
					FullName: fullName,
					Type:     "table",
				})
			}
			if len(tables) == 0 {
				continue
			}
			sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
			schemasForResource = append(schemasForResource, models.DataExplorerSchema{
				Name:   schemaNode.Name,
				Tables: tables,
			})
		}
	}

	return &models.DataExplorerResource{
		ID:           resource.ID,
		Name:         resource.Name,
		EngineType: resource.EngineType,
		Schemas:      schemasForResource,
	}, nil
}

func buildObjectStorageTree(node *models.MetaNodeLite, childNodes map[uint][]*models.MetaNodeLite, items map[uint][]*models.MetaItemLite) []models.DataExplorerTable {
	children := childNodes[node.ID]

	var entries []models.DataExplorerTable

	for _, child := range children {
		entry := models.DataExplorerTable{
			ID:          child.ID,
			Name:        child.Name,
			FullName:    child.FullName,
			Type:        "directory",
			Depth:       child.Depth,
			SizeBytes:   child.TotalSizeBytes,
			ObjectCount: int64(child.ItemCount),
		}
		entry.Children = buildObjectStorageTree(child, childNodes, items)
		entries = append(entries, entry)
	}

	for _, item := range items[node.ID] {
		if strings.ToLower(item.ItemType) != "object" {
			continue
		}
		size := int64(0)
		if item.ObjectSizeBytes != nil {
			size = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			size = *item.SizeBytes
		}
		fullName := item.FullName
		if fullName == "" {
			if v, ok := item.Attributes["relative_path"].(string); ok && v != "" {
				fullName = v
			}
		}
		entry := models.DataExplorerTable{
			ID:        item.ID,
			Name:      item.Name,
			FullName:  fullName,
			Type:      "object",
			SizeBytes: size,
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type == entries[j].Type {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Type == "directory"
	})

	return entries
}

func (r *ExplorerNodeRefreshRequest) normalizedObjectPath() string {
	if r == nil {
		return ""
	}

	candidates := make([]string, 0, 6)
	for _, raw := range []string{r.FullPath, r.FullName} {
		if cleaned := normalizeObjectPathCandidate(raw); cleaned != "" {
			candidates = append(candidates, cleaned)
		}
	}

	bucket := normalizeObjectPathCandidate(r.Schema)
	for _, raw := range []string{r.Path, r.Table} {
		cleaned := normalizeObjectPathCandidate(raw)
		if cleaned == "" {
			continue
		}
		if bucket != "" {
			if strings.HasPrefix(cleaned, bucket+"/") || cleaned == bucket {
				candidates = append(candidates, cleaned)
			} else {
				candidates = append(candidates, bucket+"/"+cleaned)
			}
		} else {
			candidates = append(candidates, cleaned)
		}
	}

	if bucket != "" {
		candidates = append(candidates, bucket)
	}

	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func normalizeObjectPathCandidate(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	trimmed = strings.Trim(trimmed, "/")
	return trimmed
}

func (s *MetadataService) listActiveEngines(tenantID *uint) ([]models.Resource, error) {
	var tenantFilter uint
	if tenantID != nil {
		tenantFilter = *tenantID
	}

	if s.systemClient != nil {
		sysResources, err := s.systemClient.ListEngines("", tenantFilter)
		if err != nil {
			logger.L().Warn("数据探查: System API 获取资源列表失败，回退数据库查询", "error", err)
		} else {
			engines := make([]models.Resource, 0, len(sysResources))
			for i := range sysResources {
				res := sysResources[i]
				if !res.IsActive {
					continue
				}
				converted := convertResource(&res)
				if converted == nil {
					continue
				}
				if !resourceAccessible(converted, tenantID) {
					continue
				}
				engines = append(engines, *converted)
			}
			logger.L().Info("数据探查: System API 获取引擎列表成功", "resource_total", len(engines))
			return engines, nil
		}
	}

	engines, err := s.resourceRepo.ListAllActive(tenantID)
	if err != nil {
		logger.L().Error("数据探查: 数据库获取引擎列表失败", "error", err)
		return nil, err
	}
	logger.L().Info("数据探查: 数据库获取引擎列表成功", "resource_total", len(engines))
	return engines, nil
}

// PreviewTable 获取表数据预览
// 当 tableName 为空时，返回 schema/bucket 的统计信息和子节点列表
func (s *MetadataService) PreviewTable(engineID uint, schemaName, tableName string, page, pageSize int, tenantID *uint) (*models.TablePreview, error) {
	return s.PreviewTableWithContext(context.Background(), engineID, schemaName, tableName, page, pageSize, tenantID)
}

func (s *MetadataService) PreviewTableWithContext(ctx context.Context, engineID uint, schemaName, tableName string, page, pageSize int, tenantID *uint) (*models.TablePreview, error) {
	resource, err := s.getResourceForTenant(engineID, tenantID)
	if err != nil {
		return nil, err
	}

	if s.previews == nil {
		return nil, fmt.Errorf("preview registry not initialized")
	}

	req := &PreviewRequest{
		Resource: resource,
		Schema:   schemaName,
		Table:    tableName,
		Page:     page,
		PageSize: pageSize,
		TenantID: tenantID,
	}

	provider, err := s.previews.Resolve(req)
	if err != nil {
		return nil, fmt.Errorf("no preview plugin available: %w", err)
	}

	result, err := provider.Preview(ctx, req)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func resourceAccessible(resource *models.Resource, tenantID *uint) bool {
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

// getResource 优先通过 System 服务获取解密后的资源信息，失败时回退到本地数据库
func (s *MetadataService) getResource(engineID uint) (*models.Resource, error) {
	if s.systemClient != nil {
		if sysResource, err := s.systemClient.GetEngine(engineID); err == nil {
			return convertResource(sysResource), nil
		}
	}
	return s.resourceRepo.GetByID(engineID)
}

func (s *MetadataService) getResourceForTenant(engineID uint, tenantID *uint) (*models.Resource, error) {
	resource, err := s.getResource(engineID)
	if err != nil {
		return nil, err
	}
	if !resourceAccessible(resource, tenantID) {
		return nil, ErrResourceAccessDenied
	}
	return resource, nil
}

func convertResource(src *commonModels.Engine) *models.Resource {
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

	return &models.Resource{
		ID:             src.ID,
		Name:           src.Name,
		EngineType:   src.EngineType,
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
	resource, err := s.getResourceForTenant(engineID, tenantID)
	if err != nil {
		return nil, 0, "", "", ErrResourceAccessDenied
	}

	// 检查是否为对象存储类型
	resourceType := strings.ToLower(resource.EngineType)
	if resourceType != "minio" && resourceType != "s3" && resourceType != "oss" {
		return nil, 0, "", "", fmt.Errorf("resource type %s does not support video streaming", resource.EngineType)
	}

	// 创建MinIO client
	cfg, err := buildObjectStorageConfig(resource.ConnectionInfo)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to build storage config: %w", err)
	}
	client, err := newMinioClient(cfg)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to create minio client: %w", err)
	}

	// 解析objectKey（格式：bucket/path/to/file.mp4）
	parts := strings.SplitN(objectKey, "/", 2)
	if len(parts) != 2 {
		return nil, 0, "", "", fmt.Errorf("invalid object key format: %s", objectKey)
	}
	bucket := parts[0]
	objectPath := parts[1]

	// 获取对象信息
	objInfo, err := client.StatObject(ctx, bucket, objectPath, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to stat object: %w", err)
	}

	// 推断Content-Type
	contentType := objInfo.ContentType
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

	// 处理Range请求
	opts := minio.GetObjectOptions{}
	var contentLength int64
	var contentRange string

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
					end = objInfo.Size - 1
				}
			} else {
				end = objInfo.Size - 1
			}

			// 确保范围有效
			if start < 0 {
				start = 0
			}
			if end >= objInfo.Size {
				end = objInfo.Size - 1
			}
			if start > end {
				return nil, 0, "", "", fmt.Errorf("invalid range: start > end")
			}

			// 设置Range
			err = opts.SetRange(start, end)
			if err != nil {
				return nil, 0, "", "", fmt.Errorf("failed to set range: %w", err)
			}

			contentLength = end - start + 1
			contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, objInfo.Size)
		}
	} else {
		// 没有Range，返回完整内容
		contentLength = objInfo.Size
		contentRange = ""
	}

	// 获取对象流
	reader, err := client.GetObject(ctx, bucket, objectPath, opts)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to get object: %w", err)
	}

	return reader, contentLength, contentRange, contentType, nil
}
