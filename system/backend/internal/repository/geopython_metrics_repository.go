package repository

import (
	"context"

	commonmodels "github.com/addp/common/models"
)

func (r *EngineRepository) DeclareGeoPythonMetrics(ctx context.Context, identity commonmodels.GeoPythonMetricsIdentity) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO system.geopython_metrics_lease (engine_id, instance_id, started_at, endpoint, lease_expires_at)
		SELECT id, ?, ?, ?, ?
		FROM system.engines
		WHERE id = ? AND engine_type = 'geopython_workflow' AND engine_origin = 'extension'
		  AND is_builtin AND tenant_id IS NULL AND lifecycle_state = 'active'
		ON CONFLICT (singleton) DO UPDATE
		SET engine_id = EXCLUDED.engine_id, instance_id = EXCLUDED.instance_id,
		    started_at = EXCLUDED.started_at, endpoint = EXCLUDED.endpoint, lease_expires_at = EXCLUDED.lease_expires_at
		WHERE EXCLUDED.started_at > geopython_metrics_lease.started_at OR
		      (EXCLUDED.started_at = geopython_metrics_lease.started_at
		       AND EXCLUDED.engine_id = geopython_metrics_lease.engine_id
		       AND EXCLUDED.instance_id = geopython_metrics_lease.instance_id
		       AND EXCLUDED.endpoint = geopython_metrics_lease.endpoint)
	`, identity.InstanceID, identity.StartedAt, identity.Endpoint, identity.LeaseExpiresAt, identity.EngineID)
	return result.RowsAffected == 1, result.Error
}
