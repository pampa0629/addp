package engineaccess

import (
	"context"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

func fixturePermitInput(ctx context.Context, r *Repository, tenant, principal int64, path engineplugin.EngineCatalogPath, allowed bool, version int64) SetDirectPermissionInput {
	return fixturePermitBindingInput(ctx, r, tenant, principal, path, factstest.Binding(path), allowed, version)
}
func fixturePermitBindingInput(ctx context.Context, r *Repository, tenant, principal int64, path engineplugin.EngineCatalogPath, binding authorizationfacts.Binding, allowed bool, version int64) SetDirectPermissionInput {
	input := SetDirectPermissionInput{Actor: Actor{TenantID: tenant, PrincipalID: principal}, EngineID: int64(path.EngineID), CatalogPath: path, ItemFingerprint: binding.ItemFingerprint, ExpectedBoundaryHash: binding.BoundaryHash, Allowed: allowed, ExpectedSource: "explicit", ExpectedVersion: version, Reason: "Explicit isolated permit fixture", Audit: iam.AuditMetadata{}}
	if version == 0 {
		input.ExpectedSource = "deployment_unknown"
		if d, e := r.catalogDeployment(ctx, false); e == nil && d != nil {
			input.ExpectedSource = "deployment_default"
			input.ExpectedVersion = d.Version
		}
	}
	return input
}
func fixtureSetPermit(ctx context.Context, r *Repository, input SetDirectPermissionInput, verify func(*Repository) error) (*directPermission, error) {
	var check func() error
	if verify != nil {
		check = func() error { return verify(r) }
	}
	return r.setDirectPermission(ctx, input, check)
}

// Repository fixture setup is used by Grant/read tests; administrator and
// live permission checks are exercised separately through the service/API.
func installFixturePermit(ctx context.Context, r *Repository, tenant, principal int64, path engineplugin.EngineCatalogPath) (*directPermission, error) {
	input := fixturePermitInput(ctx, r, tenant, principal, path, true, 0)
	var row *directPermission
	err := r.transaction(ctx, func(tx *Repository) error {
		var err error
		row, err = fixtureSetPermit(ctx, tx, input, func(*Repository) error { return nil })
		return err
	})
	return row, err
}
