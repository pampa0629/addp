package service

import (
	"context"
	"fmt"
	"math"

	"github.com/addp/catalog/internal/models"
)

const (
	CoverageDimensionBusinessDefinition = "business_definition"
	CoverageDimensionPrimaryDomain      = "primary_domain"
	CoverageDimensionAccountability     = "accountability"
	CoverageDimensionGlossary           = "glossary"
	CoverageDimensionComponentElement   = "component_element"
)

type GovernanceStatusCoverage struct {
	Status string `json:"status" enums:"discovered,curated,certified,deprecated"`
	Count  int64  `json:"count"`
}

type GovernanceCoverageDimension struct {
	Key           string  `json:"key" enums:"business_definition,primary_domain,accountability,glossary,component_element"`
	Covered       int64   `json:"covered"`
	Applicable    int64   `json:"applicable"`
	NotCovered    int64   `json:"not_covered"`
	NotApplicable int64   `json:"not_applicable"`
	CoverageRate  float64 `json:"coverage_rate"`
}

type GovernanceCoverage struct {
	View               string                        `json:"view" enums:"inventory"`
	TotalEntries       int64                         `json:"total_entries"`
	GovernanceStatuses []GovernanceStatusCoverage    `json:"governance_statuses"`
	Dimensions         []GovernanceCoverageDimension `json:"dimensions"`
}

