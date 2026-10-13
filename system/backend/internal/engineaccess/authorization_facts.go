package engineaccess

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/system/internal/authorization"
)

var ErrAuthorizationFactsUnavailable = errors.New("Meta authorization facts unavailable")

type AuthorizationFactsReader interface {
	ReadAuthorizationFacts(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)
}

func (s *Service) WithAuthorizationFactsReader(reader AuthorizationFactsReader) *Service {
	s.authorizationFacts = reader
	return s
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
