package service

import (
	"context"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func requirePostgreSQLEngine(ctx context.Context, client *commonClient.SystemServiceClient, tenantID, engineID int64) error {
	if client == nil || engineID <= 0 {
		return fmt.Errorf("%w: a PostgreSQL engine is required", commonAPI.ErrBadRequest)
	}
	engine, err := client.WithTenantID(uint(tenantID)).GetEngine(ctx, uint(engineID))
	if err != nil {
		return fmt.Errorf("get target engine: %w", err)
	}
	if !strings.EqualFold(engine.EngineType, "postgresql") {
		return fmt.Errorf("%w: quality v1 only supports PostgreSQL engines", commonAPI.ErrBadRequest)
	}
	if engine.LifecycleState != commonModels.EngineLifecycleActive {
		return fmt.Errorf("%w: target PostgreSQL engine is not active", commonAPI.ErrBadRequest)
	}
	return nil
}
