package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DocumentRepository struct{ db *gorm.DB }

var ErrDocumentPublicationHistory = errors.New("document publication history exists")

func NewDocumentRepository(db *gorm.DB) *DocumentRepository { return &DocumentRepository{db: db} }

type ListDocumentOptions struct {
	OwnerDomainID *int64
	ScopeType     string
	DocType       string
	Status        string
	Keyword       string
	Page          int
	PageSize      int
	AsOf          time.Time
}

func (r *DocumentRepository) Create(document *models.Document, revision *models.DocumentRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(document).Error; err != nil {
			return err
		}
		revision.DocumentID, revision.RevisionNo, revision.Status = document.ID, 1, models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		document.DraftRevisionID = &revision.ID
		return tx.Model(&models.Document{}).Where("id = ? AND tenant_id = ?", document.ID, document.TenantID).Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *DocumentRepository) GetByID(id, tenantID int64) (*models.Document, error) {
	var document models.Document
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&document).Error
	return &document, commonrepo.WrapDBError(err)
}

func (r *DocumentRepository) ExistsByCode(code string, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.Document{}).Where("code = ? AND tenant_id = ?", code, tenantID).Count(&count).Error
	return count > 0, err
}

func (r *DocumentRepository) GetAggregate(id, tenantID int64) (*models.DocumentAggregate, error) {
	return r.GetAggregateAt(id, tenantID, time.Time{})
}

func (r *DocumentRepository) GetAggregateAt(id, tenantID int64, asOf time.Time) (*models.DocumentAggregate, error) {
	document, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.DocumentAggregate{Document: *document}
	if revision, loadErr := r.getEffectiveRevision(r.db, document.ID, asOf); loadErr == nil {
		result.CurrentRevision = revision
	} else if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return nil, loadErr
	}
	if document.DraftRevisionID != nil {
		revision, loadErr := r.getRevisionByID(r.db, *document.DraftRevisionID, document.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	if result.CurrentRevision != nil {
		result.HasPublicationHistory = true
	} else {
		var count int64
		if err := r.db.Model(&models.DocumentRevision{}).Where("document_id = ? AND status IN ?", document.ID, []string{models.RevisionStatusPublished, models.RevisionStatusWithdrawn}).Count(&count).Error; err != nil {
			return nil, err
		}
		result.HasPublicationHistory = count > 0
	}
	return result, nil
}

func (r *DocumentRepository) List(tenantID int64, opts ListDocumentOptions) ([]models.DocumentAggregate, int64, error) {
	query := r.db.Model(&models.Document{}).Where("documents.tenant_id = ?", tenantID)
	if opts.OwnerDomainID != nil {
		query = query.Where("documents.owner_domain_id = ?", *opts.OwnerDomainID)
	}
	if opts.ScopeType != "" {
		query = query.Where("documents.scope_type = ?", opts.ScopeType)
	}
	if opts.DocType != "" {
		query = query.Where("documents.doc_type = ?", opts.DocType)
	}
	if opts.Status != "" {
		query = query.Joins("JOIN standard.document_revisions status_revision ON status_revision.document_id = documents.id AND status_revision.status = ?", opts.Status)
	}
	if keyword := strings.TrimSpace(opts.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`documents.code ILIKE ? OR documents.source_org ILIKE ? OR EXISTS (
			SELECT 1 FROM standard.document_revisions dr WHERE dr.document_id = documents.id AND (dr.name ILIKE ? OR dr.description ILIKE ?)
		)`, pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Distinct("documents.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	var identities []models.Document
	if err := query.Distinct("documents.*").Order("documents.created_at DESC").Offset((opts.Page - 1) * opts.PageSize).Limit(opts.PageSize).Find(&identities).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.DocumentAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, err := r.GetAggregateAt(identity.ID, tenantID, opts.AsOf)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *DocumentRepository) UpdateIdentity(document *models.Document, expectedVersion int64) error {
	if err := updateVersioned(r.db, document, document.ID, document.TenantID, expectedVersion, map[string]interface{}{
		"scope_type": document.ScopeType, "owner_domain_id": document.OwnerDomainID, "doc_type": document.DocType,
		"source_org": document.SourceOrg, "steward_id": document.StewardID, "tags": document.Tags, "updated_by": document.UpdatedBy,
	}); err != nil {
		return err
	}
	document.Version = expectedVersion + 1
	return nil
}

func (r *DocumentRepository) ListRevisions(documentID, tenantID int64) ([]models.DocumentRevision, error) {
	if _, err := r.GetByID(documentID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.DocumentRevision
	err := r.db.Where("document_id = ?", documentID).Order("revision_no DESC").Find(&revisions).Error
	return revisions, wrapDBError(err)
}

func (r *DocumentRepository) GetRevision(documentID, revisionID, tenantID int64) (*models.DocumentRevision, error) {
	var revision models.DocumentRevision
	err := r.db.Table("standard.document_revisions AS dr").Select("dr.*").Joins("JOIN standard.documents d ON d.id = dr.document_id").
		Where("dr.id = ? AND dr.document_id = ? AND d.tenant_id = ?", revisionID, documentID, tenantID).First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *DocumentRepository) CreateDraft(documentID, tenantID, userID, expectedVersion int64, changeSummary string) (*models.DocumentRevision, error) {
	var created models.DocumentRevision
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var document models.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if document.Version != expectedVersion {
			return ErrVersionConflict
		}
		if document.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.DocumentRevision
		if err := tx.Where("document_id = ?", document.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		created = source
		created.ID, created.RevisionNo, created.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		created.FileKey, created.FileName, created.FileSize, created.MediaType, created.ContentSHA256 = "", "", 0, "", ""
		created.ChangeSummary = changeSummary
		created.SubmittedBy, created.SubmittedAt, created.PublishedBy, created.PublishedAt = nil, nil, nil, nil
		created.CreatedBy, created.UpdatedBy, created.CreatedAt, created.UpdatedAt = userID, nil, time.Time{}, time.Time{}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return updateVersioned(tx, &models.Document{}, document.ID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": created.ID, "updated_by": userID})
	}))
	return &created, err
}

func (r *DocumentRepository) UpdateDraft(documentID, revisionID, tenantID, userID, expectedVersion int64, revision *models.DocumentRevision) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, documentID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.DocumentRevision{}).Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"name": revision.Name, "version_label": revision.VersionLabel, "publish_date": revision.PublishDate,
			"description": revision.Description, "change_summary": revision.ChangeSummary,
			"effective_from": revision.EffectiveFrom, "effective_to": revision.EffectiveTo, "updated_by": userID,
		}))
	}))
}

