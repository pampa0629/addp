package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"strings"
	"time"
)

type RuleService struct {
	repo           *repository.RuleRepository
	standardClient *commonClient.StandardClient
}

func NewRuleService(repo *repository.RuleRepository, standard *commonClient.StandardClient) *RuleService {
	return &RuleService{repo: repo, standardClient: standard}
}

type RuleWriteRequest struct {
	Code    string `json:"code"`
	Version int64  `json:"version"`
	models.RuleContent
}

func (s *RuleService) List(ctx context.Context, tenantID int64, search string, page, size int) ([]models.QualityRule, int64, error) {
	return s.repo.List(ctx, tenantID, strings.TrimSpace(search), page, size)
}
func (s *RuleService) Get(ctx context.Context, tenantID, id int64) (*models.QualityRule, error) {
	return s.repo.Get(ctx, tenantID, id)
}
func (s *RuleService) Plans(ctx context.Context, tenantID, id int64, page, size int) ([]models.QualityPlan, int64, error) {
	return s.repo.Plans(ctx, tenantID, id, page, size)
}
func (s *RuleService) Delete(ctx context.Context, tenantID, id, version int64) error {
	if version <= 0 {
		return commonAPI.ErrBadRequest
	}
	return s.repo.Delete(ctx, tenantID, id, version)
}
func (s *RuleService) Create(ctx context.Context, tenantID, userID int64, request RuleWriteRequest) (*models.QualityRule, error) {
	if request.Version != 0 {
		return nil, commonAPI.ErrBadRequest
	}
	if err := s.validate(ctx, tenantID, &request, nil); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rule := &models.QualityRule{TenantID: tenantID, Code: request.Code, RuleContent: request.RuleContent, CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}
func (s *RuleService) Update(ctx context.Context, tenantID, userID, id int64, request RuleWriteRequest) (*models.QualityRule, error) {
	if request.Version <= 0 {
		return nil, commonAPI.ErrBadRequest
	}
	previous, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if previous.Version != request.Version {
		return nil, repository.ErrVersionConflict
	}
	if err := s.validate(ctx, tenantID, &request, previous); err != nil {
		return nil, err
	}
	rule := &models.QualityRule{ID: id, TenantID: tenantID, Code: request.Code, RuleContent: request.RuleContent, UpdatedBy: userID}
	if err := s.repo.Replace(ctx, rule, request.Version); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenantID, id)
}
func definitionBinding(kind string) models.CheckBindings {
	b := models.CheckBindings{Table: "target"}
	switch kind {
	case "not_null", "allowed_values", "format", "length", "value_range":
		b.Column = "value"
	case "unique_key":
		b.Columns = []string{"value"}
	case "foreign_key":
		b.Columns = []string{"value"}
		b.ReferenceTable = "reference"
		b.ReferenceColumns = []string{"value"}
	case "predicate_implication":
		b.WhenColumn = "condition"
		b.ThenColumn = "value"
	}
	return b
}
func resolvedDefinition(content models.RuleContent) (PlanRule, error) {
	params, err := models.ResolveRuleParams(content.Type, content.Params, definitionBinding(content.Type))
	return PlanRule{RuleKey: "00000000-0000-4000-8000-000000000001", Type: content.Type, Params: params, Name: content.Name, Source: content.Source, Severity: "error"}, err
}
func (s *RuleService) validate(ctx context.Context, tenantID int64, request *RuleWriteRequest, previous *models.QualityRule) error {
	request.Code = strings.TrimSpace(request.Code)
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	if tenantID <= 0 || !planNamePattern.MatchString(request.Code) || len(request.Code) > 100 || request.Name == "" || len(request.Name) > 200 || len(request.Description) > 10000 {
		return commonAPI.ErrBadRequest
	}
	rule, err := resolvedDefinition(request.RuleContent)
	if err != nil {
		return fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	raw, _ := json.Marshal(PlanRuleDocument{SchemaVersion: planSchemaVersion, Rules: []PlanRule{rule}})
	bindings := []PlanTableBinding{{Alias: "target", Locator: "addp://engine/1/path/target?type=table"}, {Alias: "reference", Locator: "addp://engine/1/path/reference?type=table"}}
	if _, err := validatePlanContract(bindings, raw); err != nil {
		return fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	if request.Source != nil {
		var prior PlanRuleDocument
		if previous != nil {
			old, e := resolvedDefinition(previous.RuleContent)
			if e != nil {
				return e
			}
			prior.Rules = []PlanRule{old}
		}
		if !unchangedStandardConstraint(rule, prior) {
			if err := s.validateStandardSource(ctx, tenantID, rule); err != nil {
				return err
			}
		}
	}
	return nil
}
