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

var (
	ErrDraftAlreadyExists        = errors.New("standard draft revision already exists")
	ErrRevisionNotEditable       = errors.New("standard revision is not editable")
	ErrInvalidRevisionTransition = errors.New("invalid standard revision transition")
)

type ElementRepository struct{ db *gorm.DB }

func NewElementRepository(db *gorm.DB) *ElementRepository { return &ElementRepository{db: db} }

func (r *ElementRepository) Create(element *models.Element, revision *models.ElementRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(element).Error; err != nil {
			return err
		}
		revision.ElementID = element.ID
		revision.RevisionNo = 1
		revision.Status = models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		element.DraftRevisionID = &revision.ID
		return tx.Model(&models.Element{}).Where("id = ? AND tenant_id = ?", element.ID, element.TenantID).
			Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *ElementRepository) GetByID(id, tenantID int64) (*models.Element, error) {
	var element models.Element
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&element).Error
	return &element, commonrepo.WrapDBError(err)
}

func (r *ElementRepository) GetAggregate(id, tenantID int64) (*models.ElementAggregate, error) {
	element, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.ElementAggregate{Element: *element}
	if element.CurrentRevisionID != nil {
		revision, loadErr := r.getRevisionByID(r.db, *element.CurrentRevisionID, element.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		result.CurrentRevision = revision
	}
	if element.DraftRevisionID != nil {
		revision, loadErr := r.getRevisionByID(r.db, *element.DraftRevisionID, element.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	return result, nil
}

type ListElementOptions struct {
	DomainID *int64
	IDs      []int64
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

func (r *ElementRepository) List(tenantID int64, opts ListElementOptions) ([]models.ElementAggregate, int64, error) {
	query := r.db.Model(&models.Element{}).Where("elements.tenant_id = ?", tenantID)
	if opts.DomainID != nil {
		query = query.Where("elements.domain_id = ?", *opts.DomainID)
	}
	if len(opts.IDs) > 0 {
		query = query.Where("elements.id IN ?", opts.IDs)
	}
	if opts.Status != "" {
		query = query.Joins("JOIN standard.element_revisions status_revision ON status_revision.element_id = elements.id AND status_revision.status = ?", opts.Status)
	}
	if keyword := strings.TrimSpace(opts.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`elements.code ILIKE ? OR EXISTS (
			SELECT 1 FROM standard.element_revisions er
			WHERE er.element_id = elements.id AND (er.name ILIKE ? OR er.definition ILIKE ?)
		)`, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Distinct("elements.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	var identities []models.Element
	err := query.Distinct("elements.*").Order("elements.created_at DESC").Offset((opts.Page - 1) * opts.PageSize).Limit(opts.PageSize).Find(&identities).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]models.ElementAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, loadErr := r.GetAggregate(identity.ID, tenantID)
		if loadErr != nil {
			return nil, 0, loadErr
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *ElementRepository) UpdateIdentity(element *models.Element, expectedVersion int64) error {
	if err := updateVersioned(r.db, element, element.ID, element.TenantID, expectedVersion, map[string]interface{}{
		"domain_id": element.DomainID, "steward_id": element.StewardID, "tags": element.Tags, "updated_by": element.UpdatedBy,
	}); err != nil {
		return err
	}
	element.Version = expectedVersion + 1
	return nil
}

func (r *ElementRepository) ListRevisions(elementID, tenantID int64) ([]models.ElementRevision, error) {
	if _, err := r.GetByID(elementID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.ElementRevision
	err := r.db.Where("element_id = ?", elementID).Order("revision_no DESC").Find(&revisions).Error
	return revisions, wrapDBError(err)
}

func (r *ElementRepository) GetRevision(elementID, revisionID, tenantID int64) (*models.ElementRevision, error) {
	var revision models.ElementRevision
	err := r.db.Table("standard.element_revisions AS er").Select("er.*").
		Joins("JOIN standard.elements e ON e.id = er.element_id").
		Where("er.id = ? AND er.element_id = ? AND e.tenant_id = ?", revisionID, elementID, tenantID).
		First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *ElementRepository) CreateDraft(elementID, tenantID, userID, expectedVersion int64, changeSummary string) (*models.ElementRevision, error) {
	var created models.ElementRevision
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var element models.Element
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", elementID, tenantID).First(&element).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if element.Version != expectedVersion {
			return ErrVersionConflict
		}
		if element.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.ElementRevision
		if element.CurrentRevisionID != nil {
			if err := tx.Where("id = ? AND element_id = ?", *element.CurrentRevisionID, element.ID).First(&source).Error; err != nil {
				return err
			}
		} else if err := tx.Where("element_id = ?", element.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		created = source
		created.ID = 0
		created.RevisionNo = source.RevisionNo + 1
		created.Status = models.RevisionStatusDraft
		created.ChangeSummary = changeSummary
		created.SubmittedBy, created.SubmittedAt, created.PublishedBy, created.PublishedAt = nil, nil, nil, nil
		created.CreatedBy, created.UpdatedBy = userID, nil
		created.CreatedAt, created.UpdatedAt = time.Time{}, time.Time{}
		created.CompiledQualityRules = nil
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return updateVersioned(tx, &models.Element{}, element.ID, tenantID, expectedVersion, map[string]interface{}{
			"draft_revision_id": created.ID, "updated_by": userID,
		})
	}))
	return &created, err
}

func (r *ElementRepository) UpdateDraft(elementID, revisionID, tenantID, userID, expectedVersion int64, revision *models.ElementRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, elementID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Element{}, elementID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.ElementRevision{}).Where("id = ? AND element_id = ? AND status = ?", revisionID, elementID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"name": revision.Name, "definition": revision.Definition, "data_type": revision.DataType,
			"length": revision.Length, "precision_num": revision.PrecisionNum, "scale": revision.Scale,
			"nullable": revision.Nullable, "default_value": revision.DefaultValue, "format": revision.Format,
			"value_domain_kind": revision.ValueDomainKind, "range_constraint": revision.RangeConstraint,
			"code_set_revision_id": revision.CodeSetRevisionID, "unit_id": revision.UnitID,
			"security_level": revision.SecurityLevel, "classification_id": revision.ClassificationID,
			"example_values": revision.ExampleValues, "extra_quality_rules": revision.ExtraQualityRules,
			"change_summary": revision.ChangeSummary, "effective_from": revision.EffectiveFrom,
			"effective_to": revision.EffectiveTo, "updated_by": userID,
		}))
	}))
}

