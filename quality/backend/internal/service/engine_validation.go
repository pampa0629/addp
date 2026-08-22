package service

import (
	"context"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
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
	if !engineselection.IsAvailable(engine) {
		return fmt.Errorf("%w: target PostgreSQL engine is not currently available", commonAPI.ErrBadRequest)
	}
	return nil
}