func (r *DocumentRepository) ReplaceDraftFile(documentID, revisionID, tenantID, userID, expectedVersion int64, fileKey, fileName string, fileSize int64, mediaType, sha256 string) (*models.DocumentFileCleanup, error) {
	var cleanup *models.DocumentFileCleanup
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, documentID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		var revision models.DocumentRevision
		if err := tx.Where("id = ? AND document_id = ?", revisionID, documentID).First(&revision).Error; err != nil {
			return err
		}
		var err error
		cleanup, err = enqueueDocumentFileCleanup(tx, revision.FileKey)
		if err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.DocumentRevision{}).Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"file_key": fileKey, "file_name": fileName, "file_size": fileSize, "media_type": mediaType, "content_sha256": sha256, "updated_by": userID,
		}))
	}))
	if err != nil {
		return nil, err
	}
	return cleanup, nil
}

func (r *DocumentRepository) TransitionRevision(documentID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, documentID, revisionID, tenantID, from, from != models.RevisionStatusPublished); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": to, "updated_by": userID}
		if to == models.RevisionStatusInReview {
			updates["submitted_by"], updates["submitted_at"] = userID, gorm.Expr("CURRENT_TIMESTAMP")
		}
		if to == models.RevisionStatusDraft {
			updates["submitted_by"], updates["submitted_at"] = nil, nil
		}
		return requireAffectedRow(tx.Model(&models.DocumentRevision{}).Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, from).Updates(updates))
	}))
}

func (r *DocumentRepository) PublishRevision(documentID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var document models.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if document.Version != expectedVersion {
			return ErrVersionConflict
		}
		if document.DraftRevisionID == nil || *document.DraftRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		var revision models.DocumentRevision
		if err := tx.Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, models.RevisionStatusInReview).First(&revision).Error; err != nil || revision.EffectiveFrom == nil {
			return ErrInvalidRevisionTransition
		}
		var published []models.DocumentRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("document_id = ? AND status = ?", documentID, models.RevisionStatusPublished).Order("effective_from ASC, revision_no ASC").Find(&published).Error; err != nil {
			return err
		}
		for index := range published {
			candidate := &published[index]
			if candidate.EffectiveFrom == nil {
				return ErrEffectiveIntervalConflict
			}
			if candidate.EffectiveTo == nil && candidate.EffectiveFrom.Before(*revision.EffectiveFrom) {
				if err := tx.Model(&models.DocumentRevision{}).Where("id = ? AND status = ?", candidate.ID, models.RevisionStatusPublished).Update("effective_to", revision.EffectiveFrom).Error; err != nil {
					return err
				}
				closed := *revision.EffectiveFrom
				candidate.EffectiveTo = &closed
			}
			if intervalsOverlap(*candidate.EffectiveFrom, candidate.EffectiveTo, *revision.EffectiveFrom, revision.EffectiveTo) {
				return ErrEffectiveIntervalConflict
			}
		}
		if err := requireAffectedRow(tx.Model(&models.DocumentRevision{}).Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, models.RevisionStatusInReview).Updates(map[string]interface{}{
			"status": models.RevisionStatusPublished, "published_by": userID, "published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID,
		})); err != nil {
			return err
		}
		return updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": nil, "updated_by": userID})
	}))
}

