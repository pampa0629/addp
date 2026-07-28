package iam

import (
	"context"
	"testing"
)

func createUninitializedTenantFixture(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	code string,
	name string,
	description string,
) *Tenant {
	t.Helper()
	tenant := &Tenant{Code: code, Name: name, Description: description, Status: TenantStatusActive}
	if err := repository.Transaction(ctx, func(tx *Repository) error {
		return tx.CreateTenant(ctx, tenant)
	}); err != nil {
		t.Fatalf("create uninitialized tenant fixture %s: %v", code, err)
	}
	return tenant
}
