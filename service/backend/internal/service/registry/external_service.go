package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type ExternalServiceService struct {
	repo       *repository.ExternalServiceRepository
	httpClient *http.Client
}

func NewExternalServiceService(repo *repository.ExternalServiceRepository) *ExternalServiceService {
	return &ExternalServiceService{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateService 创建外部服务
func (s *ExternalServiceService) CreateService(ctx context.Context, req *models.CreateExternalServiceRequest, tenantID, userID uint) (*models.ExternalService, error) {
	// 创建服务实体
	service := &models.ExternalService{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		ServiceType: req.ServiceType,
		URL:         strings.TrimRight(req.URL, "/"), // 去除尾部斜杠
		AuthType:    req.AuthType,
		AuthConfig:  req.AuthConfig,
		Status:      "active",
		CreatedBy:   userID,
	}

	if req.HealthCheckURL != "" {
		service.HealthCheck = models.HealthCheckURL(req.HealthCheckURL)
	}

	// 如果是 OGC 服务，自动获取元数据
	if isOGCService(req.ServiceType) {
		metadata, layers, err := s.fetchOGCMetadata(ctx, service)
		if err != nil {
			// 元数据获取失败不阻止创建，记录错误状态
			service.Status = "error"
			service.Metadata = models.JSONB{"error": err.Error()}
		} else {
			service.Metadata = metadata
			service.Layers = layers
		}
	}

	// 保存到数据库
	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("failed to create external service: %w", err)
	}

	return service, nil
}

// GetService 获取外部服务详情
func (s *ExternalServiceService) GetService(ctx context.Context, serviceID, tenantID uint) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}
	return service, nil
}

// ListServices 列出外部服务
func (s *ExternalServiceService) ListServices(ctx context.Context, tenantID uint, serviceType string, page, pageSize int) ([]models.ExternalService, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	return s.repo.List(tenantID, serviceType, page, pageSize)
}

// UpdateService 更新外部服务
func (s *ExternalServiceService) UpdateService(ctx context.Context, serviceID, tenantID uint, req *models.UpdateExternalServiceRequest) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// 更新字段
	if req.Name != nil {
		service.Name = *req.Name
	}
	if req.Description != nil {
		service.Description = *req.Description
	}
	if req.URL != nil {
		service.URL = strings.TrimRight(*req.URL, "/")
	}
	if req.AuthType != nil {
		service.AuthType = *req.AuthType
	}
	if req.AuthConfig != nil {
		service.AuthConfig = *req.AuthConfig
	}
	if req.Status != nil {
		service.Status = *req.Status
	}
	if req.HealthCheckURL != nil {
		service.HealthCheck = models.HealthCheckURL(*req.HealthCheckURL)
	}

	if err := s.repo.Update(service); err != nil {
		return nil, fmt.Errorf("failed to update service: %w", err)
	}

	return service, nil
}

// DeleteService 删除外部服务
func (s *ExternalServiceService) DeleteService(ctx context.Context, serviceID, tenantID uint) error {
	// 验证服务属于该租户
	if _, err := s.repo.GetByTenantAndID(tenantID, serviceID); err != nil {
		return fmt.Errorf("service not found: %w", err)
	}

	return s.repo.Delete(serviceID)
}

// RefreshMetadata 刷新服务元数据
func (s *ExternalServiceService) RefreshMetadata(ctx context.Context, serviceID, tenantID uint) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	if !isOGCService(service.ServiceType) {
		return nil, fmt.Errorf("metadata refresh only supported for OGC services")
	}

	// 获取最新元数据
	metadata, layers, err := s.fetchOGCMetadata(ctx, service)
	if err != nil {
		// 更新错误状态
		_ = s.repo.UpdateHealthCheck(serviceID, "error")
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	// 更新元数据
	if err := s.repo.UpdateMetadata(serviceID, metadata); err != nil {
		return nil, fmt.Errorf("failed to update metadata: %w", err)
	}

	// 更新图层（删除旧的，创建新的）
	if err := s.repo.DeleteLayersByServiceID(serviceID); err != nil {
		return nil, fmt.Errorf("failed to delete old layers: %w", err)
	}

	if len(layers) > 0 {
		for i := range layers {
			layers[i].ServiceID = serviceID
		}
		if err := s.repo.BulkCreateLayers(layers); err != nil {
			return nil, fmt.Errorf("failed to create layers: %w", err)
		}
	}

	// 更新健康状态
	_ = s.repo.UpdateHealthCheck(serviceID, "active")

	// 返回更新后的服务
	return s.repo.GetByID(serviceID)
}

