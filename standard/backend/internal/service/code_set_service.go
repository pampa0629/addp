package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type CodeSetService struct {
	repo *repository.CodeSetRepository
	refs *repository.TenantReferenceRepository
}

func NewCodeSetService(repo *repository.CodeSetRepository, refs *repository.TenantReferenceRepository) *CodeSetService {
	return &CodeSetService{repo: repo, refs: refs}
}

func (s *CodeSetService) CreateCodeSet(tenantID, userID int64, req *models.CreateCodeSetRequest) (*models.CodeSetAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	if err := validateCodeSetRevision(req.Name, req.Description, req.ValueType, req.ChangeSummary, req.EffectiveFrom, req.EffectiveTo); err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByCode(tenantID, strings.TrimSpace(req.Code), 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	identity := &models.CodeSet{TenantID: tenantID, ScopeType: scopeType, OwnerDomainID: req.OwnerDomainID, Code: strings.TrimSpace(req.Code), Origin: models.CodeSetOriginTenant, StewardID: req.StewardID, Tags: req.Tags, CreatedBy: userID, LifecycleState: "active"}
	revision := &models.CodeSetRevision{Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), ValueType: req.ValueType, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, CreatedBy: userID}
	if err := s.repo.Create(identity, revision); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(identity.ID, tenantID)
}

func (s *CodeSetService) GetCodeSet(id, tenantID int64) (*models.CodeSetAggregate, error) {
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) GetCodeSetAt(id, tenantID int64, asOf time.Time) (*models.CodeSetAggregate, error) {
	return s.repo.GetAggregateAt(id, tenantID, asOf)
}

func (s *CodeSetService) ListCodeSets(tenantID int64, opts repository.ListCodeSetOptions) ([]models.CodeSetAggregate, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *CodeSetService) UpdateCodeSet(id, tenantID, userID int64, req *models.UpdateCodeSetRequest) (*models.CodeSetAggregate, error) {
	identity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if identity.Origin == models.CodeSetOriginPlatform {
		return nil, ErrPlatformCodeSetImmutable
	}
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	identity.ScopeType, identity.OwnerDomainID, identity.StewardID, identity.Tags, identity.UpdatedBy = scopeType, req.OwnerDomainID, req.StewardID, req.Tags, &userID
	if err := s.repo.UpdateIdentity(identity, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) DeleteCodeSet(id, tenantID int64) error {
	identity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return err
	}
	if identity.Origin == models.CodeSetOriginPlatform {
		return ErrPlatformCodeSetImmutable
	}
	return mapDeleteConflict(s.repo.Delete(id, tenantID), ErrCodeSetReferenced)
}

func (s *CodeSetService) ListRevisions(id, tenantID int64) ([]models.CodeSetRevision, error) {
	return s.repo.ListRevisions(id, tenantID)
}
func (s *CodeSetService) GetRevision(id, revisionID, tenantID int64) (*models.CodeSetRevision, error) {
	return s.repo.GetRevision(id, revisionID, tenantID)
}

