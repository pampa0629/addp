package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// Page original targets before resolving their sources. The recursive CTE walks
// only ancestors of selected DataItems. Node targets keep their exact scope.
func (r *Repository) mysqlAccountGrantRelations(ctx context.Context, tenantID, engineID, accountID int64, page, size int, filter SourceGrantFilter) ([]SourceGrantRelation, int64, error) {
	predicate, args := filter.targetPredicate("mysql")
	query := `WITH RECURSIVE input AS (
 SELECT m.tenant_id,p.id AS principal_id,m.id AS membership_id,p.authorization_version,
 CURRENT_TIMESTAMP(6) AS observed_at
 FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id
 JOIN system.users u ON u.id=p.id WHERE m.tenant_id=? AND p.id=? AND p.principal_type='user'
 )` + sourceReadSubjectCTEs + `, active AS (
 SELECT g.* FROM system.engine_access_grants g CROSS JOIN input
 WHERE g.tenant_id=input.tenant_id AND g.engine_id=? AND ` + sourceGrantCurrentPredicate + ` AND ` + predicate + `
 ), targets AS (
 SELECT DISTINCT SHA2(CAST(g.catalog_path AS CHAR),256) AS path_key,g.catalog_path,g.node_recursive,g.item_fingerprint,g.action
 FROM active g CROSS JOIN input WHERE (g.recipient_type='user' AND g.recipient_id=input.principal_id)
 OR EXISTS(SELECT 1 FROM recipients r WHERE r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id)
 ), page_rows AS (
 SELECT * FROM targets ORDER BY CAST(catalog_path AS CHAR) COLLATE utf8mb4_0900_bin,
 path_key,node_recursive IS NULL,node_recursive,action LIMIT ? OFFSET ?
 ), numbered AS (
 SELECT t.*,ROW_NUMBER() OVER(ORDER BY CAST(catalog_path AS CHAR) COLLATE utf8mb4_0900_bin,
 path_key,node_recursive IS NULL,node_recursive,action) AS pos FROM page_rows t
 ), ancestors AS (
 SELECT pos,catalog_path AS path,node_recursive,action,0 AS depth FROM numbered
 UNION ALL SELECT pos,JSON_REMOVE(path,'$.segments[last]'),node_recursive,action,depth+1
 FROM ancestors WHERE node_recursive IS NULL AND JSON_LENGTH(path,'$.segments')>1
 ), candidates AS (
 SELECT t.pos,t.depth,g.*,
 ` + mysqlGrantRollupColumns + `
 FROM ancestors t JOIN active g ON g.catalog_path=t.path AND
 ((t.depth=0 AND g.node_recursive <=> t.node_recursive) OR
 (t.depth>0 AND g.node_recursive IS NOT NULL AND (g.node_recursive OR t.depth=1)))
 JOIN recipients r ON r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id
 WHERE g.action=t.action OR (t.action='metadata_read' AND g.action='read')
 WINDOW rel AS (PARTITION BY t.pos,SHA2(CAST(g.catalog_path AS CHAR),256),g.catalog_path,g.node_recursive,g.recipient_type,g.recipient_id,g.action)
 )
 SELECT EXISTS(SELECT 1 FROM input), totals.n, p.pos,p.catalog_path,p.node_recursive,
 COALESCE(p.item_fingerprint,''),COALESCE(p.action,''),(SELECT observed_at FROM input),
 CASE WHEN NOT EXISTS(SELECT 1 FROM current_source) THEN 'source_unavailable'
 WHEN NOT EXISTS(SELECT 1 FROM system.engines e WHERE e.tenant_id=? AND e.id=?
 AND e.lifecycle_state='active')
 THEN 'target_unavailable' ELSE 'no_grant' END,
 s.request_id,s.catalog_path,s.node_recursive,COALESCE(s.recipient_type,''),COALESCE(s.recipient_id,0),
 COALESCE(s.action,''),COALESCE(s.grant_count,0),IF(s.permanent,'until_revoked','at_time'),
 IF(s.permanent,NULL,s.latest_expiry),s.granted_at,IF(s.first_mode=s.last_mode,s.first_mode,'mixed'),COALESCE(s.depth,0)
 FROM (SELECT COUNT(*) AS n FROM targets) totals LEFT JOIN numbered p ON TRUE
 LEFT JOIN candidates s ON s.pos=p.pos AND s.anchor_rank=1
 ORDER BY p.pos,s.recipient_type,s.recipient_id,s.action,CAST(s.catalog_path AS CHAR) COLLATE utf8mb4_0900_bin,s.node_recursive`
	params := append([]any{tenantID, accountID, engineID}, args...)
	params = append(params, size, (page-1)*size, tenantID, engineID)
	cursor, err := r.db.WithContext(ctx).Raw(query, params...).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close()
	results := make([]SourceGrantRelation, 0)
	var previous uint64
	var total int64
	for cursor.Next() {
		var exists bool
		var pos *uint64
		var target, sourcePath []byte
		var relation SourceGrantRelation
		var observed, granted *time.Time
		var reason string
		var id *uuid.UUID
		var source SourceGrantInspectionSource
		var depth int
		if err := cursor.Scan(&exists, &total, &pos, &target, &relation.NodeRecursive, &relation.ItemFingerprint, &relation.Action, &observed, &reason,
			&id, &sourcePath, &source.NodeRecursive, &source.RecipientType, &source.RecipientID, &source.Action, &source.GrantCount,
			&source.ExpiryMode, &source.ExpiresAt, &granted, &source.ApprovalMode, &depth); err != nil {
			return nil, 0, err
		}
		if !exists {
			return nil, 0, commonapi.ErrNotFound
		}
		if pos == nil {
			continue
		}
		if *pos != previous {
			relation.CatalogPath = target
			relation.RecipientType = "user"
			relation.RecipientID = accountID
			relation.Inspection = &SourceGrantInspection{
				AccountID:     accountID,
				NodeRecursive: relation.NodeRecursive, ItemFingerprint: relation.ItemFingerprint,
				ObservedAt: *observed, Reason: reason, Sources: []SourceGrantInspectionSource{},
			}
			if err := json.Unmarshal(target, &relation.Inspection.CatalogPath); err != nil {
				return nil, 0, err
			}
			results = append(results, relation)
			previous = *pos
		}
		if id == nil {
			continue
		}
		row := &results[len(results)-1]
		if depth > 0 {
			var ancestor engineplugin.EngineCatalogPath
			if err := json.Unmarshal(sourcePath, &ancestor); err != nil {
				return nil, 0, err
			}
			if !(authorization.SourceNodeScope{Target: ancestor, Recursive: *source.NodeRecursive}).Covers(row.Inspection.CatalogPath) {
				continue
			}
		}
		source.RequestID = *id
		source.CatalogPath = sourcePath
		row.Inspection.Sources = append(row.Inspection.Sources, source)
		if row.Inspection.Reason == "no_grant" {
			row.Inspection.Reason = "grant"
			row.Inspection.RuleCovered = true
		}
		row.GrantCount += source.GrantCount
		if row.GrantedAt == nil || granted.After(*row.GrantedAt) {
			row.GrantedAt = granted
		}
		if row.ApprovalMode == "" {
			row.ApprovalMode = source.ApprovalMode
		} else if row.ApprovalMode != source.ApprovalMode {
			row.ApprovalMode = "mixed"
		}
		if row.ExpiryMode != "until_revoked" {
			row.ExpiryMode = source.ExpiryMode
			if source.ExpiresAt == nil || row.ExpiresAt == nil || source.ExpiresAt.After(*row.ExpiresAt) {
				row.ExpiresAt = source.ExpiresAt
			}
		}
	}
	return results, total, cursor.Err()
}
