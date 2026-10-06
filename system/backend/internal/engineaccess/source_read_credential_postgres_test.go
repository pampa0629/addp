package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Runs inside the existing formal Grant fixture and IAM PostgreSQL owner gate.
// Real browser sessions use production IAM services; no source DB is accessed.
func exerciseSourceReadCredentials(t *testing.T, db *gorm.DB, path engineplugin.EngineCatalogPath,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time),
	grant func(*testing.T, string, int64, *time.Time) uuid.UUID,
	roles *iam.TenantRoleService, tenantID, adminID int64,
	deny func(*testing.T, string, int64, *time.Time) uuid.UUID, revoke func(*testing.T, uuid.UUID),
) {
	t.Run("trusted current User credential and precise rules", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		identity := iam.NewRepository(db)
		repo := NewRepository(db)
		issue := func(t *testing.T, user userProvenance, ttl time.Duration) (*iam.IssuedBrowserSession, *iam.TokenFamilyService) {
			t.Helper()
			tokens, err := iam.NewTokenFamilyService(identity, iam.BrowserSessionConfig{AccessTokenTTL: ttl, ResourceTicketOwners: []string{"manager"}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := iam.NewContextSelectionService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			result, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.PrincipalID,
				Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: time.Now().UTC().Add(-time.Second)}})
			if err != nil {
				t.Fatalf("issue current User session: %v", err)
			}
			session := result.Session
			if result.Challenge != nil {
				session, err = selection.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: result.Challenge.SelectionTicket,
					Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &user.MembershipID}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if session == nil {
				t.Fatal("expected real Tenant User session")
			}
			return session, tokens
		}
		t.Run("formal preview consumer intersects function permission and source rules", func(t *testing.T) {
			service := NewService(repo, nil)
			request := SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}
			user, _ := newUser(t, time.Hour)
			grantID := grant(t, "user", user.PrincipalID, nil)
			session, _ := issue(t, user, time.Minute)
			if result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("Grant without function Permission accepted: %+v %v", result, err)
			}
			if result, err := service.CheckManagerProfileResultRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("profile Grant without function accepted: %+v %v", result, err)
			}
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.preview_check", Name: "Explicit preview access",
				ScopeTypes: []string{"tenant"}, PermissionKeys: []string{ManagerPreviewReadPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			assign := func(user userProvenance) *iam.ManagedTenantRoleAssignment {
				t.Helper()
				assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID,
					RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit preview consumer fixture"})
				if err != nil || len(assignments) != 1 {
					t.Fatalf("assign=%+v %v", assignments, err)
				}
				return &assignments[0]
			}
			assignment := assign(user)
			session, _ = issue(t, user, time.Minute)
			delegation, err := iam.NewDelegationService(identity, systemauthorization.ToolAuthorizationCatalog{}, iam.DelegationServiceConfig{})
			if err != nil {
				t.Fatal(err)
			}
			delegate := func(session *iam.IssuedBrowserSession) string {
				t.Helper()
				issued, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{
					SourceAccessToken: session.AccessToken, Audience: "manager", Scopes: []string{"data.preview"},
					AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString(),
				})
				if err != nil || issued == nil {
					t.Fatalf("issue real preview delegation: %v", err)
				}
				return issued.AccessToken
			}
			delegated := delegate(session)
			credentials := []string{session.AccessToken, delegated}
			checkProfile := func(credential string, targets []engineplugin.EngineCatalogPath, want error) {
				t.Helper()
				result, err := service.CheckManagerProfileResultRead(ctx, credential, SourceReadCheckRequest{Targets: targets})
				if want == nil {
					if err != nil || result == nil || result.ObservedAt.IsZero() {
						t.Fatalf("profile result source coverage=%+v %v", result, err)
					}
				} else if result != nil || !errors.Is(err, want) {
					t.Fatalf("profile result accepted=%+v %v want=%v", result, err, want)
				}
			}
			var before, after int64
			if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			for _, credential := range credentials {
				result, err := service.CheckManagerPreviewRead(ctx, credential, request)
				if err != nil || result == nil || result.ObservedAt.IsZero() {
					t.Fatalf("function + source intersection=%+v %v", result, err)
				}
			}
			d := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "formal_preview_ungranted_D")
			checkProfile(session.AccessToken, request.Targets, nil)
			checkProfile(session.AccessToken, []engineplugin.EngineCatalogPath{path, d}, commonapi.ErrForbidden)
			checkProfile(delegated, request.Targets, commonapi.ErrUnauthorized)
			checkProfile(session.ResourceAccessTickets["manager"], request.Targets, commonapi.ErrUnauthorized)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(ctx, credential, SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path, d}}); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("partial C+D accepted=%+v %v", result, err)
				}
			}
			if result, err := repo.readCurrentUserSourceRules(ctx, delegated, request.Targets); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("delegate downgraded into ordinary User reader=%+v %v", result, err)
			}
			if result, err := service.CheckManagerPreviewRead(ctx, session.ResourceAccessTickets["manager"], request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("ticket accepted=%+v %v", result, err)
			}
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("read check wrote audits=%d/%d %v", before, after, err)
			}
			revoke(t, grantID)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(ctx, credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("revoked Grant accepted=%+v %v", result, err)
				}
			}
			grant(t, "user", user.PrincipalID, nil)
			deny(t, "user", user.PrincipalID, nil)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(ctx, credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("Deny priority lost=%+v %v", result, err)
				}
			}
			if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: tenantID, AssignmentID: assignment.ID, ActorPrincipalID: adminID, Reason: "End preview fixture"}); err != nil {
				t.Fatal(err)
			}
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrUnauthorized)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(ctx, credential, request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
					t.Fatalf("old authorization credential accepted=%+v %v", result, err)
				}
			}
			session, _ = issue(t, user, time.Minute)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			if result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked function Permission accepted=%+v %v", result, err)
			}
			ungranted, _ := newUser(t, time.Hour)
			assign(ungranted)
			session, _ = issue(t, ungranted, time.Minute)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			for _, credential := range []string{session.AccessToken, delegate(session)} {
				if result, err := service.CheckManagerPreviewRead(ctx, credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("function without Grant accepted=%+v %v", result, err)
				}
			}
			familyUser, _ := newUser(t, time.Hour)
			assign(familyUser)
			grant(t, "user", familyUser.PrincipalID, nil)
			familySession, tokens := issue(t, familyUser, time.Minute)
			familyDelegate := delegate(familySession)
			if _, err := service.CheckManagerPreviewRead(ctx, familyDelegate, request); err != nil {
				t.Fatal(err)
			}
			logout, err := iam.NewLogoutService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if err := logout.LogoutBrowserSession(ctx, iam.LogoutBrowserSessionInput{AccessToken: familySession.AccessToken, RefreshToken: familySession.RefreshToken}); err != nil {
				t.Fatal(err)
			}
			if result, err := service.CheckManagerPreviewRead(ctx, familyDelegate, request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("revoked source family delegate accepted=%+v %v", result, err)
			}
			checkProfile(familySession.AccessToken, request.Targets, commonapi.ErrUnauthorized)
		})
		t.Run("identity derived only from credential and no observation writes", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			grant(t, "user", user.PrincipalID, nil)
			session, tokens := issue(t, user, time.Minute)
			var before, after int64
			if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || !result.Covered {
				t.Fatalf("real credential=%+v %v", result, err)
			}
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("observation audit=%d/%d %v", before, after, err)
			}
			d := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "credential_ungranted_D")
			result, err = repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path, d})
			if err != nil || result == nil || result.Covered || !result.Targets[0].Covered || result.Targets[1].Reason != "no_grant" {
				t.Fatalf("credential C+D=%+v %v", result, err)
			}
			for _, token := range []string{"", "addp_at_unknown", "Bearer " + session.AccessToken, session.ResourceAccessTickets["manager"], "addp_dat_wrong", "addp_rt_wrong"} {
				result, err := repo.readCurrentUserSourceRules(ctx, token, []engineplugin.EngineCatalogPath{path})
				if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
					t.Fatal("invalid/non-User credential must return nil and unauthorized")
				}
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			result, err = NewRepository(tx).readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, errFulfillmentBinding) {
				t.Fatalf("caller transaction accepted=%+v %v", result, err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			logout, err := iam.NewLogoutService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if err := logout.LogoutBrowserSession(ctx, iam.LogoutBrowserSessionInput{AccessToken: session.AccessToken, RefreshToken: session.RefreshToken}); err != nil {
				t.Fatal(err)
			}
			result, err = repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("revoked Family accepted=%+v %v", result, err)
			}
		})
		t.Run("credential and rules use one committed read only snapshot", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			session, _ := issue(t, user, time.Minute)
			callback := "test:source-credential-snapshot"
			called := false
			if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
				if called || !strings.Contains(tx.Statement.SQL.String(), "WITH input AS MATERIALIZED") {
					return
				}
				called = true
				var isolation, readOnly string
				if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only')").Scan(&isolation, &readOnly); err != nil {
					tx.AddError(err)
					return
				}
				if isolation != "repeatable read" || readOnly != "on" {
					tx.AddError(errors.New("credential snapshot is not repeatable read and read only"))
					return
				}
				// An independent formal writer commits after IAM credential loading.
				// This observation must not mix its earlier credential with a newer Grant.
				grant(t, "user", user.PrincipalID, nil)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Row().Remove(callback) })
			result, err := repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || result.Covered || !called || result.Targets[0].Reason != "no_grant" {
				t.Fatalf("mixed credential/rule snapshot=%+v %v called=%t", result, err, called)
			}
			if err := db.Callback().Row().Remove(callback); err != nil {
				t.Fatal(err)
			}
			result, err = repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || !result.Covered {
				t.Fatalf("later committed Grant=%+v %v", result, err)
			}
		})
		t.Run("database wall clock expires credential after IAM projection", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			grant(t, "user", user.PrincipalID, nil)
			session, _ := issue(t, user, 350*time.Millisecond)
			callback := "test:source-credential-expiry"
			called := false
			if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
				if called || !strings.Contains(tx.Statement.SQL.String(), "WITH input AS MATERIALIZED") {
					return
				}
				called = true
				_, err := tx.Statement.ConnPool.ExecContext(ctx, "SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM ($1::timestamptz - clock_timestamp()))) + 0.02)", session.AccessTokenExpiresAt)
				if err != nil {
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Row().Remove(callback)
			result, err := repo.readCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) || !called {
				t.Fatalf("expired projected credential accepted=%+v %v called=%t", result, err, called)
			}
		})
	})
}
