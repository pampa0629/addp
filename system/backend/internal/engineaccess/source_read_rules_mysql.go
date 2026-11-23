package engineaccess

import (
	"encoding/json"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

// Derive possible ancestor nodes only from the already validated Meta boundary.
// A node grant must cover every file in a composition, including secondary files.
func mysqlSourceReadTargets(paths []plugin.EngineCatalogPath, rules []sourceRuleTarget) (json.RawMessage, error) {
	type ancestor struct {
		Path   plugin.EngineCatalogPath `json:"path"`
		Direct bool                     `json:"direct"`
	}
	type target struct {
		EngineID    uint       `json:"engine_id"`
		Term        string     `json:"term"`
		Action      string     `json:"action"`
		Fingerprint string     `json:"fingerprint"`
		Hash        string     `json:"hash"`
		Nodes       []ancestor `json:"nodes,omitempty"`
	}
	targets := make([]target, len(paths))
	for i, path := range paths {
		b := rules[i].Binding
		t := target{EngineID: path.EngineID, Term: path.Segments[len(path.Segments)-1].Term, Action: rules[i].Action, Fingerprint: b.ItemFingerprint, Hash: b.BoundaryHash}
		covers := func(scope authorization.SourceNodeScope) bool {
			if !scope.Covers(path) {
				return false
			}
			if b.FileComposition != nil {
				for _, ref := range b.FileComposition.Refs {
					if !scope.Covers(ref.Target) {
						return false
					}
				}
			}
			return true
		}
		for depth := 1; depth < len(path.Segments); depth++ {
			node := path
			node.Segments = path.Segments[:depth]
			if covers(authorization.SourceNodeScope{Target: node, Recursive: true}) {
				t.Nodes = append(t.Nodes, ancestor{Path: node, Direct: covers(authorization.SourceNodeScope{Target: node})})
			}
		}
		targets[i] = t
	}
	return json.Marshal(targets)
}

// CURRENT_TIMESTAMP(6) is the statement clock. All targets and all candidate
// grants see the same IAM snapshot. MySQL LEAST propagates any NULL, whereas
// PostgreSQL LEAST ignores NULL deadlines.
const mysqlSourceSubjectInput = `
WITH input AS (
 SELECT CAST(? AS SIGNED) AS tenant_id, CAST(? AS SIGNED) AS principal_id,
        CAST(? AS SIGNED) AS membership_id, CAST(? AS SIGNED) AS authorization_version,
        CURRENT_TIMESTAMP(6) AS observed_at
)`

const mysqlCurrentSourceReadRulesSQL = mysqlSourceSubjectInput + sourceReadSubjectCTEs + `, expanded AS (
 SELECT t.* FROM JSON_TABLE(CAST(? AS JSON),'$[*]' COLUMNS (
   position FOR ORDINALITY, engine_id BIGINT PATH '$.engine_id', term VARCHAR(64) PATH '$.term',
   action VARCHAR(32) PATH '$.action', fingerprint CHAR(64) PATH '$.fingerprint', boundary_hash CHAR(64) PATH '$.hash',
   NESTED PATH '$.nodes[*]' COLUMNS (path JSON PATH '$.path', direct BOOLEAN PATH '$.direct'))) t
), targets AS (
 SELECT DISTINCT position,engine_id,term,action,fingerprint,boundary_hash FROM expanded
), candidates AS (
 SELECT t.position, (g.node_recursive IS NOT NULL) AS is_node,
   COALESCE(LEAST(g.expires_at,s.membership_expires_at),g.expires_at,s.membership_expires_at) AS deadline
 FROM targets t CROSS JOIN input CROSS JOIN current_source s
 JOIN system.engine_access_grants g ON g.tenant_id=input.tenant_id AND g.engine_id=t.engine_id
 JOIN recipients r ON r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id
 WHERE (g.action=t.action OR (t.action='metadata_read' AND g.action='read'))
 AND ` + sourceGrantCurrentPredicate + `
 AND ((g.node_recursive IS NULL AND g.item_fingerprint=t.fingerprint AND g.boundary_hash=t.boundary_hash)
 OR (g.node_recursive IS NOT NULL AND EXISTS (
   SELECT 1 FROM system.engines e WHERE e.id=t.engine_id AND e.tenant_id=input.tenant_id AND e.engine_type IN ('postgresql','nfs','minio'))
   AND EXISTS (SELECT 1 FROM expanded n
     WHERE n.position=t.position AND n.path=g.catalog_path AND (g.node_recursive OR n.direct))))
), ranked AS (
 SELECT candidates.*, MAX(is_node) OVER (PARTITION BY position) AS has_node FROM candidates
), deadlines AS (
 SELECT position, CASE WHEN COUNT(*)=COUNT(deadline) THEN MAX(deadline) END AS expires_at
 FROM ranked WHERE NOT ? OR NOT has_node OR is_node GROUP BY position
), rules AS (
 SELECT t.position,input.observed_at,d.expires_at,
 CASE WHEN NOT EXISTS (SELECT 1 FROM current_source) THEN 'source_unavailable'
      WHEN NOT EXISTS (SELECT 1 FROM system.engines e WHERE e.tenant_id=input.tenant_id AND e.id=t.engine_id
        AND e.lifecycle_state='active' AND (t.action='metadata_read' OR
        ((t.term<>'file' OR e.engine_type IN ('ftp','nfs')) AND (t.term<>'object' OR e.engine_type IN ('minio','rustfs'))))) THEN 'target_unavailable'
      WHEN d.position IS NOT NULL THEN 'grant' ELSE 'no_grant' END AS reason
 FROM targets t CROSS JOIN input LEFT JOIN deadlines d ON d.position=t.position
) SELECT position,observed_at,reason,CASE WHEN reason='grant' THEN expires_at END AS expires_at
 FROM rules ORDER BY position`
