package engineaccess

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
)

var ErrAuthorizationFactsUnavailable = errors.New("Meta authorization facts unavailable")
var ErrAuthorizationBoundaryChanged = errors.Join(commonapi.ErrConflict, errors.New("DataItem authorization boundary changed or incomplete"))

type AuthorizationFactsReader interface {
	ReadAuthorizationFacts(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)
}

func (s *Service) WithAuthorizationFactsReader(reader AuthorizationFactsReader) *Service {
	s.authorizationFacts = reader
	return s
}

func (s *Service) resolveReadBindings(ctx context.Context, tenantID int64, paths []plugin.EngineCatalogPath) ([]authorizationfacts.Binding, error) {
	if s == nil || s.authorizationFacts == nil || tenantID <= 0 {
		return nil, ErrAuthorizationFactsUnavailable
	}
	result := make([]authorizationfacts.Binding, 0, len(paths))
	for _, path := range paths {
		candidate, err := authorizationfacts.NativeCandidateFingerprint(path)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		facts, err := s.authorizationFacts.ReadAuthorizationFacts(ctx, uint(tenantID), candidate)
		if err != nil {
			if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
				return nil, ErrAuthorizationBoundaryChanged
			}
			return nil, ErrAuthorizationFactsUnavailable
		}
		if facts == nil || facts.Validate() != nil || facts.ItemFingerprint != candidate {
			return nil, ErrAuthorizationFactsUnavailable
		}
		binding, err := facts.ProveNativeTarget(path)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		result = append(result, binding)
	}
	return result, nil
}

// Called only after local qualification has completed and released its locks.
// The expected digest binds explicit confirmation; owner facts remain trusted.
func (s *Service) verifyApprovedBoundary(ctx context.Context, tenantID int64, path plugin.EngineCatalogPath, fingerprint, expectedHash string) error {
	bindings, err := s.resolveReadBindings(ctx, tenantID, []plugin.EngineCatalogPath{path})
	if err != nil {
		return err
	}
	if len(bindings) != 1 || bindings[0].ItemFingerprint != fingerprint || bindings[0].BoundaryHash != expectedHash {
		return ErrAuthorizationBoundaryChanged
	}
	return nil
}

// AuthorizationFacts only observes current resources; it does not change ACLs.
// Close all IAM/engine transactions before the cross-owner request, and recheck
// the current user after it returns, including expired/revoked qualifications.
func (s *Service) GetAuthorizationFacts(ctx context.Context, actor Actor, engineID int64, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	if !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func() error {
		return s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
			func(_ *Repository, check func() error) error { return check() })
	}
	if err := qualify(); err != nil {
		return nil, err
	}
	if s.authorizationFacts == nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	facts, readErr := s.authorizationFacts.ReadAuthorizationFacts(ctx, uint(actor.TenantID), fingerprint)
	if err := qualify(); err != nil {
		return nil, err
	}
	if readErr != nil {
		if status, ok := client.TenantAPIStatusCode(readErr); ok && status == http.StatusNotFound {
			return nil, commonapi.ErrNotFound
		}
		return nil, ErrAuthorizationFactsUnavailable
	}
	if facts == nil || facts.Validate() != nil || facts.ItemFingerprint != fingerprint {
		return nil, ErrAuthorizationFactsUnavailable
	}
	if int64(facts.EngineID) != engineID {
		return nil, commonapi.ErrNotFound
	}
	return facts, nil
}