// ProxyRequest 代理请求到外部服务
func (s *ExternalServiceService) ProxyRequest(ctx context.Context, service *models.ExternalService, path string, query string) ([]byte, string, int, error) {
	// 构建目标 URL
	targetURL := fmt.Sprintf("%s%s", service.URL, path)
	if query != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, query)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	if err := s.addAuthHeaders(req, service); err != nil {
		return nil, "", 0, fmt.Errorf("failed to add auth headers: %w", err)
	}

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return body, contentType, resp.StatusCode, nil
}

// fetchOGCMetadata 获取 OGC 服务元数据
func (s *ExternalServiceService) fetchOGCMetadata(ctx context.Context, service *models.ExternalService) (models.JSONB, []models.ServiceLayer, error) {
	var capabilitiesURL string

	switch service.ServiceType {
	case "wms":
		capabilitiesURL = fmt.Sprintf("%s?service=WMS&request=GetCapabilities&version=1.3.0", service.URL)
	case "wfs":
		capabilitiesURL = fmt.Sprintf("%s?service=WFS&request=GetCapabilities&version=2.0.0", service.URL)
	case "wmts":
		capabilitiesURL = fmt.Sprintf("%s?service=WMTS&request=GetCapabilities&version=1.0.0", service.URL)
	case "ogc_api":
		capabilitiesURL = fmt.Sprintf("%s/", service.URL)
	default:
		return nil, nil, fmt.Errorf("unsupported service type: %s", service.ServiceType)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", capabilitiesURL, nil)
	if err != nil {
		return nil, nil, err
	}

	// 添加认证
	if err := s.addAuthHeaders(req, service); err != nil {
		return nil, nil, err
	}

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("capabilities request failed with status: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// 解析元数据（简化版，实际需要根据不同服务类型解析 XML/JSON）
	metadata := models.JSONB{
		"capabilities_url": capabilitiesURL,
		"raw_response":     string(body[:min(1000, len(body))]), // 保存前 1000 字符作为预览
		"fetched_at":       time.Now().Format(time.RFC3339),
	}

	// 返回空图层列表（实际需要解析 Capabilities 文档提取图层）
	layers := []models.ServiceLayer{}

	// TODO: 实现完整的 Capabilities 解析逻辑
	// - WMS: 解析 <Layer> 元素
	// - WFS: 解析 <FeatureType> 元素
	// - WMTS: 解析 <Layer> 元素
	// - OGC API: 解析 /collections 端点

	return metadata, layers, nil
}

// addAuthHeaders 添加认证头
func (s *ExternalServiceService) addAuthHeaders(req *http.Request, service *models.ExternalService) error {
	switch service.AuthType {
	case "basic":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for basic auth")
		}
		username, _ := service.AuthConfig["username"].(string)
		password, _ := service.AuthConfig["password"].(string)
		req.SetBasicAuth(username, password)

	case "bearer":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for bearer auth")
		}
		token, _ := service.AuthConfig["token"].(string)
		req.Header.Set("Authorization", "Bearer "+token)

	case "api_key":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for api_key auth")
		}
		headerName, _ := service.AuthConfig["header"].(string)
		apiKey, _ := service.AuthConfig["api_key"].(string)
		if headerName == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, apiKey)

	case "none", "":
		// 无需认证
	default:
		return fmt.Errorf("unsupported auth type: %s", service.AuthType)
	}

	return nil
}

