package engineaccess

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// Separate metadata rules are currently admitted only for PostgreSQL. Existing
// content actions still use their own canonical target/capability verification.
func sourceAccessActionSupported(action, engineType string) bool {
	return action == authorization.SourceAccessRead ||
		(action == authorization.SourceAccessMetadataRead && engineType == "postgresql")
}

func (r *Repository) checkSourceAccessAction(ctx context.Context, tenantID, engineID int64, action string) error {
	engine, err := r.engine(ctx, tenantID, engineID, false)
	if err != nil {
		return err
	}
	if engine == nil || !sourceAccessActionSupported(action, engine.EngineType) {
		return commonapi.ErrBadRequest
	}
	return nil
}
