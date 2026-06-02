package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
)

type RegisteredServiceService struct {
	repo    *repository.RegisteredServiceRepository
	baseURL string // 服务的基础URL（用于构建端点）
}

func NewRegisteredServiceService(repo *repository.RegisteredServiceRepository, baseURL string) *RegisteredServiceService {
	return &RegisteredServiceService{
		repo:    repo,
		baseURL: baseURL,
	}
}

// CreateService 创建新的注册服务
func (s *RegisteredServiceService) CreateService(req *models.CreateRegisteredServiceRequest, tenantID uint, createdBy uint) (*models.RegisteredServiceDTO, error) {
	// 1. 检查服务名称是否唯一
	unique, err := s.repo.CheckServiceNameUnique(req.ServiceName, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("check service name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("service name already exists")
	}

	// 2. 设置默认值
	authType := req.AuthType
	if authType == "" {
		authType = "none"
	}

	authConfig := make(map[string]interface{})
	if req.AuthConfig != nil {
		authConfig = req.AuthConfig
	}

	// 3. 创建服务模型
	service := &models.RegisteredService{
		TenantID:    tenantID,
		ServiceName: req.ServiceName,
		Title:       req.Title,
		Description: req.Description,
		Keywords:    models.StringArray(req.Keywords),

		ServiceType: req.ServiceType,
		EndpointURL: req.EndpointURL,

		Metadata: make(map[string]interface{}),

		AuthType:   authType,
		AuthConfig: authConfig,

		HealthCheckURL: req.HealthCheckURL,

		Status:    "active",
		CreatedBy: createdBy,
	}

	// 4. 保存到数据库
	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	// 5. 如果是OGC服务且需要自动刷新元数据
	if req.AutoRefreshMetadata && service.IsOGCService() {
		go func() {
			// 异步刷新元数据（不影响创建流程）
			if err := s.refreshOGCMetadata(service); err != nil {
				log.Printf("[RegisteredService] Failed to refresh metadata for service %s: %v", service.ServiceName, err)
			}
		}()
	}

	// 6. 重新加载服务（获取完整数据）
	service, err = s.repo.GetByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// GetService 获取服务详情
func (s *RegisteredServiceService) GetService(id uint) (*models.RegisteredServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// GetServiceByName 根据名称获取服务
func (s *RegisteredServiceService) GetServiceByName(serviceName string, tenantID uint) (*models.RegisteredServiceDTO, error) {
	service, err := s.repo.GetByNameAndTenant(serviceName, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// ListServices 列出租户下的所有注册服务
func (s *RegisteredServiceService) ListServices(tenantID uint, offset int, limit int) ([]models.RegisteredServiceDTO, int64, error) {
	services, total, err := s.repo.List(tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list services failed: %w", err)
	}

	dtos := make([]models.RegisteredServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// SearchServices 搜索服务
func (s *RegisteredServiceService) SearchServices(tenantID uint, keyword string, offset int, limit int) ([]models.RegisteredServiceDTO, int64, error) {
	services, total, err := s.repo.Search(tenantID, keyword, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search services failed: %w", err)
	}

	dtos := make([]models.RegisteredServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// UpdateService 更新服务
func (s *RegisteredServiceService) UpdateService(id uint, req *models.UpdateRegisteredServiceRequest) (*models.RegisteredServiceDTO, error) {
	// 获取现有服务
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	// 构建更新字段
	updates := make(map[string]interface{})

	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Keywords != nil {
		updates["keywords"] = models.StringArray(req.Keywords)
	}
	if req.AuthType != nil {
		updates["auth_type"] = *req.AuthType
	}
	if req.AuthConfig != nil {
		updates["auth_config"] = req.AuthConfig
	}
	if req.HealthCheckURL != nil {
		updates["health_check_url"] = *req.HealthCheckURL
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	// 执行更新
	if err := s.repo.Update(id, updates); err != nil {
		return nil, fmt.Errorf("update service failed: %w", err)
	}

	// 重新加载服务
	service, err = s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get updated service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// DeleteService 删除服务
func (s *RegisteredServiceService) DeleteService(id uint) error {
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}
	return nil
}

// RefreshMetadata 刷新服务元数据
func (s *RegisteredServiceService) RefreshMetadata(id uint, force bool) error {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return fmt.Errorf("get service failed: %w", err)
	}

	// 检查是否支持元数据刷新
	if !service.IsOGCService() {
		return errors.New("metadata refresh is only supported for OGC services")
	}

	// 刷新元数据
	if err := s.refreshOGCMetadata(service); err != nil {
		// 更新服务状态为错误
		_ = s.repo.UpdateStatus(id, "error", err.Error())
		return fmt.Errorf("refresh metadata failed: %w", err)
	}

	// 更新服务状态为活跃
	return s.repo.UpdateStatus(id, "active", "")
}

// HealthCheck 健康检查
func (s *RegisteredServiceService) HealthCheck(id uint) (*models.HealthCheckResult, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	// 执行健康检查
	result := s.performHealthCheck(service)

	// 更新最后检查时间
	_ = s.repo.UpdateHealthCheck(id)

	// 如果检查失败，更新服务状态
	if result.Status == "unhealthy" || result.Status == "error" {
		_ = s.repo.UpdateStatus(id, "error", result.Message)
	} else {
		_ = s.repo.UpdateStatus(id, "active", "")
	}

	return result, nil
}

// performHealthCheck 执行健康检查
func (s *RegisteredServiceService) performHealthCheck(service *models.RegisteredService) *models.HealthCheckResult {
	result := &models.HealthCheckResult{
		ServiceID:   service.ID,
		ServiceName: service.ServiceName,
		CheckedAt:   time.Now(),
	}

	// 确定检查URL
	checkURL := service.HealthCheckURL
	if checkURL == "" {
		// 对于 OGC 服务，使用 GetCapabilities 作为健康检查
		switch service.ServiceType {
		case "wms":
			checkURL = service.EndpointURL
			if checkURL[len(checkURL)-1] != '?' && checkURL[len(checkURL)-1] != '&' {
				checkURL += "?"
			}
			checkURL += "SERVICE=WMS&REQUEST=GetCapabilities&VERSION=1.3.0"
		case "wfs":
			checkURL = service.EndpointURL
			if checkURL[len(checkURL)-1] != '?' && checkURL[len(checkURL)-1] != '&' {
				checkURL += "?"
			}
			checkURL += "SERVICE=WFS&REQUEST=GetCapabilities"
		case "wmts":
			checkURL = service.EndpointURL
			if checkURL[len(checkURL)-1] != '?' && checkURL[len(checkURL)-1] != '&' {
				checkURL += "?"
			}
			checkURL += "SERVICE=WMTS&REQUEST=GetCapabilities"
		default:
			// REST 或其他类型，直接使用 EndpointURL
			checkURL = service.EndpointURL
		}
	}

	// 发送HTTP请求
	startTime := time.Now()
	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("Failed to create request: %v", err)
		return result
	}

	// 添加认证
	s.addAuthToRequest(req, service)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	result.ResponseTime = time.Since(startTime).Milliseconds()

	if err != nil {
		result.Status = "unhealthy"
		result.Message = fmt.Sprintf("Request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = "healthy"
		result.Message = "Service is healthy"
	} else {
		result.Status = "unhealthy"
		result.Message = fmt.Sprintf("Unexpected status code: %d", resp.StatusCode)
	}

	return result
}

// ProxyRequest 代理请求到外部服务
func (s *RegisteredServiceService) ProxyRequest(service *models.RegisteredService, path string, queryParams map[string][]string, headers map[string][]string) (*http.Response, error) {
	// 构建目标URL
	targetURL := service.EndpointURL + path

	// 创建请求
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加查询参数
	q := req.URL.Query()
	for k, v := range queryParams {
		for _, val := range v {
			q.Add(k, val)
		}
	}
	req.URL.RawQuery = q.Encode()

	// 添加请求头
	for k, v := range headers {
		for _, val := range v {
			req.Header.Add(k, val)
		}
	}

	// 添加认证
	s.addAuthToRequest(req, service)

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}

	return resp, nil
}

// addAuthToRequest 为请求添加认证信息
func (s *RegisteredServiceService) addAuthToRequest(req *http.Request, service *models.RegisteredService) {
	if !service.RequiresAuth() {
		return
	}

	switch service.AuthType {
	case "basic":
		// Basic Auth
		if username, ok := service.AuthConfig["username"].(string); ok {
			if password, ok := service.AuthConfig["password"].(string); ok {
				auth := username + ":" + password
				basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
				req.Header.Set("Authorization", basicAuth)
			}
		}

	case "bearer":
		// Bearer Token
		if token, ok := service.AuthConfig["token"].(string); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}

	case "api_key":
		// API Key（可以在 header 或 query parameter 中）
		if key, ok := service.AuthConfig["key"].(string); ok {
			if location, ok := service.AuthConfig["location"].(string); ok {
				if name, ok := service.AuthConfig["name"].(string); ok {
					if location == "header" {
						req.Header.Set(name, key)
					} else if location == "query" {
						q := req.URL.Query()
						q.Add(name, key)
						req.URL.RawQuery = q.Encode()
					}
				}
			}
		}
	}
}

// refreshOGCMetadata 刷新OGC服务元数据
func (s *RegisteredServiceService) refreshOGCMetadata(service *models.RegisteredService) error {
	log.Printf("[RegisteredService] Refreshing metadata for service %s (type: %s)", service.ServiceName, service.ServiceType)

	switch service.ServiceType {
	case "wms":
		return s.refreshWMSMetadata(service)
	case "wfs":
		return s.refreshWFSMetadata(service)
	case "wmts":
		return s.refreshWMTSMetadata(service)
	case "ogc_api":
		return s.refreshOGCAPIMetadata(service)
	default:
		return errors.New("unsupported service type for metadata refresh")
	}
}

// refreshWMSMetadata 刷新WMS服务元数据
func (s *RegisteredServiceService) refreshWMSMetadata(service *models.RegisteredService) error {
	// 构建 GetCapabilities URL
	capabilitiesURL := service.EndpointURL
	if capabilitiesURL[len(capabilitiesURL)-1] != '?' && capabilitiesURL[len(capabilitiesURL)-1] != '&' {
		capabilitiesURL += "?"
	}
	capabilitiesURL += "service=WMS&request=GetCapabilities"

	// 发送请求
	req, err := http.NewRequest("GET", capabilitiesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuthToRequest(req, service)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析 WMS Capabilities XML 并提取图层信息
	bodyStr := string(body)

	// 存储元数据
	metadata := map[string]interface{}{
		"capabilities_xml": bodyStr,
		"refreshed_at":     time.Now(),
	}

	// 提取服务标题和摘要
	if title := extractXMLValue(bodyStr, "Title"); title != "" {
		metadata["service_title"] = title
	}
	if abstract := extractXMLValue(bodyStr, "Abstract"); abstract != "" {
		metadata["service_abstract"] = abstract
	}

	// 更新元数据
	if err := s.repo.UpdateMetadata(service.ID, metadata); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	// 提取并存储图层信息
	layers := extractWMSLayers(bodyStr)
	log.Printf("[RegisteredService] WMS: extracted %d layers from service %s", len(layers), service.ServiceName)

	// 删除旧图层
	if err := s.repo.DeleteLayersByServiceID(service.ID); err != nil {
		return fmt.Errorf("failed to delete old layers: %w", err)
	}

	// 创建新图层
	for _, layer := range layers {
		layerModel := &models.RegisteredServiceLayer{
			ServiceID:    service.ID,
			LayerName:    layer.Name,
			DisplayName:  layer.Title,
			Description:  layer.Abstract,
			GeometryType: "Unknown", // WMS 不直接提供几何类型信息
			CRS:          layer.CRS,
			BBox:         layer.BBox,
			Metadata: map[string]interface{}{
				"queryable": layer.Queryable,
				"styles":    layer.Styles,
			},
			Enabled: true,
		}

		if err := s.repo.CreateLayer(layerModel); err != nil {
			log.Printf("Failed to create layer %s: %v", layer.Name, err)
			continue
		}
	}

	log.Printf("[RegisteredService] WMS metadata refreshed for service %s, found %d layers", service.ServiceName, len(layers))
	return nil
}

// refreshWFSMetadata 刷新WFS服务元数据
func (s *RegisteredServiceService) refreshWFSMetadata(service *models.RegisteredService) error {
	// 构建 GetCapabilities URL
	capabilitiesURL := service.EndpointURL
	if capabilitiesURL[len(capabilitiesURL)-1] != '?' && capabilitiesURL[len(capabilitiesURL)-1] != '&' {
		capabilitiesURL += "?"
	}
	capabilitiesURL += "service=WFS&request=GetCapabilities"

	// 发送请求
	req, err := http.NewRequest("GET", capabilitiesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuthToRequest(req, service)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch WFS capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析 WFS Capabilities XML
	// 简化实现：使用简单的正则表达式提取基本信息
	// 生产环境应使用完整的 XML 解析器
	bodyStr := string(body)

	// 提取服务元数据
	metadata := map[string]interface{}{
		"capabilities_xml": bodyStr,
		"refreshed_at":     time.Now(),
	}

	// 尝试从 XML 中提取服务信息
	if title := extractXMLValue(bodyStr, "Title"); title != "" {
		metadata["service_title"] = title
	}
	if abstract := extractXMLValue(bodyStr, "Abstract"); abstract != "" {
		metadata["service_abstract"] = abstract
	}

	// 更新服务元数据
	if err := s.repo.UpdateMetadata(service.ID, metadata); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	// 提取 FeatureType 列表并创建/更新图层
	featureTypes := extractWFSFeatureTypes(bodyStr)
	for _, ft := range featureTypes {
		// 检查图层是否已存在
		existingLayers, _ := s.repo.GetLayersByServiceID(service.ID)
		exists := false
		var existingLayerID uint
		for _, l := range existingLayers {
			if l.LayerName == ft.Name {
				exists = true
				existingLayerID = l.ID
				break
			}
		}

		if exists {
			// 更新现有图层
			updates := map[string]interface{}{
				"display_name": ft.Title,
				"description":  ft.Abstract,
				"crs":          ft.DefaultCRS,
				"bbox":         ft.BBox,
				"enabled":      true,
			}
			if err := s.repo.UpdateLayer(existingLayerID, updates); err != nil {
				log.Printf("[RegisteredService] Failed to update WFS layer %s: %v", ft.Name, err)
			}
		} else {
			// 创建新图层
			layerData := &models.RegisteredServiceLayer{
				ServiceID:    service.ID,
				LayerName:    ft.Name,
				DisplayName:  ft.Title,
				Description:  ft.Abstract,
				CRS:          ft.DefaultCRS,
				GeometryType: "",
				BBox:         ft.BBox,
				Metadata:     map[string]interface{}{},
				Enabled:      true,
			}
			if err := s.repo.CreateLayer(layerData); err != nil {
				log.Printf("[RegisteredService] Failed to create WFS layer %s: %v", ft.Name, err)
			}
		}
	}

	log.Printf("[RegisteredService] WFS metadata refreshed for service %s, found %d feature types", service.ServiceName, len(featureTypes))
	return nil
}

// refreshWMTSMetadata 刷新WMTS服务元数据
func (s *RegisteredServiceService) refreshWMTSMetadata(service *models.RegisteredService) error {
	// 构建 GetCapabilities URL
	capabilitiesURL := service.EndpointURL
	if capabilitiesURL[len(capabilitiesURL)-1] != '?' && capabilitiesURL[len(capabilitiesURL)-1] != '&' {
		capabilitiesURL += "?"
	}
	capabilitiesURL += "service=WMTS&request=GetCapabilities"

	// 发送请求
	req, err := http.NewRequest("GET", capabilitiesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuthToRequest(req, service)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch WMTS capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析 WMTS Capabilities XML
	bodyStr := string(body)

	// 提取服务元数据
	metadata := map[string]interface{}{
		"capabilities_xml": bodyStr,
		"refreshed_at":     time.Now(),
	}

	// 尝试从 XML 中提取服务信息
	if title := extractXMLValue(bodyStr, "Title"); title != "" {
		metadata["service_title"] = title
	}
	if abstract := extractXMLValue(bodyStr, "Abstract"); abstract != "" {
		metadata["service_abstract"] = abstract
	}

	// 更新服务元数据
	if err := s.repo.UpdateMetadata(service.ID, metadata); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	// 提取 Layer 列表并创建/更新图层
	layers := extractWMTSLayers(bodyStr)
	for _, layer := range layers {
		// 检查图层是否已存在
		existingLayers, _ := s.repo.GetLayersByServiceID(service.ID)
		exists := false
		var existingLayerID uint
		for _, l := range existingLayers {
			if l.LayerName == layer.Identifier {
				exists = true
				existingLayerID = l.ID
				break
			}
		}

		if exists {
			// 更新现有图层
			updates := map[string]interface{}{
				"display_name": layer.Title,
				"description":  layer.Abstract,
				"bbox":         layer.BBox,
				"enabled":      true,
				"metadata": map[string]interface{}{
					"tile_matrix_set": layer.TileMatrixSet,
				},
			}
			if err := s.repo.UpdateLayer(existingLayerID, updates); err != nil {
				log.Printf("[RegisteredService] Failed to update WMTS layer %s: %v", layer.Identifier, err)
			}
		} else {
			// 创建新图层
			layerData := &models.RegisteredServiceLayer{
				ServiceID:    service.ID,
				LayerName:    layer.Identifier,
				DisplayName:  layer.Title,
				Description:  layer.Abstract,
				CRS:          "", // WMTS 使用 TileMatrixSet 而不是 CRS
				GeometryType: "",
				BBox:         layer.BBox,
				Metadata: map[string]interface{}{
					"tile_matrix_set": layer.TileMatrixSet,
				},
				Enabled: true,
			}
			if err := s.repo.CreateLayer(layerData); err != nil {
				log.Printf("[RegisteredService] Failed to create WMTS layer %s: %v", layer.Identifier, err)
			}
		}
	}

	log.Printf("[RegisteredService] WMTS metadata refreshed for service %s, found %d layers", service.ServiceName, len(layers))
	return nil
}

// refreshOGCAPIMetadata 刷新OGC API服务元数据
func (s *RegisteredServiceService) refreshOGCAPIMetadata(service *models.RegisteredService) error {
	// OGC API Features: 访问 /collections 端点
	collectionsURL := service.EndpointURL
	if collectionsURL[len(collectionsURL)-1] != '/' {
		collectionsURL += "/"
	}
	collectionsURL += "collections"

	req, err := http.NewRequest("GET", collectionsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuthToRequest(req, service)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch collections: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 解析JSON响应
	var collectionsResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&collectionsResp); err != nil {
		return fmt.Errorf("failed to parse collections response: %w", err)
	}

	// 更新元数据
	metadata := map[string]interface{}{
		"collections":  collectionsResp,
		"refreshed_at": time.Now(),
	}

	if err := s.repo.UpdateMetadata(service.ID, metadata); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Printf("[RegisteredService] OGC API metadata refreshed for service %s", service.ServiceName)
	return nil
}

// convertToDTO 将服务模型转换为 DTO
func (s *RegisteredServiceService) convertToDTO(service *models.RegisteredService) *models.RegisteredServiceDTO {
	dto := &models.RegisteredServiceDTO{
		ID: service.ID,

		TenantID:    service.TenantID,
		ServiceName: service.ServiceName,
		Title:       service.Title,
		Description: service.Description,
		Keywords:    []string(service.Keywords),

		ServiceType: service.ServiceType,
		EndpointURL: service.EndpointURL,

		Metadata: service.Metadata,

		AuthType:       service.AuthType,
		HasAuthConfig:  len(service.AuthConfig) > 0,
		HealthCheckURL: service.HealthCheckURL,
		LastCheckedAt:  service.LastCheckedAt,

		Status:       service.Status,
		ErrorMessage: service.ErrorMessage,

		CreatedBy: service.CreatedBy,
		CreatedAt: service.CreatedAt,
		UpdatedAt: service.UpdatedAt,
	}

	// 转换图层
	dto.Layers = make([]models.RegisteredServiceLayerDTO, len(service.Layers))
	for i, layer := range service.Layers {
		dto.Layers[i] = models.RegisteredServiceLayerDTO{
			ID:           layer.ID,
			ServiceID:    layer.ServiceID,
			LayerName:    layer.LayerName,
			DisplayName:  layer.DisplayName,
			Description:  layer.Description,
			GeometryType: layer.GeometryType,
			CRS:          layer.CRS,
			BBox:         layer.BBox,
			Metadata:     layer.Metadata,
			Enabled:      layer.Enabled,
			CreatedAt:    layer.CreatedAt,
			UpdatedAt:    layer.UpdatedAt,
		}
	}

	// 构建服务端点
	dto.Endpoints = s.buildEndpoints(service)

	return dto
}

// buildEndpoints 构建服务端点URL
func (s *RegisteredServiceService) buildEndpoints(service *models.RegisteredService) map[string]string {
	endpoints := make(map[string]string)

	// 代理端点
	endpoints["proxy"] = fmt.Sprintf("%s/api/service/registered/proxy/%d", s.baseURL, service.ID)

	// 原始端点
	endpoints["original"] = service.EndpointURL

	return endpoints
}

// ProxyServiceRequest 代理服务请求到外部服务
// 完整实现：支持流式传输、认证、审计日志
func (s *RegisteredServiceService) ProxyServiceRequest(serviceID uint, tenantID uint, userID uint, c *gin.Context) error {
	// 1. 获取服务配置（包含认证信息）
	service, err := s.repo.GetByID(serviceID)
	if err != nil {
		return fmt.Errorf("failed to get service: %w", err)
	}

	// 2. 获取请求路径
	path := c.Param("path")

	// 3. 构建目标 URL
	targetURL := service.EndpointURL
	if path != "" {
		targetURL = targetURL + path
	}

	// 4. 创建代理请求
	proxyReq, err := http.NewRequest(c.Request.Method, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	// 5. 复制请求体（如果有）
	if c.Request.Body != nil {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		proxyReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		proxyReq.ContentLength = int64(len(bodyBytes))
	}

	// 6. 复制查询参数
	proxyReq.URL.RawQuery = c.Request.URL.RawQuery

	// 7. 复制客户端请求头（过滤掉某些敏感头）
	for key, values := range c.Request.Header {
		// 跳过某些不应该传递的头
		if key == "Authorization" || key == "Host" || key == "Cookie" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// 8. 添加外部服务的认证信息
	s.addAuthToRequest(proxyReq, service)

	// 9. 发送请求到外部服务
	startTime := time.Now()
	client := &http.Client{
		Timeout: 60 * time.Second, // 60秒超时
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		// 记录错误日志
		log.Printf("[RegisteredService] Proxy request failed for service %d: %v", serviceID, err)
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	responseTime := time.Since(startTime).Milliseconds()

	// 10. 记录审计日志（异步）
	go func() {
		s.logProxyRequest(serviceID, tenantID, userID, c.Request.Method, targetURL, resp.StatusCode, responseTime)
	}()

	// 11. 复制响应头到客户端
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// 12. 设置响应状态码
	c.Status(resp.StatusCode)

	// 13. 流式传输响应体到客户端（支持大文件）
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Printf("[RegisteredService] Failed to copy response body for service %d: %v", serviceID, err)
		return fmt.Errorf("failed to stream response: %w", err)
	}

	log.Printf("[RegisteredService] Proxy request completed for service %d: %s %s -> %d (%dms)",
		serviceID, c.Request.Method, targetURL, resp.StatusCode, responseTime)

	return nil
}

// logProxyRequest 记录代理请求的审计日志
func (s *RegisteredServiceService) logProxyRequest(serviceID uint, tenantID uint, userID uint, method string, url string, statusCode int, responseTime int64) {
	// TODO: 调用 System 服务的审计日志 API
	// 目前只记录到应用日志
	log.Printf("[Audit] Service=%d Tenant=%d User=%d Method=%s URL=%s Status=%d Time=%dms",
		serviceID, tenantID, userID, method, url, statusCode, responseTime)
}

// ===== XML 解析辅助函数 =====

// extractXMLValue 从 XML 字符串中提取指定标签的值（简化实现）
func extractXMLValue(xml string, tagName string) string {
	// 使用正则表达式提取标签值
	pattern := fmt.Sprintf(`<%s[^>]*>([^<]+)</%s>`, tagName, tagName)
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(xml)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// WFSFeatureType WFS 要素类型信息
type WFSFeatureType struct {
	Name       string
	Title      string
	Abstract   string
	DefaultCRS string
	BBox       map[string]interface{}
}

// extractWFSFeatureTypes 从 WFS Capabilities XML 中提取 FeatureType 列表
func extractWFSFeatureTypes(xml string) []WFSFeatureType {
	var featureTypes []WFSFeatureType

	// 使用正则表达式提取所有 FeatureType 块
	// 这是简化实现，生产环境应使用完整的 XML 解析器
	featureTypePattern := regexp.MustCompile(`(?s)<FeatureType>(.*?)</FeatureType>`)
	matches := featureTypePattern.FindAllStringSubmatch(xml, -1)

	for _, match := range matches {
		if len(match) > 1 {
			ftXML := match[1]
			ft := WFSFeatureType{
				Name:       extractXMLValue(ftXML, "Name"),
				Title:      extractXMLValue(ftXML, "Title"),
				Abstract:   extractXMLValue(ftXML, "Abstract"),
				DefaultCRS: extractXMLValue(ftXML, "DefaultCRS"),
				BBox:       make(map[string]interface{}),
			}

			// 提取边界框
			if bbox := extractWFSBBox(ftXML); bbox != nil {
				ft.BBox = bbox
			}

			// 如果 Title 为空，使用 Name
			if ft.Title == "" {
				ft.Title = ft.Name
			}

			if ft.Name != "" {
				featureTypes = append(featureTypes, ft)
			}
		}
	}

	return featureTypes
}

// extractWFSBBox 从 WFS FeatureType XML 中提取边界框
func extractWFSBBox(xml string) map[string]interface{} {
	// 提取 WGS84BoundingBox 或 LatLongBoundingBox
	lowerCorner := extractXMLValue(xml, "LowerCorner")
	upperCorner := extractXMLValue(xml, "UpperCorner")

	if lowerCorner != "" && upperCorner != "" {
		// 解析坐标值
		lowerParts := strings.Fields(lowerCorner)
		upperParts := strings.Fields(upperCorner)

		if len(lowerParts) >= 2 && len(upperParts) >= 2 {
			return map[string]interface{}{
				"minx": lowerParts[0],
				"miny": lowerParts[1],
				"maxx": upperParts[0],
				"maxy": upperParts[1],
			}
		}
	}

	return nil
}

// WMTSLayer WMTS 图层信息
// WMSLayer WMS 图层信息
type WMSLayer struct {
	Name      string
	Title     string
	Abstract  string
	CRS       string
	BBox      map[string]interface{}
	Queryable bool
	Styles    []string
}

// extractWMSLayers 从 WMS Capabilities XML 中提取图层列表
func extractWMSLayers(xml string) []WMSLayer {
	var layers []WMSLayer

	// 使用正则表达式提取所有 Layer 块（跳过根 Layer）
	// WMS Capabilities 的结构是嵌套的 Layer 标签
	layerPattern := regexp.MustCompile(`(?s)<Layer[^>]*>(.*?)</Layer>`)
	matches := layerPattern.FindAllStringSubmatch(xml, -1)

	for _, match := range matches {
		if len(match) > 1 {
			layerXML := match[1]

			// 提取 Name（WMS 使用 Name 作为唯一标识符）
			name := extractXMLValue(layerXML, "Name")

			// 只处理有 Name 的图层（叶子图层）
			// 没有 Name 的是容器图层，跳过
			if name == "" {
				continue
			}

			layer := WMSLayer{
				Name:     name,
				Title:    extractXMLValue(layerXML, "Title"),
				Abstract: extractXMLValue(layerXML, "Abstract"),
				BBox:     make(map[string]interface{}),
				Styles:   []string{},
			}

			// 如果 Title 为空，使用 Name
			if layer.Title == "" {
				layer.Title = layer.Name
			}

			// 提取 queryable 属性
			queryablePattern := regexp.MustCompile(`<Layer[^>]*\s+queryable="(\d+)"`)
			if queryMatch := queryablePattern.FindStringSubmatch(match[0]); len(queryMatch) > 1 {
				layer.Queryable = queryMatch[1] == "1"
			}

			// 提取 CRS/SRS
			crsPattern := regexp.MustCompile(`<(?:CRS|SRS)>([^<]+)</(?:CRS|SRS)>`)
			if crsMatch := crsPattern.FindStringSubmatch(layerXML); len(crsMatch) > 1 {
				layer.CRS = strings.TrimSpace(crsMatch[1])
			}

			// 提取边界框（使用 EX_GeographicBoundingBox）
			if bbox := extractWMSBBox(layerXML); bbox != nil {
				layer.BBox = bbox
			}

			// 提取样式名称
			styleNamePattern := regexp.MustCompile(`<Style>.*?<Name>([^<]+)</Name>.*?</Style>`)
			styleMatches := styleNamePattern.FindAllStringSubmatch(layerXML, -1)
			for _, styleMatch := range styleMatches {
				if len(styleMatch) > 1 {
					layer.Styles = append(layer.Styles, strings.TrimSpace(styleMatch[1]))
				}
			}

			layers = append(layers, layer)
		}
	}

	log.Printf("[WMS Parser] Extracted %d layers", len(layers))
	return layers
}

// extractWMSBBox 从 WMS Layer XML 中提取边界框
func extractWMSBBox(layerXML string) map[string]interface{} {
	// 优先使用 EX_GeographicBoundingBox
	bboxPattern := regexp.MustCompile(`(?s)<EX_GeographicBoundingBox>(.*?)</EX_GeographicBoundingBox>`)
	if bboxMatch := bboxPattern.FindStringSubmatch(layerXML); len(bboxMatch) > 1 {
		bboxXML := bboxMatch[1]

		westLon := extractXMLValue(bboxXML, "westBoundLongitude")
		eastLon := extractXMLValue(bboxXML, "eastBoundLongitude")
		southLat := extractXMLValue(bboxXML, "southBoundLatitude")
		northLat := extractXMLValue(bboxXML, "northBoundLatitude")

		if westLon != "" && eastLon != "" && southLat != "" && northLat != "" {
			return map[string]interface{}{
				"west":  westLon,
				"east":  eastLon,
				"south": southLat,
				"north": northLat,
			}
		}
	}

	// 如果没有 EX_GeographicBoundingBox，尝试使用 LatLonBoundingBox
	latlonPattern := regexp.MustCompile(`<LatLonBoundingBox[^>]*minx="([^"]+)"[^>]*miny="([^"]+)"[^>]*maxx="([^"]+)"[^>]*maxy="([^"]+)"`)
	if latlonMatch := latlonPattern.FindStringSubmatch(layerXML); len(latlonMatch) > 4 {
		return map[string]interface{}{
			"west":  latlonMatch[1],
			"south": latlonMatch[2],
			"east":  latlonMatch[3],
			"north": latlonMatch[4],
		}
	}

	return nil
}

type WMTSLayer struct {
	Identifier       string
	Title            string
	Abstract         string
	BBox             map[string]interface{}
	TileMatrixSet    string
	TileMatrixSetURI string
}

// extractWMTSLayers 从 WMTS Capabilities XML 中提取图层列表
func extractWMTSLayers(xml string) []WMTSLayer {
	var layers []WMTSLayer

	// 在 Contents 部分查找 Layer 标签
	contentsPattern := regexp.MustCompile(`(?s)<Contents>(.*?)</Contents>`)
	contentsMatches := contentsPattern.FindStringSubmatch(xml)

	if len(contentsMatches) > 1 {
		contentsXML := contentsMatches[1]

		// 提取所有 Layer 块
		layerPattern := regexp.MustCompile(`(?s)<Layer>(.*?)</Layer>`)
		layerMatches := layerPattern.FindAllStringSubmatch(contentsXML, -1)

		for _, match := range layerMatches {
			if len(match) > 1 {
				layerXML := match[1]
				layer := WMTSLayer{
					Identifier: extractXMLValue(layerXML, "Identifier"),
					Title:      extractXMLValue(layerXML, "Title"),
					Abstract:   extractXMLValue(layerXML, "Abstract"),
					BBox:       make(map[string]interface{}),
				}

				// 提取 TileMatrixSet
				tmsPattern := regexp.MustCompile(`<TileMatrixSet>([^<]+)</TileMatrixSet>`)
				tmsMatches := tmsPattern.FindStringSubmatch(layerXML)
				if len(tmsMatches) > 1 {
					layer.TileMatrixSet = strings.TrimSpace(tmsMatches[1])
				}

				// 提取边界框
				if bbox := extractWMTSBBox(layerXML); bbox != nil {
					layer.BBox = bbox
				}

				// 如果 Title 为空，使用 Identifier
				if layer.Title == "" {
					layer.Title = layer.Identifier
				}

				if layer.Identifier != "" {
					layers = append(layers, layer)
				}
			}
		}
	}

	return layers
}

// extractWMTSBBox 从 WMTS Layer XML 中提取边界框
func extractWMTSBBox(xml string) map[string]interface{} {
	// 查找 BoundingBox 标签
	bboxPattern := regexp.MustCompile(`(?s)<.*?BoundingBox[^>]*>(.*?)</.*?BoundingBox>`)
	matches := bboxPattern.FindStringSubmatch(xml)

	if len(matches) > 1 {
		bboxXML := matches[1]
		lowerCorner := extractXMLValue(bboxXML, "LowerCorner")
		upperCorner := extractXMLValue(bboxXML, "UpperCorner")

		if lowerCorner != "" && upperCorner != "" {
			lowerParts := strings.Fields(lowerCorner)
			upperParts := strings.Fields(upperCorner)

			if len(lowerParts) >= 2 && len(upperParts) >= 2 {
				return map[string]interface{}{
					"minx": lowerParts[0],
					"miny": lowerParts[1],
					"maxx": upperParts[0],
					"maxy": upperParts[1],
				}
			}
		}
	}

	return nil
}
