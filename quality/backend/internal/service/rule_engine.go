package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

// RuleEngineService 负责规则的加载和字段自动映射
type RuleEngineService struct {
	standardClient *commonClient.StandardClient
	metaClient     *commonClient.MetaClient
	ruleAppRepo    *repository.RuleApplicationRepository
	checkTaskRepo  *repository.CheckTaskRepository
}

func NewRuleEngineService(
	standardClient *commonClient.StandardClient,
	metaClient *commonClient.MetaClient,
	ruleAppRepo *repository.RuleApplicationRepository,
	checkTaskRepo *repository.CheckTaskRepository,
) *RuleEngineService {
	return &RuleEngineService{
		standardClient: standardClient,
		metaClient:     metaClient,
		ruleAppRepo:    ruleAppRepo,
		checkTaskRepo:  checkTaskRepo,
	}
}

// ListRuleApplications 列出规则应用
func (s *RuleEngineService) ListRuleApplications(tenantID, engineID int64, schemaName, tableName string) ([]models.RuleApplication, error) {
	return s.ruleAppRepo.List(tenantID, engineID, schemaName, tableName)
}

// GetRuleApplication 获取单条规则应用
func (s *RuleEngineService) GetRuleApplication(id, tenantID int64) (*models.RuleApplication, error) {
	return s.ruleAppRepo.Get(id, tenantID)
}

// CreateRuleApplication 手动创建规则应用（绑定数据元质量规则到字段）
func (s *RuleEngineService) CreateRuleApplication(tenantID, userID int64, req *CreateRuleApplicationRequest) (*models.RuleApplication, error) {
	// 从 Standard 获取数据元质量规则快照
	rules, err := s.standardClient.GetElementQualityRules(req.ElementID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get quality rules: %w", err)
	}

	ruleConfig, _ := json.Marshal(rules)

	ra := &models.RuleApplication{
		TenantID:   tenantID,
		ElementID:  req.ElementID,
		EngineID:   req.EngineID,
		SchemaName: req.SchemaName,
		Table:      req.TableName,
		ColumnName: req.ColumnName,
		RuleConfig: ruleConfig,
		Enabled:    true,
		CreatedBy:  userID,
	}
	if err := s.ruleAppRepo.Create(ra); err != nil {
		return nil, err
	}
	return ra, nil
}

// UpdateRuleApplication 更新规则应用
func (s *RuleEngineService) UpdateRuleApplication(id, tenantID int64, req *UpdateRuleApplicationRequest) (*models.RuleApplication, error) {
	ra, err := s.ruleAppRepo.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		ra.Enabled = *req.Enabled
	}
	if err := s.ruleAppRepo.Update(ra); err != nil {
		return nil, err
	}
	return ra, nil
}

// DeleteRuleApplication 删除规则应用
func (s *RuleEngineService) DeleteRuleApplication(id, tenantID int64) error {
	return s.ruleAppRepo.Delete(id, tenantID)
}

// AutoMap 自动映射：从 Meta 获取表字段，按字段名匹配 Standard 数据元 code，批量创建 rule_applications
func (s *RuleEngineService) AutoMap(ctx context.Context, tenantID, engineID int64, schemaName, tableName string, userID int64) (int, error) {
	// 获取表字段列表
	fields, err := s.metaClient.GetItemFieldsByCatalogPath(uint(engineID), fmt.Sprintf("%s.%s", schemaName, tableName), false)
	if err != nil {
		return 0, fmt.Errorf("failed to get table fields: %w", err)
	}

	created := 0
	for _, field := range fields {
		// 尝试按字段名（即数据元 code）匹配数据元
		rules, err := s.tryFindElementRules(tenantID, field.Name)
		if err != nil || rules == nil {
			continue
		}

		ruleConfig, _ := json.Marshal(rules)
		ra := &models.RuleApplication{
			TenantID:   tenantID,
			ElementID:  0, // 无法直接得到 elementID，只能存规则快照
			EngineID:   engineID,
			SchemaName: schemaName,
			Table:      tableName,
			ColumnName: field.Name,
			RuleConfig: ruleConfig,
			Enabled:    true,
			CreatedBy:  userID,
		}
		if err := s.ruleAppRepo.Create(ra); err != nil {
			log.Printf("auto-map: failed to create rule_application for column %s: %v", field.Name, err)
			continue
		}
		created++
	}
	return created, nil
}

// tryFindElementRules 尝试通过字段名（转大写）找到数据元质量规则
func (s *RuleEngineService) tryFindElementRules(tenantID int64, fieldName string) (map[string]interface{}, error) {
	// 数据元 code 通常是大写的，字段名可能是小写或带下划线
	codes := []string{
		strings.ToUpper(fieldName),
		fieldName,
	}
	for _, code := range codes {
		_ = code
		// NOTE: Standard 模块暂无按 code 搜索的接口，此处留空可扩展
		// 如需实现，可调用 GET /api/standard/elements?code=xxx
	}
	return nil, nil
}

// Request types

type CreateRuleApplicationRequest struct {
	ElementID  int64  `json:"element_id" binding:"required"`
	EngineID   int64  `json:"engine_id" binding:"required"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name" binding:"required"`
	ColumnName string `json:"column_name" binding:"required"`
}

type UpdateRuleApplicationRequest struct {
	Enabled *bool `json:"enabled"`
}
