package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataquality"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

// RuleEngineService 负责版本化规则快照与规则应用管理。
type RuleEngineService struct {
	standardClient *commonClient.StandardClient
	systemClient   *commonClient.SystemServiceClient
	ruleAppRepo    *repository.RuleApplicationRepository
}

func NewRuleEngineService(
	standardClient *commonClient.StandardClient,
	systemClient *commonClient.SystemServiceClient,
	ruleAppRepo *repository.RuleApplicationRepository,
) *RuleEngineService {
	return &RuleEngineService{
		standardClient: standardClient,
		systemClient:   systemClient,
		ruleAppRepo:    ruleAppRepo,
	}
}

// ListRuleApplications 列出规则应用
func (s *RuleEngineService) ListRuleApplications(opts repository.RuleApplicationListOptions) ([]models.RuleApplication, int64, error) {
	return s.ruleAppRepo.List(opts)
}

// GetRuleApplication 获取单条规则应用
func (s *RuleEngineService) GetRuleApplication(id, tenantID int64) (*models.RuleApplication, error) {
	return s.ruleAppRepo.Get(id, tenantID)
}

// CreateRuleApplication 手动创建规则应用（绑定数据元质量规则到字段）
func (s *RuleEngineService) CreateRuleApplication(ctx context.Context, tenantID, userID int64, req *CreateRuleApplicationRequest) (*models.RuleApplication, error) {
	if req.ElementID <= 0 {
		return nil, fmt.Errorf("%w: element_id is required", commonAPI.ErrBadRequest)
	}
	if err := requirePostgreSQLEngine(ctx, s.systemClient, tenantID, req.EngineID); err != nil {
		return nil, err
	}
	req.SchemaName = strings.TrimSpace(req.SchemaName)
	req.TableName = strings.TrimSpace(req.TableName)
	req.ColumnName = strings.TrimSpace(req.ColumnName)
	if req.SchemaName == "" || req.TableName == "" || req.ColumnName == "" {
		return nil, fmt.Errorf("%w: schema_name, table_name and column_name are required", commonAPI.ErrBadRequest)
	}
	// 从 Standard 获取数据元质量规则快照
	rules, err := s.standardClient.WithTenantID(uint(tenantID)).GetElementQualityRules(ctx, req.ElementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get quality rules: %w", err)
	}

	enabledRules := rules.EnabledRules()
	if len(enabledRules) == 0 {
		return nil, fmt.Errorf("%w: data element has no enabled quality rules", commonAPI.ErrBadRequest)
	}
	snapshot := dataquality.Document{SchemaVersion: dataquality.RulesSchemaVersion, Rules: enabledRules}
	ruleConfig, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode quality rule snapshot: %w", err)
	}

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
func (s *RuleEngineService) UpdateRuleApplication(id, tenantID, userID int64, req *UpdateRuleApplicationRequest) (*models.RuleApplication, error) {
	ra, err := s.ruleAppRepo.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	ra.Enabled = req.Enabled
	ra.UpdatedBy = &userID
	if err := s.ruleAppRepo.Update(ra); err != nil {
		return nil, err
	}
	return ra, nil
}

// DeleteRuleApplication 删除规则应用
func (s *RuleEngineService) DeleteRuleApplication(id, tenantID int64) error {
	return s.ruleAppRepo.Delete(id, tenantID)
}

// Request types

type CreateRuleApplicationRequest struct {
	ElementID  int64  `json:"element_id" binding:"required"`
	EngineID   int64  `json:"engine_id" binding:"required"`
	SchemaName string `json:"schema_name" binding:"required"`
	TableName  string `json:"table_name" binding:"required"`
	ColumnName string `json:"column_name" binding:"required"`
}

type UpdateRuleApplicationRequest struct {
	Enabled bool `json:"enabled"`
}
