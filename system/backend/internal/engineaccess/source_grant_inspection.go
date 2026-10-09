package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
)

// SourceGrantInspection is an uncached rule observation, not actual access or
// credential evidence. No denial identifiers or bodies are exposed.
type SourceGrantInspection struct {
	AccountID   int64                          `json:"account_id,string" swaggertype:"string"`
	CatalogPath engineplugin.EngineCatalogPath `json:"catalog_path"`
	ObservedAt  time.Time                      `json:"observed_at"`
	RuleCovered bool                           `json:"rule_covered"`
	Reason      string                         `json:"reason" enums:"grant,no_grant,explicit_deny,source_unavailable,target_unavailable"`
	Sources     []SourceGrantInspectionSource  `json:"sources"`
}

type SourceGrantInspectionSource struct {
	RecipientType string     `json:"recipient_type"`
	RecipientID   int64      `json:"recipient_id,string" swaggertype:"string"`
	ExpiryMode    string     `json:"expiry_mode"`
	ExpiresAt     *time.Time `json:"expires_at"`
	GrantCount    int64      `json:"grant_count"`
}

// The selected account is an object of management, never a borrowed caller.
// IAM resolution, membership expansion, rules and sources share one statement.
const sourceGrantInspectionSQL = `WITH input AS MATERIALIZED (
 SELECT m.tenant_id, p.id AS principal_id, m.id AS membership_id,
 p.authorization_version, ?::jsonb AS targets, clock_timestamp() AS observed_at
 FROM system.tenant_memberships m
 JOIN system.principals p ON p.id = m.principal_id AND p.principal_type = 'user'
 JOIN system.users u ON u.id = p.id
 WHERE m.tenant_id = ? AND p.id = ?
)` + sourceReadRuleCTEs + `, sources AS (
 SELECT recipient_type, recipient_id, count(*) AS grant_count,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at
 FROM matched_grants GROUP BY recipient_type, recipient_id
)
 SELECT rules.*, COALESCE((SELECT jsonb_agg(jsonb_build_object(
 'recipient_type', s.recipient_type, 'recipient_id', s.recipient_id::text,
 'grant_count', s.grant_count, 'expiry_mode', s.expiry_mode, 'expires_at', s.expires_at)
 ORDER BY s.recipient_type, s.recipient_id) FROM sources s), '[]'::jsonb) AS sources FROM rules`

func (s *Service) InspectSourceGrants(ctx context.Context, actor Actor, engineID, accountID int64, path engineplugin.EngineCatalogPath) (*SourceGrantInspection, error) {
	paths, batch, err := encodeSourceReadTargets([]engineplugin.EngineCatalogPath{path})
	if err != nil || accountID <= 0 || int64(path.EngineID) != engineID {
		return nil, commonapi.ErrBadRequest
	}
	var result *SourceGrantInspection
	err = s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
		func(tx *Repository, check func() error) error {
			var rows []struct {
				Position   int64
				ObservedAt time.Time
				Reason     string
				Sources    json.RawMessage
			}
			if err := tx.db.WithContext(ctx).Raw(sourceGrantInspectionSQL, string(batch), actor.TenantID, accountID).Scan(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				return commonapi.ErrNotFound
			}
			observation, err := sourceReadObservation(paths, []sourceReadRuleRow{{Position: rows[0].Position, ObservedAt: rows[0].ObservedAt, Reason: rows[0].Reason}})
			if err != nil || len(rows) != 1 {
				return errSourceReadRules
			}
			result = &SourceGrantInspection{AccountID: accountID, CatalogPath: paths[0], ObservedAt: observation.ObservedAt,
				RuleCovered: observation.Covered, Reason: observation.Targets[0].Reason, Sources: make([]SourceGrantInspectionSource, 0)}
			if err := json.Unmarshal(rows[0].Sources, &result.Sources); err != nil {
				return err
			}
			return check()
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
