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

type StandardCollectionRepository struct{ db *gorm.DB }

func NewStandardCollectionRepository(db *gorm.DB) *StandardCollectionRepository {
	return &StandardCollectionRepository{db: db}
}

func (r *StandardCollectionRepository) Create(collection *models.StandardCollection, revision *models.StandardCollectionRevision, members []models.StandardCollectionMember) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(collection).Error; err != nil {
			return err
		}
		revision.CollectionID, revision.RevisionNo, revision.Status = collection.ID, 1, models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		if err := createCollectionMembers(tx, revision.ID, revision.CreatedBy, members); err != nil {
			return err
		}
		if err := tx.Create(&models.StandardCollectionAssignment{
			CollectionID: collection.ID, PrincipalID: collection.CreatedBy, Role: models.CollectionAssignmentOwner,
			CreatedBy: collection.CreatedBy,
		}).Error; err != nil {
			return err
		}
		if err := appendCollectionEvent(tx, collection.ID, &revision.ID, models.CollectionEventCreated, collection.CreatedBy, models.JSONB{
			"revision_no":  revision.RevisionNo,
			"member_count": len(members),
		}); err != nil {
			return err
		}
		collection.DraftRevisionID = &revision.ID
		return tx.Model(&models.StandardCollection{}).Where("id = ? AND tenant_id = ?", collection.ID, collection.TenantID).
			Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *StandardCollectionRepository) ExistsByCode(code string, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.StandardCollection{}).Where("tenant_id = ? AND code = ?", tenantID, code).Count(&count).Error
	return count > 0, err
}

func (r *StandardCollectionRepository) GetByID(id, tenantID int64) (*models.StandardCollection, error) {
	var collection models.StandardCollection
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&collection).Error
	return &collection, commonrepo.WrapDBError(err)
}

func (r *StandardCollectionRepository) GetAggregate(id, tenantID, principalID int64) (*models.StandardCollectionAggregate, error) {
	collection, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.StandardCollectionAggregate{StandardCollection: *collection, Assignments: []models.StandardCollectionAssignment{}, MyRoles: []string{}}
	if collection.DraftRevisionID != nil {
		revision, loadErr := r.getRevision(r.db, collection.ID, *collection.DraftRevisionID)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	var current models.StandardCollectionRevision
	if loadErr := r.db.Where("collection_id = ? AND status = ?", collection.ID, models.RevisionStatusPublished).
		Order("revision_no DESC").First(&current).Error; loadErr == nil {
		if err := r.loadMembers(r.db, &current); err != nil {
			return nil, err
		}
		result.CurrentRevision = &current
	} else if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return nil, loadErr
	}
	if err := r.db.Where("collection_id = ?", collection.ID).Order("role ASC, principal_id ASC").Find(&result.Assignments).Error; err != nil {
		return nil, err
	}
	for _, assignment := range result.Assignments {
		if assignment.PrincipalID == principalID {
			result.MyRoles = append(result.MyRoles, assignment.Role)
		}
	}
	return result, nil
}

