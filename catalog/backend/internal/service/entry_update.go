package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StandardReferenceResolver interface {
	ResolveStandardReferences(context.Context, int64, []commonClient.StandardReference) ([]commonClient.StandardReferenceResolution, error)
}

type SystemReferenceResolver interface {
	ResolveSystemReferences(context.Context, int64, []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error)
}

type standardClientReferenceResolver struct{ client *commonClient.StandardClient }

func NewStandardClientReferenceResolver(client *commonClient.StandardClient) StandardReferenceResolver {
	return &standardClientReferenceResolver{client: client}
}

func (r *standardClientReferenceResolver) ResolveStandardReferences(
	ctx context.Context,
	tenantID int64,
	references []commonClient.StandardReference,
) ([]commonClient.StandardReferenceResolution, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, errors.New("Standard reference resolver is unavailable")
	}
	return r.client.WithTenantID(uint(tenantID)).ResolveReferences(ctx, references)
}

type systemClientReferenceResolver struct {
	client *commonClient.SystemServiceClient
}

func NewSystemClientReferenceResolver(client *commonClient.SystemServiceClient) SystemReferenceResolver {
	return &systemClientReferenceResolver{client: client}
}

func (r *systemClientReferenceResolver) ResolveSystemReferences(
	ctx context.Context,
	tenantID int64,
	references []commonClient.SystemCatalogReference,
) ([]commonClient.SystemCatalogReferenceResolution, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, errors.New("System reference resolver is unavailable")
	}
	return r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, references)
}

type DomainLinkInput struct {
	ID   int64  `json:"id" binding:"required,gt=0" minimum:"1"`
	Role string `json:"role" binding:"required" enums:"primary,secondary"`
}

type ResponsibilityInput struct {
	Role        string `json:"role" binding:"required" enums:"accountable_department,business_owner,data_steward,technical_owner"`
	SubjectType string `json:"subject_type" binding:"required" enums:"department,user"`
	SubjectID   int64  `json:"subject_id,string" binding:"required,gt=0" swaggertype:"string"`
}

type ComponentElementInput struct {
	ComponentID uuid.UUID `json:"component_id" binding:"required"`
	ElementID   int64     `json:"element_id" binding:"required,gt=0" minimum:"1"`
}

type UpdateEntryInput struct {
	Version             int64                   `json:"version" binding:"required,gt=0" minimum:"1"`
	BusinessName        *string                 `json:"business_name"`
	BusinessDescription *string                 `json:"business_description"`
	GovernanceStatus    string                  `json:"governance_status" binding:"required" enums:"discovered,curated"`
	Visibility          string                  `json:"visibility" binding:"required" enums:"inventory,department,tenant"`
	Domains             []DomainLinkInput       `json:"domains"`
	GlossaryIDs         []int64                 `json:"glossary_ids"`
	Responsibilities    []ResponsibilityInput   `json:"responsibilities"`
	ComponentElements   []ComponentElementInput `json:"component_elements"`
}

type UpdateEntryActor struct {
	Type string
	ID   string
}

type validatedEntryReferences struct {
	standard map[string]commonClient.StandardReferenceResolution
	system   map[string]commonClient.SystemCatalogReferenceResolution
}

