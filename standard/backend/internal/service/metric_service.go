package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

type MetricService struct {
	catRepo    *repository.MetricCategoryRepository
	metricRepo *repository.MetricRepository
	refs       *repository.TenantReferenceRepository
	deletion   *StandardReferenceDeletionService
}

func NewMetricService(catRepo *repository.MetricCategoryRepository, metricRepo *repository.MetricRepository, refs *repository.TenantReferenceRepository, deletion *StandardReferenceDeletionService) *MetricService {
	return &MetricService{catRepo: catRepo, metricRepo: metricRepo, refs: refs, deletion: deletion}
}

func (s *MetricService) ListCategories(tenantID int64) ([]models.MetricCategory, error) {
	return s.catRepo.List(tenantID)
}
func (s *MetricService) CreateCategory(req *models.CreateMetricCategoryRequest, tenantID, userID int64) (*models.MetricCategory, error) {
	if err := s.refs.RequireMetricCategory(tenantID, req.ParentID); err != nil {
		return nil, err
	}
	exists, err := s.catRepo.ExistsByCode(strings.TrimSpace(req.Code), tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	item := &models.MetricCategory{TenantID: tenantID, Name: strings.TrimSpace(req.Name), Code: strings.TrimSpace(req.Code), Description: req.Description, ParentID: req.ParentID, SortOrder: req.SortOrder, CreatedBy: userID}
	if err := s.catRepo.Create(item); err != nil {
		return nil, err
	}
	return item, nil
}
func (s *MetricService) UpdateCategory(id, tenantID, userID int64, req *models.UpdateMetricCategoryRequest) (*models.MetricCategory, error) {
	item, err := s.catRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateCategoryParent(id, tenantID, req.ParentID); err != nil {
		return nil, err
	}
	item.Name, item.Description, item.ParentID, item.SortOrder, item.UpdatedBy = strings.TrimSpace(req.Name), req.Description, req.ParentID, req.SortOrder, &userID
	if err := s.catRepo.Update(item, req.Version); err != nil {
		return nil, err
	}
	return s.catRepo.GetByID(id, tenantID)
}
func (s *MetricService) validateCategoryParent(id, tenantID int64, parentID *int64) error {
	if err := s.refs.RequireMetricCategory(tenantID, parentID); err != nil {
		return err
	}
	for current := parentID; current != nil; {
		if *current == id {
			return ErrMetricCategoryParentCycle
		}
		parent, err := s.catRepo.GetByID(*current, tenantID)
		if err != nil {
			return err
		}
		current = parent.ParentID
	}
	return nil
}
func (s *MetricService) DeleteCategory(id, tenantID int64) error {
	return mapDeleteConflict(s.catRepo.Delete(id, tenantID), ErrMetricCategoryReferenced)
}

func (s *MetricService) ListMetrics(tenantID int64, opts repository.ListMetricOptions) ([]models.MetricDefinitionAggregate, int64, error) {
	return s.metricRepo.List(tenantID, opts)
}
func (s *MetricService) GetMetric(id, tenantID int64) (*models.MetricDefinitionAggregate, error) {
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) GetMetricAt(id, tenantID int64, asOf time.Time) (*models.MetricDefinitionAggregate, error) {
	return s.metricRepo.GetAggregateAt(id, tenantID, asOf)
}

func (s *MetricService) CreateMetric(req *models.CreateMetricRequest, tenantID, userID int64) (*models.MetricDefinitionAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	if err := s.refs.RequireMetricCategory(tenantID, req.CategoryID); err != nil {
		return nil, err
	}
	exists, err := s.metricRepo.ExistsByCode(strings.TrimSpace(req.Code), tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	revision, dependencies, err := s.revisionFromCreate(req, tenantID, userID)
	if err != nil {
		return nil, err
	}
	identity := &models.MetricDefinition{TenantID: tenantID, CategoryID: req.CategoryID, ScopeType: scopeType, OwnerDomainID: req.OwnerDomainID, Code: strings.TrimSpace(req.Code), StewardID: req.StewardID, Tags: req.Tags, CreatedBy: userID, LifecycleState: "active"}
	if err := s.metricRepo.Create(identity, revision, dependencies); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(identity.ID, tenantID)
}

func (s *MetricService) UpdateMetric(id, tenantID, userID int64, req *models.UpdateMetricRequest) (*models.MetricDefinitionAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	if err := s.refs.RequireMetricCategory(tenantID, req.CategoryID); err != nil {
		return nil, err
	}
	identity, err := s.metricRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	identity.CategoryID, identity.ScopeType, identity.OwnerDomainID, identity.StewardID, identity.Tags, identity.UpdatedBy = req.CategoryID, scopeType, req.OwnerDomainID, req.StewardID, req.Tags, &userID
	if err := s.metricRepo.UpdateIdentity(identity, req.Version); err != nil {
		return nil, err
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}

func (s *MetricService) ListRevisions(id, tenantID int64) ([]models.MetricDefinitionRevision, error) {
	return s.metricRepo.ListRevisions(id, tenantID)
}
func (s *MetricService) GetRevision(id, revisionID, tenantID int64) (*models.MetricDefinitionRevision, error) {
	return s.metricRepo.GetRevision(id, revisionID, tenantID)
}
func (s *MetricService) CreateRevision(id, tenantID, userID int64, req *models.CreateMetricRevisionRequest) (*models.MetricDefinitionAggregate, error) {
	if err := s.metricRepo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary)); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) UpdateRevision(id, revisionID, tenantID, userID int64, req *models.UpdateMetricRevisionRequest) (*models.MetricDefinitionAggregate, error) {
	revision, dependencies, err := s.revisionFromUpdate(id, revisionID, req, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.metricRepo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision, dependencies); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) SubmitRevision(id, revisionID, tenantID, userID, version int64) (*models.MetricDefinitionAggregate, error) {
	revision, err := s.metricRepo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRevision(revision, revision.Dependencies, tenantID); err != nil {
		return nil, err
	}
	if revision.EffectiveFrom == nil {
		return nil, fmt.Errorf("%w: effective_from is required before review", ErrInvalidStandardRevision)
	}
	if err := s.metricRepo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) ReturnRevision(id, revisionID, tenantID, userID, version int64) (*models.MetricDefinitionAggregate, error) {
	if err := s.metricRepo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) PublishRevision(id, revisionID, tenantID, userID, version int64) (*models.MetricDefinitionAggregate, error) {
	revision, err := s.metricRepo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusInReview {
		return nil, ErrInvalidRevisionTransition
	}
	if err := s.validateRevision(revision, revision.Dependencies, tenantID); err != nil {
		return nil, err
	}
	for _, dependency := range revision.Dependencies {
		if _, err := s.metricRepo.GetEffectiveRevision(dependency.DependencyDefinitionID, tenantID, *revision.EffectiveFrom); err != nil {
			return nil, ErrPublishedRevisionRequired
		}
	}
	if err := s.metricRepo.PublishRevision(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) WithdrawRevision(id, revisionID, tenantID, userID, version int64) (*models.MetricDefinitionAggregate, error) {
	if err := s.metricRepo.WithdrawPublished(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapMetricRevisionError(err)
	}
	return s.metricRepo.GetAggregate(id, tenantID)
}
func (s *MetricService) GetPublishedReference(id, revisionID, tenantID int64) (*models.PublishedMetricDefinitionReference, error) {
	revision, err := s.metricRepo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusPublished {
		return nil, ErrPublishedRevisionRequired
	}
	identity, err := s.metricRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	return &models.PublishedMetricDefinitionReference{ID: identity.ID, TenantID: identity.TenantID, ScopeType: identity.ScopeType, OwnerDomainID: identity.OwnerDomainID, Code: identity.Code, Name: revision.Name, MetricType: revision.MetricType, Status: revision.Status, LifecycleState: identity.LifecycleState, Version: identity.Version, RevisionID: revision.ID, RevisionNo: revision.RevisionNo}, nil
}
func (s *MetricService) DeleteMetric(ctx context.Context, id, tenantID int64) error {
	return s.deletion.Delete(ctx, tenantID, "metric", id, func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		return mapDeleteConflict(s.metricRepo.DeleteTx(tx, resourceID, resourceTenantID), ErrMetricReferenced)
	})
}

func (s *MetricService) revisionFromCreate(req *models.CreateMetricRequest, tenantID, userID int64) (*models.MetricDefinitionRevision, []models.MetricDefinitionRevisionDependency, error) {
	revision := &models.MetricDefinitionRevision{MetricType: strings.TrimSpace(req.MetricType), Name: strings.TrimSpace(req.Name), Definition: strings.TrimSpace(req.Definition), StatisticalCaliber: strings.TrimSpace(req.StatisticalCaliber), SemanticFormula: strings.TrimSpace(req.SemanticFormula), UnitID: req.UnitID, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, CreatedBy: userID}
	dependencies := metricDependencies(req.Dependencies)
	if err := s.validateRevision(revision, dependencies, tenantID); err != nil {
		return nil, nil, err
	}
	return revision, dependencies, nil
}
func (s *MetricService) revisionFromUpdate(metricID, revisionID int64, req *models.UpdateMetricRevisionRequest, tenantID, userID int64) (*models.MetricDefinitionRevision, []models.MetricDefinitionRevisionDependency, error) {
	revision := &models.MetricDefinitionRevision{ID: revisionID, MetricDefinitionID: metricID, MetricType: strings.TrimSpace(req.MetricType), Name: strings.TrimSpace(req.Name), Definition: strings.TrimSpace(req.Definition), StatisticalCaliber: strings.TrimSpace(req.StatisticalCaliber), SemanticFormula: strings.TrimSpace(req.SemanticFormula), UnitID: req.UnitID, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, UpdatedBy: &userID}
	dependencies := metricDependencies(req.Dependencies)
	if err := s.validateRevision(revision, dependencies, tenantID); err != nil {
		return nil, nil, err
	}
	return revision, dependencies, nil
}
func metricDependencies(inputs []models.MetricDefinitionDependencyInput) []models.MetricDefinitionRevisionDependency {
	result := make([]models.MetricDefinitionRevisionDependency, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, models.MetricDefinitionRevisionDependency{DependencyDefinitionID: input.MetricDefinitionID, RelationKind: input.RelationKind, Coefficient: input.Coefficient, Note: strings.TrimSpace(input.Note)})
	}
	return result
}
func (s *MetricService) validateRevision(revision *models.MetricDefinitionRevision, dependencies []models.MetricDefinitionRevisionDependency, tenantID int64) error {
	if revision == nil || revision.MetricDefinitionID < 0 || strings.TrimSpace(revision.Name) == "" || strings.TrimSpace(revision.Definition) == "" || strings.TrimSpace(revision.StatisticalCaliber) == "" || strings.TrimSpace(revision.ChangeSummary) == "" {
		return ErrInvalidStandardRevision
	}
	if revision.EffectiveFrom != nil && revision.EffectiveTo != nil && !revision.EffectiveFrom.Before(*revision.EffectiveTo) {
		return fmt.Errorf("%w: effective_from must precede effective_to", ErrInvalidStandardRevision)
	}
	if err := s.refs.RequireUnit(tenantID, revision.UnitID); err != nil {
		return err
	}
	seen := map[int64]struct{}{}
	for _, dependency := range dependencies {
		if dependency.DependencyDefinitionID <= 0 || dependency.DependencyDefinitionID == revision.MetricDefinitionID {
			return fmt.Errorf("%w: invalid self dependency", ErrInvalidStandardRevision)
		}
		if _, ok := seen[dependency.DependencyDefinitionID]; ok {
			return fmt.Errorf("%w: duplicate dependency", ErrInvalidStandardRevision)
		}
		seen[dependency.DependencyDefinitionID] = struct{}{}
		if err := s.refs.RequireMetricDefinition(tenantID, dependency.DependencyDefinitionID); err != nil {
			return err
		}
	}
	switch revision.MetricType {
	case models.MetricTypeAtomic:
		if len(dependencies) != 0 {
			return fmt.Errorf("%w: atomic metric cannot have dependencies", ErrInvalidStandardRevision)
		}
	case models.MetricTypeDerived:
		if len(dependencies) != 1 || dependencies[0].RelationKind != models.MetricDependencyBase {
			return fmt.Errorf("%w: derived metric requires exactly one base dependency", ErrInvalidStandardRevision)
		}
	case models.MetricTypeComposite:
		if len(dependencies) == 0 {
			return fmt.Errorf("%w: composite metric requires component dependencies", ErrInvalidStandardRevision)
		}
		for _, dependency := range dependencies {
			if dependency.RelationKind != models.MetricDependencyComponent {
				return fmt.Errorf("%w: composite metric only accepts component dependencies", ErrInvalidStandardRevision)
			}
		}
	default:
		return fmt.Errorf("%w: invalid metric_type", ErrInvalidStandardRevision)
	}
	return nil
}
func mapMetricRevisionError(err error) error {
	switch {
	case errors.Is(err, repository.ErrDraftAlreadyExists):
		return ErrDraftRevisionExists
	case errors.Is(err, repository.ErrInvalidRevisionTransition), errors.Is(err, repository.ErrRevisionNotEditable):
		return ErrInvalidRevisionTransition
	case errors.Is(err, repository.ErrEffectiveIntervalConflict):
		return ErrEffectiveIntervalConflict
	default:
		return err
	}
}
