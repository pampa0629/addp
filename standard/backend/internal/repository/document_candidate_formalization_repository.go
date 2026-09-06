package repository

import (
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCandidateNotRetained          = errors.New("standard candidate is not retained")
	ErrCandidateAlreadyFormalized    = errors.New("standard candidate is already formalized")
	ErrCandidateFormalizationStale   = errors.New("standard candidate formalization state changed")
	ErrCandidateScopeConflict        = errors.New("standard candidate scope conflicts with target")
	ErrCandidateTargetDraftExists    = errors.New("standard candidate target has a work revision")
	ErrCandidateReferenceUnavailable = errors.New("standard candidate reference cannot be resolved uniquely")
)

type DocumentCandidateContext struct {
	Candidate models.DocumentExtractionCandidate
	Document  models.Document
}

type DocumentCandidateFormalizationPlan struct {
	Action                string
	CandidateType         string
	CandidateVersion      int64
	SourceDocumentVersion int64
	ChangeSummary         string
	MetricType            string
	TargetStandardID      int64
	TargetStandardVersion int64
	TargetRevisionID      int64
}

func (r *DocumentRepository) GetCandidateContext(candidateID, tenantID int64) (*DocumentCandidateContext, error) {
	var candidate models.DocumentExtractionCandidate
	if err := r.db.Table("standard.document_extraction_candidates AS candidate").Select("candidate.*").
		Joins("JOIN standard.document_extractions extraction ON extraction.id = candidate.extraction_id").
		Where("candidate.id = ? AND extraction.tenant_id = ?", candidateID, tenantID).
		Preload("Formalization").First(&candidate).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	var document models.Document
	if err := r.db.Table("standard.documents AS document").Select("document.*").
		Joins("JOIN standard.document_revisions revision ON revision.document_id = document.id").
		Joins("JOIN standard.document_extractions extraction ON extraction.document_revision_id = revision.id").
		Where("extraction.id = ? AND document.tenant_id = ?", candidate.ExtractionID, tenantID).
		First(&document).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &DocumentCandidateContext{Candidate: candidate, Document: document}, nil
}

func (r *DocumentRepository) FormalizeCandidate(candidateID, tenantID, userID int64, plan DocumentCandidateFormalizationPlan) (*models.DocumentCandidateFormalizationResponse, error) {
	var response models.DocumentCandidateFormalizationResponse
	err := wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		context, err := lockDocumentCandidateContext(tx, candidateID, tenantID)
		if err != nil {
			return err
		}
		candidate, document := &context.Candidate, &context.Document
		if candidate.Version != plan.CandidateVersion || document.Version != plan.SourceDocumentVersion || candidate.CandidateType != plan.CandidateType {
			return ErrCandidateFormalizationStale
		}
		if candidate.Status != "retained" {
			return ErrCandidateNotRetained
		}
		var count int64
		if err := tx.Model(&models.DocumentCandidateFormalization{}).Where("candidate_id = ?", candidateID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrCandidateAlreadyFormalized
		}

		formalization := models.DocumentCandidateFormalization{
			CandidateID: candidate.ID, Action: plan.Action, StandardCode: candidate.Code,
			ChangeSummary: plan.ChangeSummary, CreatedBy: userID,
		}
		switch plan.Action {
		case models.CandidateFormalizationCreatedIdentity:
			if err := createCandidateStandardIdentity(tx, tenantID, userID, document, candidate, plan.MetricType, plan.ChangeSummary, &formalization); err != nil {
				return err
			}
		case models.CandidateFormalizationCreatedRevision:
			if err := createCandidateStandardRevision(tx, tenantID, userID, document, candidate, plan, &formalization); err != nil {
				return err
			}
		case models.CandidateFormalizationLinkedExisting:
			if err := linkCandidateStandardRevision(tx, tenantID, document, candidate, plan, &formalization); err != nil {
				return err
			}
		default:
			return ErrCandidateFormalizationStale
		}
		if err := tx.Create(&formalization).Error; err != nil {
			return err
		}
		result := tx.Model(&models.DocumentExtractionCandidate{}).
			Where("id = ? AND version = ? AND status = ?", candidate.ID, plan.CandidateVersion, "retained").
			Updates(map[string]interface{}{"version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCandidateFormalizationStale
		}
		response = models.DocumentCandidateFormalizationResponse{
			DocumentCandidateFormalization: formalization,
			CandidateType:                  candidate.CandidateType, CandidateVersion: candidate.Version + 1,
		}
		return nil
	}))
	return &response, mapCandidateFormalizationDBError(err)
}

