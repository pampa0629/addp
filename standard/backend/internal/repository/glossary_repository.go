package repository

import (
	"errors"
	"strings"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GlossaryRepository struct{ db *gorm.DB }

var ErrGlossaryPublicationHistory = errors.New("glossary publication history exists")

func NewGlossaryRepository(db *gorm.DB) *GlossaryRepository { return &GlossaryRepository{db: db} }

func (r *GlossaryRepository) Create(glossary *models.Glossary, revision *models.GlossaryRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(glossary).Error; err != nil {
			return err
		}
		revision.GlossaryID, revision.RevisionNo, revision.Status = glossary.ID, 1, models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		glossary.DraftRevisionID = &revision.ID
		return tx.Model(&models.Glossary{}).Where("id = ? AND tenant_id = ?", glossary.ID, glossary.TenantID).
			Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *GlossaryRepository) GetByID(id, tenantID int64) (*models.Glossary, error) {
	var glossary models.Glossary
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&glossary).Error
	return &glossary, commonrepo.WrapDBError(err)
}

func (r *GlossaryRepository) GetAggregate(id, tenantID int64) (*models.GlossaryAggregate, error) {
	return r.GetAggregateAt(id, tenantID, time.Time{})
}

func (r *GlossaryRepository) GetAggregateAt(id, tenantID int64, asOf time.Time) (*models.GlossaryAggregate, error) {
	glossary, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.GlossaryAggregate{Glossary: *glossary}
	if revision, loadErr := r.getEffectiveRevision(r.db, glossary.ID, asOf); loadErr == nil {
		result.CurrentRevision = revision
	} else if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return nil, loadErr
	}
	if glossary.DraftRevisionID != nil {
		revision, loadErr := r.getRevisionByID(r.db, *glossary.DraftRevisionID, glossary.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	if result.CurrentRevision != nil {
		result.HasPublicationHistory = true
	} else {
		var count int64
		if err := r.db.Model(&models.GlossaryRevision{}).
			Where("glossary_id = ? AND status IN ?", glossary.ID, []string{models.RevisionStatusPublished, models.RevisionStatusWithdrawn}).
			Count(&count).Error; err != nil {
			return nil, err
		}
		result.HasPublicationHistory = count > 0
	}
	return result, nil
}

type ListGlossaryOptions struct {
	OwnerDomainID *int64
	ElementID     *int64
	ScopeType     string
	Status        string
	Keyword       string
	Page          int
	PageSize      int
	AsOf          time.Time
}

func (r *GlossaryRepository) List(tenantID int64, opts ListGlossaryOptions) ([]models.GlossaryAggregate, int64, error) {
	query := r.db.Model(&models.Glossary{}).Where("glossaries.tenant_id = ?", tenantID)
	if opts.OwnerDomainID != nil {
		query = query.Where("glossaries.owner_domain_id = ?", *opts.OwnerDomainID)
	}
	if opts.ElementID != nil {
		query = query.Joins("JOIN standard.glossary_element_mappings element_mapping ON element_mapping.glossary_id = glossaries.id AND element_mapping.element_id = ?", *opts.ElementID)
	}
	if opts.ScopeType != "" {
		query = query.Where("glossaries.scope_type = ?", opts.ScopeType)
	}
	if opts.Status != "" {
		query = query.Joins("JOIN standard.glossary_revisions status_revision ON status_revision.glossary_id = glossaries.id AND status_revision.status = ?", opts.Status)
	}
	if keyword := strings.TrimSpace(opts.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`glossaries.code ILIKE ? OR EXISTS (
			SELECT 1 FROM standard.glossary_revisions gr
			WHERE gr.glossary_id = glossaries.id AND (gr.name ILIKE ? OR gr.definition ILIKE ?)
		)`, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Distinct("glossaries.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	var identities []models.Glossary
	err := query.Distinct("glossaries.*").Order("glossaries.created_at DESC").Offset((opts.Page - 1) * opts.PageSize).Limit(opts.PageSize).Find(&identities).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]models.GlossaryAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, loadErr := r.GetAggregateAt(identity.ID, tenantID, opts.AsOf)
		if loadErr != nil {
			return nil, 0, loadErr
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *GlossaryRepository) UpdateIdentity(glossary *models.Glossary, expectedVersion int64) error {
	if err := updateVersioned(r.db, glossary, glossary.ID, glossary.TenantID, expectedVersion, map[string]interface{}{
		"scope_type": glossary.ScopeType, "owner_domain_id": glossary.OwnerDomainID,
		"steward_id": glossary.StewardID, "tags": glossary.Tags, "updated_by": glossary.UpdatedBy,
	}); err != nil {
		return err
	}
	glossary.Version = expectedVersion + 1
	return nil
}

func (r *GlossaryRepository) UpdateElements(glossaryID, tenantID, userID, expectedVersion int64, elementIDs []int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.Glossary{}, glossaryID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		if err := tx.Where("glossary_id = ?", glossaryID).Delete(&models.GlossaryElementMapping{}).Error; err != nil {
			return err
		}
		for _, elementID := range uniqueInt64s(elementIDs) {
			if err := tx.Create(&models.GlossaryElementMapping{GlossaryID: glossaryID, ElementID: elementID}).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *GlossaryRepository) ListRevisions(glossaryID, tenantID int64) ([]models.GlossaryRevision, error) {
	if _, err := r.GetByID(glossaryID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.GlossaryRevision
	err := r.db.Where("glossary_id = ?", glossaryID).Order("revision_no DESC").Find(&revisions).Error
	return revisions, wrapDBError(err)
}

func (r *GlossaryRepository) GetRevision(glossaryID, revisionID, tenantID int64) (*models.GlossaryRevision, error) {
	var revision models.GlossaryRevision
	err := r.db.Table("standard.glossary_revisions AS gr").Select("gr.*").
		Joins("JOIN standard.glossaries g ON g.id = gr.glossary_id").
		Where("gr.id = ? AND gr.glossary_id = ? AND g.tenant_id = ?", revisionID, glossaryID, tenantID).
		First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *GlossaryRepository) CreateDraft(glossaryID, tenantID, userID, expectedVersion int64, changeSummary string) (*models.GlossaryRevision, error) {
	var created models.GlossaryRevision
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var glossary models.Glossary
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", glossaryID, tenantID).First(&glossary).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if glossary.Version != expectedVersion {
			return ErrVersionConflict
		}
		if glossary.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.GlossaryRevision
		if err := tx.Where("glossary_id = ?", glossary.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		created = source
		created.ID, created.RevisionNo, created.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		created.ChangeSummary = changeSummary
		created.SubmittedBy, created.SubmittedAt, created.PublishedBy, created.PublishedAt = nil, nil, nil, nil
		created.CreatedBy, created.UpdatedBy = userID, nil
		created.CreatedAt, created.UpdatedAt = time.Time{}, time.Time{}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return updateVersioned(tx, &models.Glossary{}, glossary.ID, tenantID, expectedVersion, map[string]interface{}{
			"draft_revision_id": created.ID, "updated_by": userID,
		})
	}))
	return &created, err
}

func (r *GlossaryRepository) UpdateDraft(glossaryID, revisionID, tenantID, userID, expectedVersion int64, revision *models.GlossaryRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, glossaryID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Glossary{}, glossaryID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.GlossaryRevision{}).Where("id = ? AND glossary_id = ? AND status = ?", revisionID, glossaryID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"name": revision.Name, "alias": revision.Alias, "definition": revision.Definition,
			"example": revision.Example, "note": revision.Note, "related_ids": revision.RelatedIDs,
			"change_summary": revision.ChangeSummary, "effective_from": revision.EffectiveFrom,
			"effective_to": revision.EffectiveTo, "updated_by": userID,
		}))
	}))
}

