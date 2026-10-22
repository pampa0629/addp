package repository

import (
	"context"
	"github.com/addp/common/domainreference"
)

func (r *CatalogResourceRepository) ListDomainStandardReferences(ctx context.Context, tenantID, domainID int64, kind string, page, size int) (*domainreference.Page, error) {
	if tenantID <= 0 || domainID <= 0 {
		return nil, domainreference.ErrInvalidQuery
	}
	var query string
	switch kind {
	case "glossary":
		query = `SELECT 'element' AS source_type,m.element_id::TEXT AS source_identity,'standard' AS owner_module,'glossary' AS resource_type,g.id::TEXT AS resource_id,COALESCE(d.name,g.code) AS resource_name,'element_mapping' AS position,'' AS revision_id,'' AS resource_revision_id,COALESCE(d.status,'draft') AS state,'standard.glossary.element' AS relation_kind FROM standard.glossaries g JOIN standard.glossary_element_mappings m ON m.glossary_id=g.id JOIN standard.elements target ON target.id=m.element_id AND target.tenant_id=g.tenant_id LEFT JOIN LATERAL (SELECT * FROM standard.glossary_revisions r WHERE r.glossary_id=g.id ORDER BY (g.lifecycle_state='active' AND r.status='published' AND r.effective_from<=CURRENT_TIMESTAMP AND (r.effective_to IS NULL OR r.effective_to>CURRENT_TIMESTAMP)) DESC NULLS LAST,r.revision_no DESC LIMIT 1) d ON TRUE WHERE g.tenant_id=? AND g.owner_domain_id=?`
	case "element":
		query = `SELECT 'code_set' AS source_type,c.code_set_id::TEXT AS source_identity,'standard' AS owner_module,'element' AS resource_type,e.id::TEXT AS resource_id,d.name AS resource_name,'value_domain' AS position,d.code_set_revision_id::TEXT AS revision_id,d.id::TEXT AS resource_revision_id,d.status AS state,'standard.element.code_set' AS relation_kind FROM standard.elements e JOIN LATERAL (SELECT * FROM standard.element_revisions r WHERE r.element_id=e.id ORDER BY (e.lifecycle_state='active' AND r.status='published' AND r.effective_from<=CURRENT_TIMESTAMP AND (r.effective_to IS NULL OR r.effective_to>CURRENT_TIMESTAMP)) DESC NULLS LAST,r.revision_no DESC LIMIT 1) d ON TRUE JOIN standard.code_set_revisions c ON c.id=d.code_set_revision_id JOIN standard.code_sets target ON target.id=c.code_set_id AND target.tenant_id=e.tenant_id WHERE e.tenant_id=? AND e.owner_domain_id=?`
	case "metric":
		query = `SELECT 'metric' AS source_type,m.dependency_definition_id::TEXT AS source_identity,'standard' AS owner_module,'metric' AS resource_type,e.id::TEXT AS resource_id,d.name AS resource_name,m.relation_kind AS position,COALESCE(m.dependency_revision_id::TEXT,'') AS revision_id,d.id::TEXT AS resource_revision_id,d.status AS state,'standard.metric.dependency' AS relation_kind FROM standard.metric_definitions e JOIN LATERAL (SELECT * FROM standard.metric_definition_revisions r WHERE r.metric_definition_id=e.id ORDER BY (e.lifecycle_state='active' AND r.status='published' AND r.effective_from<=CURRENT_TIMESTAMP AND (r.effective_to IS NULL OR r.effective_to>CURRENT_TIMESTAMP)) DESC NULLS LAST,r.revision_no DESC LIMIT 1) d ON TRUE JOIN standard.metric_definition_revision_dependencies m ON m.metric_definition_revision_id=d.id JOIN standard.metric_definitions target ON target.id=m.dependency_definition_id AND target.tenant_id=e.tenant_id WHERE e.tenant_id=? AND e.owner_domain_id=?`
	default:
		return nil, domainreference.ErrInvalidQuery
	}
	// Every relation is selected from local persisted facts; no cross-owner SQL.
	return domainreference.ReadPage(ctx, r.db, r.db.Raw(query, tenantID, domainID), page, size)
}