func (r *StandardCollectionRepository) List(tenantID, principalID int64, keyword, status string, page, pageSize int) ([]models.StandardCollectionAggregate, int64, error) {
	query := r.db.Model(&models.StandardCollection{}).Where("tenant_id = ?", tenantID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`(code ILIKE ? OR EXISTS (
			SELECT 1 FROM standard.standard_collection_revisions revision
			WHERE revision.collection_id = standard_collections.id AND (revision.name ILIKE ? OR revision.description ILIKE ?)
		))`, pattern, pattern, pattern)
	}
	if status != "" {
		query = query.Where(`(EXISTS (
			SELECT 1 FROM standard.standard_collection_revisions revision
			WHERE revision.id = standard_collections.draft_revision_id AND revision.status = ?
		) OR (standard_collections.draft_revision_id IS NULL AND EXISTS (
			SELECT 1 FROM standard.standard_collection_revisions revision
			WHERE revision.collection_id = standard_collections.id AND revision.status = ?
		)))`, status, status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var identities []models.StandardCollection
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&identities).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.StandardCollectionAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, err := r.GetAggregate(identity.ID, tenantID, principalID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *StandardCollectionRepository) ListRevisions(collectionID, tenantID int64) ([]models.StandardCollectionRevision, error) {
	if _, err := r.GetByID(collectionID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.StandardCollectionRevision
	if err := r.db.Where("collection_id = ?", collectionID).Order("revision_no DESC").Find(&revisions).Error; err != nil {
		return nil, err
	}
	for index := range revisions {
		if err := r.loadMembers(r.db, &revisions[index]); err != nil {
			return nil, err
		}
	}
	return revisions, nil
}

func (r *StandardCollectionRepository) ListEvents(collectionID, tenantID int64, page, pageSize int) ([]models.StandardCollectionEvent, int64, error) {
	if _, err := r.GetByID(collectionID, tenantID); err != nil {
		return nil, 0, err
	}
	query := r.db.Model(&models.StandardCollectionEvent{}).Where("collection_id = ?", collectionID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []models.StandardCollectionEvent
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func (r *StandardCollectionRepository) UpdateDraft(collectionID, revisionID, tenantID, userID, expectedVersion int64, revision *models.StandardCollectionRevision, members []models.StandardCollectionMember) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.lockCollectionDraft(tx, collectionID, revisionID, tenantID, expectedVersion, models.RevisionStatusDraft); err != nil {
			return err
		}
		if err := tx.Model(&models.StandardCollectionRevision{}).Where("id = ? AND collection_id = ? AND status = ?", revisionID, collectionID, models.RevisionStatusDraft).
			Updates(map[string]any{"name": revision.Name, "description": revision.Description, "change_summary": revision.ChangeSummary, "updated_by": userID}).Error; err != nil {
			return err
		}
		if err := tx.Where("collection_revision_id = ?", revisionID).Delete(&models.StandardCollectionMember{}).Error; err != nil {
			return err
		}
		if err := createCollectionMembers(tx, revisionID, userID, members); err != nil {
			return err
		}
		if err := appendCollectionEvent(tx, collectionID, &revisionID, models.CollectionEventDraftUpdated, userID, models.JSONB{
			"member_count": len(members),
		}); err != nil {
			return err
		}
		return updateVersioned(tx, &models.StandardCollection{}, collectionID, tenantID, expectedVersion, map[string]any{"updated_by": userID})
	}))
}

func (r *StandardCollectionRepository) CreateDraft(collectionID, tenantID, userID, expectedVersion int64, changeSummary string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var collection models.StandardCollection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", collectionID, tenantID).First(&collection).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if collection.Version != expectedVersion {
			return ErrVersionConflict
		}
		if collection.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.StandardCollectionRevision
		if err := tx.Where("collection_id = ?", collectionID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		if err := r.loadMembers(tx, &source); err != nil {
			return err
		}
		created := source
		created.ID, created.RevisionNo, created.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		created.ChangeSummary = changeSummary
		created.SubmittedBy, created.SubmittedAt, created.PublishedBy, created.PublishedAt = nil, nil, nil, nil
		created.CreatedBy, created.UpdatedBy, created.CreatedAt, created.UpdatedAt, created.Members = userID, nil, time.Time{}, time.Time{}, nil
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if err := createCollectionMembers(tx, created.ID, userID, source.Members); err != nil {
			return err
		}
		if err := appendCollectionEvent(tx, collectionID, &created.ID, models.CollectionEventDraftCreated, userID, models.JSONB{
			"revision_no":  created.RevisionNo,
			"member_count": len(source.Members),
		}); err != nil {
			return err
		}
		return updateVersioned(tx, &models.StandardCollection{}, collectionID, tenantID, expectedVersion, map[string]any{"draft_revision_id": created.ID, "updated_by": userID})
	}))
}

