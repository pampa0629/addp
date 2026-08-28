package repository

import (
	"strings"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CodeSetRepository struct{ db *gorm.DB }

func NewCodeSetRepository(db *gorm.DB) *CodeSetRepository { return &CodeSetRepository{db: db} }

func (r *CodeSetRepository) Create(codeSet *models.CodeSet, revision *models.CodeSetRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(codeSet).Error; err != nil {
			return err
		}
		revision.CodeSetID, revision.RevisionNo, revision.Status = codeSet.ID, 1, models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		codeSet.DraftRevisionID = &revision.ID
		return tx.Model(&models.CodeSet{}).Where("id = ? AND tenant_id = ?", codeSet.ID, codeSet.TenantID).Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *CodeSetRepository) GetByID(id, tenantID int64) (*models.CodeSet, error) {
	var codeSet models.CodeSet
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&codeSet).Error
	return &codeSet, commonrepo.WrapDBError(err)
}

func (r *CodeSetRepository) GetAggregate(id, tenantID int64) (*models.CodeSetAggregate, error) {
	identity, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.CodeSetAggregate{CodeSet: *identity}
	if identity.CurrentRevisionID != nil {
		revision, loadErr := r.getRevision(r.db, *identity.CurrentRevisionID, identity.ID, true)
		if loadErr != nil {
			return nil, loadErr
		}
		result.CurrentRevision = revision
	}
	if identity.DraftRevisionID != nil {
		revision, loadErr := r.getRevision(r.db, *identity.DraftRevisionID, identity.ID, true)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	return result, nil
}

func (r *CodeSetRepository) List(tenantID int64, domainID *int64, keyword, status string, page, pageSize int) ([]models.CodeSetAggregate, int64, error) {
	query := r.db.Model(&models.CodeSet{}).Where("code_sets.tenant_id = ?", tenantID)
	if domainID != nil {
		query = query.Where("code_sets.domain_id = ?", *domainID)
	}
	if status != "" {
		query = query.Joins("JOIN standard.code_set_revisions status_revision ON status_revision.code_set_id = code_sets.id AND status_revision.status = ?", status)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`code_sets.code ILIKE ? OR EXISTS (
			SELECT 1 FROM standard.code_set_revisions csr
			WHERE csr.code_set_id = code_sets.id AND (csr.name ILIKE ? OR csr.description ILIKE ?)
		)`, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Distinct("code_sets.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var identities []models.CodeSet
	if err := query.Distinct("code_sets.*").Order("code_sets.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&identities).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.CodeSetAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, err := r.GetAggregate(identity.ID, tenantID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *CodeSetRepository) UpdateIdentity(codeSet *models.CodeSet, expectedVersion int64) error {
	if err := updateVersioned(r.db, codeSet, codeSet.ID, codeSet.TenantID, expectedVersion, map[string]interface{}{
		"domain_id": codeSet.DomainID, "steward_id": codeSet.StewardID, "tags": codeSet.Tags, "updated_by": codeSet.UpdatedBy,
	}); err != nil {
		return err
	}
	codeSet.Version = expectedVersion + 1
	return nil
}

func (r *CodeSetRepository) ListRevisions(codeSetID, tenantID int64) ([]models.CodeSetRevision, error) {
	if _, err := r.GetByID(codeSetID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.CodeSetRevision
	if err := r.db.Where("code_set_id = ?", codeSetID).Order("revision_no DESC").Find(&revisions).Error; err != nil {
		return nil, err
	}
	for index := range revisions {
		if err := r.loadItems(r.db, &revisions[index]); err != nil {
			return nil, err
		}
	}
	return revisions, nil
}

func (r *CodeSetRepository) GetRevision(codeSetID, revisionID, tenantID int64) (*models.CodeSetRevision, error) {
	var revision models.CodeSetRevision
	err := r.db.Table("standard.code_set_revisions AS csr").Select("csr.*").
		Joins("JOIN standard.code_sets cs ON cs.id = csr.code_set_id").
		Where("csr.id = ? AND csr.code_set_id = ? AND cs.tenant_id = ?", revisionID, codeSetID, tenantID).First(&revision).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if err := r.loadItems(r.db, &revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *CodeSetRepository) GetPublishedRevision(revisionID, tenantID int64) (*models.CodeSetRevision, error) {
	var revision models.CodeSetRevision
	err := r.db.Table("standard.code_set_revisions AS csr").Select("csr.*").
		Joins("JOIN standard.code_sets cs ON cs.id = csr.code_set_id AND cs.current_revision_id = csr.id").
		Where("csr.id = ? AND cs.tenant_id = ? AND cs.lifecycle_state = ? AND csr.status = ?", revisionID, tenantID, "active", models.RevisionStatusPublished).
		First(&revision).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if err := r.loadItems(r.db, &revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *CodeSetRepository) CreateDraft(codeSetID, tenantID, userID, expectedVersion int64, changeSummary string) (*models.CodeSetRevision, error) {
	var created models.CodeSetRevision
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var identity models.CodeSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", codeSetID, tenantID).First(&identity).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if identity.Version != expectedVersion {
			return ErrVersionConflict
		}
		if identity.Origin == models.CodeSetOriginPlatform {
			return ErrRevisionNotEditable
		}
		if identity.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.CodeSetRevision
		if identity.CurrentRevisionID != nil {
			if err := tx.Where("id = ? AND code_set_id = ?", *identity.CurrentRevisionID, identity.ID).First(&source).Error; err != nil {
				return err
			}
		} else if err := tx.Where("code_set_id = ?", identity.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		created = source
		created.ID, created.RevisionNo, created.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		created.ChangeSummary = changeSummary
		created.SubmittedBy, created.SubmittedAt, created.PublishedBy, created.PublishedAt = nil, nil, nil, nil
		created.CreatedBy, created.UpdatedBy, created.CreatedAt, created.UpdatedAt = userID, nil, time.Time{}, time.Time{}
		created.Items = nil
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		var sourceItems []models.CodeSetRevisionItem
		if err := tx.Where("code_set_revision_id = ?", source.ID).Order("sort_order ASC, id ASC").Find(&sourceItems).Error; err != nil {
			return err
		}
		for _, item := range sourceItems {
			item.ID, item.CodeSetRevisionID, item.ReplacementItemID = 0, created.ID, nil
			item.CreatedAt, item.UpdatedAt = time.Time{}, time.Time{}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return updateVersioned(tx, &models.CodeSet{}, identity.ID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": created.ID, "updated_by": userID})
	}))
	return &created, err
}

func (r *CodeSetRepository) UpdateDraft(codeSetID, revisionID, tenantID, userID, expectedVersion int64, revision *models.CodeSetRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, codeSetID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := r.requireTenantMutable(tx, codeSetID, tenantID); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.CodeSetRevision{}).Where("id = ? AND code_set_id = ? AND status = ?", revisionID, codeSetID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"name": revision.Name, "description": revision.Description, "value_type": revision.ValueType,
			"change_summary": revision.ChangeSummary, "effective_from": revision.EffectiveFrom, "effective_to": revision.EffectiveTo, "updated_by": userID,
		}))
	}))
}