func lockDocumentCandidateContext(tx *gorm.DB, candidateID, tenantID int64) (*DocumentCandidateContext, error) {
	var documentID int64
	if err := tx.Table("standard.documents AS document").Select("document.id").
		Joins("JOIN standard.document_revisions revision ON revision.document_id = document.id").
		Joins("JOIN standard.document_extractions extraction ON extraction.document_revision_id = revision.id").
		Joins("JOIN standard.document_extraction_candidates candidate ON candidate.extraction_id = extraction.id").
		Where("candidate.id = ? AND document.tenant_id = ?", candidateID, tenantID).Scan(&documentID).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if documentID == 0 {
		return nil, commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	var document models.Document
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	var candidate models.DocumentExtractionCandidate
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("standard.document_extraction_candidates AS candidate").Select("candidate.*").
		Joins("JOIN standard.document_extractions extraction ON extraction.id = candidate.extraction_id").
		Where("candidate.id = ? AND extraction.tenant_id = ?", candidateID, tenantID).First(&candidate).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &DocumentCandidateContext{Candidate: candidate, Document: document}, nil
}

func createCandidateStandardIdentity(tx *gorm.DB, tenantID, userID int64, document *models.Document, candidate *models.DocumentExtractionCandidate, metricType, changeSummary string, formalization *models.DocumentCandidateFormalization) error {
	switch candidate.CandidateType {
	case "glossary":
		identity := models.Glossary{TenantID: tenantID, ScopeType: document.ScopeType, OwnerDomainID: document.OwnerDomainID, Code: candidate.Code, CreatedBy: userID, LifecycleState: "active"}
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		revision := models.GlossaryRevision{GlossaryID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: candidate.Name, Definition: candidate.Definition, ChangeSummary: changeSummary, CreatedBy: userID}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		if err := tx.Model(&identity).Update("draft_revision_id", revision.ID).Error; err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, revision.ID, 1, revision.Status)
	case "element":
		unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
		if err != nil {
			return err
		}
		codeSetRevisionID, err := resolveCandidateCodeSetRevision(tx, tenantID, candidate.Payload.CodeSetCode)
		if err != nil {
			return err
		}
		identity := models.Element{TenantID: tenantID, ScopeType: document.ScopeType, OwnerDomainID: document.OwnerDomainID, Code: candidate.Code, CreatedBy: userID, LifecycleState: "active"}
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		revision := models.ElementRevision{ElementID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: candidate.Name, Definition: candidate.Definition, DataType: requiredCandidateText(candidate.Payload.DataType), Nullable: true, ValueDomainKind: requiredCandidateText(candidate.Payload.ValueDomainKind), UnitID: unitID, CodeSetRevisionID: codeSetRevisionID, ExtraQualityRules: models.JSONB{}, CompiledQualityRules: models.JSONB{}, ChangeSummary: changeSummary, CreatedBy: userID}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		if err := tx.Model(&identity).Update("draft_revision_id", revision.ID).Error; err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, revision.ID, 1, revision.Status)
	case "code_set":
		identity := models.CodeSet{TenantID: tenantID, ScopeType: document.ScopeType, OwnerDomainID: document.OwnerDomainID, Code: candidate.Code, Origin: models.CodeSetOriginTenant, CreatedBy: userID, LifecycleState: "active"}
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		revision := models.CodeSetRevision{CodeSetID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: candidate.Name, Description: candidate.Definition, ValueType: requiredCandidateText(candidate.Payload.DataType), ChangeSummary: changeSummary, CreatedBy: userID}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		if err := replaceCandidateCodeSetItems(tx, revision.ID, candidate.Payload.Items); err != nil {
			return err
		}
		if err := tx.Model(&identity).Update("draft_revision_id", revision.ID).Error; err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, revision.ID, 1, revision.Status)
	case "metric":
		unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
		if err != nil {
			return err
		}
		identity := models.MetricDefinition{TenantID: tenantID, ScopeType: document.ScopeType, OwnerDomainID: document.OwnerDomainID, Code: candidate.Code, CreatedBy: userID, LifecycleState: "active"}
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		revision := models.MetricDefinitionRevision{MetricDefinitionID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, MetricType: metricType, Name: candidate.Name, Definition: candidate.Definition, StatisticalCaliber: optionalCandidateText(candidate.Payload.StatisticalScope), SemanticFormula: optionalCandidateText(candidate.Payload.CalculationFormula), UnitID: unitID, ChangeSummary: changeSummary, CreatedBy: userID}
		if err := tx.Omit("Dependencies").Create(&revision).Error; err != nil {
			return err
		}
		if err := tx.Model(&identity).Update("draft_revision_id", revision.ID).Error; err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, revision.ID, 1, revision.Status)
	default:
		return ErrCandidateFormalizationStale
	}
	return nil
}