func (r *DocumentRepository) WithdrawPublished(documentID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		if err := requireAffectedRow(tx.Model(&models.DocumentRevision{}).Where("id = ? AND document_id = ? AND status = ?", revisionID, documentID, models.RevisionStatusPublished).Updates(map[string]interface{}{"status": models.RevisionStatusWithdrawn, "updated_by": userID})); err != nil {
			return ErrInvalidRevisionTransition
		}
		return nil
	}))
}

func (r *DocumentRepository) DeleteUnpublished(id, tenantID int64) ([]models.DocumentFileCleanup, error) {
	var cleanups []models.DocumentFileCleanup
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var document models.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&document).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		var count int64
		if err := tx.Model(&models.DocumentRevision{}).Where("document_id = ? AND status IN ?", id, []string{models.RevisionStatusPublished, models.RevisionStatusWithdrawn}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrDocumentPublicationHistory
		}
		var revisions []models.DocumentRevision
		if err := tx.Where("document_id = ?", id).Find(&revisions).Error; err != nil {
			return err
		}
		for _, revision := range revisions {
			cleanup, err := enqueueDocumentFileCleanup(tx, revision.FileKey)
			if err != nil {
				return err
			}
			if cleanup != nil {
				cleanups = append(cleanups, *cleanup)
			}
		}
		return requireAffectedRow(tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Document{}))
	}))
	return cleanups, err
}

func (r *DocumentRepository) CreateExtraction(documentID, tenantID, expectedVersion int64, extraction *models.DocumentExtraction) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.Document{}, documentID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		candidates := extraction.Candidates
		extraction.Candidates = nil
		if err := tx.Create(extraction).Error; err != nil {
			return err
		}
		for index := range candidates {
			evidences := candidates[index].Evidences
			candidates[index].Evidences = nil
			candidates[index].ExtractionID = extraction.ID
			if err := tx.Create(&candidates[index]).Error; err != nil {
				return err
			}
			for evidenceIndex := range evidences {
				evidences[evidenceIndex].CandidateID = candidates[index].ID
				if err := tx.Create(&evidences[evidenceIndex]).Error; err != nil {
					return err
				}
			}
			candidates[index].Evidences = evidences
		}
		extraction.Candidates = candidates
		return nil
	}))
}

func (r *DocumentRepository) ListExtractions(documentID, tenantID int64) ([]models.DocumentExtraction, error) {
	if _, err := r.GetByID(documentID, tenantID); err != nil {
		return nil, err
	}
	var extractions []models.DocumentExtraction
	err := r.db.Table("standard.document_extractions AS extraction").Select("extraction.*").
		Joins("JOIN standard.document_revisions revision ON revision.id = extraction.document_revision_id").
		Where("revision.document_id = ? AND extraction.tenant_id = ?", documentID, tenantID).
		Order("extraction.id DESC").Preload("Candidates", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).Preload("Candidates.Evidences", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).Find(&extractions).Error
	return extractions, wrapDBError(err)
}

type DocumentCandidateComparisonTarget struct {
	CandidateType      string
	StandardID         int64
	Code               string
	ScopeType          string
	OwnerDomainID      *int64
	RevisionID         int64
	RevisionNo         int64
	RevisionStatus     string
	Name               string
	Definition         string
	DataType           string
	ValueDomainKind    string
	CodeSetRevisionID  *int64
	CodeSetCode        string
	StatisticalCaliber string
	SemanticFormula    string
	UnitID             *int64
	UnitName           string
	UnitSymbol         string
	Items              []models.DocumentExtractionCandidateComparisonItem
}

type documentCandidateComparisonRevision struct {
	ID                 int64
	RevisionNo         int64
	Status             string
	Name               string
	Definition         string
	DataType           string
	ValueDomainKind    string
	CodeSetRevisionID  *int64
	StatisticalCaliber string
	SemanticFormula    string
	UnitID             *int64
	EffectiveFrom      *time.Time
	EffectiveTo        *time.Time
}