func (r *CodeSetRepository) TransitionRevision(codeSetID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireTenantMutable(tx, codeSetID, tenantID); err != nil {
			return err
		}
		if err := r.requireRevisionState(tx, codeSetID, revisionID, tenantID, from, from != models.RevisionStatusPublished); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
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
		return requireAffectedRow(tx.Model(&models.CodeSetRevision{}).Where("id = ? AND code_set_id = ? AND status = ?", revisionID, codeSetID, from).Updates(updates))
	}))
}

func (r *CodeSetRepository) PublishRevision(codeSetID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var identity models.CodeSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", codeSetID, tenantID).First(&identity).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if identity.Version != expectedVersion {
			return ErrVersionConflict
		}
		if identity.Origin == models.CodeSetOriginPlatform || identity.DraftRevisionID == nil || *identity.DraftRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		var itemCount int64
		if err := tx.Model(&models.CodeSetRevisionItem{}).Where("code_set_revision_id = ? AND status = ?", revisionID, models.CodeItemStatusActive).Count(&itemCount).Error; err != nil {
			return err
		}
		if itemCount == 0 {
			return ErrInvalidRevisionTransition
		}
		if identity.CurrentRevisionID != nil {
			if err := requireAffectedRow(tx.Model(&models.CodeSetRevision{}).Where("id = ? AND status = ?", *identity.CurrentRevisionID, models.RevisionStatusPublished).Update("status", models.RevisionStatusSuperseded)); err != nil {
				return err
			}
		}
		if err := requireAffectedRow(tx.Model(&models.CodeSetRevision{}).Where("id = ? AND code_set_id = ? AND status = ?", revisionID, codeSetID, models.RevisionStatusInReview).Updates(map[string]interface{}{
			"status": models.RevisionStatusPublished, "published_by": userID, "published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID,
		})); err != nil {
			return ErrInvalidRevisionTransition
		}
		return updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{"current_revision_id": revisionID, "draft_revision_id": nil, "updated_by": userID})
	}))
}

