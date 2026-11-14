package engineaccess

import (
	"context"

	"github.com/google/uuid"
)

// lockAccessRequest serializes issuance and fulfillment for one request, even
// before a Grant exists. The caller's transaction owns the lock until it ends.
func (r *Repository) lockAccessRequest(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_request|"+id.String()).Error
}
