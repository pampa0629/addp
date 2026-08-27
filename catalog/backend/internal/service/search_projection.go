package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const projectionLeaseTimeout = 5 * time.Minute

type CatalogSearchDocument struct {
	ID                      string   `json:"id"`
	TenantID                int64    `json:"tenant_id"`
	EntryStatus             string   `json:"entry_status"`
	EntryType               string   `json:"entry_type"`
	BusinessName            string   `json:"business_name"`
	BusinessDescription     string   `json:"business_description"`
	SourceName              string   `json:"source_name"`
	SourceIdentity          string   `json:"source_identity"`
	SourceStatus            string   `json:"source_status"`
	SourceEngineID          int64    `json:"source_engine_id,omitempty"`
	GovernanceStatus        string   `json:"governance_status"`
	Visibility              string   `json:"visibility"`
	PrimaryDomainID         string   `json:"primary_domain_id,omitempty"`
	DomainNames             []string `json:"domain_names"`
	GlossaryNames           []string `json:"glossary_names"`
	ResponsibilityNames     []string `json:"responsibility_names"`
	AccountableDepartmentID string   `json:"accountable_department_id,omitempty"`
	ComponentNames          []string `json:"component_names"`
	UpdatedAt               string   `json:"updated_at"`
}

type CatalogSearchProjection interface {
	Upsert(context.Context, CatalogSearchDocument) error
	Delete(context.Context, string) error
}

type CatalogSearchResolver interface {
	SearchCatalogEntries(context.Context, int64, EntryAccess, EntryListFilter) ([]uuid.UUID, int64, error)
}

type ProjectionWorker struct {
	db       *gorm.DB
	index    CatalogSearchProjection
	interval time.Duration
}

func NewProjectionWorker(db *gorm.DB, index CatalogSearchProjection, interval time.Duration) *ProjectionWorker {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &ProjectionWorker{db: db, index: index, interval: interval}
}

func (w *ProjectionWorker) Start(ctx context.Context) {
	if w == nil || w.db == nil || w.index == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			if err := w.ProcessNext(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("Catalog search projection failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *ProjectionWorker) ProcessNext(ctx context.Context) error {
	if w == nil || w.db == nil || w.index == nil {
		return errors.New("Catalog projection worker is unavailable")
	}
	task, err := w.claimNext(ctx)
	if err != nil || task == nil {
		return err
	}
	document, deleteDocument, err := buildCatalogSearchDocument(w.db.WithContext(ctx), task.TenantID, task.CatalogEntryID)
	if err == nil {
		if deleteDocument {
			err = w.index.Delete(ctx, task.CatalogEntryID.String())
		} else {
			err = w.index.Upsert(ctx, *document)
		}
	}
	if err != nil {
		if releaseErr := w.releaseFailed(ctx, *task); releaseErr != nil {
			return fmt.Errorf("project Catalog entry: %v; release task: %w", err, releaseErr)
		}
		return fmt.Errorf("project Catalog entry %s: %w", task.CatalogEntryID, err)
	}
	if err := w.db.WithContext(ctx).Where("id = ? AND status = ?", task.ID, "processing").Delete(&models.ProjectionTask{}).Error; err != nil {
		return fmt.Errorf("complete Catalog projection task: %w", err)
	}
	return nil
}

func (w *ProjectionWorker) claimNext(ctx context.Context) (*models.ProjectionTask, error) {
	var claimed *models.ProjectionTask
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&models.ProjectionTask{}).Where(
			"status = ? AND updated_at < ?", "processing", now.Add(-projectionLeaseTimeout),
		).Updates(map[string]interface{}{"status": "pending", "available_at": now, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("recover stale Catalog projection tasks: %w", err)
		}
		var task models.ProjectionTask
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"status = ? AND available_at <= ?", "pending", now,
		).Order("id ASC").Limit(1)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("claim Catalog projection task: %w", err)
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{"status": "processing", "updated_at": now}).Error; err != nil {
			return fmt.Errorf("mark Catalog projection task processing: %w", err)
		}
		task.Status = "processing"
		claimed = &task
		return nil
	})
	return claimed, err
}