func (r *CodeSetRepository) WithdrawPublished(codeSetID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var identity models.CodeSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", codeSetID, tenantID).First(&identity).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if identity.Version != expectedVersion {
			return ErrVersionConflict
		}
		if identity.Origin == models.CodeSetOriginPlatform || identity.CurrentRevisionID == nil || *identity.CurrentRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		if err := requireAffectedRow(tx.Model(&models.CodeSetRevision{}).Where("id = ? AND code_set_id = ? AND status = ?", revisionID, codeSetID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn)); err != nil {
			return ErrInvalidRevisionTransition
		}
		return updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{"current_revision_id": nil, "updated_by": userID})
	}))
}

func (r *CodeSetRepository) CreateItem(codeSetID, revisionID, tenantID int64, item *models.CodeSetRevisionItem, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireTenantMutable(tx, codeSetID, tenantID); err != nil {
			return err
		}
		if err := r.requireRevisionState(tx, codeSetID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		item.CodeSetRevisionID = revisionID
		return tx.Create(item).Error
	}))
}

func (r *CodeSetRepository) UpdateItem(codeSetID, revisionID, itemID, tenantID int64, item *models.CodeSetRevisionItem, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireTenantMutable(tx, codeSetID, tenantID); err != nil {
			return err
		}
		if err := r.requireRevisionState(tx, codeSetID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.CodeSetRevisionItem{}).Where("id = ? AND code_set_revision_id = ?", itemID, revisionID).Updates(map[string]interface{}{
			"label": item.Label, "definition": item.Definition, "sort_order": item.SortOrder, "status": item.Status, "replacement_item_id": item.ReplacementItemID,
		}))
	}))
}

func (r *CodeSetRepository) DeleteItem(codeSetID, revisionID, itemID, tenantID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireTenantMutable(tx, codeSetID, tenantID); err != nil {
			return err
		}
		if err := r.requireRevisionState(tx, codeSetID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, codeSetID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Where("id = ? AND code_set_revision_id = ?", itemID, revisionID).Delete(&models.CodeSetRevisionItem{}))
	}))
}

func (r *CodeSetRepository) ExistsByCode(tenantID int64, code string, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.CodeSet{}).Where("tenant_id = ? AND code = ?", tenantID, code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func (r *CodeSetRepository) ExistsItemByCode(revisionID int64, code string, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.CodeSetRevisionItem{}).Where("code_set_revision_id = ? AND code = ?", revisionID, code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func (r *CodeSetRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.CodeSet{}, "id = ? AND tenant_id = ?", id, tenantID)
}

func (r *CodeSetRepository) getRevision(db *gorm.DB, revisionID, codeSetID int64, withItems bool) (*models.CodeSetRevision, error) {
	var revision models.CodeSetRevision
	if err := db.Where("id = ? AND code_set_id = ?", revisionID, codeSetID).First(&revision).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if withItems {
		if err := r.loadItems(db, &revision); err != nil {
			return nil, err
		}
	}
	return &revision, nil
}

func (r *CodeSetRepository) loadItems(db *gorm.DB, revision *models.CodeSetRevision) error {
	return db.Where("code_set_revision_id = ?", revision.ID).Order("sort_order ASC, id ASC").Find(&revision.Items).Error
}

func (r *CodeSetRepository) requireTenantMutable(tx *gorm.DB, codeSetID, tenantID int64) error {
	var count int64
	if err := tx.Model(&models.CodeSet{}).Where("id = ? AND tenant_id = ? AND origin = ?", codeSetID, tenantID, models.CodeSetOriginTenant).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrRevisionNotEditable
	}
	return nil
}

func (r *CodeSetRepository) requireRevisionState(tx *gorm.DB, codeSetID, revisionID, tenantID int64, status string, requireDraftPointer bool) error {
	query := tx.Table("standard.code_set_revisions AS csr").Joins("JOIN standard.code_sets cs ON cs.id = csr.code_set_id").
		Where("csr.id = ? AND csr.code_set_id = ? AND cs.tenant_id = ? AND csr.status = ?", revisionID, codeSetID, tenantID, status)
	if requireDraftPointer {
		query = query.Where("cs.draft_revision_id = csr.id")
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
