package engineaccess

import (
	"context"
	"errors"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
)

type DataItemIdentityReader interface {
	QueryDataItemIdentities(context.Context, uint, authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error)
}

func (s *Service) WithDataItemIdentityReader(reader DataItemIdentityReader) *Service {
	s.dataItemIdentities = reader
	return s
}

type DirectPermissionView struct {
	EngineID        int64  `json:"engine_id,string" swaggertype:"string"`
	ItemFingerprint string `json:"item_fingerprint"`
	State           string `json:"state" enums:"allowed,denied,unknown"`
	Source          string `json:"source" enums:"explicit,deployment_default,deployment_unknown"`
	Version         int64  `json:"version,string" swaggertype:"string"`
}
type DirectPermissionIdentityView struct {
	DirectPermissionView
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	ItemType string `json:"item_type"`
}
type DirectPermissionFilter struct{ Search, State, ItemFingerprint string }
type SetDirectPermissionInput struct {
	Actor                                 Actor
	EngineID                              int64
	CatalogPath                           engineplugin.EngineCatalogPath
	ItemFingerprint, ExpectedBoundaryHash string
	Allowed                               bool
	ExpectedSource                        string
	ExpectedVersion                       int64
	Reason                                string
	Audit                                 iam.AuditMetadata
}

func directPermissionView(engineID int64, fingerprint string, decision directPermissionDecision) DirectPermissionView {
	return DirectPermissionView{EngineID: engineID, ItemFingerprint: fingerprint, State: decision.State, Source: decision.Source, Version: decision.Version}
}
func validDirectPermissionVersion(source string, version int64) bool {
	return (source == directPermissionExplicit || source == directPermissionDeployment) && version > 0 || source == directPermissionDeploymentUnknown && version == 0
}

// Managing a permit requires BOTH its dedicated Permission and the same
// current administrator qualification used by engine delegation management.
// A delegate, business owner or isolated manage Permission cannot replace it.
func (r *Repository) checkDirectPermissionAdministrator(ctx context.Context, actor Actor) error {
	now, err := r.wallClock(ctx)
	if err != nil {
		return err
	}
	rows, err := r.identity().ListEffectiveRoleAssignmentPermissions(ctx, actor.PrincipalID, iam.PrincipalTypeUser,
		iam.ContextTypeTenant, &actor.TenantID, &actor.MembershipID, now)
	if err != nil {
		return err
	}
	if !hasCurrentTenantPermission(rows, actor.TenantID, authorization.PermissionSystemEngineAccessDelegationCreate, now) {
		return commonapi.ErrForbidden
	}
	return nil
}
func (s *Service) SetDirectPermission(ctx context.Context, input SetDirectPermissionInput) (*DirectPermissionView, error) {
	if input.EngineID <= 0 || int64(input.CatalogPath.EngineID) != input.EngineID ||
		validateExpectedPrimary(input.CatalogPath, input.ItemFingerprint, input.ExpectedBoundaryHash) != nil ||
		!validDirectPermissionVersion(input.ExpectedSource, input.ExpectedVersion) || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func(tx *Repository, check func() error) error {
		if err := check(); err != nil {
			return err
		}
		return tx.checkDirectPermissionAdministrator(ctx, input.Actor)
	}
	if err := s.withEngineManagementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessDirectPermissionUpdate, true, qualify); err != nil {
		return nil, err
	}
	if err := s.verifyApprovedBoundary(ctx, input.Actor.TenantID, input.CatalogPath, input.ItemFingerprint, input.ExpectedBoundaryHash); err != nil {
		return nil, err
	}
	var result DirectPermissionView
	err := s.withEngineManagementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessDirectPermissionUpdate, true,
		func(tx *Repository, check func() error) error {
			row, err := tx.setDirectPermission(ctx, input, func() error { return qualify(tx, check) })
			if err != nil {
				return err
			}
			decision, err := resolveDirectPermission(&directPermissionSetting{Allowed: row.Allowed, Version: row.Version}, nil)
			if err != nil {
				return err
			}
			result = directPermissionView(row.EngineID, row.ItemFingerprint, decision)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
func (s *Service) ListDirectPermissions(ctx context.Context, actor Actor, engineID int64, page, size int, filter DirectPermissionFilter) ([]DirectPermissionIdentityView, int64, error) {
	if filter.ItemFingerprint != "" && !authorizationfacts.ValidItemFingerprint(filter.ItemFingerprint) {
		return nil, 0, commonapi.ErrBadRequest
	}
	var before *directPermissionObservation
	capture := func() error {
		return s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessDirectPermissionRead, false,
			func(tx *Repository, check func() error) error {
				var err error
				before, err = tx.directPermissionObservation(ctx, actor.TenantID, engineID)
				if err != nil {
					return err
				}
				return check()
			})
	}
	if err := capture(); err != nil {
		return nil, 0, err
	}
	query, err := prepareDirectPermissionIdentityQuery(uint(engineID), page, size, filter, before)
	if err != nil {
		return nil, 0, err
	}
	if s.dataItemIdentities == nil {
		return nil, 0, ErrAuthorizationFactsUnavailable
	}
	identities, readErr := s.dataItemIdentities.QueryDataItemIdentities(ctx, uint(actor.TenantID), query)
	err = s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessDirectPermissionRead, false,
		func(tx *Repository, check func() error) error {
			after, err := tx.directPermissionObservation(ctx, actor.TenantID, engineID)
			if err != nil {
				return err
			}
			if err = check(); err != nil {
				return err
			}
			if !reflect.DeepEqual(before, after) {
				return ErrDirectPermissionVersion
			}
			return nil
		})
	if err != nil {
		return nil, 0, err
	}
	if readErr != nil || identities == nil || identities.Validate(query) != nil {
		return nil, 0, ErrAuthorizationFactsUnavailable
	}
	rows := make([]DirectPermissionIdentityView, 0, len(identities.Data))
	for _, item := range identities.Data {
		decision, err := decisionForIdentity(before, item.ItemFingerprint)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, DirectPermissionIdentityView{DirectPermissionView: directPermissionView(engineID, item.ItemFingerprint, decision), Name: item.Name, FullName: item.FullName, ItemType: item.ItemType})
	}
	return rows, identities.Total, nil
}