func createCandidateStandardRevision(tx *gorm.DB, tenantID, userID int64, document *models.Document, candidate *models.DocumentExtractionCandidate, plan DocumentCandidateFormalizationPlan, formalization *models.DocumentCandidateFormalization) error {
	switch candidate.CandidateType {
	case "glossary":
		var identity models.Glossary
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var source models.GlossaryRevision
		if err := tx.Where("glossary_id = ?", identity.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		source.ID, source.RevisionNo, source.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		source.Name, source.Definition, source.ChangeSummary = candidate.Name, candidate.Definition, plan.ChangeSummary
		resetGlossaryRevisionAudit(&source, userID)
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Glossary{}, identity.ID, tenantID, plan.TargetStandardVersion, map[string]interface{}{"draft_revision_id": source.ID, "updated_by": userID}); err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, source.ID, source.RevisionNo, source.Status)
	case "element":
		var identity models.Element
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var source models.ElementRevision
		if err := tx.Where("element_id = ?", identity.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
		if err != nil {
			return err
		}
		codeSetRevisionID, err := resolveCandidateCodeSetRevision(tx, tenantID, candidate.Payload.CodeSetCode)
		if err != nil {
			return err
		}
		source.ID, source.RevisionNo, source.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		source.Name, source.Definition, source.ChangeSummary = candidate.Name, candidate.Definition, plan.ChangeSummary
		if candidate.Payload.DataType != nil {
			source.DataType = *candidate.Payload.DataType
		}
		if candidate.Payload.ValueDomainKind != nil {
			previousKind := source.ValueDomainKind
			source.ValueDomainKind = *candidate.Payload.ValueDomainKind
			switch source.ValueDomainKind {
			case models.ValueDomainUnrestricted:
				source.RangeConstraint, source.CodeSetRevisionID = nil, nil
			case models.ValueDomainRange:
				source.CodeSetRevisionID = nil
				if previousKind != models.ValueDomainRange {
					source.RangeConstraint = nil
				}
			case models.ValueDomainEnumeration:
				source.RangeConstraint, source.CodeSetRevisionID = nil, codeSetRevisionID
			}
		}
		if candidate.Payload.Unit != nil {
			source.UnitID = unitID
		}
		if candidate.Payload.CodeSetCode != nil {
			source.CodeSetRevisionID = codeSetRevisionID
		}
		resetElementRevisionAudit(&source, userID)
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.Element{}, identity.ID, tenantID, plan.TargetStandardVersion, map[string]interface{}{"draft_revision_id": source.ID, "updated_by": userID}); err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, source.ID, source.RevisionNo, source.Status)
	case "code_set":
		var identity models.CodeSet
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var source models.CodeSetRevision
		if err := tx.Where("code_set_id = ?", identity.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		var sourceItems []models.CodeSetRevisionItem
		if err := tx.Where("code_set_revision_id = ?", source.ID).Order("sort_order ASC, id ASC").Find(&sourceItems).Error; err != nil {
			return err
		}
		source.ID, source.RevisionNo, source.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		source.Name, source.Description, source.ChangeSummary = candidate.Name, candidate.Definition, plan.ChangeSummary
		if candidate.Payload.DataType != nil {
			source.ValueType = *candidate.Payload.DataType
		}
		resetCodeSetRevisionAudit(&source, userID)
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		if len(candidate.Payload.Items) > 0 {
			if err := replaceCandidateCodeSetItems(tx, source.ID, candidate.Payload.Items); err != nil {
				return err
			}
		} else if err := cloneCandidateCodeSetItems(tx, source.ID, sourceItems); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.CodeSet{}, identity.ID, tenantID, plan.TargetStandardVersion, map[string]interface{}{"draft_revision_id": source.ID, "updated_by": userID}); err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, source.ID, source.RevisionNo, source.Status)
	case "metric":
		var identity models.MetricDefinition
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var source models.MetricDefinitionRevision
		if err := tx.Where("metric_definition_id = ?", identity.ID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		var dependencies []models.MetricDefinitionRevisionDependency
		if err := tx.Where("metric_definition_revision_id = ?", source.ID).Find(&dependencies).Error; err != nil {
			return err
		}
		unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
		if err != nil {
			return err
		}
		source.ID, source.RevisionNo, source.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		source.Name, source.Definition, source.ChangeSummary = candidate.Name, candidate.Definition, plan.ChangeSummary
		if candidate.Payload.StatisticalScope != nil {
			source.StatisticalCaliber = *candidate.Payload.StatisticalScope
		}
		if candidate.Payload.CalculationFormula != nil {
			source.SemanticFormula = *candidate.Payload.CalculationFormula
		}
		if candidate.Payload.Unit != nil {
			source.UnitID = unitID
		}
		resetMetricRevisionAudit(&source, userID)
		if err := tx.Omit("Dependencies").Create(&source).Error; err != nil {
			return err
		}
		for index := range dependencies {
			dependencies[index].ID = 0
			dependencies[index].MetricDefinitionRevisionID = source.ID
			dependencies[index].DependencyRevisionID = nil
			dependencies[index].CreatedAt = time.Time{}
		}
		if err := replaceMetricRevisionDependencies(tx, source.ID, dependencies, false); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.MetricDefinition{}, identity.ID, tenantID, plan.TargetStandardVersion, map[string]interface{}{"draft_revision_id": source.ID, "updated_by": userID}); err != nil {
			return err
		}
		setFormalizationTarget(formalization, identity.ID, source.ID, source.RevisionNo, source.Status)
	default:
		return ErrCandidateFormalizationStale
	}
	return nil
}