func (r *GlossaryRepository) TransitionRevision(glossaryID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, glossaryID, revisionID, tenantID, from, from != models.RevisionStatusPublished); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Glossary{}, glossaryID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": to, "updated_by": userID}
		if to == models.RevisionStatusInReview {
			updates["submitted_by"], updates["submitted_at"] = userID, gorm.Expr("CURRENT_TIMESTAMP")
		}
		if to == models.RevisionStatusDraft {
			updates["submitted_by"], updates["submitted_at"] = nil, nil
		}
		return requireAffectedRow(tx.Model(&models.GlossaryRevision{}).Where("id = ? AND glossary_id = ? AND status = ?", revisionID, glossaryID, from).Updates(updates))
	}))
}

func (r *GlossaryRepository) PublishRevision(glossaryID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var glossary models.Glossary
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", glossaryID, tenantID).First(&glossary).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if glossary.Version != expectedVersion {
			return ErrVersionConflict
		}
		if glossary.DraftRevisionID == nil || *glossary.DraftRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		var revision models.GlossaryRevision
		if err := tx.Where("id = ? AND glossary_id = ? AND status = ?", revisionID, glossaryID, models.RevisionStatusInReview).First(&revision).Error; err != nil || revision.EffectiveFrom == nil {
			return ErrInvalidRevisionTransition
		}
		var published []models.GlossaryRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("glossary_id = ? AND status = ?", glossaryID, models.RevisionStatusPublished).Order("effective_from ASC, revision_no ASC").Find(&published).Error; err != nil {
			return err
		}
		for index := range published {
			candidate := &published[index]
			if candidate.EffectiveFrom == nil {
				return ErrEffectiveIntervalConflict
			}
			if candidate.EffectiveTo == nil && candidate.EffectiveFrom.Before(*revision.EffectiveFrom) {
				if err := tx.Model(&models.GlossaryRevision{}).Where("id = ? AND status = ?", candidate.ID, models.RevisionStatusPublished).Update("effective_to", revision.EffectiveFrom).Error; err != nil {
					return err
				}
				closed := *revision.EffectiveFrom
				candidate.EffectiveTo = &closed
			}
			if intervalsOverlap(*candidate.EffectiveFrom, candidate.EffectiveTo, *revision.EffectiveFrom, revision.EffectiveTo) {
				return ErrEffectiveIntervalConflict
			}
		}
		if err := requireAffectedRow(tx.Model(&models.GlossaryRevision{}).Where("id = ? AND glossary_id = ? AND status = ?", revisionID, glossaryID, models.RevisionStatusInReview).Updates(map[string]interface{}{
			"status": models.RevisionStatusPublished, "published_by": userID,
			"published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID,
		})); err != nil {
			return err
		}
		return updateVersioned(tx, &models.Glossary{}, glossaryID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": nil, "updated_by": userID})
	}))
}