func (s *EntryService) Update(
	ctx context.Context,
	tenantID int64,
	id uuid.UUID,
	input UpdateEntryInput,
	actor UpdateEntryActor,
) (*EntryDetail, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || input.Version <= 0 ||
		strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return nil, ErrInvalidEntryUpdate
	}
	normalizeEditableText(&input.BusinessName)
	normalizeEditableText(&input.BusinessDescription)
	if err := validateUpdateShape(input); err != nil {
		return nil, err
	}
	references, err := s.resolveUpdateReferences(ctx, tenantID, input)
	if err != nil {
		return nil, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", tenantID, id).First(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEntryNotFound
			}
			return fmt.Errorf("lock Catalog entry: %w", err)
		}
		if entry.EntryStatus != models.EntryStatusActive {
			return ErrEntryNotEditable
		}
		if entry.Version != input.Version {
			return ErrEntryVersionConflict
		}
		var source models.SourceBinding
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, id, true,
		).First(&source).Error; err != nil {
			return fmt.Errorf("lock Catalog source binding: %w", err)
		}
		ownerPrimaryDomain, err := validateOwnerSemanticInput(entry.EntryType, source, input)
		if err != nil {
			return err
		}
		if err := validateCurationTransition(entry.GovernanceStatus, input, ownerPrimaryDomain); err != nil {
			return err
		}
		if err := validateComponentOwnership(tx, tenantID, id, input.ComponentElements); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := replaceSemanticAssociations(tx, tenantID, id, input, references, now); err != nil {
			return err
		}
		if err := replaceResponsibilities(tx, tenantID, id, input.Responsibilities, references, now); err != nil {
			return err
		}
		if err := resolveSupersededResponsibilityTasks(tx, tenantID, id, input.Responsibilities, now); err != nil {
			return err
		}
		if err := replaceComponentElements(tx, tenantID, id, input.ComponentElements, references, now); err != nil {
			return err
		}

		result := tx.Model(&models.Entry{}).
			Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, input.Version).
			Updates(map[string]interface{}{
				"business_name": input.BusinessName, "business_description": input.BusinessDescription,
				"governance_status": input.GovernanceStatus, "visibility": input.Visibility,
				"version": input.Version + 1, "updated_at": now,
			})
		if result.Error != nil {
			return fmt.Errorf("update Catalog entry: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrEntryVersionConflict
		}
		auditDetails := commonModels.JSONMap{
			"previous_version": input.Version, "version": input.Version + 1,
			"previous_governance_status": entry.GovernanceStatus,
			"governance_status":          input.GovernanceStatus, "visibility": input.Visibility,
			"domain_count": len(input.Domains), "glossary_count": len(input.GlossaryIDs),
			"responsibility_count":    len(input.Responsibilities),
			"component_element_count": len(input.ComponentElements),
		}
		auditEventType := "catalog.entry.updated"
		if isWithdrawCurationTransition(entry.GovernanceStatus, input.GovernanceStatus) {
			auditEventType = "catalog.entry.curation_withdrawn"
		}
		if err := tx.Create(&models.AuditEvent{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: id, EventType: auditEventType,
			ActorType: actor.Type, ActorID: actor.ID, Details: auditDetails, CreatedAt: now,
		}).Error; err != nil {
			return fmt.Errorf("create Catalog audit event: %w", err)
		}
		if err := tx.Create(&models.ProjectionTask{
			TenantID: tenantID, CatalogEntryID: id, Projection: "search", Status: "pending",
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			return fmt.Errorf("create Catalog projection task: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, EntryAccess{Inventory: true}, id)
}

func equalOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateRecommendedSuccessor(tx *gorm.DB, tenantID int64, entryID, successorID uuid.UUID) error {
	if successorID == uuid.Nil || successorID == entryID {
		return ErrInvalidRecommendedSuccessor
	}
	var count int64
	err := tx.Table("catalog.entries AS successor").
		Joins("JOIN catalog.source_bindings AS source ON source.tenant_id = successor.tenant_id AND source.catalog_entry_id = successor.id AND source.is_current = ?", true).
		Where("successor.tenant_id = ? AND successor.id = ?", tenantID, successorID).
		Where("successor.entry_status = ?", models.EntryStatusActive).
		Where("successor.governance_status IN ?", []string{models.GovernanceStatusCurated, models.GovernanceStatusCertified}).
		Where("source.source_status = ?", models.SourceStatusActive).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("validate recommended Catalog successor: %w", err)
	}
	if count != 1 {
		return ErrInvalidRecommendedSuccessor
	}
	return nil
}

func validateUpdateShape(input UpdateEntryInput) error {
	if !oneOf(input.GovernanceStatus, models.GovernanceStatusDiscovered, models.GovernanceStatusCurated, models.GovernanceStatusCertified, models.GovernanceStatusDeprecated) ||
		!oneOf(input.Visibility, models.VisibilityInventory, models.VisibilityDepartment, models.VisibilityTenant) {
		return ErrInvalidEntryUpdate
	}
	if len(input.Domains)+len(input.GlossaryIDs)+len(input.ComponentElements) > 200 || len(input.Responsibilities) > 200 {
		return ErrInvalidEntryUpdate
	}
	seenStandard := make(map[string]struct{})
	primaryDomains := 0
	for _, domain := range input.Domains {
		if domain.ID <= 0 || !oneOf(domain.Role, models.SemanticRolePrimary, models.SemanticRoleSecondary) {
			return ErrInvalidEntryUpdate
		}
		key := fmt.Sprintf("domain:%d", domain.ID)
		if _, exists := seenStandard[key]; exists {
			return ErrInvalidEntryUpdate
		}
		seenStandard[key] = struct{}{}
		if domain.Role == models.SemanticRolePrimary {
			primaryDomains++
		}
	}
	if primaryDomains > 1 {
		return ErrInvalidEntryUpdate
	}
	for _, glossaryID := range input.GlossaryIDs {
		key := fmt.Sprintf("glossary:%d", glossaryID)
		if glossaryID <= 0 {
			return ErrInvalidEntryUpdate
		}
		if _, exists := seenStandard[key]; exists {
			return ErrInvalidEntryUpdate
		}
		seenStandard[key] = struct{}{}
	}
	seenResponsibilities := make(map[string]struct{})
	roleCounts := make(map[string]int)
	for _, responsibility := range input.Responsibilities {
		if responsibility.SubjectID <= 0 || !validResponsibilityShape(responsibility.Role, responsibility.SubjectType) {
			return ErrInvalidEntryUpdate
		}
		key := fmt.Sprintf("%s:%s:%d", responsibility.Role, responsibility.SubjectType, responsibility.SubjectID)
		if _, exists := seenResponsibilities[key]; exists {
			return ErrInvalidEntryUpdate
		}
		seenResponsibilities[key] = struct{}{}
		roleCounts[responsibility.Role]++
	}
	if roleCounts[models.ResponsibilityRoleAccountableDepartment] > 1 || roleCounts[models.ResponsibilityRoleBusinessOwner] > 1 {
		return ErrInvalidEntryUpdate
	}
	seenComponents := make(map[uuid.UUID]struct{})
	for _, component := range input.ComponentElements {
		if component.ComponentID == uuid.Nil || component.ElementID <= 0 {
			return ErrInvalidEntryUpdate
		}
		if _, exists := seenComponents[component.ComponentID]; exists {
			return ErrInvalidEntryUpdate
		}
		seenComponents[component.ComponentID] = struct{}{}
	}
	return nil
}

func (s *EntryService) resolveUpdateReferences(
	ctx context.Context,
	tenantID int64,
	input UpdateEntryInput,
) (*validatedEntryReferences, error) {
	validated := &validatedEntryReferences{
		standard: make(map[string]commonClient.StandardReferenceResolution),
		system:   make(map[string]commonClient.SystemCatalogReferenceResolution),
	}
	standardReferences := make([]commonClient.StandardReference, 0, len(input.Domains)+len(input.GlossaryIDs)+len(input.ComponentElements))
	for _, domain := range input.Domains {
		standardReferences = append(standardReferences, commonClient.StandardReference{ObjectType: models.SemanticTypeDomain, ID: domain.ID})
	}
	for _, glossaryID := range input.GlossaryIDs {
		standardReferences = append(standardReferences, commonClient.StandardReference{ObjectType: models.SemanticTypeGlossary, ID: glossaryID})
	}
	for _, component := range input.ComponentElements {
		standardReferences = append(standardReferences, commonClient.StandardReference{ObjectType: "element", ID: component.ElementID})
	}
	if len(standardReferences) > 0 {
		if s.standard == nil {
			return nil, ErrReferenceValidationUnavailable
		}
		results, err := s.standard.ResolveStandardReferences(ctx, tenantID, standardReferences)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
		}
		if len(results) != len(standardReferences) {
			return nil, ErrReferenceValidationUnavailable
		}
		for index, result := range results {
			reference := standardReferences[index]
			if result.ObjectType != reference.ObjectType || result.ID != reference.ID {
				return nil, ErrReferenceValidationUnavailable
			}
			if !result.Found || !result.Referenceable {
				return nil, ErrReferenceNotReferenceable
			}
			validated.standard[fmt.Sprintf("%s:%d", result.ObjectType, result.ID)] = result
		}
	}

	systemReferences := make([]commonClient.SystemCatalogReference, 0, len(input.Responsibilities))
	for _, responsibility := range input.Responsibilities {
		systemReferences = append(systemReferences, commonClient.SystemCatalogReference{SubjectType: responsibility.SubjectType, ID: responsibility.SubjectID})
	}
	if len(systemReferences) > 0 {
		if s.system == nil {
			return nil, ErrReferenceValidationUnavailable
		}
		results, err := s.system.ResolveSystemReferences(ctx, tenantID, systemReferences)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
		}
		if len(results) != len(systemReferences) {
			return nil, ErrReferenceValidationUnavailable
		}
		for index, result := range results {
			reference := systemReferences[index]
			if result.SubjectType != reference.SubjectType || result.ID != reference.ID {
				return nil, ErrReferenceValidationUnavailable
			}
			if !result.Found || !result.Referenceable {
				return nil, ErrReferenceNotReferenceable
			}
			validated.system[fmt.Sprintf("%s:%d", result.SubjectType, result.ID)] = result
		}
	}
	return validated, nil
}