func linkCandidateStandardRevision(tx *gorm.DB, tenantID int64, document *models.Document, candidate *models.DocumentExtractionCandidate, plan DocumentCandidateFormalizationPlan, formalization *models.DocumentCandidateFormalization) error {
	var revisionNo int64
	var status string
	switch candidate.CandidateType {
	case "glossary":
		var identity models.Glossary
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var revision models.GlossaryRevision
		if err := tx.Where("id = ? AND glossary_id = ?", plan.TargetRevisionID, identity.ID).First(&revision).Error; err != nil {
			return err
		}
		revisionNo, status = revision.RevisionNo, revision.Status
	case "element":
		var identity models.Element
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var revision models.ElementRevision
		if err := tx.Where("id = ? AND element_id = ?", plan.TargetRevisionID, identity.ID).First(&revision).Error; err != nil {
			return err
		}
		if err := verifyCandidateElementReferences(tx, tenantID, candidate, &revision); err != nil {
			return err
		}
		revisionNo, status = revision.RevisionNo, revision.Status
	case "code_set":
		var identity models.CodeSet
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var revision models.CodeSetRevision
		if err := tx.Where("id = ? AND code_set_id = ?", plan.TargetRevisionID, identity.ID).First(&revision).Error; err != nil {
			return err
		}
		revisionNo, status = revision.RevisionNo, revision.Status
	case "metric":
		var identity models.MetricDefinition
		if err := lockCandidateIdentity(tx, tenantID, document, candidate.Code, plan, &identity); err != nil {
			return err
		}
		var revision models.MetricDefinitionRevision
		if err := tx.Where("id = ? AND metric_definition_id = ?", plan.TargetRevisionID, identity.ID).First(&revision).Error; err != nil {
			return err
		}
		if candidate.Payload.Unit != nil {
			unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
			if err != nil {
				return err
			}
			if !sameOptionalInt64(unitID, revision.UnitID) {
				return ErrCandidateFormalizationStale
			}
		}
		revisionNo, status = revision.RevisionNo, revision.Status
	default:
		return ErrCandidateFormalizationStale
	}
	if status != models.RevisionStatusDraft && status != models.RevisionStatusInReview && status != models.RevisionStatusPublished {
		return ErrCandidateFormalizationStale
	}
	setFormalizationTarget(formalization, plan.TargetStandardID, plan.TargetRevisionID, revisionNo, status)
	return nil
}

