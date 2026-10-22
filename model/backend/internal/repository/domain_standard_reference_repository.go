package repository

import (
	"context"
	"github.com/addp/common/domainreference"
)

func (r *EntityRepository) ListDomainStandardReferences(ctx context.Context, tenantID, domainID int64, kind string, page, size int) (*domainreference.Page, error) {
	if tenantID <= 0 || domainID <= 0 {
		return nil, domainreference.ErrInvalidQuery
	}
	var query string
	switch kind {
	case "entity":
		query = `SELECT 'element' AS source_type,a.element_id::TEXT AS source_identity,'model' AS owner_module,'entity' AS resource_type,e.id::TEXT AS resource_id,e.name AS resource_name,a.name AS position,COALESCE(a.element_revision_id::TEXT,'') AS revision_id,'' AS resource_revision_id,e.status AS state,'model.entity.element' AS relation_kind FROM model.entities e JOIN model.entity_attributes a ON a.entity_id=e.id WHERE e.tenant_id=? AND e.domain_id=? AND a.element_id IS NOT NULL`
	case "logical_table":
		query = `SELECT 'element' AS source_type,f.element_id::TEXT AS source_identity,'model' AS owner_module,'logical_table' AS resource_type,t.id::TEXT AS resource_id,t.name AS resource_name,f.column_name AS position,COALESCE(f.element_revision_id::TEXT,'') AS revision_id,'' AS resource_revision_id,t.status AS state,'model.logical_table.element' AS relation_kind FROM model.logical_tables t JOIN model.logical_fields f ON f.table_id=t.id WHERE t.tenant_id=? AND t.domain_id=? AND f.element_id IS NOT NULL`
	case "metric_implementation":
		query = `SELECT 'metric' AS source_type,i.metric_definition_id::TEXT AS source_identity,'model' AS owner_module,'metric_implementation' AS resource_type,i.id::TEXT AS resource_id,i.name AS resource_name,'metric_definition' AS position,COALESCE(r.metric_definition_revision_id::TEXT,'') AS revision_id,COALESCE(r.id::TEXT,'') AS resource_revision_id,COALESCE(r.status,'draft') AS state,'model.metric_implementation.definition' AS relation_kind FROM model.metric_implementations i JOIN model.logical_tables t ON t.id=i.fact_table_id AND t.tenant_id=i.tenant_id LEFT JOIN LATERAL (SELECT * FROM model.metric_implementation_revisions r WHERE r.implementation_id=i.id AND r.tenant_id=i.tenant_id ORDER BY (r.status='published') DESC,r.revision_no DESC LIMIT 1) r ON TRUE WHERE i.tenant_id=? AND t.domain_id=?`
	default:
		return nil, domainreference.ErrInvalidQuery
	}
	return domainreference.ReadPage(ctx, r.db, r.db.Raw(query, tenantID, domainID), page, size)
}