func (r *DocumentRepository) ListCandidateComparisonTargets(tenantID int64, codesByType map[string][]string) (map[string]DocumentCandidateComparisonTarget, error) {
	targets := map[string]DocumentCandidateComparisonTarget{}
	now := time.Now().UTC()
	if err := r.loadGlossaryComparisonTargets(tenantID, uniqueStrings(codesByType["glossary"]), now, targets); err != nil {
		return nil, err
	}
	if err := r.loadElementComparisonTargets(tenantID, uniqueStrings(codesByType["element"]), now, targets); err != nil {
		return nil, err
	}
	if err := r.loadCodeSetComparisonTargets(tenantID, uniqueStrings(codesByType["code_set"]), now, targets); err != nil {
		return nil, err
	}
	if err := r.loadMetricComparisonTargets(tenantID, uniqueStrings(codesByType["metric"]), now, targets); err != nil {
		return nil, err
	}
	if err := r.loadComparisonCodeSets(tenantID, targets); err != nil {
		return nil, err
	}
	if err := r.loadComparisonUnits(targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (r *DocumentRepository) loadGlossaryComparisonTargets(tenantID int64, codes []string, now time.Time, targets map[string]DocumentCandidateComparisonTarget) error {
	if len(codes) == 0 {
		return nil
	}
	var identities []models.Glossary
	if err := r.db.Select("id, scope_type, owner_domain_id, code, draft_revision_id").Where("tenant_id = ? AND lifecycle_state = ? AND code IN ?", tenantID, "active", codes).Find(&identities).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.ID)
	}
	var revisions []models.GlossaryRevision
	if len(ids) > 0 {
		if err := r.db.Select("id, glossary_id, revision_no, status, name, definition, effective_from, effective_to").Where("glossary_id IN ?", ids).Order("glossary_id ASC, revision_no DESC").Find(&revisions).Error; err != nil {
			return err
		}
	}
	byIdentity := map[int64][]documentCandidateComparisonRevision{}
	for _, revision := range revisions {
		byIdentity[revision.GlossaryID] = append(byIdentity[revision.GlossaryID], documentCandidateComparisonRevision{ID: revision.ID, RevisionNo: revision.RevisionNo, Status: revision.Status, Name: revision.Name, Definition: revision.Definition, EffectiveFrom: revision.EffectiveFrom, EffectiveTo: revision.EffectiveTo})
	}
	for _, identity := range identities {
		revision, ok := selectDocumentCandidateComparisonRevision(identity.DraftRevisionID, byIdentity[identity.ID], now)
		if !ok {
			continue
		}
		targets[documentCandidateComparisonKey("glossary", identity.Code)] = comparisonTarget("glossary", identity.ID, identity.Code, identity.ScopeType, identity.OwnerDomainID, revision)
	}
	return nil
}

func (r *DocumentRepository) loadElementComparisonTargets(tenantID int64, codes []string, now time.Time, targets map[string]DocumentCandidateComparisonTarget) error {
	if len(codes) == 0 {
		return nil
	}
	var identities []models.Element
	if err := r.db.Select("id, scope_type, owner_domain_id, code, draft_revision_id").Where("tenant_id = ? AND lifecycle_state = ? AND code IN ?", tenantID, "active", codes).Find(&identities).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.ID)
	}
	var revisions []models.ElementRevision
	if len(ids) > 0 {
		if err := r.db.Select("id, element_id, revision_no, status, name, definition, data_type, value_domain_kind, code_set_revision_id, unit_id, effective_from, effective_to").Where("element_id IN ?", ids).Order("element_id ASC, revision_no DESC").Find(&revisions).Error; err != nil {
			return err
		}
	}
	byIdentity := map[int64][]documentCandidateComparisonRevision{}
	for _, revision := range revisions {
		byIdentity[revision.ElementID] = append(byIdentity[revision.ElementID], documentCandidateComparisonRevision{ID: revision.ID, RevisionNo: revision.RevisionNo, Status: revision.Status, Name: revision.Name, Definition: revision.Definition, DataType: revision.DataType, ValueDomainKind: revision.ValueDomainKind, CodeSetRevisionID: revision.CodeSetRevisionID, UnitID: revision.UnitID, EffectiveFrom: revision.EffectiveFrom, EffectiveTo: revision.EffectiveTo})
	}
	for _, identity := range identities {
		revision, ok := selectDocumentCandidateComparisonRevision(identity.DraftRevisionID, byIdentity[identity.ID], now)
		if !ok {
			continue
		}
		targets[documentCandidateComparisonKey("element", identity.Code)] = comparisonTarget("element", identity.ID, identity.Code, identity.ScopeType, identity.OwnerDomainID, revision)
	}
	return nil
}

