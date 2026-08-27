package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

var materializationGroupCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type MaterializationGroupService struct {
	repo            *repository.MaterializationGroupRepository
	materialization *MaterializationService
}

type MaterializationGroupWrite struct {
	Code            string
	Name            string
	Description     string
	LogicalTableIDs []int64
	Version         int64
}

func NewMaterializationGroupService(repo *repository.MaterializationGroupRepository, materialization *MaterializationService) *MaterializationGroupService {
	return &MaterializationGroupService{repo: repo, materialization: materialization}
}

func (s *MaterializationGroupService) Create(ctx context.Context, tenantID, userID int64, input MaterializationGroupWrite) (*models.MaterializationGroup, error) {
	members, versions, err := s.validateDefinition(tenantID, input)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	group := &models.MaterializationGroup{
		TenantID: tenantID, Code: strings.TrimSpace(input.Code), Name: strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description), Version: 1, CreatedBy: userID, UpdatedBy: userID,
		CreatedAt: now, UpdatedAt: now, Members: members,
	}
	if err := s.repo.Create(ctx, group, versions); err != nil {
		return nil, materializationGroupResourceError(err)
	}
	return group, nil
}

func (s *MaterializationGroupService) Update(ctx context.Context, tenantID, userID, id int64, input MaterializationGroupWrite) (*models.MaterializationGroup, error) {
	if id <= 0 || input.Version <= 0 {
		return nil, apperrors.Validation("materialization_group_invalid", modeli18n.MsgMaterializationInvalid)
	}
	current, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, materializationGroupResourceError(err)
	}
	if strings.TrimSpace(input.Code) != current.Code {
		return nil, apperrors.Validation("materialization_group_code_immutable", modeli18n.MsgMaterializationInvalid)
	}
	members, versions, err := s.validateDefinition(tenantID, input)
	if err != nil {
		return nil, err
	}
	group := &models.MaterializationGroup{
		ID: id, TenantID: tenantID, Code: current.Code, Name: strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description), Version: input.Version, UpdatedBy: userID, Members: members,
	}
	if err := s.repo.Update(ctx, group, input.Version, versions); err != nil {
		return nil, materializationGroupResourceError(err)
	}
	return s.repo.GetByID(ctx, tenantID, id)
}

func (s *MaterializationGroupService) Get(ctx context.Context, tenantID, id int64) (*models.MaterializationGroup, error) {
	if id <= 0 {
		return nil, apperrors.Validation("materialization_group_invalid", modeli18n.MsgMaterializationInvalid)
	}
	group, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, materializationGroupResourceError(err)
	}
	return group, nil
}

func (s *MaterializationGroupService) List(ctx context.Context, tenantID int64, page, pageSize int) ([]models.MaterializationGroup, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.repo.List(ctx, tenantID, page, pageSize)
}

func (s *MaterializationGroupService) ListAll(ctx context.Context, tenantID int64) ([]models.MaterializationGroup, error) {
	return s.repo.ListAll(ctx, tenantID)
}

func (s *MaterializationGroupService) Delete(ctx context.Context, tenantID, id, version int64) error {
	if id <= 0 || version <= 0 {
		return apperrors.Validation("materialization_group_invalid", modeli18n.MsgMaterializationInvalid)
	}
	if err := s.repo.Delete(ctx, tenantID, id, version); err != nil {
		return materializationGroupResourceError(err)
	}
	return nil
}

func (s *MaterializationGroupService) validateDefinition(tenantID int64, input MaterializationGroupWrite) ([]models.MaterializationGroupMember, map[int64]int64, error) {
	code := strings.TrimSpace(input.Code)
	name := strings.TrimSpace(input.Name)
	if tenantID <= 0 || code == "" || len(code) > 100 || !materializationGroupCodePattern.MatchString(code) || name == "" || len(name) > 200 || len(input.LogicalTableIDs) == 0 || len(input.LogicalTableIDs) > 100 {
		return nil, nil, apperrors.Validation("materialization_group_invalid", modeli18n.MsgMaterializationInvalid)
	}
	seenIDs := make(map[int64]struct{}, len(input.LogicalTableIDs))
	seenTargets := make(map[string]struct{}, len(input.LogicalTableIDs))
	members := make([]models.MaterializationGroupMember, 0, len(input.LogicalTableIDs))
	versions := make(map[int64]int64, len(input.LogicalTableIDs))
	var engineID uint
	for position, logicalTableID := range input.LogicalTableIDs {
		if logicalTableID <= 0 {
			return nil, nil, apperrors.Validation("materialization_group_invalid", modeli18n.MsgMaterializationInvalid)
		}
		if _, exists := seenIDs[logicalTableID]; exists {
			return nil, nil, apperrors.Validation("materialization_group_duplicate_member", modeli18n.MsgMaterializationInvalid)
		}
		seenIDs[logicalTableID] = struct{}{}
		table, _, locator, targetName, _, err := s.materialization.loadApprovedDefinition(logicalTableID, tenantID)
		if err != nil {
			return nil, nil, err
		}
		versions[logicalTableID] = table.Version
		if engineID == 0 {
			engineID = locator.EngineID
		} else if engineID != locator.EngineID {
			return nil, nil, apperrors.Conflict("materialization_group_cross_engine", modeli18n.MsgMaterializationConflict)
		}
		targetKey := locator.ToURI() + "/" + targetName
		if _, exists := seenTargets[targetKey]; exists {
			return nil, nil, apperrors.Conflict("materialization_group_duplicate_target", modeli18n.MsgMaterializationConflict)
		}
		seenTargets[targetKey] = struct{}{}
		members = append(members, models.MaterializationGroupMember{TenantID: tenantID, LogicalTableID: logicalTableID, Position: position})
	}
	return members, versions, nil
}

func materializationGroupResourceError(err error) error {
	if err == nil {
		return nil
	}
	if repository.IsMaterializationGroupNotFound(err) || err == gorm.ErrRecordNotFound {
		return apperrors.NotFound("materialization_group_not_found", modeli18n.MsgResourceNotFound)
	}
	if err == gorm.ErrDuplicatedKey {
		return apperrors.Conflict("materialization_group_conflict", modeli18n.MsgMaterializationConflict)
	}
	if strings.Contains(err.Error(), commonAPI.ErrConflict.Error()) {
		return apperrors.Conflict("materialization_group_version_conflict", modeli18n.MsgMaterializationConflict)
	}
	return err
}