// GetServiceCatalog 获取服务目录（按类型分类）
func (s *ExternalServiceService) GetServiceCatalog(ctx context.Context, tenantID uint) (map[string][]models.ExternalService, error) {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return nil, err
	}

	catalog := make(map[string][]models.ExternalService)
	for _, service := range services {
		catalog[service.ServiceType] = append(catalog[service.ServiceType], service)
	}

	return catalog, nil
}

// SearchServices 搜索服务
func (s *ExternalServiceService) SearchServices(ctx context.Context, tenantID uint, keyword string, page, pageSize int) ([]models.ExternalService, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	return s.repo.SearchServices(tenantID, keyword, page, pageSize)
}

// ToDTO 转换为 DTO
func (s *ExternalServiceService) ToDTO(service *models.ExternalService) *models.ExternalServiceDTO {
	dto := &models.ExternalServiceDTO{
		ID:             service.ID,
		TenantID:       service.TenantID,
		Name:           service.Name,
		Description:    service.Description,
		ServiceType:    service.ServiceType,
		URL:            service.URL,
		Metadata:       service.Metadata,
		AuthType:       service.AuthType,
		Status:         service.Status,
		HealthCheckURL: string(service.HealthCheck),
		LastCheckedAt:  service.LastChecked,
		CreatedBy:      service.CreatedBy,
		CreatedAt:      service.CreatedAt,
		UpdatedAt:      service.UpdatedAt,
	}

	// 转换图层
	if len(service.Layers) > 0 {
		dto.Layers = make([]models.ServiceLayerDTO, len(service.Layers))
		for i, layer := range service.Layers {
			dto.Layers[i] = models.ServiceLayerDTO{
				ID:           layer.ID,
				ServiceID:    layer.ServiceID,
				LayerName:    layer.LayerName,
				DisplayName:  layer.DisplayName,
				GeometryType: layer.GeometryType,
				CRS:          layer.CRS,
				BBox:         layer.BBox,
				Metadata:     layer.Metadata,
				Enabled:      layer.Enabled,
				CreatedAt:    layer.CreatedAt,
			}
		}
	}

	return dto
}

// isOGCService 判断是否为 OGC 服务
func isOGCService(serviceType string) bool {
	return serviceType == "wms" || serviceType == "wfs" || serviceType == "wmts" || serviceType == "ogc_api"
}

// min 返回两个整数的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// HealthCheck 执行健康检查
func (s *ExternalServiceService) HealthCheck(ctx context.Context, serviceID, tenantID uint) error {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return fmt.Errorf("service not found: %w", err)
	}

	checkURL := string(service.HealthCheck)
	if checkURL == "" {
		checkURL = service.URL
	}

	req, err := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	if err != nil {
		return err
	}

	if err := s.addAuthHeaders(req, service); err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		_ = s.repo.UpdateHealthCheck(serviceID, "error")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = s.repo.UpdateHealthCheck(serviceID, "active")
		return nil
	}

	_ = s.repo.UpdateHealthCheck(serviceID, "error")
	return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
}

// BatchHealthCheck 批量健康检查
func (s *ExternalServiceService) BatchHealthCheck(ctx context.Context, tenantID uint) error {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return err
	}

	for _, service := range services {
		// 异步执行健康检查（实际应该用 goroutine + 并发控制）
		_ = s.HealthCheck(ctx, service.ID, tenantID)
	}

	return nil
}

// ExportServiceConfig 导出服务配置（用于备份）
func (s *ExternalServiceService) ExportServiceConfig(ctx context.Context, tenantID uint) ([]byte, error) {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return nil, err
	}

	// 移除敏感信息
	for i := range services {
		services[i].AuthConfig = nil // 不导出认证配置
	}

	return json.MarshalIndent(services, "", "  ")
}
