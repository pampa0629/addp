package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"github.com/ory/fosite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStorageAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset OAuth storage test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	principalID, membershipID, authorizationVersion := seedOAuthIdentity(t, db, now)
	storage, err := NewStorage(db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	storage.now = func() time.Time { return now }
	client, err := storage.GetClient(ctx, "addp-cli")
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}

	t.Run("transaction lifecycle", func(t *testing.T) {
		txCtx, err := storage.BeginTX(ctx)
		if err != nil {
			t.Fatalf("BeginTX() error = %v", err)
		}
		if _, err := storage.BeginTX(txCtx); err == nil {
			t.Fatal("nested transaction was accepted")
		}
		if err := storage.Rollback(txCtx); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
		if err := storage.Commit(txCtx); err == nil {
			t.Fatal("commit after rollback was accepted")
		}
	})

	t.Run("family creation uses database transaction time", func(t *testing.T) {
		databaseTimestamp, err := storage.databaseNow(ctx)
		if err != nil {
			t.Fatalf("read database time: %v", err)
		}
		originalNow := storage.now
		storage.now = func() time.Time { return databaseTimestamp.Add(-time.Second) }
		defer func() { storage.now = originalNow }()

		session := approvedTestSession(principalID, membershipID, authorizationVersion, databaseTimestamp)
		session.AuthenticatedAt = databaseTimestamp
		session.SetExpiresAt(fosite.AccessToken, databaseTimestamp.Add(15*time.Minute))
		session.SetExpiresAt(fosite.RefreshToken, databaseTimestamp.Add(time.Hour))
		requestID := uuid.New()
		request := &fosite.Request{
			ID:                requestID.String(),
			RequestedAt:       databaseTimestamp,
			Client:            client,
			RequestedScope:    fosite.Arguments{"addp.api"},
			GrantedScope:      fosite.Arguments{"addp.api"},
			RequestedAudience: fosite.Arguments{"addp.api"},
			GrantedAudience:   fosite.Arguments{"addp.api"},
			Session:           session,
		}
		family, err := storage.createOAuthFamily(ctx, iam.NewRepository(db), requestID, request)
		if err != nil {
			t.Fatalf("createOAuthFamily() error = %v", err)
		}
		if family.CreatedAt.Before(family.AuthenticatedAt) {
			t.Fatalf("family created_at %s is before authenticated_at %s", family.CreatedAt, family.AuthenticatedAt)
		}
	})

	t.Run("authorization code PKCE and refresh replay", func(t *testing.T) {
		requestID := uuid.New()
		insertApprovedAuthorizationRequest(
			t, db, requestID, principalID, membershipID, authorizationVersion, now,
		)
		session := approvedTestSession(principalID, membershipID, authorizationVersion, now)
		session.SetExpiresAt(fosite.AuthorizeCode, now.Add(5*time.Minute))
		authorizeRequest := &fosite.Request{
			ID:                requestID.String(),
			RequestedAt:       now.Add(-time.Minute),
			Client:            client,
			RequestedScope:    fosite.Arguments{"addp.api"},
			GrantedScope:      fosite.Arguments{"addp.api"},
			RequestedAudience: fosite.Arguments{"addp.api"},
			GrantedAudience:   fosite.Arguments{"addp.api"},
			Session:           session,
		}
		codeSignature := opaqueSignature("addp_ac_storage-postgres-code")
		if err := storage.CreateAuthorizeCodeSession(ctx, codeSignature, authorizeRequest); err != nil {
			t.Fatalf("CreateAuthorizeCodeSession() error = %v", err)
		}
		if err := storage.CreatePKCERequestSession(ctx, codeSignature, authorizeRequest); err != nil {
			t.Fatalf("CreatePKCERequestSession() error = %v", err)
		}
		pkceRequest, err := storage.GetPKCERequestSession(ctx, codeSignature, NewIAMSession())
		if err != nil || pkceRequest.GetRequestForm().Get("code_challenge") != testCodeChallenge {
			t.Fatalf("GetPKCERequestSession() = %#v, %v", pkceRequest, err)
		}
		if err := storage.DeletePKCERequestSession(ctx, codeSignature); err != nil {
			t.Fatalf("DeletePKCERequestSession() error = %v", err)
		}

		storedRequest, err := storage.GetAuthorizeCodeSession(ctx, codeSignature, NewIAMSession())
		if err != nil {
			t.Fatalf("GetAuthorizeCodeSession() error = %v", err)
		}
		storedRequest.GetSession().SetExpiresAt(fosite.AccessToken, now.Add(15*time.Minute))
		storedRequest.GetSession().SetExpiresAt(fosite.RefreshToken, now.Add(30*24*time.Hour))
		accessSignature := opaqueSignature("addp_at_storage-postgres-access-1")
		refreshSignature := opaqueSignature("addp_rt_storage-postgres-refresh-1")
		txCtx, err := storage.BeginTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.InvalidateAuthorizeCodeSession(txCtx, codeSignature); err != nil {
			t.Fatalf("InvalidateAuthorizeCodeSession() error = %v", err)
		}
		if err := storage.CreateAccessTokenSession(txCtx, accessSignature, storedRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("CreateAccessTokenSession() error = %v", err)
		}
		if err := storage.CreateRefreshTokenSession(txCtx, refreshSignature, accessSignature, storedRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("CreateRefreshTokenSession() error = %v", err)
		}
		if err := storage.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		replayed, err := storage.GetAuthorizeCodeSession(ctx, codeSignature, NewIAMSession())
		if replayed == nil || !errors.Is(err, fosite.ErrInvalidatedAuthorizeCode) || replayed.GetID() != requestID.String() {
			t.Fatalf("code replay = %#v, %v", replayed, err)
		}

		refreshRequest, err := storage.GetRefreshTokenSession(ctx, refreshSignature, NewIAMSession())
		if err != nil {
			t.Fatalf("GetRefreshTokenSession() error = %v", err)
		}
		refreshRequest.GetRequestForm().Set("grant_type", string(fosite.GrantTypeRefreshToken))
		refreshRequest.GetSession().SetExpiresAt(fosite.AccessToken, now.Add(15*time.Minute))
		newAccessSignature := opaqueSignature("addp_at_storage-postgres-access-2")
		newRefreshSignature := opaqueSignature("addp_rt_storage-postgres-refresh-2")
		txCtx, err = storage.BeginTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.RotateRefreshToken(txCtx, requestID.String(), refreshSignature); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("RotateRefreshToken() error = %v", err)
		}
		if err := storage.CreateAccessTokenSession(txCtx, newAccessSignature, refreshRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("create rotated access token: %v", err)
		}
		if err := storage.CreateRefreshTokenSession(txCtx, newRefreshSignature, newAccessSignature, refreshRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("create rotated refresh token: %v", err)
		}
		if err := storage.Commit(txCtx); err != nil {
			t.Fatalf("commit refresh rotation: %v", err)
		}
		inactiveRequest, err := storage.GetRefreshTokenSession(ctx, refreshSignature, NewIAMSession())
		if inactiveRequest == nil || !errors.Is(err, fosite.ErrInactiveToken) {
			t.Fatalf("old refresh token = %#v, %v", inactiveRequest, err)
		}

		repository := iam.NewRepository(db)
		advancedVersion, err := repository.IncrementPrincipalAuthorizationVersion(ctx, principalID)
		if err != nil || advancedVersion <= authorizationVersion {
			t.Fatalf("advance OAuth principal authorization version = %d err=%v", advancedVersion, err)
		}
		if _, err := storage.GetAccessTokenSession(ctx, newAccessSignature, NewIAMSession()); !errors.Is(err, fosite.ErrInactiveToken) {
			t.Fatalf("OAuth access token after authorization change error = %v, want inactive token", err)
		}
		currentRefreshRequest, err := storage.GetRefreshTokenSession(ctx, newRefreshSignature, NewIAMSession())
		if err != nil {
			t.Fatalf("load current OAuth refresh token after authorization change: %v", err)
		}
		currentRefreshRequest.GetRequestForm().Set("grant_type", string(fosite.GrantTypeRefreshToken))
		currentRefreshRequest.GetSession().SetExpiresAt(fosite.AccessToken, now.Add(15*time.Minute))
		advancedAccessPlain := "addp_at_storage-postgres-access-3"
		advancedAccessSignature := opaqueSignature(advancedAccessPlain)
		advancedRefreshSignature := opaqueSignature("addp_rt_storage-postgres-refresh-3")
		txCtx, err = storage.BeginTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.RotateRefreshToken(txCtx, requestID.String(), newRefreshSignature); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("rotate OAuth refresh token after authorization change: %v", err)
		}
		if err := storage.CreateAccessTokenSession(txCtx, advancedAccessSignature, currentRefreshRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("create OAuth access token after authorization change: %v", err)
		}
		if err := storage.CreateRefreshTokenSession(txCtx, advancedRefreshSignature, advancedAccessSignature, currentRefreshRequest); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("create OAuth refresh token after authorization change: %v", err)
		}
		if err := storage.Commit(txCtx); err != nil {
			t.Fatalf("commit OAuth authorization-version rotation: %v", err)
		}
		var advancedFamily iam.RefreshTokenFamily
		if err := db.Where("protocol_request_id = ?", requestID).Take(&advancedFamily).Error; err != nil {
			t.Fatal(err)
		}
		if advancedFamily.IssuedAuthorizationVersion != advancedVersion || advancedFamily.RevokedAt != nil {
			t.Fatalf("OAuth family after authorization-version rotation = %#v", advancedFamily)
		}
		authContextService, err := iam.NewAuthContextService(repository)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authContextService.ResolveUserAccessToken(ctx, advancedAccessPlain); err != nil {
			t.Fatalf("resolve OAuth access token after authorization-version rotation: %v", err)
		}
		authorizationVersion = advancedVersion

		replayAuditContext := WithTransactionAudit(ctx, iam.AuditEvent{
			EventName:  "oauth.token.issued",
			Result:     iam.AuditResultSucceeded,
			RiskLevel:  iam.AuditRiskMedium,
			ModuleName: "system",
			EntityType: "oauth_security_event",
			EntityID:   "oauth.token.issued",
			Details: map[string]any{
				"client_id":  "addp-cli",
				"grant_type": "refresh_token",
				"scope":      "addp.api",
			},
		})
		txCtx, err = storage.BeginTX(replayAuditContext)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.DeleteRefreshTokenSession(txCtx, refreshSignature); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("DeleteRefreshTokenSession() error = %v", err)
		}
		if err := storage.RevokeRefreshToken(txCtx, requestID.String()); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("RevokeRefreshToken() error = %v", err)
		}
		if err := storage.RevokeAccessToken(txCtx, requestID.String()); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("RevokeAccessToken() error = %v", err)
		}
		if err := storage.Commit(txCtx); err != nil {
			t.Fatalf("commit refresh replay handling: %v", err)
		}
		committedEvent, committed := TransactionAuditCommitted(replayAuditContext)
		if !committed || committedEvent.EventName != "oauth.token.refresh_reuse_detected" ||
			committedEvent.Result != iam.AuditResultDenied || committedEvent.RiskLevel != iam.AuditRiskHigh {
			t.Fatalf("refresh replay audit = %#v, committed=%t", committedEvent, committed)
		}
		var family iam.RefreshTokenFamily
		if err := db.Where("protocol_request_id = ?", requestID).Take(&family).Error; err != nil {
			t.Fatal(err)
		}
		if family.RevokedAt == nil || family.RevokedReason == nil || *family.RevokedReason != oauthReplayRevocationReason {
			t.Fatalf("family replay revocation = %#v", family)
		}
		var replayAuditCount int64
		if err := db.Raw(`
			SELECT count(*) FROM system.audit_logs
			WHERE event_name = 'oauth.token.refresh_reuse_detected'
			  AND result = 'denied' AND risk_level = 'high'
		`).Scan(&replayAuditCount).Error; err != nil {
			t.Fatal(err)
		}
		if replayAuditCount != 1 {
			t.Fatalf("refresh replay audit count = %d, want 1", replayAuditCount)
		}
	})

	t.Run("device polling decision and replay", func(t *testing.T) {
		requestID := uuid.New()
		deviceSession := NewIAMSession()
		deviceSession.SetExpiresAt(fosite.DeviceCode, now.Add(10*time.Minute))
		deviceSession.SetExpiresAt(fosite.UserCode, now.Add(10*time.Minute))
		deviceRequest := &fosite.DeviceRequest{
			Request: fosite.Request{
				ID:                requestID.String(),
				RequestedAt:       now.Add(-10 * time.Second),
				Client:            client,
				RequestedScope:    fosite.Arguments{"addp.api"},
				RequestedAudience: fosite.Arguments{"addp.api"},
				Session:           deviceSession,
			},
		}
		deviceSignature := opaqueSignature("addp_dc_storage-postgres-device")
		userSignature := userCodeSignature([]byte("0123456789abcdef0123456789abcdef"), "2345ABCD")
		if err := storage.CreateDeviceAuthSession(ctx, deviceSignature, userSignature, deviceRequest); err != nil {
			t.Fatalf("CreateDeviceAuthSession() error = %v", err)
		}
		limited, err := storage.ShouldRateLimit(ctx, deviceSignature)
		if err != nil || limited {
			t.Fatalf("on-time ShouldRateLimit() = %v, %v", limited, err)
		}
		limited, err = storage.ShouldRateLimit(ctx, deviceSignature)
		if err != nil || !limited {
			t.Fatalf("repeated ShouldRateLimit() = %v, %v", limited, err)
		}
		if err := storage.DecideDeviceAuthorization(ctx, userSignature, DeviceDecisionApprove, &ApprovedIdentityFacts{
			PrincipalID:                principalID,
			ContextType:                "tenant",
			TenantMembershipID:         &membershipID,
			IssuedAuthorizationVersion: authorizationVersion,
			GrantedScopes:              []string{"addp.api"},
			GrantedAudiences:           []string{"addp.api"},
			AuthenticationMethods:      []string{"password"},
			AssuranceLevel:             "aal1",
			AuthenticatedAt:            now.Add(-time.Minute),
		}); err != nil {
			t.Fatalf("DecideDeviceAuthorization() error = %v", err)
		}
		approved, err := storage.GetDeviceCodeSession(ctx, deviceSignature, NewIAMSession())
		if err != nil || approved.GetUserCodeState() != fosite.UserCodeAccepted {
			t.Fatalf("approved device = %#v, %v", approved, err)
		}
		txCtx, err := storage.BeginTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.InvalidateDeviceCodeSession(txCtx, deviceSignature); err != nil {
			_ = storage.Rollback(txCtx)
			t.Fatalf("InvalidateDeviceCodeSession() error = %v", err)
		}
		if err := storage.Commit(txCtx); err != nil {
			t.Fatal(err)
		}
		replayed, err := storage.GetDeviceCodeSession(ctx, deviceSignature, NewIAMSession())
		if replayed == nil || replayed.GetID() != requestID.String() || !errors.Is(err, fosite.ErrInvalidatedDeviceCode) {
			t.Fatalf("device replay = %#v, %v", replayed, err)
		}

		concurrentRequest := *deviceRequest
		concurrentRequest.ID = uuid.NewString()
		concurrentDeviceSignature := opaqueSignature("addp_dc_storage-postgres-concurrent")
		concurrentUserSignature := userCodeSignature(
			[]byte("0123456789abcdef0123456789abcdef"),
			"2345ABCE",
		)
		if err := storage.CreateDeviceAuthSession(
			ctx,
			concurrentDeviceSignature,
			concurrentUserSignature,
			&concurrentRequest,
		); err != nil {
			t.Fatalf("create concurrent device authorization: %v", err)
		}
		facts := &ApprovedIdentityFacts{
			PrincipalID:                principalID,
			ContextType:                "tenant",
			TenantMembershipID:         &membershipID,
			IssuedAuthorizationVersion: authorizationVersion,
			GrantedScopes:              []string{"addp.api"},
			GrantedAudiences:           []string{"addp.api"},
			AuthenticationMethods:      []string{"password"},
			AssuranceLevel:             "aal1",
			AuthenticatedAt:            now.Add(-time.Minute),
		}
		results := make(chan error, 2)
		for range 2 {
			go func() {
				results <- storage.DecideDeviceAuthorization(
					ctx,
					concurrentUserSignature,
					DeviceDecisionApprove,
					facts,
				)
			}()
		}
		successes := 0
		for range 2 {
			if err := <-results; err == nil {
				successes++
			} else if !errors.Is(err, fosite.ErrInvalidGrant) {
				t.Fatalf("concurrent decision error = %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent decision successes = %d, want 1", successes)
		}
	})

	t.Run("hosted consent and provider authorization code flow", func(t *testing.T) {
		providerConfig := ProviderConfig{
			AccessTokenLifespan:   15 * time.Minute,
			RefreshTokenLifespan:  30 * 24 * time.Hour,
			AuthorizeCodeLifespan: 5 * time.Minute,
			DeviceCodeLifespan:    10 * time.Minute,
			DevicePollingInterval: 5 * time.Second,
			DeviceVerificationURL: "http://localhost:5170/oauth/device",
			TokenEndpointURL:      "http://localhost:8000/api/v1/system/oauth/token",
		}
		provider, err := NewProvider(db, providerConfig, StrategyConfig{
			AccessTokenLifespan:   providerConfig.AccessTokenLifespan,
			RefreshTokenLifespan:  providerConfig.RefreshTokenLifespan,
			AuthorizeCodeLifespan: providerConfig.AuthorizeCodeLifespan,
			DeviceCodeLifespan:    providerConfig.DeviceCodeLifespan,
			UserCodePepper:        []byte("0123456789abcdef0123456789abcdef"),
		})
		if err != nil {
			t.Fatal(err)
		}
		bridge, err := NewConsentBridge(provider, iam.NewRepository(db), 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		verifier := strings.Repeat("B", 43)
		challengeDigest := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
		created, err := bridge.CreateAuthorizationRequest(ctx, AuthorizationRequestInput{
			ClientID:            "addp-cli",
			RedirectURI:         "http://127.0.0.1:59473/callback",
			Scope:               "addp.api",
			CodeChallenge:       challenge,
			CodeChallengeMethod: "S256",
		})
		if err != nil {
			t.Fatalf("CreateAuthorizationRequest() error = %v", err)
		}
		if !strings.HasPrefix(created.RequestSecret, authorizationRequestSecretPrefix) || created.ExpiresIn != 300 {
			t.Fatalf("created authorization request = %#v", created)
		}
		view, err := bridge.GetAuthorizationRequest(ctx, created.RequestID)
		if err != nil || view.ClientID != "addp-cli" || view.Scope != "addp.api" {
			t.Fatalf("GetAuthorizationRequest() = %#v, %v", view, err)
		}
		var tenantID int64
		if err := db.Raw(`SELECT tenant_id FROM system.tenant_memberships WHERE id = ?`, membershipID).
			Scan(&tenantID).Error; err != nil {
			t.Fatal(err)
		}
		principalIDText := strconv.FormatInt(principalID, 10)
		tenantIDText := strconv.FormatInt(tenantID, 10)
		membershipIDText := strconv.FormatInt(membershipID, 10)
		clientID := "addp-web"
		authContext := commonauth.AuthContext{
			SchemaVersion: commonauth.AuthContextSchemaVersion,
			Principal:     commonauth.AuthPrincipal{Type: "user", ID: principalIDText},
			Context: commonauth.AuthSessionContext{
				Type:               "tenant",
				TenantID:           &tenantIDText,
				TenantMembershipID: &membershipIDText,
			},
			Authentication: commonauth.AuthenticationFacts{
				Methods:         []string{"password"},
				AssuranceLevel:  "aal1",
				AuthenticatedAt: time.Now().UTC().Add(-time.Minute),
			},
			Client: commonauth.ClientConstraints{
				ClientID:  &clientID,
				Audiences: []string{"addp.api"},
				ScopeMode: "unrestricted",
				Scopes:    []string{},
			},
			Organization: commonauth.OrganizationContext{
				Departments:   []commonauth.DepartmentMembership{},
				ProjectGroups: []commonauth.ProjectGroupMembership{},
			},
			Authorization: commonauth.AuthorizationFacts{
				AuthorizationVersion: strconv.FormatInt(authorizationVersion, 10),
				RoleAssignments:      []commonauth.RoleAssignment{},
			},
			Token: commonauth.TokenFacts{
				Type:      "first_party_access_token",
				IssuedAt:  time.Now().UTC().Add(-time.Minute),
				ExpiresAt: time.Now().UTC().Add(14 * time.Minute),
			},
		}
		decision, err := bridge.DecideAuthorization(
			ctx,
			created.RequestID,
			AuthorizationDecisionApprove,
			authContext,
			iam.AuditMetadata{},
		)
		if err != nil {
			var oauthError *fosite.RFC6749Error
			if errors.As(err, &oauthError) {
				t.Fatalf("DecideAuthorization() error = %+v debug=%q cause=%v", err, oauthError.Debug(), oauthError.Cause())
			}
			t.Fatalf("DecideAuthorization() error = %+v", err)
		}
		redirect, err := url.Parse(decision.RedirectURL)
		if err != nil || redirect.Query().Get("state") != created.RequestID ||
			!strings.HasPrefix(redirect.Query().Get("code"), authorizeCodePrefix) {
			t.Fatalf("authorization redirect = %q, %v", decision.RedirectURL, err)
		}
		code := redirect.Query().Get("code")

		wrongForm := url.Values{
			"grant_type":    []string{"authorization_code"},
			"client_id":     []string{"addp-cli"},
			"code":          []string{code},
			"redirect_uri":  []string{"http://127.0.0.1:59473/callback"},
			"code_verifier": []string{strings.Repeat("C", 43)},
		}
		wrongRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(wrongForm.Encode()))
		wrongRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := provider.OAuth2.NewAccessRequest(ctx, wrongRequest, NewIAMSession()); err == nil {
			t.Fatal("wrong PKCE verifier was accepted")
		}
		var verifiedCount int64
		if err := db.Raw(`
			SELECT count(*) FROM system.oauth_pkce_sessions
			WHERE authorization_request_id = ? AND verified_at IS NOT NULL
		`, created.RequestID).Scan(&verifiedCount).Error; err != nil {
			t.Fatal(err)
		}
		if verifiedCount != 0 {
			t.Fatal("wrong PKCE verifier consumed the PKCE session")
		}

		tokenForm := url.Values{
			"grant_type":    []string{"authorization_code"},
			"client_id":     []string{"addp-cli"},
			"code":          []string{code},
			"redirect_uri":  []string{"http://127.0.0.1:59473/callback"},
			"code_verifier": []string{verifier},
		}
		tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		tokenAuditContext := WithTransactionAudit(ctx, iam.AuditEvent{
			EventName:  "oauth.token.issued",
			Result:     iam.AuditResultSucceeded,
			RiskLevel:  iam.AuditRiskMedium,
			ModuleName: "system",
			EntityType: "oauth_security_event",
			EntityID:   "oauth.token.issued",
			Details: map[string]any{
				"client_id":  "addp-cli",
				"grant_type": "authorization_code",
				"scope":      "addp.api",
			},
		})
		tokenRequest = tokenRequest.WithContext(tokenAuditContext)
		accessRequest, err := provider.OAuth2.NewAccessRequest(tokenAuditContext, tokenRequest, NewIAMSession())
		if err != nil {
			t.Fatalf("NewAccessRequest() error = %v", err)
		}
		accessResponse, err := provider.OAuth2.NewAccessResponse(tokenAuditContext, accessRequest)
		if err != nil {
			t.Fatalf("NewAccessResponse() error = %v", err)
		}
		recorder := httptest.NewRecorder()
		provider.OAuth2.WriteAccessResponse(ctx, recorder, accessRequest, accessResponse)
		body, err := io.ReadAll(recorder.Result().Body)
		if err != nil {
			t.Fatal(err)
		}
		var tokenResponse map[string]interface{}
		if err := json.Unmarshal(body, &tokenResponse); err != nil {
			t.Fatalf("decode token response %q: %v", body, err)
		}
		if recorder.Code != http.StatusOK || !strings.HasPrefix(tokenResponse["access_token"].(string), accessTokenPrefix) ||
			!strings.HasPrefix(tokenResponse["refresh_token"].(string), refreshTokenPrefix) {
			t.Fatalf("token response status=%d body=%s", recorder.Code, body)
		}
		authContextService, err := iam.NewAuthContextService(iam.NewRepository(db))
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := authContextService.ResolveUserAccessToken(
			ctx,
			tokenResponse["access_token"].(string),
		)
		if err != nil {
			t.Fatalf("ResolveUserAccessToken() error = %v", err)
		}
		if resolved.Token.Type != "oauth_access_token" || resolved.Client.ClientID == nil ||
			*resolved.Client.ClientID != "addp-cli" || resolved.Client.ScopeMode != "restricted" ||
			len(resolved.Client.Scopes) != 1 || resolved.Client.Scopes[0] != "addp.api" {
			t.Fatalf("OAuth AuthContext = %#v", resolved)
		}
		var auditCount int64
		if err := db.Raw(`
			SELECT count(*) FROM system.audit_logs
			WHERE event_name = 'oauth.token.issued'
			  AND principal_id = ?
			  AND context_type = 'tenant'
			  AND tenant_id = ?
			  AND details = '{"client_id":"addp-cli","grant_type":"authorization_code","scope":"addp.api"}'::jsonb
		`, principalID, tenantID).Scan(&auditCount).Error; err != nil {
			t.Fatal(err)
		}
		if auditCount != 1 {
			t.Fatalf("OAuth token audit count = %d, want 1", auditCount)
		}
	})
}

