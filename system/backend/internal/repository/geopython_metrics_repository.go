package repository

import (
	"context"

	commonmodels "github.com/addp/common/models"
)

func (r *EngineRepository) DeclareRuntimeMetrics(ctx context.Context, identity commonmodels.RuntimeMetricsIdentity) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO system.runtime_metrics_leases (engine_id, instance_id, started_at, endpoint, lease_expires_at)
		SELECT id, ?, ?, ?, ?
		FROM system.engines
		WHERE id = ? AND engine_type = ? AND engine_origin = 'extension'
		  AND is_builtin AND tenant_id IS NULL AND lifecycle_state = 'active'
		ON CONFLICT (engine_id) DO UPDATE
		SET engine_id = EXCLUDED.engine_id, instance_id = EXCLUDED.instance_id,
		    started_at = EXCLUDED.started_at, endpoint = EXCLUDED.endpoint, lease_expires_at = EXCLUDED.lease_expires_at
		WHERE EXCLUDED.started_at > runtime_metrics_leases.started_at OR
		      (EXCLUDED.started_at = runtime_metrics_leases.started_at
		       AND EXCLUDED.engine_id = runtime_metrics_leases.engine_id
		       AND EXCLUDED.instance_id = runtime_metrics_leases.instance_id
		       AND EXCLUDED.endpoint = runtime_metrics_leases.endpoint)
	`, identity.InstanceID, identity.StartedAt, identity.Endpoint, identity.LeaseExpiresAt, identity.EngineID, commonmodels.RuntimeMetricsEngineType(identity.Kind))
	return result.RowsAffected == 1, result.Error
}
