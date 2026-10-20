package engineaccess

import (
	"context"
	"strconv"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

// Every fixed consumer uses this one credential and rule snapshot after Meta
// preparation. No caller-supplied AuthContext is accepted as identity evidence.
type sourceReadEngineVersion struct {
	ID      uint
	Version int64
}

func (r *Repository) observeSourceRules(ctx context.Context, credential string, targets []engineplugin.EngineCatalogPath, bindings []authorizationfacts.Binding, expectedTenantID int64, resolve func(*iam.AuthContextService) (*commonauth.AuthContext, error), qualify func(commonauth.AuthContext) error, expectedEngine *sourceReadEngineVersion) (*sourceReadRules, error) {
	paths, _, err := encodeSourceReadTargets(targets)
	if err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, commonapi.ErrUnauthorized
	}
	if r == nil || r.db == nil {
		return nil, errSourceReadRules
	}
	batch, err := encodeBoundSourceTargets(paths, bindings)
	if err != nil || expectedTenantID <= 0 {
		return nil, errSourceReadRules
	}
	var result *sourceReadRules
	err = r.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		// IAM's nested read-only projection shares the outer Repeatable Read
		// transaction and its committed snapshot, rather than opening another pool.
		current, err := resolve(auth)
		if err != nil {
			return err
		}
		if current.Principal.Type != "user" || current.Context.Type != "tenant" ||
			current.Context.TenantID == nil || current.Context.TenantMembershipID == nil {
			return commonapi.ErrForbidden
		}
		if qualify != nil {
			if err := qualify(*current); err != nil {
				return err
			}
		}
		parse := func(value string) (int64, error) {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
				return 0, errSourceReadRules
			}
			return id, nil
		}
		tenantID, err := parse(*current.Context.TenantID)
		if err != nil {
			return err
		}
		if tenantID != expectedTenantID {
			return commonapi.ErrForbidden
		}
		if expectedEngine != nil {
			engine, err := tx.engine(ctx, tenantID, int64(expectedEngine.ID), false)
			if err != nil {
				return err
			}
			if engine.EngineType != "ftp" || engine.LifecycleState != "active" || engine.Version != expectedEngine.Version || engine.DeletedAt != nil {
				return commonapi.ErrForbidden
			}
		}
		principalID, err := parse(current.Principal.ID)
		if err != nil {
			return err
		}
		membershipID, err := parse(*current.Context.TenantMembershipID)
		if err != nil {
			return err
		}
		version, err := parse(current.Authorization.AuthorizationVersion)
		if err != nil {
			return err
		}
		observation, err := tx.queryCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID,
			Source: userProvenance{PrincipalID: principalID, MembershipID: membershipID, AuthorizationVersion: version}}, paths, batch)
		if err != nil {
			return err
		}
		// Transaction-start time must not prolong a naturally expiring credential
		// after waiting for a query/connection. Failure to read this clock denies.
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !current.Token.ExpiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		result = observation
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