func (w *ProjectionWorker) releaseFailed(ctx context.Context, task models.ProjectionTask) error {
	attempt := task.AttemptCount + 1
	delay := time.Duration(1<<min(attempt, 8)) * time.Second
	return w.db.WithContext(ctx).Model(&models.ProjectionTask{}).Where("id = ? AND status = ?", task.ID, "processing").Updates(map[string]interface{}{
		"status": "pending", "attempt_count": attempt, "available_at": time.Now().UTC().Add(delay),
	}).Error
}

func buildCatalogSearchDocument(db *gorm.DB, tenantID int64, entryID uuid.UUID) (*CatalogSearchDocument, bool, error) {
	var entry models.Entry
	if err := db.Where("tenant_id = ? AND id = ?", tenantID, entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("read Catalog entry for projection: %w", err)
	}
	if entry.EntryStatus == models.EntryStatusMerged {
		return nil, true, nil
	}
	var source models.SourceBinding
	if err := db.Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true).First(&source).Error; err != nil {
		return nil, false, fmt.Errorf("read Catalog source for projection: %w", err)
	}
	document := &CatalogSearchDocument{
		ID: entry.ID.String(), TenantID: tenantID, EntryStatus: entry.EntryStatus, EntryType: entry.EntryType,
		BusinessName: textValue(entry.BusinessName), BusinessDescription: textValue(entry.BusinessDescription),
		SourceIdentity: source.SourceIdentity, SourceStatus: source.SourceStatus,
		GovernanceStatus: entry.GovernanceStatus, Visibility: entry.Visibility,
		DomainNames: []string{}, GlossaryNames: []string{}, ResponsibilityNames: []string{}, ComponentNames: []string{},
		UpdatedAt: entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	document.SourceName, _ = source.ObservedSnapshot["name"].(string)
	document.SourceEngineID, _ = numericInt64(source.ObservedSnapshot["engine_id"])

	var semanticLinks []models.SemanticAssociation
	if err := db.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Find(&semanticLinks).Error; err != nil {
		return nil, false, fmt.Errorf("read Catalog semantics for projection: %w", err)
	}
	for _, link := range semanticLinks {
		name, _ := link.ObservedSnapshot["name"].(string)
		if link.SemanticType == models.SemanticTypeDomain {
			document.DomainNames = appendNonEmpty(document.DomainNames, name)
			if link.RelationRole == models.SemanticRolePrimary {
				document.PrimaryDomainID = fmt.Sprintf("%d", link.SemanticID)
			}
		} else if link.SemanticType == models.SemanticTypeGlossary {
			document.GlossaryNames = appendNonEmpty(document.GlossaryNames, name)
		}
	}
	if document.PrimaryDomainID == "" && (source.SourceModule == models.SourceModuleModel || source.SourceModule == models.SourceModuleStandard) {
		if domainID, ok := source.ObservedSnapshot["domain_id"].(string); ok {
			document.PrimaryDomainID = domainID
		}
	}
	var responsibilities []models.Responsibility
	if err := db.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Find(&responsibilities).Error; err != nil {
		return nil, false, fmt.Errorf("read Catalog responsibilities for projection: %w", err)
	}
	for _, responsibility := range responsibilities {
		name, _ := responsibility.ObservedSnapshot["name"].(string)
		document.ResponsibilityNames = appendNonEmpty(document.ResponsibilityNames, name)
		if responsibility.Role == models.ResponsibilityRoleAccountableDepartment && responsibility.Status == models.ResponsibilityStatusActive {
			document.AccountableDepartmentID = fmt.Sprintf("%d", responsibility.SubjectID)
		}
	}
	var components []models.Component
	if err := db.Where("tenant_id = ? AND catalog_entry_id = ? AND component_status = ?", tenantID, entryID, models.SourceStatusActive).Find(&components).Error; err != nil {
		return nil, false, fmt.Errorf("read Catalog components for projection: %w", err)
	}
	for _, component := range components {
		document.ComponentNames = appendNonEmpty(document.ComponentNames, component.DisplayName)
	}
	sort.Strings(document.DomainNames)
	sort.Strings(document.GlossaryNames)
	sort.Strings(document.ResponsibilityNames)
	sort.Strings(document.ComponentNames)
	return document, false, nil
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func appendNonEmpty(values []string, value string) []string {
	if value = strings.TrimSpace(value); value != "" {
		return append(values, value)
	}
	return values
}
