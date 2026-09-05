package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type GlossaryService struct {
	repo *repository.GlossaryRepository
	refs *repository.TenantReferenceRepository
}

func NewGlossaryService(repo *repository.GlossaryRepository, refs *repository.TenantReferenceRepository) *GlossaryService {
	return &GlossaryService{repo: repo, refs: refs}
}

func (s *GlossaryService) CreateGlossary(req *models.CreateGlossaryRequest, tenantID, userID int64) (*models.GlossaryAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, ErrInvalidStandardRevision
	}
	exists, err := s.repo.ExistsByCode(code, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	revision := &models.GlossaryRevision{
		Name: strings.TrimSpace(req.Name), Alias: req.Alias, Definition: strings.TrimSpace(req.Definition),
		Example: req.Example, Note: req.Note, RelatedIDs: req.RelatedIDs,
		ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom,
		EffectiveTo: req.EffectiveTo, CreatedBy: userID,
	}
	if err := s.validateRevision(revision, tenantID, 0); err != nil {
		return nil, err
	}
	glossary := &models.Glossary{
		TenantID: tenantID, ScopeType: scopeType, OwnerDomainID: req.OwnerDomainID, Code: code,
		StewardID: req.StewardID, Tags: req.Tags, CreatedBy: userID, LifecycleState: "active",
	}
	if err := s.repo.Create(glossary, revision); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(glossary.ID, tenantID)
}

func (s *GlossaryService) GetGlossary(id, tenantID int64) (*models.GlossaryAggregate, error) {
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) GetGlossaryAt(id, tenantID int64, asOf time.Time) (*models.GlossaryAggregate, error) {
	return s.repo.GetAggregateAt(id, tenantID, asOf)
}

func (s *GlossaryService) ListGlossaries(tenantID int64, opts repository.ListGlossaryOptions) ([]models.GlossaryAggregate, int64, error) {
	if opts.ElementID != nil {
		if err := s.refs.RequireElement(tenantID, *opts.ElementID); err != nil {
			return nil, 0, err
		}
	}
	return s.repo.List(tenantID, opts)
}

func (s *GlossaryService) UpdateGlossary(id, tenantID, userID int64, req *models.UpdateGlossaryRequest) (*models.GlossaryAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	glossary, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	glossary.ScopeType, glossary.OwnerDomainID, glossary.StewardID, glossary.Tags, glossary.UpdatedBy = scopeType, req.OwnerDomainID, req.StewardID, req.Tags, &userID
	if err := s.repo.UpdateIdentity(glossary, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) UpdateElements(id, tenantID, userID int64, req *models.UpdateGlossaryElementsRequest) (*models.GlossaryAggregate, error) {
	if _, err := s.repo.GetByID(id, tenantID); err != nil {
		return nil, err
	}
	if err := s.refs.RequireElements(tenantID, req.ElementIDs); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateElements(id, tenantID, userID, req.Version, req.ElementIDs); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) ListRevisions(id, tenantID int64) ([]models.GlossaryRevision, error) {
	return s.repo.ListRevisions(id, tenantID)
}

func (s *GlossaryService) GetRevision(id, revisionID, tenantID int64) (*models.GlossaryRevision, error) {
	return s.repo.GetRevision(id, revisionID, tenantID)
}

func (s *GlossaryService) CreateRevision(id, tenantID, userID int64, req *models.CreateGlossaryRevisionRequest) (*models.GlossaryAggregate, error) {
	if strings.TrimSpace(req.ChangeSummary) == "" {
		return nil, ErrInvalidStandardRevision
	}
	if _, err := s.repo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary)); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) UpdateRevision(id, revisionID, tenantID, userID int64, req *models.UpdateGlossaryRevisionRequest) (*models.GlossaryAggregate, error) {
	revision := &models.GlossaryRevision{
		ID: revisionID, GlossaryID: id, Name: strings.TrimSpace(req.Name), Alias: req.Alias,
		Definition: strings.TrimSpace(req.Definition), Example: req.Example, Note: req.Note,
		RelatedIDs: req.RelatedIDs, ChangeSummary: strings.TrimSpace(req.ChangeSummary),
		EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, UpdatedBy: &userID,
	}
	if err := s.validateRevision(revision, tenantID, id); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) SubmitRevision(id, revisionID, tenantID, userID, version int64) (*models.GlossaryAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRevision(revision, tenantID, id); err != nil {
		return nil, err
	}
	if revision.EffectiveFrom == nil {
		return nil, fmt.Errorf("%w: effective_from is required before review", ErrInvalidStandardRevision)
	}
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) ReturnRevision(id, revisionID, tenantID, userID, version int64) (*models.GlossaryAggregate, error) {
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) PublishRevision(id, revisionID, tenantID, userID, version int64) (*models.GlossaryAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusInReview {
		return nil, ErrInvalidRevisionTransition
	}
	if err := s.validateRevision(revision, tenantID, id); err != nil {
		return nil, err
	}
	if err := s.repo.PublishRevision(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) WithdrawRevision(id, revisionID, tenantID, userID, version int64) (*models.GlossaryAggregate, error) {
	if err := s.repo.WithdrawPublished(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *GlossaryService) DeleteGlossary(id, tenantID int64) error {
	if err := s.repo.DeleteUnpublished(id, tenantID); err != nil {
		if errors.Is(err, repository.ErrGlossaryPublicationHistory) {
			return ErrGlossaryPublicationHistory
		}
		return err
	}
	return nil
}

func (s *GlossaryService) GetMappedElements(glossaryID, tenantID int64) ([]models.PublishedElementReference, error) {
	if _, err := s.repo.GetByID(glossaryID, tenantID); err != nil {
		return nil, err
	}
	return s.repo.GetMappedElements(glossaryID, tenantID)
}

func (s *GlossaryService) validateRevision(revision *models.GlossaryRevision, tenantID, glossaryID int64) error {
	if revision == nil || strings.TrimSpace(revision.Name) == "" || strings.TrimSpace(revision.Definition) == "" || strings.TrimSpace(revision.ChangeSummary) == "" {
		return ErrInvalidStandardRevision
	}
	if revision.EffectiveFrom != nil && revision.EffectiveTo != nil && !revision.EffectiveFrom.Before(*revision.EffectiveTo) {
		return fmt.Errorf("%w: effective_from must precede effective_to", ErrInvalidStandardRevision)
	}
	seen := map[int64]struct{}{}
	for _, relatedID := range revision.RelatedIDs {
		if relatedID <= 0 || relatedID == glossaryID {
			return fmt.Errorf("%w: related glossary must be a different positive identity", ErrInvalidStandardRevision)
		}
		seen[relatedID] = struct{}{}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return s.refs.RequireGlossaries(tenantID, ids)
}