const testCodeChallenge = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func seedOAuthIdentity(t *testing.T, db *gorm.DB, now time.Time) (int64, int64, int64) {
	t.Helper()
	var principalID, tenantID, membershipID, authorizationVersion int64
	if err := db.Raw(`
		INSERT INTO system.principals (principal_type, status)
		VALUES ('user', 'active') RETURNING id
	`).Scan(&principalID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES (?, 'OAuth Test User')`, principalID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`
		INSERT INTO system.tenants (code, name, description, status)
		VALUES ('oauth-storage-test', 'OAuth Storage Test', '', 'active') RETURNING id
	`).Scan(&tenantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES (?, ?, 'active', 'manual', ?, ?) RETURNING id
	`, tenantID, principalID, now.Add(-time.Hour), principalID).Scan(&membershipID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT authorization_version FROM system.principals WHERE id = ?`, principalID).
		Scan(&authorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	return principalID, membershipID, authorizationVersion
}

func insertApprovedAuthorizationRequest(
	t *testing.T,
	db *gorm.DB,
	requestID uuid.UUID,
	principalID int64,
	membershipID int64,
	authorizationVersion int64,
	now time.Time,
) {
	t.Helper()
	requestSecretHash := strings.Repeat("a", 64)
	if err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, status, requested_at, expires_at, created_at)
		VALUES (?, ?, 'addp-cli', 'http://127.0.0.1/callback', ARRAY['code']::text[], 'query',
		        ARRAY['addp.api']::text[], ARRAY['addp.api']::text[], 'pending', ?, ?, ?)
	`, requestID, requestSecretHash, now.Add(-time.Minute), now.Add(5*time.Minute), now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		INSERT INTO system.oauth_pkce_sessions
		    (authorization_request_id, code_challenge, code_challenge_method, expires_at, created_at)
		VALUES (?, ?, 'S256', ?, ?)
	`, requestID, testCodeChallenge, now.Add(5*time.Minute), now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved', principal_id = ?, context_type = 'tenant', tenant_membership_id = ?,
		    issued_authorization_version = ?, granted_scopes = ARRAY['addp.api']::text[],
		    granted_audiences = ARRAY['addp.api']::text[], authentication_methods = ARRAY['password']::text[],
		    assurance_level = 'aal1', authenticated_at = ?, completed_at = ?
		WHERE id = ?
	`, principalID, membershipID, authorizationVersion, now.Add(-time.Minute), now, requestID).Error; err != nil {
		t.Fatal(err)
	}
}

func approvedTestSession(
	principalID int64,
	membershipID int64,
	authorizationVersion int64,
	now time.Time,
) *IAMSession {
	session := NewIAMSession()
	session.PrincipalID = principalID
	session.ContextType = "tenant"
	session.TenantMembershipID = &membershipID
	session.IssuedAuthorizationVersion = authorizationVersion
	session.Subject = "oauth-test-subject"
	session.AuthenticationMethods = []string{"password"}
	session.AssuranceLevel = "aal1"
	session.AuthenticatedAt = now.Add(-time.Minute)
	session.RequestedAt = now.Add(-time.Minute)
	return session
}
