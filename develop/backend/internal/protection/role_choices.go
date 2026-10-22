package protection

import (
	"context"
	"github.com/addp/common/dataprotection"
	"time"
)

func (g *Gate) RoleOptions(ctx context.Context, tenant int64, target dataprotection.ResourceReference, roles []string) ([]dataprotection.FieldRoleOptions, error) {
	end, err := g.begin(uint(tenant))
	if err != nil {
		return nil, err
	}
	defer end()
	return g.store.RoleOptions(ctx, tenant, target, "query", roles, time.Now().UTC())
}
func (g *Gate) ValidateRoleChoices(ctx context.Context, tenant int64, selection *dataprotection.QueryRoleSelection) error {
	end, err := g.begin(uint(tenant))
	if err != nil {
		return err
	}
	defer end()
	return g.store.ValidateRoleChoices(ctx, tenant, selection, "query", time.Now().UTC())
}