func validateCurationTransition(current string, input UpdateEntryInput, ownerPrimaryDomain bool) error {
	if current == models.GovernanceStatusCertified || current == models.GovernanceStatusDeprecated ||
		input.GovernanceStatus == models.GovernanceStatusCertified || input.GovernanceStatus == models.GovernanceStatusDeprecated {
		return ErrEntryNotEditable
	}
	if input.GovernanceStatus != current {
		allowed := (current == models.GovernanceStatusDiscovered && input.GovernanceStatus == models.GovernanceStatusCurated) ||
			(isWithdrawCurationTransition(current, input.GovernanceStatus) && isWithdrawnCurationShape(input))
		if !allowed {
			return ErrInvalidGovernanceTransition
		}
	}
	if input.GovernanceStatus == models.GovernanceStatusDiscovered && input.Visibility != models.VisibilityInventory {
		return ErrInvalidEntryUpdate
	}
	if input.GovernanceStatus != models.GovernanceStatusDiscovered {
		if input.BusinessName == nil || input.BusinessDescription == nil {
			return ErrCurationRequirementsNotMet
		}
		primaryDomains := 0
		roleCounts := make(map[string]int)
		for _, domain := range input.Domains {
			if domain.Role == models.SemanticRolePrimary {
				primaryDomains++
			}
		}
		for _, responsibility := range input.Responsibilities {
			roleCounts[responsibility.Role]++
		}
		if (primaryDomains != 1 && !ownerPrimaryDomain) || (primaryDomains != 0 && ownerPrimaryDomain) ||
			roleCounts[models.ResponsibilityRoleAccountableDepartment] != 1 ||
			roleCounts[models.ResponsibilityRoleBusinessOwner] != 1 || roleCounts[models.ResponsibilityRoleDataSteward] < 1 {
			return ErrCurationRequirementsNotMet
		}
	}
	return nil
}

