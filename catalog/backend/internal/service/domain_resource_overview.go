package service

import (
	"context"
	"database/sql"
	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/domainreference"
	"gorm.io/gorm"
	"time"
)

type DomainResourceOverview struct {
	DomainID           int64                      `json:"domain_id,string" swaggertype:"string"`
	View               string                     `json:"view" enums:"governance,inventory"`
	ObservedAt         time.Time                  `json:"observed_at"`
	EntryTypes         []EntryTypeFacet           `json:"entry_types"`
	TotalEntries       int64                      `json:"total_entries"`
	GovernanceStatuses []GovernanceStatusCoverage `json:"governance_statuses"`
	Coverage           *GovernanceCoverage        `json:"coverage,omitempty"`
}

func (s *EntryService) GetDomainResourceOverview(ctx context.Context, tenantID int64, access EntryAccess, domainID int64, view string) (*DomainResourceOverview, error) {
	var result *DomainResourceOverview
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := *s
		scoped.db = tx
		var err error
		result, err = scoped.domainResourceOverview(ctx, tenantID, access, domainID, view)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
func (s *EntryService) domainResourceOverview(ctx context.Context, tenantID int64, access EntryAccess, domainID int64, view string) (*DomainResourceOverview, error) {
	if domainID <= 0 {
		return nil, ErrInvalidPage
	}
	view = normalizeEntryView(view)
	query, err := s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	query = applyPrimaryDomainFilter(query, domainID)
	statuses := []GovernanceStatusCoverage{}
	if err := query.Select("entries.governance_status AS status,COUNT(*) AS count").Group("entries.governance_status").Order("entries.governance_status ASC").Scan(&statuses).Error; err != nil {
		return nil, err
	}
	types, err := s.entryTypeFacetCounts(ctx, tenantID, access, EntryFacetFilter{View: view, PrimaryDomainID: domainID})
	if err != nil {
		return nil, err
	}
	result := &DomainResourceOverview{DomainID: domainID, View: view, ObservedAt: time.Now().UTC(), EntryTypes: types, GovernanceStatuses: statuses}
	for _, item := range types {
		result.TotalEntries += item.Count
	}
	if view == EntryViewInventory {
		result.Coverage, err = s.GetGovernanceCoverage(ctx, tenantID, access, domainID)
	}
	return result, err
}

// ListDomainStandardReferences reads only Catalog-owned associations and audited mappings.
func (s *EntryService) ListDomainStandardReferences(ctx context.Context, tenantID int64, access EntryAccess, domainID int64, view string, page, pageSize int) (*domainreference.Page, error) {
	if domainID <= 0 {
		return nil, ErrInvalidPage
	}
	visible, err := s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	visible = applyPrimaryDomainFilter(visible, domainID).Select("entries.id,COALESCE(entries.business_name,source_name.observed_snapshot->>'name','') AS name, entries.governance_status").Joins("LEFT JOIN catalog.source_bindings source_name ON source_name.tenant_id=entries.tenant_id AND source_name.catalog_entry_id=entries.id AND source_name.is_current=TRUE")
	evidence := s.db.Raw(`WITH visible AS (?)
  SELECT 'glossary' AS source_type,a.semantic_id::TEXT AS source_identity,'catalog' AS owner_module,'entry' AS resource_type,v.id::TEXT AS resource_id,v.name AS resource_name,'semantic_association' AS position,'' AS revision_id,'' AS resource_revision_id,v.governance_status AS state,'catalog.entry.glossary' AS relation_kind
   FROM visible v JOIN catalog.semantic_associations a ON a.catalog_entry_id=v.id AND a.tenant_id=? AND a.semantic_type='glossary'
  UNION ALL
  SELECT 'element',m.element_id::TEXT,'catalog','entry',v.id::TEXT,v.name,c.display_name,m.element_revision_id::TEXT,'',m.review_status,'catalog.component.standard_mapping'
   FROM visible v JOIN catalog.standard_mappings m ON m.catalog_entry_id=v.id AND m.tenant_id=? AND m.review_status=?
   JOIN catalog.components c ON c.id=m.component_id AND c.tenant_id=m.tenant_id AND c.component_status='active'`, visible, tenantID, tenantID, models.StandardMappingApproved)
	return domainreference.ReadPage(ctx, s.db, evidence, page, pageSize)
}