func (r *DocumentRepository) loadCodeSetComparisonTargets(tenantID int64, codes []string, now time.Time, targets map[string]DocumentCandidateComparisonTarget) error {
	if len(codes) == 0 {
		return nil
	}
	var identities []models.CodeSet
	if err := r.db.Select("id, scope_type, owner_domain_id, code, draft_revision_id").Where("tenant_id = ? AND lifecycle_state = ? AND code IN ?", tenantID, "active", codes).Find(&identities).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.ID)
	}
	var revisions []models.CodeSetRevision
	if len(ids) > 0 {
		if err := r.db.Select("id, code_set_id, revision_no, status, name, description, value_type, effective_from, effective_to").Where("code_set_id IN ?", ids).Order("code_set_id ASC, revision_no DESC").Find(&revisions).Error; err != nil {
			return err
		}
	}
	byIdentity := map[int64][]documentCandidateComparisonRevision{}
	for _, revision := range revisions {
		byIdentity[revision.CodeSetID] = append(byIdentity[revision.CodeSetID], documentCandidateComparisonRevision{ID: revision.ID, RevisionNo: revision.RevisionNo, Status: revision.Status, Name: revision.Name, Definition: revision.Description, DataType: revision.ValueType, EffectiveFrom: revision.EffectiveFrom, EffectiveTo: revision.EffectiveTo})
	}
	revisionIDs := make([]int64, 0, len(identities))
	for _, identity := range identities {
		revision, ok := selectDocumentCandidateComparisonRevision(identity.DraftRevisionID, byIdentity[identity.ID], now)
		if !ok {
			continue
		}
		target := comparisonTarget("code_set", identity.ID, identity.Code, identity.ScopeType, identity.OwnerDomainID, revision)
		targets[documentCandidateComparisonKey("code_set", identity.Code)] = target
		revisionIDs = append(revisionIDs, revision.ID)
	}
	if len(revisionIDs) == 0 {
		return nil
	}
	var items []models.CodeSetRevisionItem
	if err := r.db.Select("id, code_set_revision_id, code, label, definition, sort_order").Where("code_set_revision_id IN ?", revisionIDs).Order("code_set_revision_id ASC, sort_order ASC, id ASC").Find(&items).Error; err != nil {
		return err
	}
	itemsByRevision := map[int64][]models.DocumentExtractionCandidateComparisonItem{}
	for _, item := range items {
		itemsByRevision[item.CodeSetRevisionID] = append(itemsByRevision[item.CodeSetRevisionID], models.DocumentExtractionCandidateComparisonItem{Code: item.Code, Name: item.Label, Definition: item.Definition})
	}
	for key, target := range targets {
		if target.CandidateType == "code_set" {
			target.Items = itemsByRevision[target.RevisionID]
			targets[key] = target
		}
	}
	return nil
}

func (r *DocumentRepository) loadMetricComparisonTargets(tenantID int64, codes []string, now time.Time, targets map[string]DocumentCandidateComparisonTarget) error {
	if len(codes) == 0 {
		return nil
	}
	var identities []models.MetricDefinition
	if err := r.db.Select("id, scope_type, owner_domain_id, code, draft_revision_id").Where("tenant_id = ? AND lifecycle_state = ? AND code IN ?", tenantID, "active", codes).Find(&identities).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.ID)
	}
	var revisions []models.MetricDefinitionRevision
	if len(ids) > 0 {
		if err := r.db.Select("id, metric_definition_id, revision_no, status, name, definition, statistical_caliber, semantic_formula, unit_id, effective_from, effective_to").Where("metric_definition_id IN ?", ids).Order("metric_definition_id ASC, revision_no DESC").Find(&revisions).Error; err != nil {
			return err
		}
	}
	byIdentity := map[int64][]documentCandidateComparisonRevision{}
	for _, revision := range revisions {
		byIdentity[revision.MetricDefinitionID] = append(byIdentity[revision.MetricDefinitionID], documentCandidateComparisonRevision{ID: revision.ID, RevisionNo: revision.RevisionNo, Status: revision.Status, Name: revision.Name, Definition: revision.Definition, StatisticalCaliber: revision.StatisticalCaliber, SemanticFormula: revision.SemanticFormula, UnitID: revision.UnitID, EffectiveFrom: revision.EffectiveFrom, EffectiveTo: revision.EffectiveTo})
	}
	for _, identity := range identities {
		revision, ok := selectDocumentCandidateComparisonRevision(identity.DraftRevisionID, byIdentity[identity.ID], now)
		if !ok {
			continue
		}
		targets[documentCandidateComparisonKey("metric", identity.Code)] = comparisonTarget("metric", identity.ID, identity.Code, identity.ScopeType, identity.OwnerDomainID, revision)
	}
	return nil
}

