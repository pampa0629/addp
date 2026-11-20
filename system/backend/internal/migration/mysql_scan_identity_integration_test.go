package migration

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"testing"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

func testMySQLScanIdentity(t *testing.T, db *gorm.DB, tenantID, adminID int64) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	identityService := iam.NewIdentityService(repo, nil)
	password := rand.Text()
	newUser, err := identityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "scan-member", Password: password, DisplayName: "Scan member"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := iam.NewTenantMembershipService(repo, nil).EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenantID, PrincipalID: newUser.PrincipalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	role, err := repo.GetActiveBuiltinRoleByKey(ctx, "tenant.data_steward")
	if err != nil {
		t.Fatal(err)
	}
	assignments, err := iam.NewTenantRoleService(repo, nil).CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: member.Membership.ID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "metadata scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected one assignment, got %d", len(assignments))
	}
	if got := assignments[0]; got.RoleKey != role.RoleKey || got.EffectiveState != "effective" {
		dbNow, err := repo.CurrentDatabaseTime(ctx)
		t.Fatalf("created role=%q state=%q valid_from=%s db_now=%s: %v", got.RoleKey, got.EffectiveState, got.ValidFrom, dbNow, err)
	}
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: models.BrowserResourceAccessOwners}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cipher, err := iam.NewMFACredentialCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	mfa, err := iam.NewMFAService(repo, cipher, iam.MFAServiceConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	login, err := iam.NewBrowserLoginService(identityService, mfa, selection)
	if err != nil {
		t.Fatal(err)
	}
	session, err := login.LoginLocalBrowser(ctx, iam.LoginLocalBrowserInput{Username: "scan-member", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if session.NextAction != iam.ContextSelectionNextActionSessionIssued || session.Session == nil {
		t.Fatal("scan member did not receive a browser session")
	}
	resolver, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := resolver.ResolveFirstPartyAccessToken(ctx, session.Session.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Context.TenantID == nil || *auth.Context.TenantID != strconv.FormatInt(tenantID, 10) || auth.Principal.ID != fmt.Sprint(newUser.PrincipalID) ||
		len(auth.Authorization.RoleAssignments) != 1 || !commonauth.HasContextPermissions(*auth, "meta.scan_task.execute", "meta.scan_task.read") ||
		commonauth.HasContextPermissions(*auth, "iam.tenant_role_assignment.create") {
		t.Fatal("scan member has unexpected identity or permissions")
	}
}