func isWithdrawCurationTransition(current, next string) bool {
	return current == models.GovernanceStatusCurated && next == models.GovernanceStatusDiscovered
}

func isWithdrawnCurationShape(input UpdateEntryInput) bool {
	return input.BusinessName == nil && input.BusinessDescription == nil &&
		input.Visibility == models.VisibilityInventory && len(input.Domains) == 0 &&
		len(input.GlossaryIDs) == 0 && len(input.Responsibilities) == 0 &&
		len(input.ComponentElements) == 0
}

func validateOwnerSemanticInput(entryType string, source models.SourceBinding, input UpdateEntryInput) (bool, error) {
	if source.SourceModule != models.SourceModuleMeta && len(input.ComponentElements) > 0 {
		return false, ErrInvalidEntryUpdate
	}
	ownerManaged := ownerManagesPrimaryDomain(entryType, source)
	if !ownerManaged {
		return false, nil
	}
	for _, domain := range input.Domains {
		if domain.Role == models.SemanticRolePrimary {
			return false, ErrInvalidEntryUpdate
		}
	}
	ownerPrimaryDomain, valid := observedOwnerPrimaryDomain(source)
	if !valid {
		return false, ErrInvalidEntryUpdate
	}
	return ownerPrimaryDomain, nil
}

func ownerManagesPrimaryDomain(entryType string, source models.SourceBinding) bool {
	return (source.SourceModule == models.SourceModuleModel &&
		(entryType == models.EntryTypeBusinessEntity || entryType == models.EntryTypeLogicalModel)) ||
		(source.SourceModule == models.SourceModuleStandard && entryType == models.EntryTypeMetric && source.SourceType == models.SourceTypeMetric)
}