func (s *CodeSetService) CreateRevision(id, tenantID, userID int64, req *models.CreateCodeSetRevisionRequest) (*models.CodeSetAggregate, error) {
	if strings.TrimSpace(req.ChangeSummary) == "" {
		return nil, ErrInvalidStandardRevision
	}
	if _, err := s.repo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary)); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) UpdateRevision(id, revisionID, tenantID, userID int64, req *models.UpdateCodeSetRevisionRequest) (*models.CodeSetAggregate, error) {
	if err := validateCodeSetRevision(req.Name, req.Description, req.ValueType, req.ChangeSummary, req.EffectiveFrom, req.EffectiveTo); err != nil {
		return nil, err
	}
	revision := &models.CodeSetRevision{ID: revisionID, CodeSetID: id, Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), ValueType: req.ValueType, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, UpdatedBy: &userID}
	if err := s.repo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) SubmitRevision(id, revisionID, tenantID, userID, version int64) (*models.CodeSetAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := validateCodeSetRevision(revision.Name, revision.Description, revision.ValueType, revision.ChangeSummary, revision.EffectiveFrom, revision.EffectiveTo); err != nil {
		return nil, err
	}
	if revision.EffectiveFrom == nil {
		return nil, fmt.Errorf("%w: effective_from is required before review", ErrInvalidStandardRevision)
	}
	active := 0
	for _, item := range revision.Items {
		if item.Status == models.CodeItemStatusActive {
			active++
		}
	}
	if active == 0 {
		return nil, fmt.Errorf("%w: code set revision requires an active item", ErrInvalidStandardRevision)
	}
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) ReturnRevision(id, revisionID, tenantID, userID, version int64) (*models.CodeSetAggregate, error) {
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) PublishRevision(id, revisionID, tenantID, userID, version int64) (*models.CodeSetAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusInReview {
		return nil, ErrInvalidRevisionTransition
	}
	if err := validateCodeSetRevision(revision.Name, revision.Description, revision.ValueType, revision.ChangeSummary, revision.EffectiveFrom, revision.EffectiveTo); err != nil {
		return nil, err
	}
	if revision.EffectiveFrom == nil {
		return nil, fmt.Errorf("%w: effective_from is required before publish", ErrInvalidStandardRevision)
	}
	if err := s.repo.PublishRevision(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) WithdrawRevision(id, revisionID, tenantID, userID, version int64) (*models.CodeSetAggregate, error) {
	if err := s.repo.WithdrawPublished(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *CodeSetService) CreateCodeItem(id, revisionID, tenantID int64, req *models.CreateCodeItemRequest) (*models.CodeItemMutationResponse, error) {
	if err := validateCodeItem(req.Code, req.Label, req.Status); err != nil {
		return nil, err
	}
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := validateCodeValue(revision.ValueType, req.Code); err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsItemByCode(revisionID, strings.TrimSpace(req.Code), 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	item := &models.CodeSetRevisionItem{Code: strings.TrimSpace(req.Code), Label: strings.TrimSpace(req.Label), Definition: strings.TrimSpace(req.Definition), SortOrder: req.SortOrder, Status: req.Status}
	if err := s.repo.CreateItem(id, revisionID, tenantID, item, req.Version); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return &models.CodeItemMutationResponse{Item: item, Version: req.Version + 1}, nil
}

func (s *CodeSetService) UpdateCodeItem(id, revisionID, itemID, tenantID int64, req *models.UpdateCodeItemRequest) (*models.CodeItemMutationResponse, error) {
	if err := validateCodeItem("existing", req.Label, req.Status); err != nil {
		return nil, err
	}
	if req.ReplacementItemID != nil {
		if *req.ReplacementItemID == itemID {
			return nil, fmt.Errorf("%w: replacement item must be different", ErrInvalidStandardRevision)
		}
		revision, err := s.repo.GetRevision(id, revisionID, tenantID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, item := range revision.Items {
			if item.ID == *req.ReplacementItemID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: replacement item must belong to the same revision", ErrInvalidStandardRevision)
		}
	}
	item := &models.CodeSetRevisionItem{ID: itemID, CodeSetRevisionID: revisionID, Label: strings.TrimSpace(req.Label), Definition: strings.TrimSpace(req.Definition), SortOrder: req.SortOrder, Status: req.Status, ReplacementItemID: req.ReplacementItemID}
	if err := s.repo.UpdateItem(id, revisionID, itemID, tenantID, item, req.Version); err != nil {
		return nil, mapCodeSetRevisionError(err)
	}
	return &models.CodeItemMutationResponse{Item: item, Version: req.Version + 1}, nil
}

func (s *CodeSetService) DeleteCodeItem(id, revisionID, itemID, tenantID, version int64) error {
	return mapCodeSetRevisionError(mapDeleteConflict(s.repo.DeleteItem(id, revisionID, itemID, tenantID, version), ErrCodeItemReferenced))
}

func validateCodeSetRevision(name, description, valueType, changeSummary string, effectiveFrom, effectiveTo *time.Time) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(description) == "" || strings.TrimSpace(changeSummary) == "" {
		return ErrInvalidStandardRevision
	}
	if valueType != "string" && valueType != "int" && valueType != "bigint" {
		return fmt.Errorf("%w: invalid code set value_type", ErrInvalidStandardRevision)
	}
	if effectiveFrom != nil && effectiveTo != nil && !effectiveFrom.Before(*effectiveTo) {
		return fmt.Errorf("%w: effective_from must precede effective_to", ErrInvalidStandardRevision)
	}
	return nil
}

func validateCodeItem(code, label, status string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(label) == "" {
		return ErrInvalidStandardRevision
	}
	if status != models.CodeItemStatusActive && status != models.CodeItemStatusDeprecated {
		return fmt.Errorf("%w: invalid code item status", ErrInvalidStandardRevision)
	}
	return nil
}

func mapCodeSetRevisionError(err error) error {
	if errors.Is(err, repository.ErrRevisionNotEditable) {
		return ErrPlatformCodeSetImmutable
	}
	return mapRevisionError(err)
}

func validateCodeValue(valueType, code string) error {
	if valueType == "int" || valueType == "bigint" {
		if _, err := strconv.ParseInt(code, 10, 64); err != nil {
			return fmt.Errorf("%w: code does not match value_type", ErrInvalidStandardRevision)
		}
	}
	return nil
}
