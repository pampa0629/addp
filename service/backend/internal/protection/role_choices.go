package protection

import (
	"context"
	"time"

	"github.com/addp/common/dataprotection"
)

func (g *Gate) RoleOptions(ctx context.Context, tenant int64, target dataprotection.ResourceReference, roles []string) ([]dataprotection.FieldRoleOptions, error) {
	end, err := g.begin(uint(tenant))
	if err != nil {
		return nil, err
	}
	defer end()
	return g.store.RoleOptions(ctx, tenant, target, "service_execute", roles, time.Now().UTC())
}

func (g *Gate) ValidateRoleChoices(ctx context.Context, tenant int64, selection *dataprotection.QueryRoleSelection) error {
	end, err := g.begin(uint(tenant))
	if err != nil {
		return err
	}
	defer end()
	return g.store.ValidateRoleChoices(ctx, tenant, selection, "service_execute", time.Now().UTC())
}
