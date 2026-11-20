package migration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/google/uuid"
	"github.com/ory/fosite"
	"gorm.io/gorm"
)

func testMySQLTokenSession(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	storage, err := iamoauth.NewStorage(db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	const clientID = "addp_svc_correcttenant001"
	client, err := storage.GetClient(ctx, clientID)
	if err != nil {
		t.Fatal(err)
	}
	request := &fosite.AccessRequest{
		GrantTypes: fosite.Arguments{"client_credentials"},
		Request: fosite.Request{
			ID: uuid.NewString(), Client: client, Session: iamoauth.NewIAMSession(),
			RequestedScope: fosite.Arguments{"addp.api"}, RequestedAudience: fosite.Arguments{"addp.api"},
		},
	}
	if err := storage.PopulateServiceCredentialSession(ctx, request, "", "10"); !errors.Is(err, fosite.ErrInvalidGrant) {
		t.Fatalf("wrong tenant service session returned %v", err)
	}
	if err := storage.PopulateServiceCredentialSession(ctx, request, "", "90"); err != nil {
		t.Fatal(err)
	}
	var lifespan time.Duration
	if lifespan, err = storage.ServiceCredentialResponseLifespan(ctx, request); err != nil || lifespan < 4*time.Minute {
		t.Fatalf("database service lifespan = %v, %v", lifespan, err)
	}
	const serviceSignatureHex = "a"
	signature := strings.Repeat(serviceSignatureHex, 64)
	txContext, err := storage.BeginTX(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.CreateAccessTokenSession(txContext, signature, request); err != nil {
		_ = storage.Rollback(txContext)
		t.Fatal(err)
	}
	if err := storage.Commit(txContext); err != nil {
		t.Fatal(err)
	}
	loaded, err := storage.GetAccessTokenSession(ctx, signature, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := loaded.GetSession().(*iamoauth.IAMSession)
	if !ok || session.PrincipalID != 105 || session.TenantMembershipID == nil ||
		loaded.GetID() != request.GetID() || !loaded.GetGrantedScopes().Has("addp.api") {
		t.Fatal("OAuth storage did not return the persisted bound session")
	}

	repo := iam.NewRepository(db)
	snapshot, err := repo.GetAccessTokenAuthSnapshot(ctx, signature)
	if err != nil || snapshot.FamilyPrincipalID != 105 || snapshot.TenantID == nil || *snapshot.TenantID != 90 ||
		snapshot.PrincipalType != iam.PrincipalTypeServicePrincipal ||
		snapshot.FamilyAuthorizationVersion != snapshot.PrincipalAuthorizationVersion ||
		len(snapshot.FamilyScopes) != 1 || snapshot.FamilyScopes[0] != "addp.api" ||
		len(snapshot.FamilyAuthenticationMethods) != 1 || snapshot.FamilyAuthenticationMethods[0] != "service_secret" ||
		snapshot.DatabaseTime.Before(snapshot.CredentialCreatedAt) || !snapshot.DatabaseTime.Before(snapshot.CredentialExpiresAt) {
		t.Fatalf("service session returned invalid authentication facts: %v", err)
	}
	if _, err := repo.IncrementPrincipalAuthorizationVersion(ctx, 105); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.GetAccessTokenSession(ctx, signature, nil); !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("outdated service session returned %v", err)
	}
	token, err := repo.GetAccessTokenByHash(ctx, signature)
	if err != nil {
		t.Fatal(err)
	}
	family, err := repo.GetRefreshTokenFamily(ctx, token.FamilyID)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a process clock behind the database without changing either clock.
	processTime := family.CreatedAt.Add(-time.Second)
	if _, err := repo.RevokeActiveTokenFamilies(ctx, 105, processTime, "test-session-revoked"); err != nil {
		t.Fatal(err)
	}
	family, err = repo.GetRefreshTokenFamily(ctx, token.FamilyID)
	if err != nil || family.RevokedAt == nil || family.RevokedAt.Before(family.CreatedAt) {
		t.Fatalf("service family did not remain revoked: %v", err)
	}
}

func testMySQLRefreshTokenLinkage(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	principal, err := repo.GetPrincipal(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := repo.LockTenantMembership(ctx, 90, 100)
	if err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := db.Raw("SELECT CURRENT_TIMESTAMP(6)").Row().Scan(&now); err != nil {
		t.Fatal(err)
	}
	family := &iam.RefreshTokenFamily{
		PrincipalID: 100, ContextType: iam.ContextTypeTenant, TenantMembershipID: &membership.ID,
		IssuedAuthorizationVersion: principal.AuthorizationVersion, ClientID: "addp-web", AuthType: "first_party",
		Audiences: iam.DBStringList{"addp.api"}, Scopes: iam.DBStringList{}, AuthenticationMethods: iam.DBStringList{"password"},
		AssuranceLevel: "aal1", AuthenticatedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.CreateRefreshTokenFamily(ctx, family); err != nil {
		t.Fatal(err)
	}
	originalAccess := &iam.AccessToken{FamilyID: family.ID, TokenHash: strings.Repeat("b", 64), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := repo.CreateAccessToken(ctx, originalAccess); err != nil {
		t.Fatal(err)
	}
	original := &iam.RefreshToken{FamilyID: family.ID, IssuedAccessTokenID: originalAccess.ID, TokenHash: strings.Repeat("c", 64), CreatedAt: now, ExpiresAt: family.ExpiresAt}
	if err := repo.CreateRefreshToken(ctx, original); err != nil {
		t.Fatal(err)
	}

	rollback := errors.New("rollback token rotation")
	rotate := func(tx *iam.Repository) error {
		if err := tx.RevokeAccessToken(ctx, originalAccess.ID, now); err != nil {
			return err
		}
		if err := tx.MarkRefreshTokenUsed(ctx, original.ID, now); err != nil {
			return err
		}
		access := &iam.AccessToken{FamilyID: family.ID, TokenHash: strings.Repeat("d", 64), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
		if err := tx.CreateAccessToken(ctx, access); err != nil {
			return err
		}
		replacement := &iam.RefreshToken{
			FamilyID: family.ID, IssuedAccessTokenID: access.ID, ParentTokenID: &original.ID,
			TokenHash: strings.Repeat("e", 64), CreatedAt: now, ExpiresAt: family.ExpiresAt,
		}
		if err := tx.CreateRefreshToken(ctx, replacement); err != nil {
			return err
		}
		return tx.LinkRefreshTokenReplacement(ctx, original.ID, replacement.ID)
	}
	err = repo.Transaction(ctx, func(tx *iam.Repository) error {
		if err := rotate(tx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rotation transaction did not reach rollback: %v", err)
	}
	restored, err := repo.GetRefreshTokenByHash(ctx, original.TokenHash)
	if err != nil || restored.UsedAt != nil || restored.ReplacedByTokenID != nil {
		t.Fatalf("rollback changed original refresh token: %v", err)
	}
	if err := repo.Transaction(ctx, rotate); err != nil {
		t.Fatal(err)
	}
	used, err := repo.GetRefreshTokenByHash(ctx, original.TokenHash)
	if err != nil || used.UsedAt == nil || used.ReplacedByTokenID == nil {
		t.Fatalf("rotation did not record the original refresh token's replacement: %v", err)
	}
	replacement, err := repo.GetRefreshTokenByHash(ctx, strings.Repeat("e", 64))
	if err != nil || replacement.ParentTokenID == nil || *replacement.ParentTokenID != original.ID || replacement.ID != *used.ReplacedByTokenID {
		t.Fatalf("refresh replacement is missing the persisted linkage: %v", err)
	}
}
