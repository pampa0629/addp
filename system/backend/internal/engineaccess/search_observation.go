package engineaccess

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/system/internal/iam"
)

var ErrSearchContextChanged = errors.Join(commonapi.ErrConflict, errors.New("search observation basis changed"))

type AuthorizationBasisRevisionReader interface {
	ReadAuthorizationBasisRevision(context.Context, uint) (*authorizationfacts.Revision, error)
}

type searchObservationCaptureKey struct{}
type searchObservationCapture struct {
	Key   []byte
	Nonce string
	Meta  authorizationfacts.Revision
	Tag   string
}

func (s *Service) ConfigureSearchObservation(key []byte, reader AuthorizationBasisRevisionReader) error {
	if len(key) != 32 {
		return errors.New("search observation requires the configured 32-byte System encryption key")
	}
	derived, err := hkdf.Key(sha256.New, key, nil, "manager-search-observation/v1", 32)
	if err != nil {
		return err
	}
	s.searchObservationKey = derived
	s.searchRevisionReader = reader
	return nil
}

// Called inside the exact read-only snapshot that resolves the credential and
// observes Grant rules. Never use a second Repeatable Read as a fresh recheck.
func (r *Repository) captureSearchObservation(ctx context.Context, current *shared.AuthContext, observedAt time.Time) error {
	capture, _ := ctx.Value(searchObservationCaptureKey{}).(*searchObservationCapture)
	if capture == nil {
		return nil
	}
	tag, err := r.searchObservationTag(ctx, current, observedAt, capture)
	if err != nil {
		return err
	}
	capture.Tag = tag
	return nil
}

func (r *Repository) searchObservationTag(ctx context.Context, current *shared.AuthContext, observedAt time.Time, capture *searchObservationCapture) (string, error) {
	tenant, err := strconv.ParseInt(*current.Context.TenantID, 10, 64)
	if err != nil {
		return "", err
	}
	principal, err := strconv.ParseInt(current.Principal.ID, 10, 64)
	if err != nil {
		return "", err
	}
	membership, err := strconv.ParseInt(*current.Context.TenantMembershipID, 10, 64)
	if err != nil {
		return "", err
	}
	version, err := strconv.ParseInt(current.Authorization.AuthorizationVersion, 10, 64)
	if err != nil {
		return "", err
	}
	var revision struct{ Generation, Version string }
	if err = r.db.WithContext(ctx).Raw(`SELECT g.generation::text AS generation,COALESCE(r.version,0)::text AS version
 FROM system.search_authorization_generation g LEFT JOIN system.search_authorization_revisions r ON r.tenant_id=? WHERE g.singleton`, tenant).Scan(&revision).Error; err != nil {
		return "", err
	}
	if revision.Generation == "" || revision.Version == "" {
		return "", errSourceReadRules
	}
	var boundary struct{ NextBoundary *time.Time }
	// Reuse the current source/recipient CTE; this is a time aggregate, not an
	// enumeration of accessible DataItems. Unrelated role deadlines may expire early.
	query := `WITH input AS (SELECT ?::bigint tenant_id,?::bigint principal_id,?::bigint membership_id,
 ?::bigint authorization_version,?::timestamptz observed_at)` + sourceReadSubjectCTEs + `, boundaries AS (
 SELECT g.granted_at AS boundary FROM system.engine_access_grants g JOIN recipients r USING(recipient_type,recipient_id)
 WHERE g.tenant_id=? AND NOT EXISTS(SELECT 1 FROM system.engine_access_grant_revocations v WHERE v.request_id=g.request_id)
 UNION ALL SELECT g.expires_at FROM system.engine_access_grants g JOIN recipients r USING(recipient_type,recipient_id)
 WHERE g.tenant_id=? AND NOT EXISTS(SELECT 1 FROM system.engine_access_grant_revocations v WHERE v.request_id=g.request_id)
 UNION ALL SELECT expires_at FROM system.tenant_memberships WHERE id=?
 UNION ALL SELECT valid_from FROM system.role_assignments WHERE principal_id=? AND tenant_id=? AND status='active'
 UNION ALL SELECT valid_until FROM system.role_assignments WHERE principal_id=? AND tenant_id=? AND status='active'
 ) SELECT min(boundary) AS next_boundary FROM boundaries WHERE boundary>?`
	if err = r.db.WithContext(ctx).Raw(query, tenant, principal, membership, version, observedAt, tenant, tenant, membership, principal, tenant, principal, tenant, observedAt).Scan(&boundary).Error; err != nil {
		return "", err
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return "", err
	}
	if boundary.NextBoundary != nil {
		value := boundary.NextBoundary.UTC()
		boundary.NextBoundary = &value
	}
	if boundary.NextBoundary != nil && !now.Before(*boundary.NextBoundary) {
		return "", ErrSearchContextChanged
	}
	stable := struct {
		Principal                       shared.AuthPrincipal
		Context                         shared.AuthSessionContext
		Client                          shared.ClientConstraints
		Organization                    shared.OrganizationContext
		Authorization                   shared.AuthorizationFacts
		Delegation                      *shared.DelegationFacts
		CredentialType                  string
		SystemGeneration, SystemVersion string
		Meta                            authorizationfacts.Revision
		NextBoundary                    *time.Time
		Nonce, Semantics                string
	}{current.Principal, current.Context, current.Client, current.Organization, current.Authorization, current.Delegation, current.Token.Type, revision.Generation, revision.Version, capture.Meta, boundary.NextBoundary, capture.Nonce, "manager-search/v1"}
	data, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, capture.Key)
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) freshSearchObservation(ctx context.Context, credential string, capture *searchObservationCapture) error {
	return s.repository.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		current, err := resolveSearchCredential(ctx, auth, credential)
		if err != nil {
			return err
		}
		if err = qualifyManagerSearchRead(*current); err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !now.Before(current.Token.ExpiresAt) {
			return commonapi.ErrUnauthorized
		}
		capture.Tag, err = tx.searchObservationTag(ctx, current, now, capture)
		return err
	})
}

