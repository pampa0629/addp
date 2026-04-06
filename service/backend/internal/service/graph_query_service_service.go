package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type GraphQueryServiceService struct {
	repo    *repository.GraphQueryServiceRepository
	baseURL string // Gateway URL，用于构建对外端点
}

func NewGraphQueryServiceService(
	repo *repository.GraphQueryServiceRepository,
	baseURL string,
) *GraphQueryServiceService {
	return &GraphQueryServiceService{repo: repo, baseURL: baseURL}
}

// CreateService 创建图查询服务
func (s *GraphQueryServiceService) CreateService(
	req *models.CreateGraphQueryServiceRequest,
	tenantID, createdBy uint,
) (*models.GraphQueryServiceDTO, error) {
	// 1. 按配置类型校验
	switch req.ConfigType {
	case "label":
		if req.NodeLabel == "" {
			return nil, errors.New("node_label is required for label mode")
		}
	case "cypher":
		if req.CypherQuery == "" {
			return nil, errors.New("cypher_query is required for cypher mode")
		}
		if err := validateCypher(req.CypherQuery); err != nil {
			return nil, err
		}
	}

	// 2. 检查服务名称唯一性
	unique, err := s.repo.CheckServiceNameUnique(req.ServiceName, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("check service name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("service name already exists")
	}

	// 3. 数据库名默认值
	dbName := req.DatabaseName
	if dbName == "" {
		dbName = "neo4j"
	}

	// 4. 构建 data_config（参数定义也存在这里）
	dataConfig := make(map[string]interface{})
	if req.DataConfig != nil {
		dataConfig = req.DataConfig
	}

	if req.ConfigType == "cypher" {
		// 设置结果类型默认值
		if _, exists := dataConfig["result_type"]; !exists {
			dataConfig["result_type"] = "table"
		}
		// 处理参数定义
		params := req.Parameters
		if len(params) == 0 {
			// 自动从 Cypher 中提取参数
			for _, name := range extractCypherParams(req.CypherQuery) {
				params = append(params, models.ParameterDef{
					Name:     name,
					Type:     "string",
					Required: true,
				})
			}
		}
		if len(params) > 0 {
			dataConfig["parameters"] = params
		}
	}

	// 5. 最大记录数默认值
	maxRecords := req.MaxRecords
	if maxRecords == 0 {
		maxRecords = 500
	}

	service := &models.GraphQueryService{
		TenantID:     tenantID,
		ServiceName:  req.ServiceName,
		Title:        req.Title,
		Description:  req.Description,
		Keywords:     models.StringArray(req.Keywords),
		EngineID:     req.EngineID,
		DatabaseName: dbName,
		ConfigType:   req.ConfigType,
		NodeLabel:    req.NodeLabel,
		CypherQuery:  req.CypherQuery,
		DataConfig:   dataConfig,
		PublicAccess: req.PublicAccess,
		MaxRecords:   maxRecords,
		Status:       "active",
		CreatedBy:    createdBy,
	}

	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	service, err = s.repo.GetByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.toDTO(service), nil
}

// UpdateService 更新图查询服务
func (s *GraphQueryServiceService) UpdateService(
	id uint,
	req *models.UpdateGraphQueryServiceRequest,
) (*models.GraphQueryServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

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
	if req.CypherQuery != nil {
		if err := validateCypher(*req.CypherQuery); err != nil {
			return nil, err
		}
		updates["cypher_query"] = *req.CypherQuery
	}
	if req.DataConfig != nil {
		current := service.DataConfig
		if current == nil {
			current = make(map[string]interface{})
		}
		for k, v := range req.DataConfig {
			current[k] = v
		}
		updates["data_config"] = current
	}
	if req.PublicAccess != nil {
		updates["public_access"] = *req.PublicAccess
	}
	if req.MaxRecords != nil {
		updates["max_records"] = *req.MaxRecords
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if err := s.repo.Update(id, updates); err != nil {
		return nil, fmt.Errorf("update service failed: %w", err)
	}

	service, err = s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get updated service failed: %w", err)
	}

	return s.toDTO(service), nil
}

func (s *GraphQueryServiceService) DeleteService(id uint) error {
	return s.repo.Delete(id)
}

func (s *GraphQueryServiceService) GetService(id uint) (*models.GraphQueryServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return s.toDTO(service), nil
}

func (s *GraphQueryServiceService) GetServiceModelByNameOnly(serviceName string) (*models.GraphQueryService, error) {
	return s.repo.GetByName(serviceName)
}

func (s *GraphQueryServiceService) ListServices(tenantID uint, offset, limit int) ([]models.GraphQueryServiceDTO, int64, error) {
	services, total, err := s.repo.List(tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list services failed: %w", err)
	}
	dtos := make([]models.GraphQueryServiceDTO, len(services))
	for i := range services {
		dtos[i] = *s.toDTO(&services[i])
	}
	return dtos, total, nil
}

func (s *GraphQueryServiceService) SearchServices(tenantID uint, keyword string, offset, limit int) ([]models.GraphQueryServiceDTO, int64, error) {
	services, total, err := s.repo.Search(tenantID, keyword, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search services failed: %w", err)
	}
	dtos := make([]models.GraphQueryServiceDTO, len(services))
	for i := range services {
		dtos[i] = *s.toDTO(&services[i])
	}
	return dtos, total, nil
}

// toDTO 将模型转换为 DTO
func (s *GraphQueryServiceService) toDTO(service *models.GraphQueryService) *models.GraphQueryServiceDTO {
	dto := &models.GraphQueryServiceDTO{
		ID:           service.ID,
		TenantID:     service.TenantID,
		ServiceName:  service.ServiceName,
		Title:        service.Title,
		Description:  service.Description,
		Keywords:     []string(service.Keywords),
		EngineID:     service.EngineID,
		DatabaseName: service.DatabaseName,
		ConfigType:   service.ConfigType,
		NodeLabel:    service.NodeLabel,
		CypherQuery:  service.CypherQuery,
		DataConfig:   service.DataConfig,
		PublicAccess: service.PublicAccess,
		MaxRecords:   service.MaxRecords,
		Status:       service.Status,
		ErrorMessage: service.ErrorMessage,
		CreatedBy:    service.CreatedBy,
		CreatedAt:    service.CreatedAt,
		UpdatedAt:    service.UpdatedAt,
		Endpoints: map[string]string{
			"execute": fmt.Sprintf("%s/api/v1/gquery/%s", s.baseURL, service.ServiceName),
		},
	}

	// 从 data_config 中提取参数定义
	dto.Parameters = extractParametersFromConfig(service.DataConfig)

	return dto
}

// extractParametersFromConfig 从 data_config 中读取参数定义
func extractParametersFromConfig(config models.JSONB) []models.ParameterDef {
	if config == nil {
		return nil
	}
	raw, ok := config["parameters"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var params []models.ParameterDef
	if err := json.Unmarshal(b, &params); err != nil {
		return nil
	}
	return params
}

// ---- 工具函数 ----

// validateCypher 检查 Cypher 是否包含写操作关键字
var writeKeywordsRe = regexp.MustCompile(
	`(?i)\b(CREATE|MERGE|DELETE|DETACH|SET|REMOVE|DROP)\b`,
)

func validateCypher(cypher string) error {
	matches := writeKeywordsRe.FindStringSubmatch(cypher)
	if len(matches) > 0 {
		return fmt.Errorf("cypher query contains write operation '%s', only read operations are allowed", matches[1])
	}
	return nil
}

// extractCypherParams 从 Cypher 字符串中提取 $param 参数名，排除内置分页参数
var cypherParamRe = regexp.MustCompile(`\$(\w+)`)

var builtinParams = map[string]bool{
	"offset":    true,
	"limit":     true,
	"page_size": true,
	"skip":      true,
}

func extractCypherParams(cypher string) []string {
	matches := cypherParamRe.FindAllStringSubmatch(cypher, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		name := strings.ToLower(m[1])
		if !builtinParams[name] && !seen[name] {
			seen[name] = true
			result = append(result, m[1]) // 保留原始大小写
		}
	}
	return result
}