func (s *EntryService) GetGovernanceCoverage(ctx context.Context, tenantID int64, access EntryAccess) (*GovernanceCoverage, error) {
	if tenantID <= 0 {
		return nil, ErrInvalidPage
	}
	if !access.Inventory {
		return nil, ErrInventoryPermissionRequired
	}
	type aggregateRow struct {
		TotalEntries              int64
		Discovered                int64
		Curated                   int64
		Certified                 int64
		Deprecated                int64
		BusinessDefinitionCovered int64
		PrimaryDomainCovered      int64
		AccountabilityCovered     int64
		GlossaryCovered           int64
		ComponentApplicable       int64
		ComponentElementCovered   int64
	}
	var aggregate aggregateRow
	componentApplicable := `EXISTS (
		SELECT 1 FROM catalog.components component
		WHERE component.tenant_id = entries.tenant_id
		  AND component.catalog_entry_id = entries.id
		  AND component.component_status = 'active'
	)`
	selectExpression := fmt.Sprintf(`
		COUNT(*) AS total_entries,
		COALESCE(SUM(CASE WHEN entries.governance_status = 'discovered' THEN 1 ELSE 0 END), 0) AS discovered,
		COALESCE(SUM(CASE WHEN entries.governance_status = 'curated' THEN 1 ELSE 0 END), 0) AS curated,
		COALESCE(SUM(CASE WHEN entries.governance_status = 'certified' THEN 1 ELSE 0 END), 0) AS certified,
		COALESCE(SUM(CASE WHEN entries.governance_status = 'deprecated' THEN 1 ELSE 0 END), 0) AS deprecated,
		COALESCE(SUM(CASE WHEN
			entries.business_name IS NOT NULL AND TRIM(entries.business_name) <> ''
			AND entries.business_description IS NOT NULL AND TRIM(entries.business_description) <> ''
		THEN 1 ELSE 0 END), 0) AS business_definition_covered,
		COALESCE(SUM(CASE WHEN
			EXISTS (
				SELECT 1 FROM catalog.semantic_associations semantic
				WHERE semantic.tenant_id = entries.tenant_id
				  AND semantic.catalog_entry_id = entries.id
				  AND semantic.semantic_type = 'domain'
				  AND semantic.relation_role = 'primary'
			) OR EXISTS (
				SELECT 1 FROM catalog.source_bindings source
				WHERE source.tenant_id = entries.tenant_id
				  AND source.catalog_entry_id = entries.id
				  AND source.is_current = TRUE
				  AND source.source_module IN ('model', 'standard')
				  AND COALESCE(source.observed_snapshot ->> 'domain_id', '') <> ''
			)
		THEN 1 ELSE 0 END), 0) AS primary_domain_covered,
		COALESCE(SUM(CASE WHEN
			EXISTS (
				SELECT 1 FROM catalog.responsibilities responsibility
				WHERE responsibility.tenant_id = entries.tenant_id AND responsibility.catalog_entry_id = entries.id
				  AND responsibility.role = 'accountable_department' AND responsibility.status = 'active'
			) AND EXISTS (
				SELECT 1 FROM catalog.responsibilities responsibility
				WHERE responsibility.tenant_id = entries.tenant_id AND responsibility.catalog_entry_id = entries.id
				  AND responsibility.role = 'business_owner' AND responsibility.status = 'active'
			) AND EXISTS (
				SELECT 1 FROM catalog.responsibilities responsibility
				WHERE responsibility.tenant_id = entries.tenant_id AND responsibility.catalog_entry_id = entries.id
				  AND responsibility.role = 'data_steward' AND responsibility.status = 'active'
			)
		THEN 1 ELSE 0 END), 0) AS accountability_covered,
		COALESCE(SUM(CASE WHEN EXISTS (
			SELECT 1 FROM catalog.semantic_associations semantic
			WHERE semantic.tenant_id = entries.tenant_id
			  AND semantic.catalog_entry_id = entries.id
			  AND semantic.semantic_type = 'glossary'
		) THEN 1 ELSE 0 END), 0) AS glossary_covered,
		COALESCE(SUM(CASE WHEN %s THEN 1 ELSE 0 END), 0) AS component_applicable,
		COALESCE(SUM(CASE WHEN %s AND NOT EXISTS (
			SELECT 1 FROM catalog.components component
			WHERE component.tenant_id = entries.tenant_id
			  AND component.catalog_entry_id = entries.id
			  AND component.component_status = 'active'
			  AND NOT EXISTS (
				SELECT 1 FROM catalog.component_element_associations element_link
				WHERE element_link.tenant_id = component.tenant_id
				  AND element_link.catalog_entry_id = component.catalog_entry_id
				  AND element_link.component_id = component.id
			  )
		) THEN 1 ELSE 0 END), 0) AS component_element_covered`, componentApplicable, componentApplicable)
	if err := s.visibleEntriesQuery(ctx, tenantID, access).
		Where("entries.entry_status = ?", models.EntryStatusActive).
		Select(selectExpression).Scan(&aggregate).Error; err != nil {
		return nil, fmt.Errorf("aggregate Catalog governance coverage: %w", err)
	}
	total := aggregate.TotalEntries
	statuses := make([]GovernanceStatusCoverage, 0, 4)
	for _, item := range []GovernanceStatusCoverage{
		{Status: models.GovernanceStatusDiscovered, Count: aggregate.Discovered},
		{Status: models.GovernanceStatusCurated, Count: aggregate.Curated},
		{Status: models.GovernanceStatusCertified, Count: aggregate.Certified},
		{Status: models.GovernanceStatusDeprecated, Count: aggregate.Deprecated},
	} {
		statuses = append(statuses, item)
	}

	dimensions := []GovernanceCoverageDimension{
		coverageDimension(CoverageDimensionBusinessDefinition, aggregate.BusinessDefinitionCovered, total, total),
		coverageDimension(CoverageDimensionPrimaryDomain, aggregate.PrimaryDomainCovered, total, total),
		coverageDimension(CoverageDimensionAccountability, aggregate.AccountabilityCovered, total, total),
		coverageDimension(CoverageDimensionGlossary, aggregate.GlossaryCovered, total, total),
		coverageDimension(CoverageDimensionComponentElement, aggregate.ComponentElementCovered, aggregate.ComponentApplicable, total),
	}

	return &GovernanceCoverage{
		View: EntryViewInventory, TotalEntries: total,
		GovernanceStatuses: statuses, Dimensions: dimensions,
	}, nil
}

func coverageDimension(key string, covered, applicable, total int64) GovernanceCoverageDimension {
	rate := float64(0)
	if applicable > 0 {
		rate = math.Round(float64(covered)/float64(applicable)*10000) / 100
	}
	return GovernanceCoverageDimension{
		Key: key, Covered: covered, Applicable: applicable,
		NotCovered: applicable - covered, NotApplicable: total - applicable, CoverageRate: rate,
	}
}