func resolveSearchCredential(ctx context.Context, auth *iam.AuthContextService, credential string) (*shared.AuthContext, error) {
	if len(credential) >= 9 && credential[:9] == "addp_dat_" {
		return auth.ResolveDelegatedAccessToken(ctx, credential)
	}
	return auth.ResolveUserAccessToken(ctx, credential)
}

func (s *Service) CheckManagerSearchRead(ctx context.Context, credential string, request shared.SourceSearchCheckRequest) (*shared.SourceSearchCheck, error) {
	if !request.Valid() {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	resolve := func(auth *iam.AuthContextService) (*shared.AuthContext, error) {
		return resolveSearchCredential(ctx, auth, credential)
	}
	started := time.Now()
	tenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualifyManagerSearchRead)
	if timing := managerSearchTiming(ctx); timing != nil {
		timing.preMetaCredential = time.Since(started)
	}
	if err != nil {
		return nil, err
	}
	if len(s.searchObservationKey) != 32 || s.searchRevisionReader == nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	m0, err := s.searchRevisionReader.ReadAuthorizationBasisRevision(ctx, uint(tenant))
	if err != nil || m0 == nil || m0.Validate() != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	capture := &searchObservationCapture{Key: s.searchObservationKey, Nonce: request.QueryNonce, Meta: *m0}
	observedCtx := context.WithValue(ctx, searchObservationCaptureKey{}, capture)
	result, err := s.checkManagerSearchTargets(observedCtx, credential, tenant, shared.SourceMetadataCheckRequest{Targets: request.Targets})
	if err != nil {
		return nil, err
	}
	m1, err := s.searchRevisionReader.ReadAuthorizationBasisRevision(ctx, uint(tenant))
	if err != nil || m1 == nil || m1.Validate() != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	if *m0 != *m1 {
		return nil, ErrSearchContextChanged
	}
	fresh := &searchObservationCapture{Key: s.searchObservationKey, Nonce: request.QueryNonce, Meta: *m1}
	if err = s.freshSearchObservation(ctx, credential, fresh); err != nil {
		return nil, err
	}
	if capture.Tag == "" || !hmac.Equal([]byte(capture.Tag), []byte(fresh.Tag)) || request.ExpectedObservationTag != nil && !hmac.Equal([]byte(*request.ExpectedObservationTag), []byte(fresh.Tag)) {
		return nil, ErrSearchContextChanged
	}
	result.ObservationTag = fresh.Tag
	return result, nil
}