func lockCandidateIdentity(tx *gorm.DB, tenantID int64, document *models.Document, code string, plan DocumentCandidateFormalizationPlan, identity interface{}) error {
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND code = ? AND lifecycle_state = ?", plan.TargetStandardID, tenantID, code, "active").First(identity).Error; err != nil {
		return commonrepo.WrapDBError(err)
	}
	var scopeType string
	var ownerDomainID *int64
	var version int64
	var draftRevisionID *int64
	switch value := identity.(type) {
	case *models.Glossary:
		scopeType, ownerDomainID, version, draftRevisionID = value.ScopeType, value.OwnerDomainID, value.Version, value.DraftRevisionID
	case *models.Element:
		scopeType, ownerDomainID, version, draftRevisionID = value.ScopeType, value.OwnerDomainID, value.Version, value.DraftRevisionID
	case *models.CodeSet:
		scopeType, ownerDomainID, version, draftRevisionID = value.ScopeType, value.OwnerDomainID, value.Version, value.DraftRevisionID
	case *models.MetricDefinition:
		scopeType, ownerDomainID, version, draftRevisionID = value.ScopeType, value.OwnerDomainID, value.Version, value.DraftRevisionID
	default:
		return ErrCandidateFormalizationStale
	}
	if scopeType != document.ScopeType || !sameOptionalInt64(ownerDomainID, document.OwnerDomainID) {
		return ErrCandidateScopeConflict
	}
	if version != plan.TargetStandardVersion {
		return ErrCandidateFormalizationStale
	}
	if plan.Action == models.CandidateFormalizationCreatedRevision && draftRevisionID != nil {
		return ErrCandidateTargetDraftExists
	}
	return nil
}

func resolveCandidateUnit(tx *gorm.DB, tenantID int64, candidate *string) (*int64, error) {
	if candidate == nil || strings.TrimSpace(*candidate) == "" {
		return nil, nil
	}
	var units []models.Unit
	if err := tx.Where("tenant_id = ?", tenantID).Find(&units).Error; err != nil {
		return nil, err
	}
	normalized := normalizeCandidateReference(*candidate)
	matches := make([]int64, 0, 1)
	for _, unit := range units {
		if normalizeCandidateReference(unit.Name) == normalized || (strings.TrimSpace(unit.Symbol) != "" && normalizeCandidateReference(unit.Symbol) == normalized) {
			matches = append(matches, unit.ID)
		}
	}
	if len(matches) != 1 {
		return nil, ErrCandidateReferenceUnavailable
	}
	return &matches[0], nil
}

func resolveCandidateCodeSetRevision(tx *gorm.DB, tenantID int64, candidate *string) (*int64, error) {
	if candidate == nil || strings.TrimSpace(*candidate) == "" {
		return nil, nil
	}
	var revision models.CodeSetRevision
	now := time.Now().UTC()
	err := tx.Table("standard.code_set_revisions AS revision").Select("revision.*").
		Joins("JOIN standard.code_sets identity ON identity.id = revision.code_set_id").
		Where("identity.tenant_id = ? AND identity.code = ? AND identity.lifecycle_state = ?", tenantID, strings.TrimSpace(*candidate), "active").
		Where("revision.status = ? AND revision.effective_from <= ? AND (revision.effective_to IS NULL OR revision.effective_to > ?)", models.RevisionStatusPublished, now, now).
		Order("revision.effective_from DESC, revision.revision_no DESC").First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCandidateReferenceUnavailable
	}
	if err != nil {
		return nil, err
	}
	return &revision.ID, nil
}

