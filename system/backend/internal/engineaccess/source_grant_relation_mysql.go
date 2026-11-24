package engineaccess

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/addp/common/authorization"
)

// A path filter includes both scopes at the exact node. Only validated ancestors
// may cover descendants, with nonrecursive grants limited to direct children.
func (f SourceGrantFilter) mysqlTargetPredicate() (string, []any) {
	if f.CatalogPath != nil {
		path := *f.CatalogPath
		encoded, _ := encodeGrantFilterTarget(path)
		clauses := []string{"catalog_path = CAST(? AS JSON)"}
		args := []any{string(encoded)}
		for depth := 1; depth < len(path.Segments); depth++ {
			node := path
			node.Segments = path.Segments[:depth]
			scope := authorization.SourceNodeScope{Target: node, Recursive: true}
			if !scope.Covers(path) {
				continue
			}
			scope.Recursive = false
			match := "node_recursive = TRUE"
			if scope.Covers(path) {
				match = "node_recursive IS NOT NULL"
			}
			ancestor, _ := json.Marshal(node)
			clauses = append(clauses, "(catalog_path = CAST(? AS JSON) AND "+match+")")
			args = append(args, string(ancestor))
		}
		return "(" + strings.Join(clauses, " OR ") + ")", args
	}
	if f.TableSearch != "" {
		// Join segment names in order, including empty root names. Recursive
		// concatenation avoids GROUP_CONCAT's session-dependent length limit.
		return `EXISTS (WITH RECURSIVE path_names (pos, full_name) AS (
 SELECT 0, CAST('' AS CHAR(65535) CHARACTER SET utf8mb4)
 UNION ALL SELECT pos+1, CONCAT(full_name, IF(pos=0,'',' / '),
 JSON_UNQUOTE(JSON_EXTRACT(catalog_path, CONCAT('$.segments[',pos,'].name'))))
 FROM path_names WHERE pos < JSON_LENGTH(catalog_path,'$.segments')
 ) SELECT 1 FROM path_names WHERE pos=JSON_LENGTH(catalog_path,'$.segments')
 AND LOCATE(LOWER(?) COLLATE utf8mb4_0900_bin, LOWER(full_name) COLLATE utf8mb4_0900_bin)>0)`, []any{f.TableSearch}
	}
	return "TRUE", nil
}

// A source relation keeps the latest withdrawal anchor, the longest effective
// lifetime, and the approval modes of all its active issuance records.
const mysqlGrantRollupColumns = `ROW_NUMBER() OVER (rel ORDER BY g.granted_at DESC,g.request_id DESC) AS anchor_rank,
 COUNT(*) OVER rel AS grant_count, MAX(g.expiry_mode='until_revoked') OVER rel AS permanent,
 MAX(g.expires_at) OVER rel AS latest_expiry,
 MIN(g.approval_mode) OVER rel AS first_mode, MAX(g.approval_mode) OVER rel AS last_mode`

// One statement yields the total even for an empty page. Native DATETIME fields
// are decoded by the platform connection with its configured session timezone.
func (r *Repository) mysqlDirectGrantRelations(ctx context.Context, tenantID, engineID int64, page, size int, filter SourceGrantFilter) ([]SourceGrantRelation, int64, error) {
	predicate, args := filter.predicate("mysql")
	query := `WITH active AS (
 SELECT g.*, ` + mysqlGrantRollupColumns + `
 FROM system.engine_access_grants g
 WHERE tenant_id=? AND engine_id=? AND granted_at<=CURRENT_TIMESTAMP(6)
 AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(6))
 AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations v WHERE v.request_id=g.request_id)
 AND ` + predicate + `
 WINDOW rel AS (PARTITION BY item_fingerprint,SHA2(CAST(catalog_path AS CHAR),256),catalog_path,node_recursive,recipient_type,recipient_id,action)
 ), heads AS (SELECT * FROM active WHERE anchor_rank=1),
 page_rows AS (SELECT * FROM heads ORDER BY granted_at DESC,request_id DESC LIMIT ? OFFSET ?)
 SELECT totals.n,p.request_id,p.catalog_path,COALESCE(p.item_fingerprint,''),p.node_recursive,
 COALESCE(p.recipient_type,''),COALESCE(p.recipient_id,0),COALESCE(p.action,''),
 IF(p.permanent,'until_revoked','at_time'),IF(p.permanent,NULL,p.latest_expiry),
 p.granted_at,COALESCE(p.grant_count,0),IF(p.first_mode=p.last_mode,p.first_mode,'mixed')
 FROM (SELECT COUNT(*) AS n FROM heads) totals LEFT JOIN page_rows p ON TRUE
 ORDER BY p.granted_at DESC,p.request_id DESC`
	parameters := append([]any{tenantID, engineID}, args...)
	parameters = append(parameters, size, (page-1)*size)
	cursor, err := r.db.WithContext(ctx).Raw(query, parameters...).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close()
	relations := make([]SourceGrantRelation, 0)
	var total int64
	for cursor.Next() {
		var row SourceGrantRelation
		var path []byte
		if err := cursor.Scan(&total, &row.RequestID, &path, &row.ItemFingerprint, &row.NodeRecursive, &row.RecipientType, &row.RecipientID, &row.Action, &row.ExpiryMode, &row.ExpiresAt, &row.GrantedAt, &row.GrantCount, &row.ApprovalMode); err != nil {
			return nil, 0, err
		}
		if row.RequestID != nil {
			row.CatalogPath = path
			relations = append(relations, row)
		}
	}
	return relations, total, cursor.Err()
}
