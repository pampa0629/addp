package engineaccess

import (
	"context"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

// This private synchronous adapter accepts a real User credential, never an
// HTTP-supplied identity or AuthContext. The credential remains method-scoped.
// Covered is still a SOURCE RULE observation, not a complete execution Allow:
// owner function/client scope, execution and Security gates remain mandatory.
func (r *Repository) readCurrentUserSourceRules(ctx context.Context, credential string, targets []engineplugin.EngineCatalogPath) (*sourceReadRules, error) {
	return r.observeCurrentUserSourceRules(ctx, credential, targets, nil)
}

// qualify is supplied by a fixed server-side consumer, never by an HTTP body.
// It runs against the SAME current credential projection as the source rules.
func (r *Repository) observeCurrentUserSourceRules(ctx context.Context, credential string, targets []engineplugin.EngineCatalogPath, qualify func(commonauth.AuthContext) error) (*sourceReadRules, error) {
	return r.observeSourceRules(ctx, credential, targets, func(auth *iam.AuthContextService) (*commonauth.AuthContext, error) {
		return auth.ResolveUserAccessToken(ctx, credential)
	}, qualify)
}

// This fixed consumer resolves real delegated credentials without treating them
// as ordinary User tokens. Every credential still uses the same source snapshot.
func (r *Repository) observeManagerPreviewSourceRules(ctx context.Context, credential string, targets []engineplugin.EngineCatalogPath) (*sourceReadRules, error) {
	return r.observeSourceRules(ctx, credential, targets, func(auth *iam.AuthContextService) (*commonauth.AuthContext, error) {
		if strings.HasPrefix(credential, "addp_dat_") {
			return auth.ResolveDelegatedAccessToken(ctx, credential)
		}
		return auth.ResolveUserAccessToken(ctx, credential)
	}, qualifyManagerPreviewRead)
}

func (r *Repository) observeSourceRules(ctx context.Context, credential string, targets []engineplugin.EngineCatalogPath, resolve func(*iam.AuthContextService) (*commonauth.AuthContext, error), qualify func(commonauth.AuthContext) error) (*sourceReadRules, error) {
	paths, batch, err := encodeSourceReadTargets(targets)
	if err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, commonapi.ErrUnauthorized
	}
	if r == nil || r.db == nil {
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