func observedOwnerPrimaryDomain(source models.SourceBinding) (bool, bool) {
	rawDomain, ok := source.ObservedSnapshot["domain_id"]
	if !ok || rawDomain == nil {
		return false, true
	}
	domainID, ok := rawDomain.(string)
	if !ok {
		return false, false
	}
	parsed, err := strconv.ParseInt(domainID, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != domainID {
		return false, false
	}
	return true, true
}

func validateComponentOwnership(tx *gorm.DB, tenantID int64, entryID uuid.UUID, inputs []ComponentElementInput) error {
	if len(inputs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(inputs))
	for _, input := range inputs {
		ids = append(ids, input.ComponentID)
	}
	var count int64
	if err := tx.Model(&models.Component{}).
		Where("tenant_id = ? AND catalog_entry_id = ? AND id IN ? AND component_status = ?", tenantID, entryID, ids, models.SourceStatusActive).
		Count(&count).Error; err != nil {
		return fmt.Errorf("validate Catalog components: %w", err)
	}
	if count != int64(len(ids)) {
		return ErrReferenceNotReferenceable
	}
	return nil
}

func replaceSemanticAssociations(tx *gorm.DB, tenantID int64, entryID uuid.UUID, input UpdateEntryInput, references *validatedEntryReferences, now time.Time) error {
	if err := tx.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Delete(&models.SemanticAssociation{}).Error; err != nil {
		return fmt.Errorf("replace Catalog semantic links: %w", err)
	}
	rows := make([]models.SemanticAssociation, 0, len(input.Domains)+len(input.GlossaryIDs))
	for _, domain := range input.Domains {
		resolved := references.standard[fmt.Sprintf("domain:%d", domain.ID)]
		rows = append(rows, newSemanticAssociation(tenantID, entryID, domain.ID, models.SemanticTypeDomain, domain.Role, resolved, now))
	}
	for _, glossaryID := range input.GlossaryIDs {
		resolved := references.standard[fmt.Sprintf("glossary:%d", glossaryID)]
		rows = append(rows, newSemanticAssociation(tenantID, entryID, glossaryID, models.SemanticTypeGlossary, models.SemanticRoleApplies, resolved, now))
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create Catalog semantic links: %w", err)
		}
	}
	return nil
}

func newSemanticAssociation(tenantID int64, entryID uuid.UUID, semanticID int64, semanticType, role string, resolved commonClient.StandardReferenceResolution, now time.Time) models.SemanticAssociation {
	return models.SemanticAssociation{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID,
		SemanticType: semanticType, SemanticID: semanticID, RelationRole: role,
		ObservedVersion: resolved.Version, ObservedSnapshot: standardSnapshot(resolved),
		VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func replaceResponsibilities(tx *gorm.DB, tenantID int64, entryID uuid.UUID, inputs []ResponsibilityInput, references *validatedEntryReferences, now time.Time) error {
	if err := tx.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Delete(&models.Responsibility{}).Error; err != nil {
		return fmt.Errorf("replace Catalog responsibilities: %w", err)
	}
	rows := make([]models.Responsibility, 0, len(inputs))
	for _, input := range inputs {
		resolved := references.system[fmt.Sprintf("%s:%d", input.SubjectType, input.SubjectID)]
		rows = append(rows, models.Responsibility{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID,
			Role: input.Role, SubjectType: input.SubjectType, SubjectID: input.SubjectID,
			Status: models.ResponsibilityStatusActive, ObservedSnapshot: systemSnapshot(resolved),
			VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
		})
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create Catalog responsibilities: %w", err)
		}
	}
	return nil
}

func replaceComponentElements(tx *gorm.DB, tenantID int64, entryID uuid.UUID, inputs []ComponentElementInput, references *validatedEntryReferences, now time.Time) error {
	if err := tx.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Delete(&models.ComponentElementAssociation{}).Error; err != nil {
		return fmt.Errorf("replace Catalog component elements: %w", err)
	}
	rows := make([]models.ComponentElementAssociation, 0, len(inputs))
	for _, input := range inputs {
		resolved := references.standard[fmt.Sprintf("element:%d", input.ElementID)]
		rows = append(rows, models.ComponentElementAssociation{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID,
			ComponentID: input.ComponentID, ElementID: input.ElementID,
			ObservedVersion: resolved.Version, ObservedSnapshot: standardSnapshot(resolved),
			VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
		})
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create Catalog component elements: %w", err)
		}
	}
	return nil
}

func standardSnapshot(result commonClient.StandardReferenceResolution) commonModels.JSONMap {
	return commonModels.JSONMap{
		"name": result.Name, "code": result.Code, "status": result.Status,
		"lifecycle_state": result.LifecycleState,
	}
}

func systemSnapshot(result commonClient.SystemCatalogReferenceResolution) commonModels.JSONMap {
	return commonModels.JSONMap{
		"name": result.Name, "code": result.Code, "status": result.Status,
		"principal_status": result.PrincipalStatus, "membership_status": result.MembershipStatus,
	}
}

func normalizeEditableText(value **string) {
	if value == nil || *value == nil {
		return
	}
	trimmed := strings.TrimSpace(**value)
	if trimmed == "" {
		*value = nil
		return
	}
	*value = &trimmed
}

func validResponsibilityShape(role, subjectType string) bool {
	if role == models.ResponsibilityRoleAccountableDepartment {
		return subjectType == models.ResponsibilitySubjectDepartment
	}
	return oneOf(role, models.ResponsibilityRoleBusinessOwner, models.ResponsibilityRoleDataSteward, models.ResponsibilityRoleTechnicalOwner) &&
		subjectType == models.ResponsibilitySubjectUser
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