func (r *DocumentRepository) loadComparisonUnits(targets map[string]DocumentCandidateComparisonTarget) error {
	ids := make([]int64, 0)
	for _, target := range targets {
		if target.UnitID != nil {
			ids = append(ids, *target.UnitID)
		}
	}
	ids = uniqueInt64s(ids)
	if len(ids) == 0 {
		return nil
	}
	var units []models.Unit
	if err := r.db.Select("id, name, symbol").Where("id IN ?", ids).Find(&units).Error; err != nil {
		return err
	}
	byID := map[int64]models.Unit{}
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	for key, target := range targets {
		if target.UnitID != nil {
			if unit, ok := byID[*target.UnitID]; ok {
				target.UnitName, target.UnitSymbol = unit.Name, unit.Symbol
				targets[key] = target
			}
		}
	}
	return nil
}

func (r *DocumentRepository) loadComparisonCodeSets(tenantID int64, targets map[string]DocumentCandidateComparisonTarget) error {
	ids := make([]int64, 0)
	for _, target := range targets {
		if target.CodeSetRevisionID != nil {
			ids = append(ids, *target.CodeSetRevisionID)
		}
	}
	ids = uniqueInt64s(ids)
	if len(ids) == 0 {
		return nil
	}
	type codeSetReference struct {
		RevisionID int64
		Code       string
	}
	var references []codeSetReference
	if err := r.db.Table("standard.code_set_revisions AS revision").
		Select("revision.id AS revision_id, identity.code").
		Joins("JOIN standard.code_sets AS identity ON identity.id = revision.code_set_id").
		Where("revision.id IN ? AND identity.tenant_id = ?", ids, tenantID).
		Find(&references).Error; err != nil {
		return err
	}
	codeByRevisionID := make(map[int64]string, len(references))
	for _, reference := range references {
		codeByRevisionID[reference.RevisionID] = reference.Code
	}
	for key, target := range targets {
		if target.CodeSetRevisionID != nil {
			target.CodeSetCode = codeByRevisionID[*target.CodeSetRevisionID]
			targets[key] = target
		}
	}
	return nil
}

func comparisonTarget(candidateType string, standardID int64, code, scopeType string, ownerDomainID *int64, revision documentCandidateComparisonRevision) DocumentCandidateComparisonTarget {
	return DocumentCandidateComparisonTarget{CandidateType: candidateType, StandardID: standardID, Code: code, ScopeType: scopeType, OwnerDomainID: ownerDomainID, RevisionID: revision.ID, RevisionNo: revision.RevisionNo, RevisionStatus: revision.Status, Name: revision.Name, Definition: revision.Definition, DataType: revision.DataType, ValueDomainKind: revision.ValueDomainKind, CodeSetRevisionID: revision.CodeSetRevisionID, StatisticalCaliber: revision.StatisticalCaliber, SemanticFormula: revision.SemanticFormula, UnitID: revision.UnitID}
}

func selectDocumentCandidateComparisonRevision(draftRevisionID *int64, revisions []documentCandidateComparisonRevision, now time.Time) (documentCandidateComparisonRevision, bool) {
	if draftRevisionID != nil {
		for _, revision := range revisions {
			if revision.ID == *draftRevisionID {
				return revision, true
			}
		}
	}
	for _, revision := range revisions {
		if revision.Status == models.RevisionStatusPublished && revision.EffectiveFrom != nil && !revision.EffectiveFrom.After(now) && (revision.EffectiveTo == nil || revision.EffectiveTo.After(now)) {
			return revision, true
		}
	}
	if len(revisions) > 0 {
		return revisions[0], true
	}
	return documentCandidateComparisonRevision{}, false
}

func documentCandidateComparisonKey(candidateType, code string) string {
	return candidateType + "\x00" + code
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (r *DocumentRepository) UpdateCandidateStatus(candidateID, tenantID, userID, expectedVersion int64, status string) (*models.DocumentExtractionCandidate, error) {
	var candidate models.DocumentExtractionCandidate
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("standard.document_extraction_candidates AS candidate").Select("candidate.*").
			Joins("JOIN standard.document_extractions extraction ON extraction.id = candidate.extraction_id").
			Where("candidate.id = ? AND extraction.tenant_id = ?", candidateID, tenantID).First(&candidate).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if candidate.Version != expectedVersion {
			return ErrVersionConflict
		}
		now := time.Now().UTC()
		result := tx.Model(&models.DocumentExtractionCandidate{}).Where("id = ? AND version = ?", candidateID, expectedVersion).Updates(map[string]interface{}{
			"status": status, "reviewed_by": userID, "reviewed_at": now, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVersionConflict
		}
		candidate.Status, candidate.ReviewedBy, candidate.ReviewedAt, candidate.Version = status, &userID, &now, expectedVersion+1
		return tx.Where("candidate_id = ?", candidateID).Order("id ASC").Find(&candidate.Evidences).Error
	}))
	return &candidate, err
}