func (r *StandardCollectionRepository) Transition(collectionID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.lockCollectionDraft(tx, collectionID, revisionID, tenantID, expectedVersion, from); err != nil {
			return err
		}
		updates := map[string]any{"status": to, "updated_by": userID}
		if to == models.RevisionStatusInReview {
			updates["submitted_by"], updates["submitted_at"] = userID, gorm.Expr("CURRENT_TIMESTAMP")
		}
		if to == models.RevisionStatusDraft {
			updates["submitted_by"], updates["submitted_at"] = nil, nil
		}
		if err := requireAffectedRow(tx.Model(&models.StandardCollectionRevision{}).Where("id = ? AND collection_id = ? AND status = ?", revisionID, collectionID, from).Updates(updates)); err != nil {
			return ErrInvalidRevisionTransition
		}
		eventType := models.CollectionEventSubmitted
		if to == models.RevisionStatusDraft {
			eventType = models.CollectionEventReturned
		}
		if err := appendCollectionEvent(tx, collectionID, &revisionID, eventType, userID, models.JSONB{"from": from, "to": to}); err != nil {
			return err
		}
		return updateVersioned(tx, &models.StandardCollection{}, collectionID, tenantID, expectedVersion, map[string]any{"updated_by": userID})
	}))
}

func (r *StandardCollectionRepository) Publish(collectionID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.lockCollectionDraft(tx, collectionID, revisionID, tenantID, expectedVersion, models.RevisionStatusInReview); err != nil {
			return err
		}
		if err := tx.Model(&models.StandardCollectionRevision{}).Where("collection_id = ? AND status = ?", collectionID, models.RevisionStatusPublished).
			Updates(map[string]any{"status": models.RevisionStatusWithdrawn, "updated_by": userID}).Error; err != nil {
			return err
		}
		if err := requireAffectedRow(tx.Model(&models.StandardCollectionRevision{}).Where("id = ? AND collection_id = ? AND status = ?", revisionID, collectionID, models.RevisionStatusInReview).
			Updates(map[string]any{"status": models.RevisionStatusPublished, "published_by": userID, "published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID})); err != nil {
			return ErrInvalidRevisionTransition
		}
		if err := appendCollectionEvent(tx, collectionID, &revisionID, models.CollectionEventPublished, userID, models.JSONB{
			"from": models.RevisionStatusInReview,
			"to":   models.RevisionStatusPublished,
		}); err != nil {
			return err
		}
		return updateVersioned(tx, &models.StandardCollection{}, collectionID, tenantID, expectedVersion, map[string]any{"draft_revision_id": nil, "updated_by": userID})
	}))
}

func (r *StandardCollectionRepository) ReplaceAssignments(collectionID, tenantID, userID, expectedVersion int64, assignments []models.StandardCollectionAssignment) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var collection models.StandardCollection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", collectionID, tenantID).First(&collection).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if collection.Version != expectedVersion {
			return ErrVersionConflict
		}
		if err := tx.Where("collection_id = ?", collectionID).Delete(&models.StandardCollectionAssignment{}).Error; err != nil {
			return err
		}
		for index := range assignments {
			assignments[index].CollectionID, assignments[index].CreatedBy = collectionID, userID
		}
		if len(assignments) > 0 {
			if err := tx.Create(&assignments).Error; err != nil {
				return err
			}
		}
		snapshot := make([]map[string]any, 0, len(assignments))
		for _, assignment := range assignments {
			snapshot = append(snapshot, map[string]any{"principal_id": assignment.PrincipalID, "role": assignment.Role})
		}
		if err := appendCollectionEvent(tx, collectionID, nil, models.CollectionEventAssignmentsReplaced, userID, models.JSONB{
			"assignments": snapshot,
		}); err != nil {
			return err
		}
		return updateVersioned(tx, &models.StandardCollection{}, collectionID, tenantID, expectedVersion, map[string]any{"updated_by": userID})
	}))
}

