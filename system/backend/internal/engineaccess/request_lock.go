package engineaccess

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lockAccessRequest serializes issuance and fulfillment for one request, even
// before a Grant exists. The caller's transaction owns the lock until it ends.
func (r *Repository) lockAccessRequest(ctx context.Context, id uuid.UUID) error {
	switch r.db.Dialector.Name() {
	case "postgres":
		return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_request|"+id.String()).Error
	case "mysql":
		if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok {
			return fmt.Errorf("MySQL access request lock requires the outer transaction")
		}
		return r.db.WithContext(ctx).Exec(
			"INSERT INTO `system`.`engine_access_request_locks` (request_id) VALUES (UUID_TO_BIN(?)) ON DUPLICATE KEY UPDATE request_id = request_id",
			id.String(),
		).Error
	default:
		return fmt.Errorf("unsupported access request lock database %q", r.db.Dialector.Name())
	}
}