func replaceCandidateCodeSetItems(tx *gorm.DB, revisionID int64, items []models.DocumentExtractionCandidatePayloadItem) error {
	for index, item := range items {
		row := models.CodeSetRevisionItem{CodeSetRevisionID: revisionID, Code: strings.TrimSpace(item.Code), Label: strings.TrimSpace(item.Name), Definition: strings.TrimSpace(item.Definition), SortOrder: index, Status: models.CodeItemStatusActive}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func cloneCandidateCodeSetItems(tx *gorm.DB, revisionID int64, items []models.CodeSetRevisionItem) error {
	for _, item := range items {
		item.ID, item.CodeSetRevisionID, item.ReplacementItemID = 0, revisionID, nil
		item.CreatedAt, item.UpdatedAt = time.Time{}, time.Time{}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func verifyCandidateElementReferences(tx *gorm.DB, tenantID int64, candidate *models.DocumentExtractionCandidate, revision *models.ElementRevision) error {
	if candidate.Payload.Unit != nil {
		unitID, err := resolveCandidateUnit(tx, tenantID, candidate.Payload.Unit)
		if err != nil {
			return err
		}
		if !sameOptionalInt64(unitID, revision.UnitID) {
			return ErrCandidateFormalizationStale
		}
	}
	if candidate.Payload.CodeSetCode != nil {
		codeSetRevisionID, err := resolveCandidateCodeSetRevision(tx, tenantID, candidate.Payload.CodeSetCode)
		if err != nil {
			return err
		}
		if !sameOptionalInt64(codeSetRevisionID, revision.CodeSetRevisionID) {
			return ErrCandidateFormalizationStale
		}
	}
	return nil
}

func resetGlossaryRevisionAudit(revision *models.GlossaryRevision, userID int64) {
	revision.EffectiveFrom, revision.EffectiveTo, revision.SubmittedBy, revision.SubmittedAt, revision.PublishedBy, revision.PublishedAt = nil, nil, nil, nil, nil, nil
	revision.CreatedBy, revision.UpdatedBy, revision.CreatedAt, revision.UpdatedAt = userID, nil, time.Time{}, time.Time{}
}

func resetElementRevisionAudit(revision *models.ElementRevision, userID int64) {
	revision.EffectiveFrom, revision.EffectiveTo, revision.SubmittedBy, revision.SubmittedAt, revision.PublishedBy, revision.PublishedAt = nil, nil, nil, nil, nil, nil
	revision.CreatedBy, revision.UpdatedBy, revision.CreatedAt, revision.UpdatedAt = userID, nil, time.Time{}, time.Time{}
}

func resetCodeSetRevisionAudit(revision *models.CodeSetRevision, userID int64) {
	revision.EffectiveFrom, revision.EffectiveTo, revision.SubmittedBy, revision.SubmittedAt, revision.PublishedBy, revision.PublishedAt = nil, nil, nil, nil, nil, nil
	revision.CreatedBy, revision.UpdatedBy, revision.CreatedAt, revision.UpdatedAt = userID, nil, time.Time{}, time.Time{}
}

func resetMetricRevisionAudit(revision *models.MetricDefinitionRevision, userID int64) {
	revision.EffectiveFrom, revision.EffectiveTo, revision.SubmittedBy, revision.SubmittedAt, revision.PublishedBy, revision.PublishedAt = nil, nil, nil, nil, nil, nil
	revision.CreatedBy, revision.UpdatedBy, revision.CreatedAt, revision.UpdatedAt = userID, nil, time.Time{}, time.Time{}
	revision.Dependencies = nil
}

func setFormalizationTarget(formalization *models.DocumentCandidateFormalization, standardID, revisionID, revisionNo int64, status string) {
	formalization.StandardID, formalization.RevisionID, formalization.RevisionNo, formalization.TargetRevisionStatus = standardID, revisionID, revisionNo, status
}

func requiredCandidateText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalCandidateText(value *string) string { return requiredCandidateText(value) }

func normalizeCandidateReference(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mapCandidateFormalizationDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrVersionConflict) {
		return ErrCandidateFormalizationStale
	}
	if errors.Is(err, commonapi.ErrConflict) {
		return ErrCandidateFormalizationStale
	}
	return err
}