func (r *ElementRepository) TransitionRevision(elementID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, elementID, revisionID, tenantID, from, from != models.RevisionStatusPublished); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Element{}, elementID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": to, "updated_by": userID}
		if to == models.RevisionStatusInReview {
			updates["submitted_by"] = userID
			updates["submitted_at"] = gorm.Expr("CURRENT_TIMESTAMP")
		}
		if to == models.RevisionStatusDraft {
			updates["submitted_by"] = nil
			updates["submitted_at"] = nil
		}
		return requireAffectedRow(tx.Model(&models.ElementRevision{}).Where("id = ? AND element_id = ? AND status = ?", revisionID, elementID, from).Updates(updates))
	}))
}

func (r *ElementRepository) PublishRevision(elementID, revisionID, tenantID, userID, expectedVersion int64, compiled models.JSONB) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var element models.Element
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", elementID, tenantID).First(&element).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if element.Version != expectedVersion {
			return ErrVersionConflict
		}
		if element.DraftRevisionID == nil || *element.DraftRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		var revision models.ElementRevision
		if err := tx.Where("id = ? AND element_id = ? AND status = ?", revisionID, elementID, models.RevisionStatusInReview).First(&revision).Error; err != nil {
			return ErrInvalidRevisionTransition
		}
		if element.CurrentRevisionID != nil {
			if err := requireAffectedRow(tx.Model(&models.ElementRevision{}).Where("id = ? AND element_id = ? AND status = ?", *element.CurrentRevisionID, elementID, models.RevisionStatusPublished).Update("status", models.RevisionStatusSuperseded)); err != nil {
				return err
			}
		}
		if err := requireAffectedRow(tx.Model(&models.ElementRevision{}).Where("id = ? AND element_id = ? AND status = ?", revisionID, elementID, models.RevisionStatusInReview).Updates(map[string]interface{}{
			"status": models.RevisionStatusPublished, "compiled_quality_rules": compiled,
			"published_by": userID, "published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID,
		})); err != nil {
			return err
		}
		return updateVersioned(tx, &models.Element{}, elementID, tenantID, expectedVersion, map[string]interface{}{
			"current_revision_id": revisionID, "draft_revision_id": nil, "updated_by": userID,
		})
	}))
}

func (r *ElementRepository) WithdrawPublished(elementID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var element models.Element
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", elementID, tenantID).First(&element).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if element.Version != expectedVersion {
			return ErrVersionConflict
		}
		if element.CurrentRevisionID == nil || *element.CurrentRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		if err := requireAffectedRow(tx.Model(&models.ElementRevision{}).Where("id = ? AND element_id = ? AND status = ?", revisionID, elementID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn)); err != nil {
			return ErrInvalidRevisionTransition
		}
		return updateVersioned(tx, &models.Element{}, elementID, tenantID, expectedVersion, map[string]interface{}{"current_revision_id": nil, "updated_by": userID})
	}))
}

func (r *ElementRepository) GetPublishedRevision(elementID, tenantID int64) (*models.ElementRevision, error) {
	var revision models.ElementRevision
	err := r.db.Table("standard.element_revisions AS er").Select("er.*").
		Joins("JOIN standard.elements e ON e.current_revision_id = er.id").
		Where("e.id = ? AND e.tenant_id = ? AND e.lifecycle_state = ? AND er.status = ?", elementID, tenantID, "active", models.RevisionStatusPublished).
		First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *ElementRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.Element{}, "id = ? AND tenant_id = ?", id, tenantID)
}
func (r *ElementRepository) DeleteTx(tx *gorm.DB, id, tenantID int64) error {
	return requireAffectedRow(tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Element{}))
}

func (r *ElementRepository) ExistsByCode(code string, tenantID, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.Element{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func (r *ElementRepository) getRevisionByID(db *gorm.DB, id, elementID int64) (*models.ElementRevision, error) {
	var revision models.ElementRevision
	err := db.Where("id = ? AND element_id = ?", id, elementID).First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *ElementRepository) requireRevisionState(tx *gorm.DB, elementID, revisionID, tenantID int64, status string, requireDraftPointer bool) error {
	query := tx.Table("standard.element_revisions AS er").
		Joins("JOIN standard.elements e ON e.id = er.element_id").
		Where("er.id = ? AND er.element_id = ? AND e.tenant_id = ? AND er.status = ?", revisionID, elementID, tenantID, status)
	if requireDraftPointer {
		query = query.Where("e.draft_revision_id = er.id")
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