func (r *DocumentRepository) getRevisionByID(db *gorm.DB, id, documentID int64) (*models.DocumentRevision, error) {
	var revision models.DocumentRevision
	err := db.Where("id = ? AND document_id = ?", id, documentID).First(&revision).Error
	return &revision, commonrepo.WrapDBError(err)
}

func (r *DocumentRepository) getEffectiveRevision(db *gorm.DB, documentID int64, asOf time.Time) (*models.DocumentRevision, error) {
	var revision models.DocumentRevision
	query := db.Table("standard.document_revisions AS dr").Select("dr.*").Where("dr.document_id = ?", documentID)
	err := effectiveAt(query, "dr", asOf).Order("dr.effective_from DESC, dr.revision_no DESC").First(&revision).Error
	return &revision, err
}

func (r *DocumentRepository) requireRevisionState(tx *gorm.DB, documentID, revisionID, tenantID int64, status string, requireDraftPointer bool) error {
	query := tx.Table("standard.document_revisions AS dr").Joins("JOIN standard.documents d ON d.id = dr.document_id").Where("dr.id = ? AND dr.document_id = ? AND d.tenant_id = ? AND dr.status = ?", revisionID, documentID, tenantID, status)
	if requireDraftPointer {
		query = query.Where("d.draft_revision_id = dr.id")
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

func (r *DocumentRepository) EnqueueFileCleanup(objectKey string) (*models.DocumentFileCleanup, error) {
	var cleanup *models.DocumentFileCleanup
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var err error
		cleanup, err = enqueueDocumentFileCleanup(tx, objectKey)
		return err
	}))
	return cleanup, err
}

func enqueueDocumentFileCleanup(tx *gorm.DB, objectKey string) (*models.DocumentFileCleanup, error) {
	if objectKey == "" {
		return nil, nil
	}
	cleanup := &models.DocumentFileCleanup{ObjectKey: objectKey, NextAttemptAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "object_key"}}, DoNothing: true}).Create(cleanup).Error; err != nil {
		return nil, err
	}
	if cleanup.ID == 0 {
		if err := tx.Where("object_key = ?", objectKey).First(cleanup).Error; err != nil {
			return nil, err
		}
	}
	return cleanup, nil
}
func (r *DocumentRepository) ListDueFileCleanups(now time.Time, limit int) ([]models.DocumentFileCleanup, error) {
	var rows []models.DocumentFileCleanup
	err := r.db.Where("next_attempt_at <= ?", now).Order("next_attempt_at ASC, id ASC").Limit(limit).Find(&rows).Error
	return rows, wrapDBError(err)
}
func (r *DocumentRepository) CompleteFileCleanup(id int64) error {
	return requireAffectedRow(r.db.Delete(&models.DocumentFileCleanup{}, id))
}
func (r *DocumentRepository) FailFileCleanup(id int64, attempts int, next time.Time, last string) error {
	return requireAffectedRow(r.db.Model(&models.DocumentFileCleanup{}).Where("id = ?", id).Updates(map[string]interface{}{"attempts": attempts, "next_attempt_at": next, "last_error": last}))
}

func (r *DocumentRepository) GetElementMappings(docID, tenantID int64) ([]models.DocumentElementMapping, error) {
	var rows []models.DocumentElementMapping
	err := r.db.Model(&models.DocumentElementMapping{}).Select(`standard.document_element_mappings.*,
		COALESCE((SELECT er.name FROM standard.element_revisions er WHERE er.element_id=e.id ORDER BY CASE er.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, er.revision_no DESC LIMIT 1), e.code) AS name`).Joins("JOIN standard.elements e ON e.id = standard.document_element_mappings.element_id AND e.tenant_id = ?", tenantID).Where("standard.document_element_mappings.document_id = ?", docID).Find(&rows).Error
	return rows, err
}
func (r *DocumentRepository) GetGlossaryMappings(docID, tenantID int64) ([]models.DocumentGlossaryMapping, error) {
	var rows []models.DocumentGlossaryMapping
	err := r.db.Model(&models.DocumentGlossaryMapping{}).Select(`standard.document_glossary_mappings.*,
		COALESCE((SELECT gr.name FROM standard.glossary_revisions gr WHERE gr.glossary_id=g.id ORDER BY CASE gr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, gr.revision_no DESC LIMIT 1), g.code) AS name`).Joins("JOIN standard.glossaries g ON g.id = standard.document_glossary_mappings.glossary_id AND g.tenant_id = ?", tenantID).Where("standard.document_glossary_mappings.document_id = ?", docID).Find(&rows).Error
	return rows, err
}
func (r *DocumentRepository) GetMetricMappings(docID, tenantID int64) ([]models.DocumentMetricMapping, error) {
	var rows []models.DocumentMetricMapping
	err := r.db.Model(&models.DocumentMetricMapping{}).Select(`standard.document_metric_mappings.*,
		COALESCE((SELECT mr.name FROM standard.metric_definition_revisions mr WHERE mr.metric_definition_id=m.id ORDER BY CASE mr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, mr.revision_no DESC LIMIT 1), m.code) AS name`).Joins("JOIN standard.metric_definitions m ON m.id = standard.document_metric_mappings.metric_id AND m.tenant_id = ?", tenantID).Where("standard.document_metric_mappings.document_id = ?", docID).Find(&rows).Error
	return rows, err
}