func (r *StandardCollectionRepository) HasRole(collectionID, tenantID, principalID int64, roles ...string) (bool, error) {
	var count int64
	err := r.db.Table("standard.standard_collection_assignments assignment").
		Joins("JOIN standard.standard_collections collection ON collection.id = assignment.collection_id").
		Where("assignment.collection_id = ? AND collection.tenant_id = ? AND assignment.principal_id = ? AND assignment.role IN ?", collectionID, tenantID, principalID, roles).
		Count(&count).Error
	return count > 0, err
}

func (r *StandardCollectionRepository) CountReviewersExcept(collectionID, principalID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.StandardCollectionAssignment{}).Where("collection_id = ? AND role = ? AND principal_id <> ?", collectionID, models.CollectionAssignmentReviewer, principalID).Count(&count).Error
	return count, err
}

func (r *StandardCollectionRepository) GetRevision(collectionID, revisionID, tenantID int64) (*models.StandardCollectionRevision, error) {
	if _, err := r.GetByID(collectionID, tenantID); err != nil {
		return nil, err
	}
	return r.getRevision(r.db, collectionID, revisionID)
}

func (r *StandardCollectionRepository) Delete(collectionID, tenantID, principalID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var published int64
		if err := tx.Model(&models.StandardCollectionRevision{}).Where("collection_id = ? AND status = ?", collectionID, models.RevisionStatusPublished).Count(&published).Error; err != nil {
			return err
		}
		if published > 0 {
			return ErrRevisionNotEditable
		}
		return requireAffectedRow(tx.Where("id = ? AND tenant_id = ? AND version = ?", collectionID, tenantID, expectedVersion).Delete(&models.StandardCollection{}))
	}))
}

func (r *StandardCollectionRepository) lockCollectionDraft(tx *gorm.DB, collectionID, revisionID, tenantID, expectedVersion int64, status string) error {
	var collection models.StandardCollection
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", collectionID, tenantID).First(&collection).Error; err != nil {
		return commonrepo.WrapDBError(err)
	}
	if collection.Version != expectedVersion {
		return ErrVersionConflict
	}
	if collection.DraftRevisionID == nil || *collection.DraftRevisionID != revisionID {
		return ErrInvalidRevisionTransition
	}
	var count int64
	if err := tx.Model(&models.StandardCollectionRevision{}).Where("id = ? AND collection_id = ? AND status = ?", revisionID, collectionID, status).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidRevisionTransition
	}
	return nil
}

func (r *StandardCollectionRepository) getRevision(db *gorm.DB, collectionID, revisionID int64) (*models.StandardCollectionRevision, error) {
	var revision models.StandardCollectionRevision
	if err := db.Where("id = ? AND collection_id = ?", revisionID, collectionID).First(&revision).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if err := r.loadMembers(db, &revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *StandardCollectionRepository) loadMembers(db *gorm.DB, revision *models.StandardCollectionRevision) error {
	return db.Where("collection_revision_id = ?", revision.ID).Order("member_type ASC, member_id ASC").Find(&revision.Members).Error
}

func createCollectionMembers(tx *gorm.DB, revisionID, userID int64, members []models.StandardCollectionMember) error {
	if len(members) == 0 {
		return nil
	}
	created := make([]models.StandardCollectionMember, len(members))
	copy(created, members)
	for index := range created {
		created[index].ID, created[index].CollectionRevisionID, created[index].CreatedBy, created[index].CreatedAt = 0, revisionID, userID, time.Time{}
		created[index].Name, created[index].Code = "", ""
	}
	return tx.Create(&created).Error
}

func appendCollectionEvent(tx *gorm.DB, collectionID int64, revisionID *int64, eventType string, actorID int64, detail models.JSONB) error {
	return tx.Create(&models.StandardCollectionEvent{
		CollectionID: collectionID,
		RevisionID:   revisionID,
		EventType:    eventType,
		ActorID:      actorID,
		Detail:       detail,
	}).Error
}
