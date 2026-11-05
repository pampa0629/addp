package engineaccess

import (
	"context"
	"errors"
	"slices"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
)

var ErrAuthorizationFactsUnavailable = errors.New("Meta authorization facts unavailable")
var ErrAuthorizationBoundaryChanged = errors.Join(commonapi.ErrConflict, errors.New("DataItem authorization boundary changed or incomplete"))

type AuthorizationFactsReader interface {
	ReadAuthorizationFacts(context.Context, uint, []string) (*authorizationfacts.AuthorizationFactsBatch, error)
}

// Single-item consumers use the same batch owner contract, with no alternate HTTP route.
func (s *Service) readAuthorizationFact(ctx context.Context, tenant uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
	facts, err := s.readAuthorizationFacts(ctx, tenant, []string{fp})
	if err != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	if facts[fp] == nil {
		return nil, commonapi.ErrNotFound
	}
	return facts[fp], nil
}

// Deduplicate candidate lookups within this request. Consumers still prove
// each typed target independently before observing any current source rules.
func (s *Service) readAuthorizationFacts(ctx context.Context, tenant uint, fingerprints []string) (map[string]*authorizationfacts.AuthorizationFacts, error) {
	facts := make(map[string]*authorizationfacts.AuthorizationFacts, len(fingerprints))
	unique := make([]string, 0, len(fingerprints))
	for _, fp := range fingerprints {
		if _, seen := facts[fp]; !seen {
			facts[fp] = nil
			unique = append(unique, fp)
		}
	}
	for chunk := range slices.Chunk(unique, authorizationfacts.MaxAuthorizationFactsQueryItems) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query := authorizationfacts.AuthorizationFactsQuery{Fingerprints: chunk}
		batch, err := s.authorizationFacts.ReadAuthorizationFacts(ctx, tenant, chunk)
		if err != nil || batch.Validate(query) != nil {
			return nil, ErrAuthorizationFactsUnavailable
		}
		for _, entry := range batch.Data {
			facts[entry.ItemFingerprint] = entry.Facts
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func (s *Service) WithAuthorizationFactsReader(reader AuthorizationFactsReader) *Service {
	s.authorizationFacts = reader
	return s
}

func (s *Service) resolveReadBindings(ctx context.Context, tenantID int64, paths []plugin.EngineCatalogPath) ([]authorizationfacts.Binding, error) {
	if s == nil || s.authorizationFacts == nil || tenantID <= 0 {
		return nil, ErrAuthorizationFactsUnavailable
	}
	fingerprints := make([]string, len(paths))
	for i, path := range paths {
		candidate, err := authorizationfacts.PrimaryCandidateFingerprint(path)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		fingerprints[i] = candidate
	}
	factsByFingerprint, err := s.readAuthorizationFacts(ctx, uint(tenantID), fingerprints)
	if err != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	result := make([]authorizationfacts.Binding, 0, len(paths))
	for i, path := range paths {
		facts := factsByFingerprint[fingerprints[i]]
		if facts == nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		binding, err := facts.ProvePrimaryTarget(path)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		if binding.Scope != nil {
			engine, engineErr := s.repository.engine(ctx, tenantID, int64(path.EngineID), false)
			if engineErr != nil {
				return nil, engineErr
			}
			if engine.EngineType != "nfs" {
				return nil, commonapi.ErrForbidden
			}
		}
		result = append(result, binding)
	}
	return result, nil
}

// Called only after local qualification has completed and released its locks.
// The expected digest binds explicit confirmation; owner facts remain trusted.
func (s *Service) verifyApprovedBoundary(ctx context.Context, tenantID int64, path plugin.EngineCatalogPath, fingerprint, expectedHash string) error {
	_, err := s.resolveApprovedBoundary(ctx, tenantID, path, fingerprint, expectedHash)
	return err
}

func (s *Service) resolveApprovedBoundary(ctx context.Context, tenantID int64, path plugin.EngineCatalogPath, fingerprint, expectedHash string) (*authorizationfacts.Binding, error) {
	bindings, err := s.resolveReadBindings(ctx, tenantID, []plugin.EngineCatalogPath{path})
	if err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].ItemFingerprint != fingerprint || bindings[0].BoundaryHash != expectedHash {
		return nil, ErrAuthorizationBoundaryChanged
	}
	return &bindings[0], nil
}

// Validate only the submitted identity/hash shape before owner facts are read.
// A file hash cannot prove a composition until resolveApprovedBoundary succeeds.
func validateExpectedPrimary(path plugin.EngineCatalogPath, fingerprint, hash string) error {
	if authorizationfacts.ValidWholeScopeRoot(path) || len(path.Segments) > 0 && (path.Segments[len(path.Segments)-1].Term == plugin.EngineCatalogTermFile || path.Segments[len(path.Segments)-1].Term == plugin.EngineCatalogTermObject) {
		candidate, err := authorizationfacts.PrimaryCandidateFingerprint(path)
		if err != nil || candidate != fingerprint || !authorizationfacts.ValidItemFingerprint(hash) {
			return commonapi.ErrBadRequest
		}
		return nil
	}
	return (authorizationfacts.Binding{ItemFingerprint: fingerprint, BoundaryHash: hash, NativeTarget: path}).Validate()
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
	facts, readErr := s.readAuthorizationFact(ctx, uint(actor.TenantID), fingerprint)
	if err := qualify(); err != nil {
		return nil, err
	}
	if readErr != nil {
		if errors.Is(readErr, commonapi.ErrNotFound) {
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

// Identity-only discovery for authorization candidates. No source enumeration,
// permission settings or contents are returned by this route.
func (s *Service) ListAuthorizationItems(ctx context.Context, actor Actor, engineID int64, query authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	query.EngineID = uint(engineID)
	if engineID <= 0 || query.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func() error {
		return s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
			func(_ *Repository, check func() error) error { return check() })
	}
	if err := qualify(); err != nil {
		return nil, err
	}
	if s.dataItemIdentities == nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	result, err := s.dataItemIdentities.QueryDataItemIdentities(ctx, uint(actor.TenantID), query)
	if qualificationErr := qualify(); qualificationErr != nil {
		return nil, qualificationErr
	}
	if err != nil || result == nil || result.Validate(query) != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	return result, nil
}