func prepareDirectPermissionIdentityQuery(engineID uint, page, size int, filter DirectPermissionFilter, observation *directPermissionObservation) (authorizationfacts.IdentityQuery, error) {
	query, err := directPermissionIdentityQuery(engineID, filter.Search, filter.State, page, size, observation.Settings, observation.Deployment)
	if errors.Is(err, authorizationfacts.ErrIdentityQueryCapacity) {
		// The caller supplied a valid filter, but the complete owner predicate
		// exceeds the bounded query. Never truncate it or return an empty page.
		return authorizationfacts.IdentityQuery{}, ErrAuthorizationFactsUnavailable
	}
	if err != nil {
		return authorizationfacts.IdentityQuery{}, commonapi.ErrBadRequest
	}
	if filter.ItemFingerprint != "" {
		decision, err := decisionForIdentity(observation, filter.ItemFingerprint)
		if err != nil {
			return authorizationfacts.IdentityQuery{}, err
		}
		query.FingerprintFilter, query.Fingerprints = "include", []string{}
		if filter.State == "" || filter.State == decision.State {
			query.Fingerprints = []string{filter.ItemFingerprint}
		}
	}
	return query, nil
}

func decisionForIdentity(observation *directPermissionObservation, fingerprint string) (directPermissionDecision, error) {
	if setting, ok := observation.Settings[fingerprint]; ok {
		return resolveDirectPermission(&setting, nil)
	}
	return resolveDirectPermission(nil, observation.Deployment)
}
func (s *Service) GetDirectPermission(ctx context.Context, actor Actor, engineID int64, fingerprint string) (*DirectPermissionView, error) {
	if !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return nil, commonapi.ErrBadRequest
	}
	rows, total, err := s.ListDirectPermissions(ctx, actor, engineID, 1, 1, DirectPermissionFilter{ItemFingerprint: fingerprint})
	if err != nil {
		return nil, err
	}
	if total != 1 || len(rows) != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &rows[0].DirectPermissionView, nil
}
