package engineaccess

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// Metadata admits registered engine types; trusted item identity is proved
// independently. Content retains its existing exact boundary verification.
func sourceAccessActionSupported(action, engineType string) bool {
	return action == authorization.SourceAccessRead ||
		(action == authorization.SourceAccessMetadataRead && engineType != "")
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

// Catalog fulfillment retains its admitted PG metadata profile. Direct
// metadata grants and fixed Meta checks have their own exact identity proof.
func sourceFulfillmentActionSupported(action, engineType string) bool {
	return sourceAccessActionSupported(action, engineType) && (action != authorization.SourceAccessMetadataRead || engineType == "postgresql")
}
func (r *Repository) checkFulfillmentSourceAccessAction(ctx context.Context, tenantID, engineID int64, action string) error {
	engine, err := r.engine(ctx, tenantID, engineID, false)
	if err != nil {
		return err
	}
	if engine == nil || !sourceFulfillmentActionSupported(action, engine.EngineType) {
		return commonapi.ErrBadRequest
	}
	return nil
}