func (r *DocumentRepository) SetMappings(docID, tenantID, expectedVersion int64, elementIDs, glossaryIDs, metricIDs []int64, locations map[string]string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.Document{}, docID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		for _, model := range []interface{}{&models.DocumentElementMapping{}, &models.DocumentGlossaryMapping{}, &models.DocumentMetricMapping{}} {
			if err := tx.Where("document_id = ?", docID).Delete(model).Error; err != nil {
				return err
			}
		}
		for _, id := range uniqueInt64s(elementIDs) {
			if err := tx.Create(&models.DocumentElementMapping{DocumentID: docID, ElementID: id, ReferenceLocation: locations[fmt.Sprintf("element_%d", id)]}).Error; err != nil {
				return err
			}
		}
		for _, id := range uniqueInt64s(glossaryIDs) {
			if err := tx.Create(&models.DocumentGlossaryMapping{DocumentID: docID, GlossaryID: id, ReferenceLocation: locations[fmt.Sprintf("glossary_%d", id)]}).Error; err != nil {
				return err
			}
		}
		for _, id := range uniqueInt64s(metricIDs) {
			if err := tx.Create(&models.DocumentMetricMapping{DocumentID: docID, MetricID: id, ReferenceLocation: locations[fmt.Sprintf("metric_%d", id)]}).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *DocumentRepository) CreateWithMappingVersioned(document *models.Document, revision *models.DocumentRevision, mapping interface{}, parent interface{}, parentID, tenantID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, parent, parentID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		if err := tx.Create(document).Error; err != nil {
			return err
		}
		revision.DocumentID, revision.RevisionNo, revision.Status = document.ID, 1, models.RevisionStatusDraft
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		document.DraftRevisionID = &revision.ID
		if err := tx.Model(&models.Document{}).Where("id = ?", document.ID).Update("draft_revision_id", revision.ID).Error; err != nil {
			return err
		}
		switch value := mapping.(type) {
		case *models.DocumentElementMapping:
			value.DocumentID = document.ID
		case *models.DocumentGlossaryMapping:
			value.DocumentID = document.ID
		case *models.DocumentMetricMapping:
			value.DocumentID = document.ID
		default:
			return fmt.Errorf("unsupported document mapping type %T", mapping)
		}
		return tx.Create(mapping).Error
	}))
}

func (r *DocumentRepository) MutateMappingVersioned(parent interface{}, parentID, tenantID, expectedVersion int64, mapping interface{}, add bool, query string, args ...interface{}) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, parent, parentID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		if add {
			return tx.Where(query, args...).FirstOrCreate(mapping).Error
		}
		return requireAffectedRow(tx.Where(query, args...).Delete(mapping))
	}))
}

func (r *DocumentRepository) listByMapping(tenantID int64, join, condition string, id int64) ([]models.DocumentAggregate, error) {
	var docs []models.Document
	err := r.db.Model(&models.Document{}).Joins(join).Where("documents.tenant_id = ? AND "+condition, tenantID, id).Order("documents.created_at DESC").Find(&docs).Error
	if err != nil {
		return nil, err
	}
	result := make([]models.DocumentAggregate, 0, len(docs))
	for _, doc := range docs {
		item, err := r.GetAggregate(doc.ID, tenantID)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, nil
}
func (r *DocumentRepository) ListByElementID(tenantID, elementID int64) ([]models.DocumentAggregate, error) {
	return r.listByMapping(tenantID, "JOIN standard.document_element_mappings dem ON dem.document_id = documents.id", "dem.element_id = ?", elementID)
}
func (r *DocumentRepository) ListByGlossaryID(tenantID, glossaryID int64) ([]models.DocumentAggregate, error) {
	return r.listByMapping(tenantID, "JOIN standard.document_glossary_mappings dgm ON dgm.document_id = documents.id", "dgm.glossary_id = ?", glossaryID)
}
func (r *DocumentRepository) ListByMetricID(tenantID, metricID int64) ([]models.DocumentAggregate, error) {
	return r.listByMapping(tenantID, "JOIN standard.document_metric_mappings dmm ON dmm.document_id = documents.id", "dmm.metric_id = ?", metricID)
}
