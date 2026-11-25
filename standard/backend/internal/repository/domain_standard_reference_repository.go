package repository

import (
	"context"

	"github.com/addp/common/domainreference"
)

func (r *CatalogResourceRepository) ListDomainStandardReferences(ctx context.Context, tenantID, domainID int64, kind string, page, size int) (*domainreference.Page, error) {
	if tenantID <= 0 || domainID <= 0 {
		return nil, domainreference.ErrInvalidQuery
	}
	// Prefer an effective published revision. Preserve false before unknown when a date is absent.
	const preferredRevision = ` ORDER BY CASE (o.lifecycle_state='active' AND r.status='published'
		AND r.effective_from<=CURRENT_TIMESTAMP AND (r.effective_to IS NULL OR r.effective_to>CURRENT_TIMESTAMP))
		WHEN TRUE THEN 0 WHEN FALSE THEN 1 ELSE 2 END, r.revision_no DESC LIMIT 1`
	var query string
	switch kind {
	case "glossary":
		query = `SELECT 'element' AS source_type, CONCAT('',m.element_id) AS source_identity,
			'standard' AS owner_module, 'glossary' AS resource_type, CONCAT('',o.id) AS resource_id,
			COALESCE(d.name,o.code) AS resource_name, 'element_mapping' AS position,
			'' AS revision_id, '' AS resource_revision_id, COALESCE(d.status,'draft') AS state,
			'standard.glossary.element' AS relation_kind
			FROM standard.glossaries o JOIN standard.glossary_element_mappings m ON m.glossary_id=o.id
			JOIN standard.elements target ON target.id=m.element_id AND target.tenant_id=o.tenant_id
			LEFT JOIN LATERAL (SELECT r.name,r.status FROM standard.glossary_revisions r WHERE r.glossary_id=o.id` + preferredRevision + `) d ON TRUE
			WHERE o.tenant_id=? AND o.owner_domain_id=?`
	case "element":
		query = `SELECT 'code_set' AS source_type, CONCAT('',c.code_set_id) AS source_identity,
			'standard' AS owner_module, 'element' AS resource_type, CONCAT('',o.id) AS resource_id,
			d.name AS resource_name, 'value_domain' AS position, CONCAT('',d.code_set_revision_id) AS revision_id,
			CONCAT('',d.id) AS resource_revision_id, d.status AS state, 'standard.element.code_set' AS relation_kind
			FROM standard.elements o JOIN LATERAL (SELECT r.id,r.name,r.status,r.code_set_revision_id
				FROM standard.element_revisions r WHERE r.element_id=o.id` + preferredRevision + `) d ON TRUE
			JOIN standard.code_set_revisions c ON c.id=d.code_set_revision_id
			JOIN standard.code_sets target ON target.id=c.code_set_id AND target.tenant_id=o.tenant_id
			WHERE o.tenant_id=? AND o.owner_domain_id=?`
	case "metric":
		query = `SELECT 'metric' AS source_type, CONCAT('',m.dependency_definition_id) AS source_identity,
			'standard' AS owner_module, 'metric' AS resource_type, CONCAT('',o.id) AS resource_id,
			d.name AS resource_name, m.relation_kind AS position, COALESCE(CONCAT('',m.dependency_revision_id),'') AS revision_id,
			CONCAT('',d.id) AS resource_revision_id, d.status AS state, 'standard.metric.dependency' AS relation_kind
			FROM standard.metric_definitions o JOIN LATERAL (SELECT r.id,r.name,r.status FROM standard.metric_definition_revisions r
				WHERE r.metric_definition_id=o.id` + preferredRevision + `) d ON TRUE
			JOIN standard.metric_definition_revision_dependencies m ON m.metric_definition_revision_id=d.id
			JOIN standard.metric_definitions target ON target.id=m.dependency_definition_id AND target.tenant_id=o.tenant_id
			WHERE o.tenant_id=? AND o.owner_domain_id=?`
	default:
		return nil, domainreference.ErrInvalidQuery
	}
	return domainreference.ReadPage(ctx, r.db, r.db.Raw(query, tenantID, domainID), page, size)
}