func (r *GlossaryRepository) WithdrawPublished(glossaryID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.Glossary{}, glossaryID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		if err := requireAffectedRow(tx.Model(&models.GlossaryRevision{}).Where("id = ? AND glossary_id = ? AND status = ?", revisionID, glossaryID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn)); err != nil {
			return ErrInvalidRevisionTransition
		}
		return nil
	}))
}

func (r *GlossaryRepository) DeleteUnpublished(id, tenantID int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var glossary models.Glossary
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&glossary).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		var count int64
		if err := tx.Model(&models.GlossaryRevision{}).Where("glossary_id = ? AND status IN ?", id, []string{models.RevisionStatusPublished, models.RevisionStatusWithdrawn}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrGlossaryPublicationHistory
		}
		return requireAffectedRow(tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Glossary{}))
	}))
}

func (r *GlossaryRepository) ExistsByCode(code string, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.Glossary{}).Where("code = ? AND tenant_id = ?", code, tenantID).Count(&count).Error
	return count > 0, err
}

func (r *GlossaryRepository) GetMappedElements(glossaryID, tenantID int64) ([]models.PublishedElementReference, error) {
	var elements []models.PublishedElementReference
	asOf := time.Now().UTC()
	err := r.db.Raw(`
		SELECT e.id, e.tenant_id, e.scope_type, e.owner_domain_id, e.code, e.lifecycle_state, e.version,
			er.id AS revision_id, er.revision_no, er.name, er.status
		FROM standard.elements e
		INNER JOIN standard.glossary_element_mappings gem ON gem.element_id = e.id
		INNER JOIN standard.element_revisions er ON er.element_id = e.id
			AND er.status = 'published'
			AND er.effective_from <= ?
			AND (er.effective_to IS NULL OR er.effective_to > ?)
		WHERE gem.glossary_id = ? AND e.tenant_id = ? AND e.lifecycle_state = 'active'
	`, asOf, asOf, glossaryID, tenantID).Scan(&elements).Error
	return elements, err
}

func (r *GlossaryRepository) getRevisionByID(db *gorm.DB, id, glossaryID int64) (*models.GlossaryRevision, error) {
	var revision models.GlossaryRevision
	err := db.Where("id = ? AND glossary_id = ?", id, glossaryID).First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *GlossaryRepository) getEffectiveRevision(db *gorm.DB, glossaryID int64, asOf time.Time) (*models.GlossaryRevision, error) {
	var revision models.GlossaryRevision
	query := db.Table("standard.glossary_revisions AS gr").Select("gr.*").Where("gr.glossary_id = ?", glossaryID)
	err := effectiveAt(query, "gr", asOf).Order("gr.effective_from DESC, gr.revision_no DESC").First(&revision).Error
	return &revision, err
}

func (r *GlossaryRepository) requireRevisionState(tx *gorm.DB, glossaryID, revisionID, tenantID int64, status string, requireDraftPointer bool) error {
	query := tx.Table("standard.glossary_revisions AS gr").Joins("JOIN standard.glossaries g ON g.id = gr.glossary_id").
		Where("gr.id = ? AND gr.glossary_id = ? AND g.tenant_id = ? AND gr.status = ?", revisionID, glossaryID, tenantID, status)
	if requireDraftPointer {
		query = query.Where("g.draft_revision_id = gr.id")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidRevisionTransition
	}
	return nil
}
