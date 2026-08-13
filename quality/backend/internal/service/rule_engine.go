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
func (s *RuleEngineService) ListRuleApplications(ctx context.Context, opts repository.RuleApplicationListOptions) ([]RuleApplicationListItem, int64, error) {
	applications, total, err := s.ruleAppRepo.List(opts)
	if err != nil {
		return nil, 0, err
	}
	if len(applications) == 0 {
		return []RuleApplicationListItem{}, total, nil
	}

	elementIDs := make([]int64, 0, len(applications))
	seen := make(map[int64]struct{}, len(applications))
	for _, application := range applications {
		if _, exists := seen[application.ElementID]; exists {
			continue
		}
		seen[application.ElementID] = struct{}{}
		elementIDs = append(elementIDs, application.ElementID)
	}
	elements, err := s.standardClient.WithTenantID(uint(opts.TenantID)).ListElementSummaries(ctx, elementIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("list rule application elements: %w", err)
	}
	elementsByID := make(map[int64]RuleApplicationElementSummary, len(elements))
	for _, element := range elements {
		elementsByID[element.ID] = RuleApplicationElementSummary{ID: element.ID, Name: element.Name, Code: element.Code}
	}

	items := make([]RuleApplicationListItem, 0, len(applications))
	for _, application := range applications {
		element, exists := elementsByID[application.ElementID]
		if !exists {
			return nil, 0, fmt.Errorf("list rule application elements: element %d not found", application.ElementID)
		}
		items = append(items, RuleApplicationListItem{RuleApplication: application, Element: element})
	}
	return items, total, nil
}

// GetRuleApplication 获取单条规则应用
func (s *RuleEngineService) GetRuleApplication(id, tenantID int64) (*models.RuleApplication, error) {
	return s.ruleAppRepo.Get(id, tenantID)
}

func (s *RuleEngineService) ListElementCandidates(ctx context.Context, tenantID int64, keyword string, page, pageSize int) ([]RuleApplicationElementCandidate, int64, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, 0, fmt.Errorf("%w: keyword is required", commonAPI.ErrBadRequest)
	}
	elements, total, err := s.standardClient.WithTenantID(uint(tenantID)).ListElementCandidates(ctx, keyword, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list rule application element candidates: %w", err)
	}
	items := make([]RuleApplicationElementCandidate, len(elements))
	for index, element := range elements {
		items[index] = RuleApplicationElementCandidate{
			ID: element.ID, Name: element.Name, Code: element.Code, QualityRules: element.QualityRules,
		}
	}
	return items, total, nil
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
	table, err := requirePostgreSQLCatalogTable(ctx, s.systemClient, tenantID, req.EngineID, req.SchemaName, req.TableName)
	if err != nil {
		return nil, err
	}
	if err := requirePostgreSQLCatalogColumn(ctx, s.systemClient, tenantID, req.EngineID, table, req.ColumnName); err != nil {
		return nil, err
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
func (s *RuleEngineService) UpdateRuleApplication(ctx context.Context, id, tenantID, userID int64, req *UpdateRuleApplicationRequest) (*models.RuleApplication, error) {
	if req == nil || req.Enabled == nil {
		return nil, fmt.Errorf("%w: enabled is required", commonAPI.ErrBadRequest)
	}
	if *req.Enabled {
		application, err := s.ruleAppRepo.Get(id, tenantID)
		if err != nil {
			return nil, err
		}
		if err := requirePostgreSQLEngine(ctx, s.systemClient, tenantID, application.EngineID); err != nil {
			return nil, err
		}
	}
	if err := s.ruleAppRepo.UpdateEnabled(id, tenantID, userID, *req.Enabled); err != nil {
		return nil, err
	}
	return s.ruleAppRepo.Get(id, tenantID)
}

// DeleteRuleApplication 删除规则应用
func (s *RuleEngineService) DeleteRuleApplication(ctx context.Context, id, tenantID int64) error {
	return s.ruleAppRepo.Delete(ctx, id, tenantID)
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
	Enabled *bool `json:"enabled" binding:"required"`
}

type RuleApplicationListItem struct {
	models.RuleApplication
	Element RuleApplicationElementSummary `json:"element"`
}

type RuleApplicationElementSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type RuleApplicationElementCandidate struct {
	ID           int64                `json:"id"`
	Name         string               `json:"name"`
	Code         string               `json:"code"`
	QualityRules dataquality.Document `json:"quality_rules"`
}
