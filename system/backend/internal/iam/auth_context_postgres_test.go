package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/migration"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthContextServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset AuthContext test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager", "standard"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSelectionService: %v", err)
	}
	authContextService, err := NewAuthContextService(repository)
	if err != nil {
		t.Fatalf("create AuthContextService: %v", err)
	}
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)

	t.Run("tenant projection isolates context and filters effective facts", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("auth-context-tenant")}
		user := createContextSelectionUser(t, ctx, identityService, "auth-context-tenant", audit)
		tenantA := createContextSelectionTenant(t, ctx, membershipService, "context-a", audit)
		tenantB := createContextSelectionTenant(t, ctx, membershipService, "context-b", audit)
		membershipA := establishContextSelectionMembership(
			t, ctx, membershipService, tenantA.ID, user.PrincipalID, audit,
		)
		establishContextSelectionMembership(
			t, ctx, membershipService, tenantB.ID, user.PrincipalID, audit,
		)

		rootDepartmentID := insertDepartment(t, db, tenantA.ID, nil, "root")
		childDepartmentID := insertDepartment(t, db, tenantA.ID, &rootDepartmentID, "child")
		departmentMembershipID := insertDepartmentMembership(
			t, db, tenantA.ID, childDepartmentID, membershipA.Membership.ID,
		)
		projectGroupID := insertProjectGroup(t, db, tenantA.ID, "context-project")
		projectMembershipID := insertProjectGroupMembership(
			t, db, tenantA.ID, projectGroupID, membershipA.Membership.ID,
		)

		validFrom := currentTime.Add(-time.Hour)
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "tenant", &tenantA.ID, nil, nil, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "department", &tenantA.ID, &childDepartmentID, nil, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "project_group", &tenantA.ID, nil, &projectGroupID, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "tenant", &tenantB.ID, nil, nil, validFrom, nil, "manual")
		expiredAt := currentTime.Add(-time.Minute)
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.ai_user", "tenant", &tenantA.ID, nil, nil, validFrom, &expiredAt, "manual")

		authentication := SessionAuthentication{
			Methods:         []string{"password"},
			AssuranceLevel:  AssuranceLevelAAL1,
			AuthenticatedAt: currentTime.Add(-time.Minute),
		}
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID:    user.PrincipalID,
			Authentication: authentication,
			Audit:          audit,
		})
		if err != nil {
			t.Fatalf("begin tenant AuthContext selection: %v", err)
		}
		if selection.Challenge == nil {
			t.Fatalf("tenant AuthContext selection = %#v", selection)
		}
		session, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
			SelectionTicket: selection.Challenge.SelectionTicket,
			Choice: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &membershipA.Membership.ID,
			},
			Audit: audit,
		})
		if err != nil {
			t.Fatalf("consume tenant AuthContext selection: %v", err)
		}

		resolved, err := authContextService.ResolveFirstPartyAccessToken(ctx, session.AccessToken)
		if err != nil {
			t.Fatalf("resolve tenant AuthContext: %v", err)
		}
		if err := commonauth.ValidateAuthContext(*resolved); err != nil {
			t.Fatalf("validate projected tenant AuthContext: %v", err)
		}
		assertTenantAuthContext(
			t,
			resolved,
			user.PrincipalID,
			tenantA.ID,
			membershipA.Membership.ID,
			rootDepartmentID,
			childDepartmentID,
			departmentMembershipID,
			projectGroupID,
			projectMembershipID,
		)
		encoded, err := json.Marshal(resolved)
		if err != nil {
			t.Fatalf("marshal projected tenant AuthContext: %v", err)
		}
		if _, err := commonauth.DecodeAuthContext(bytes.NewReader(encoded)); err != nil {
			t.Fatalf("decode projected tenant AuthContext: %v", err)
		}
		managerTicket := session.ResourceAccessTickets["manager"]
		resourceContext, err := authContextService.ResolveAuthContext(ctx, managerTicket)
		if err != nil {
			t.Fatalf("resolve manager Resource Ticket AuthContext: %v", err)
		}
		assertResourceTicketAuthContext(t, resourceContext, resolved, "manager")
		var sourceAccessToken AccessToken
		if err := db.Where("token_hash = ?", hashOpaqueToken(session.AccessToken)).First(&sourceAccessToken).Error; err != nil {
			t.Fatalf("read delegated source Access Token: %v", err)
		}
		delegatedToken := insertDelegatedAuthToken(
			t,
			db,
			sourceAccessToken.ID,
			"addp_dat_tenant_projection",
			"develop",
			[]string{"workflow.run"},
			currentTime,
		)
		delegatedContext, err := authContextService.ResolveAuthContext(ctx, delegatedToken)
		if err != nil {
			t.Fatalf("resolve Delegated Token AuthContext: %v", err)
		}
		assertDelegatedAuthContext(t, delegatedContext, resolved, "addp-web", "develop", []string{"workflow.run"})

		rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("auth-context-token-rotated")},
		})
		if err != nil {
			t.Fatalf("rotate tenant session after AuthContext projection: %v", err)
		}
		assertAccessTokenReason(
			t,
			authContextService,
			ctx,
			session.AccessToken,
			CredentialInvalidTokenRevoked,
		)
		assertCredentialReason(
			t,
			authContextService,
			ctx,
			managerTicket,
			CredentialInvalidTokenRevoked,
		)
		assertCredentialReason(
			t,
			authContextService,
			ctx,
			delegatedToken,
			CredentialInvalidTokenRevoked,
		)
		if _, err := authContextService.ResolveFirstPartyAccessToken(ctx, rotated.AccessToken); err != nil {
			t.Fatalf("resolve replacement access token: %v", err)
		}
		rotatedAccessContext, err := authContextService.ResolveAuthContext(ctx, rotated.AccessToken)
		if err != nil {
			t.Fatalf("dispatch replacement access token: %v", err)
		}
		rotatedResourceContext, err := authContextService.ResolveResourceAccessTicket(
			ctx,
			rotated.ResourceAccessTickets["manager"],
		)
		if err != nil {
			t.Fatalf("resolve replacement resource ticket: %v", err)
		}
		assertResourceTicketAuthContext(t, rotatedResourceContext, rotatedAccessContext, "manager")
		var rotatedAccessToken AccessToken
		if err := db.Where("token_hash = ?", hashOpaqueToken(rotated.AccessToken)).First(&rotatedAccessToken).Error; err != nil {
			t.Fatalf("read replacement delegated source Access Token: %v", err)
		}
		rotatedDelegatedToken := insertDelegatedAuthToken(
			t,
			db,
			rotatedAccessToken.ID,
			"addp_dat_rotated_projection",
			"develop",
			[]string{"workflow.run"},
			currentTime,
		)
		rotatedDelegatedContext, err := authContextService.ResolveDelegatedAccessToken(ctx, rotatedDelegatedToken)
		if err != nil {
			t.Fatalf("resolve replacement Delegated Token: %v", err)
		}
		assertDelegatedAuthContext(
			t,
			rotatedDelegatedContext,
			rotatedAccessContext,
			"addp-web",
			"develop",
			[]string{"workflow.run"},
		)
	})

	t.Run("platform projection is isolated and version mismatch is unauthorized", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("auth-context-platform")}
		user := createContextSelectionUser(t, ctx, identityService, "auth-context-platform", audit)
		assignmentID := insertRoleAssignment(
			t,
			db,
			user.PrincipalID,
			"platform.statistics_viewer",
			"platform",
			nil,
			nil,
			nil,
			currentTime.Add(-time.Hour),
			nil,
			"bootstrap",
		)
		authentication := SessionAuthentication{
			Methods:         []string{"password", "totp"},
			AssuranceLevel:  AssuranceLevelAAL2,
			AuthenticatedAt: currentTime.Add(-time.Minute),
		}
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID:    user.PrincipalID,
			Authentication: authentication,
			Audit:          audit,
		})
		if err != nil {
			t.Fatalf("issue platform session: %v", err)
		}
		if selection.Session == nil {
			t.Fatalf("platform selection = %#v", selection)
		}
		resolved, err := authContextService.ResolveFirstPartyAccessToken(ctx, selection.Session.AccessToken)
		if err != nil {
			t.Fatalf("resolve platform AuthContext: %v", err)
		}
		if resolved.Context.Type != "platform" || resolved.Context.TenantID != nil ||
			len(resolved.Organization.Departments) != 0 || len(resolved.Organization.ProjectGroups) != 0 ||
			len(resolved.Authorization.RoleAssignments) != 1 ||
			resolved.Authorization.RoleAssignments[0].AssignmentID != formatIAMID(assignmentID) ||
			resolved.Authorization.RoleAssignments[0].Scope.Type != "platform" {
			t.Fatalf("projected platform AuthContext = %#v", resolved)
		}

		if err := db.Exec(`
			UPDATE system.principals
			SET authorization_version = authorization_version + 1, updated_at = now()
			WHERE id = ?
		`, user.PrincipalID).Error; err != nil {
			t.Fatalf("increment platform principal authorization version: %v", err)
		}
		assertAccessTokenReason(
			t,
			authContextService,
			ctx,
			selection.Session.AccessToken,
			CredentialInvalidAuthorizationVersion,
		)
	})

	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, resourceTicketInvalidationFixture)
	}{
		{
			name: "family revoked",
			mutate: func(t *testing.T, fixture resourceTicketInvalidationFixture) {
				t.Helper()
				if err := db.Model(&RefreshTokenFamily{}).Where("id = ?", fixture.FamilyID).Updates(map[string]any{
					"revoked_at":     currentTime,
					"revoked_reason": "auth_context_test",
				}).Error; err != nil {
					t.Fatalf("revoke Resource Ticket family: %v", err)
				}
			},
		},
		{
			name: "authorization version changed",
			mutate: func(t *testing.T, fixture resourceTicketInvalidationFixture) {
				t.Helper()
				if err := db.Model(&Principal{}).Where("id = ?", fixture.PrincipalID).
					Update("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
					t.Fatalf("increment Resource Ticket authorization version: %v", err)
				}
			},
		},
		{
			name: "tenant membership suspended",
			mutate: func(t *testing.T, fixture resourceTicketInvalidationFixture) {
				t.Helper()
				if err := db.Model(&TenantMembership{}).Where("id = ?", fixture.TenantMembershipID).
					Update("status", TenantMembershipStatusSuspended).Error; err != nil {
					t.Fatalf("suspend Resource Ticket membership: %v", err)
				}
			},
		},
		{
			name: "tenant suspended",
			mutate: func(t *testing.T, fixture resourceTicketInvalidationFixture) {
				t.Helper()
				if err := db.Model(&Tenant{}).Where("id = ?", fixture.TenantID).
					Update("status", TenantStatusSuspended).Error; err != nil {
					t.Fatalf("suspend Resource Ticket tenant: %v", err)
				}
			},
		},
	} {
		t.Run("resource ticket rejects "+testCase.name, func(t *testing.T) {
			fixture := issueResourceTicketInvalidationFixture(
				t,
				ctx,
				identityService,
				membershipService,
				selectionService,
				db,
				currentTime,
				strings.ReplaceAll(testCase.name, " ", "-"),
			)
			if _, err := authContextService.ResolveResourceAccessTicket(ctx, fixture.Ticket); err != nil {
				t.Fatalf("resolve Resource Ticket before invalidation: %v", err)
			}
			testCase.mutate(t, fixture)
			_, err := authContextService.ResolveResourceAccessTicket(ctx, fixture.Ticket)
			if !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("resolve invalidated Resource Ticket error = %v, want unauthorized", err)
			}
		})
	}

	for _, testCase := range []struct {
		name       string
		wantReason CredentialInvalidReason
		mutate     func(*testing.T, delegatedInvalidationFixture)
	}{
		{
			name:       "delegated token revoked",
			wantReason: CredentialInvalidTokenRevoked,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&DelegatedAccessToken{}).Where("id = ?", fixture.DelegatedID).
					Update("revoked_at", currentTime).Error; err != nil {
					t.Fatalf("revoke Delegated Token: %v", err)
				}
			},
		},
		{
			name:       "source access token revoked",
			wantReason: CredentialInvalidTokenRevoked,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&AccessToken{}).Where("id = ?", fixture.AccessTokenID).
					Update("revoked_at", currentTime).Error; err != nil {
					t.Fatalf("revoke Delegated source Access Token: %v", err)
				}
			},
		},
		{
			name:       "family revoked",
			wantReason: CredentialInvalidTokenRevoked,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&RefreshTokenFamily{}).Where("id = ?", fixture.FamilyID).Updates(map[string]any{
					"revoked_at":     currentTime,
					"revoked_reason": "delegated_auth_context_test",
				}).Error; err != nil {
					t.Fatalf("revoke Delegated Token family: %v", err)
				}
			},
		},
		{
			name:       "authorization version changed",
			wantReason: CredentialInvalidAuthorizationVersion,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&Principal{}).Where("id = ?", fixture.PrincipalID).
					Update("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
					t.Fatalf("increment Delegated Token authorization version: %v", err)
				}
			},
		},
		{
			name:       "tenant membership suspended",
			wantReason: CredentialInvalidAuthorizationVersion,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&TenantMembership{}).Where("id = ?", fixture.TenantMembershipID).
					Update("status", TenantMembershipStatusSuspended).Error; err != nil {
					t.Fatalf("suspend Delegated Token membership: %v", err)
				}
			},
		},
		{
			name:       "tenant suspended",
			wantReason: CredentialInvalidTenantInactive,
			mutate: func(t *testing.T, fixture delegatedInvalidationFixture) {
				t.Helper()
				if err := db.Model(&Tenant{}).Where("id = ?", fixture.TenantID).
					Update("status", TenantStatusSuspended).Error; err != nil {
					t.Fatalf("suspend Delegated Token tenant: %v", err)
				}
			},
		},
	} {
		t.Run("delegated token rejects "+testCase.name, func(t *testing.T) {
			fixture := issueDelegatedInvalidationFixture(
				t,
				ctx,
				identityService,
				membershipService,
				selectionService,
				db,
				currentTime,
				strings.ReplaceAll(testCase.name, " ", "-"),
			)
			if _, err := authContextService.ResolveDelegatedAccessToken(ctx, fixture.Token); err != nil {
				t.Fatalf("resolve Delegated Token before invalidation: %v", err)
			}
			testCase.mutate(t, fixture)
			assertCredentialReason(t, authContextService, ctx, fixture.Token, testCase.wantReason)
		})
	}

	t.Run("delegated token accepts OAuth source and enforces source scopes", func(t *testing.T) {
		positive := issueResourceTicketInvalidationFixture(
			t, ctx, identityService, membershipService, selectionService, db, currentTime, "delegated-oauth-positive",
		)
		sourceContext, err := authContextService.ResolveFirstPartyAccessToken(ctx, positive.Session.AccessToken)
		if err != nil {
			t.Fatalf("resolve source context before OAuth projection: %v", err)
		}
		positiveAccessTokenID := insertOAuthSourceAccessToken(
			t,
			db,
			positive.PrincipalID,
			positive.TenantMembershipID,
			"11111111-1111-4111-8111-111111111111",
			"addp_at_oauth_positive",
			[]string{"workflow.run"},
			currentTime,
		)
		positiveToken := insertDelegatedAuthToken(
			t, db, positiveAccessTokenID, "addp_dat_oauth_positive", "develop", []string{"workflow.run"}, currentTime,
		)
		resolved, err := authContextService.ResolveDelegatedAccessToken(ctx, positiveToken)
		if err != nil {
			t.Fatalf("resolve OAuth-source Delegated Token: %v", err)
		}
		assertDelegatedAuthContext(t, resolved, sourceContext, "addp-cli", "develop", []string{"workflow.run"})

		negative := issueResourceTicketInvalidationFixture(
			t, ctx, identityService, membershipService, selectionService, db, currentTime, "delegated-oauth-negative",
		)
		negativeAccessTokenID := insertOAuthSourceAccessToken(
			t,
			db,
			negative.PrincipalID,
			negative.TenantMembershipID,
			"22222222-2222-4222-8222-222222222222",
			"addp_at_oauth_negative",
			[]string{"execution.get"},
			currentTime,
		)
		negativeToken := insertDelegatedAuthToken(
			t, db, negativeAccessTokenID, "addp_dat_oauth_negative", "develop", []string{"workflow.run"}, currentTime,
		)
		assertCredentialReason(t, authContextService, ctx, negativeToken, CredentialInvalidContext)
	})

	assertCredentialReason(t, authContextService, ctx, "invalid", CredentialInvalidFormat)
	assertCredentialReason(t, authContextService, ctx, "addp_at_unknown", CredentialInvalidNotFound)
	assertCredentialReason(t, authContextService, ctx, "addp_rat_", CredentialInvalidFormat)
	assertCredentialReason(t, authContextService, ctx, "addp_rat_unknown", CredentialInvalidNotFound)
	assertCredentialReason(t, authContextService, ctx, "addp_dat_", CredentialInvalidFormat)
	assertCredentialReason(t, authContextService, ctx, "addp_dat_unknown", CredentialInvalidNotFound)
}

type resourceTicketInvalidationFixture struct {
	PrincipalID        int64
	TenantID           int64
	TenantMembershipID int64
	FamilyID           int64
	AccessTokenID      int64
	Ticket             string
	Session            *IssuedBrowserSession
}

type delegatedInvalidationFixture struct {
	resourceTicketInvalidationFixture
	DelegatedID int64
	Token       string
}

func issueResourceTicketInvalidationFixture(
	t *testing.T,
	ctx context.Context,
	identityService *IdentityService,
	membershipService *TenantMembershipService,
	selectionService *ContextSelectionService,
	db *gorm.DB,
	now time.Time,
	suffix string,
) resourceTicketInvalidationFixture {
	t.Helper()
	audit := AuditMetadata{RequestID: stringPointer("resource-ticket-" + suffix)}
	user := createContextSelectionUser(t, ctx, identityService, "resource-ticket-"+suffix, audit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "resource-ticket-"+suffix, audit)
	membership := establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
	selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID: user.PrincipalID,
		Authentication: SessionAuthentication{
			Methods:         []string{"password"},
			AssuranceLevel:  AssuranceLevelAAL1,
			AuthenticatedAt: now.Add(-time.Minute),
		},
		Audit: audit,
	})
	if err != nil || selection.Session == nil {
		t.Fatalf("issue Resource Ticket invalidation fixture: selection=%#v error=%v", selection, err)
	}
	ticket := selection.Session.ResourceAccessTickets["manager"]
	var stored ResourceAccessTicket
	if err := db.Where("token_hash = ?", hashOpaqueToken(ticket)).First(&stored).Error; err != nil {
		t.Fatalf("read Resource Ticket invalidation fixture: %v", err)
	}
	return resourceTicketInvalidationFixture{
		PrincipalID:        user.PrincipalID,
		TenantID:           tenant.ID,
		TenantMembershipID: membership.Membership.ID,
		FamilyID:           stored.FamilyID,
		AccessTokenID:      readAccessTokenByHash(t, db, selection.Session.AccessToken).ID,
		Ticket:             ticket,
		Session:            selection.Session,
	}
}

func issueDelegatedInvalidationFixture(
	t *testing.T,
	ctx context.Context,
	identityService *IdentityService,
	membershipService *TenantMembershipService,
	selectionService *ContextSelectionService,
	db *gorm.DB,
	now time.Time,
	suffix string,
) delegatedInvalidationFixture {
	t.Helper()
	source := issueResourceTicketInvalidationFixture(
		t, ctx, identityService, membershipService, selectionService, db, now, "delegated-"+suffix,
	)
	token := "addp_dat_" + strings.ReplaceAll(suffix, "-", "_")
	stored := insertDelegatedAuthToken(t, db, source.AccessTokenID, token, "develop", []string{"workflow.run"}, now)
	var delegated DelegatedAccessToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(stored)).First(&delegated).Error; err != nil {
		t.Fatalf("read Delegated Token invalidation fixture: %v", err)
	}
	return delegatedInvalidationFixture{
		resourceTicketInvalidationFixture: source,
		DelegatedID:                       delegated.ID,
		Token:                             stored,
	}
}

func insertDelegatedAuthToken(
	t *testing.T,
	db *gorm.DB,
	sourceAccessTokenID int64,
	plainToken string,
	audience string,
	scopes []string,
	now time.Time,
) string {
	t.Helper()
	token := DelegatedAccessToken{
		TokenHash:           hashOpaqueToken(plainToken),
		SourceAccessTokenID: sourceAccessTokenID,
		Audience:            audience,
		Scopes:              append([]string(nil), scopes...),
		AgentRunID:          "run-" + plainToken,
		ToolCallID:          "call-" + plainToken,
		ExpiresAt:           now.Add(2 * time.Minute),
		CreatedAt:           now,
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("insert Delegated Token %s: %v", plainToken, err)
	}
	return plainToken
}

func insertOAuthSourceAccessToken(
	t *testing.T,
	db *gorm.DB,
	principalID int64,
	tenantMembershipID int64,
	protocolRequestID string,
	plainAccessToken string,
	scopes []string,
	now time.Time,
) int64 {
	t.Helper()
	var authorizationVersion int64
	if err := db.Table("system.principals").Select("authorization_version").Where("id = ?", principalID).
		Scan(&authorizationVersion).Error; err != nil || authorizationVersion == 0 {
		t.Fatalf("read OAuth source authorization version: version=%d error=%v", authorizationVersion, err)
	}
	var familyID int64
	if err := db.Raw(`
		INSERT INTO system.refresh_token_families (
			protocol_request_id, principal_id, context_type, tenant_membership_id,
			issued_authorization_version, client_id, auth_type, audiences, scopes,
			authentication_methods, assurance_level, authenticated_at, expires_at, created_at
		)
		VALUES (
			CAST(? AS uuid), ?, 'tenant', ?, ?, 'addp-cli', 'oauth', ARRAY['addp.api']::text[], ?,
			ARRAY['password']::text[], 'aal1', ?, ?, ?
		)
		RETURNING id
	`,
		protocolRequestID,
		principalID,
		tenantMembershipID,
		authorizationVersion,
		pq.Array(scopes),
		now.Add(-time.Minute),
		now.Add(30*time.Minute),
		now,
	).Scan(&familyID).Error; err != nil {
		t.Fatalf("insert OAuth source Family: %v", err)
	}
	var accessTokenID int64
	if err := db.Raw(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, hashOpaqueToken(plainAccessToken), familyID, now.Add(15*time.Minute), now).Scan(&accessTokenID).Error; err != nil {
		t.Fatalf("insert OAuth source Access Token: %v", err)
	}
	return accessTokenID
}

func assertTenantAuthContext(
	t *testing.T,
	authContext *commonauth.AuthContext,
	principalID int64,
	tenantID int64,
	tenantMembershipID int64,
	rootDepartmentID int64,
	childDepartmentID int64,
	departmentMembershipID int64,
	projectGroupID int64,
	projectMembershipID int64,
) {
	t.Helper()
	if authContext.SchemaVersion != commonauth.AuthContextSchemaVersion ||
		authContext.Principal.Type != "user" || authContext.Principal.ID != formatIAMID(principalID) ||
		authContext.Context.Type != "tenant" || authContext.Context.TenantID == nil ||
		*authContext.Context.TenantID != formatIAMID(tenantID) || authContext.Context.TenantMembershipID == nil ||
		*authContext.Context.TenantMembershipID != formatIAMID(tenantMembershipID) {
		t.Fatalf("projected tenant identity/context = %#v", authContext)
	}
	if authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-web" ||
		authContext.Client.ScopeMode != "unrestricted" ||
		!reflect.DeepEqual(authContext.Client.Audiences, []string{"addp.api"}) || len(authContext.Client.Scopes) != 0 {
		t.Fatalf("projected tenant client = %#v", authContext.Client)
	}
	if len(authContext.Organization.Departments) != 1 {
		t.Fatalf("projected departments = %#v", authContext.Organization.Departments)
	}
	department := authContext.Organization.Departments[0]
	if department.MembershipID != formatIAMID(departmentMembershipID) ||
		department.DepartmentID != formatIAMID(childDepartmentID) ||
		!reflect.DeepEqual(department.AncestorIDs, []string{formatIAMID(rootDepartmentID)}) {
		t.Fatalf("projected department = %#v", department)
	}
	if len(authContext.Organization.ProjectGroups) != 1 ||
		authContext.Organization.ProjectGroups[0].MembershipID != formatIAMID(projectMembershipID) ||
		authContext.Organization.ProjectGroups[0].ProjectGroupID != formatIAMID(projectGroupID) {
		t.Fatalf("projected project groups = %#v", authContext.Organization.ProjectGroups)
	}
	if len(authContext.Authorization.RoleAssignments) != 3 {
		t.Fatalf("projected role assignments = %#v", authContext.Authorization.RoleAssignments)
	}
	wantScopes := []string{"tenant", "department", "project_group"}
	for index, assignment := range authContext.Authorization.RoleAssignments {
		if assignment.Scope.Type != wantScopes[index] || assignment.RoleKey != "tenant.data_viewer" ||
			!reflect.DeepEqual(assignment.Permissions, []string{
				"manager.content.read",
				"manager.data_item.read",
				"manager.search.execute",
				"meta.catalog.read",
			}) {
			t.Fatalf("projected role assignment %d = %#v", index, assignment)
		}
	}
	if authContext.Token.Type != "first_party_access_token" || authContext.Delegation != nil {
		t.Fatalf("projected token facts = token:%#v delegation:%#v", authContext.Token, authContext.Delegation)
	}
}

func assertResourceTicketAuthContext(
	t *testing.T,
	resourceContext *commonauth.AuthContext,
	accessContext *commonauth.AuthContext,
	owner string,
) {
	t.Helper()
	if resourceContext == nil || accessContext == nil {
		t.Fatalf("resource/access AuthContext must not be nil: resource=%#v access=%#v", resourceContext, accessContext)
	}
	if !reflect.DeepEqual(resourceContext.Principal, accessContext.Principal) ||
		!reflect.DeepEqual(resourceContext.Context, accessContext.Context) ||
		!reflect.DeepEqual(resourceContext.Authentication, accessContext.Authentication) ||
		!reflect.DeepEqual(resourceContext.Organization, accessContext.Organization) ||
		!reflect.DeepEqual(resourceContext.Authorization, accessContext.Authorization) {
		t.Fatalf("resource ticket did not inherit browser family facts: resource=%#v access=%#v", resourceContext, accessContext)
	}
	if resourceContext.Client.ClientID == nil || *resourceContext.Client.ClientID != "addp-web" ||
		!reflect.DeepEqual(resourceContext.Client.Audiences, []string{owner}) ||
		resourceContext.Client.ScopeMode != "restricted" ||
		!reflect.DeepEqual(resourceContext.Client.Scopes, []string{commonauth.BrowserResourceAccessScope}) ||
		resourceContext.Token.Type != "resource_access_ticket" || resourceContext.Delegation != nil ||
		resourceContext.Token.ExpiresAt.After(accessContext.Token.ExpiresAt) {
		t.Fatalf("resource ticket constraints = client:%#v token:%#v delegation:%#v",
			resourceContext.Client, resourceContext.Token, resourceContext.Delegation)
	}
	if err := commonauth.ValidateAuthContext(*resourceContext); err != nil {
		t.Fatalf("validate Resource Ticket AuthContext: %v", err)
	}
}

func assertDelegatedAuthContext(
	t *testing.T,
	delegatedContext *commonauth.AuthContext,
	sourceContext *commonauth.AuthContext,
	clientID string,
	audience string,
	scopes []string,
) {
	t.Helper()
	if delegatedContext == nil || sourceContext == nil {
		t.Fatalf("delegated/source AuthContext must not be nil: delegated=%#v source=%#v", delegatedContext, sourceContext)
	}
	if !reflect.DeepEqual(delegatedContext.Principal, sourceContext.Principal) ||
		!reflect.DeepEqual(delegatedContext.Context, sourceContext.Context) ||
		!reflect.DeepEqual(delegatedContext.Authentication, sourceContext.Authentication) ||
		!reflect.DeepEqual(delegatedContext.Organization, sourceContext.Organization) ||
		!reflect.DeepEqual(delegatedContext.Authorization, sourceContext.Authorization) {
		t.Fatalf("Delegated Token did not inherit source Family facts: delegated=%#v source=%#v", delegatedContext, sourceContext)
	}
	if delegatedContext.Client.ClientID == nil || *delegatedContext.Client.ClientID != clientID ||
		!reflect.DeepEqual(delegatedContext.Client.Audiences, []string{audience}) ||
		delegatedContext.Client.ScopeMode != "restricted" ||
		!reflect.DeepEqual(delegatedContext.Client.Scopes, scopes) ||
		delegatedContext.Token.Type != "delegated_access_token" || delegatedContext.Delegation == nil ||
		delegatedContext.Delegation.DelegatedByClientID != clientID ||
		delegatedContext.Delegation.AgentRunID == "" || delegatedContext.Delegation.ToolCallID == "" ||
		delegatedContext.Token.ExpiresAt.After(sourceContext.Token.ExpiresAt) {
		t.Fatalf("Delegated Token constraints = client:%#v token:%#v delegation:%#v",
			delegatedContext.Client, delegatedContext.Token, delegatedContext.Delegation)
	}
	if err := commonauth.ValidateAuthContext(*delegatedContext); err != nil {
		t.Fatalf("validate Delegated Token AuthContext: %v", err)
	}
}

func assertAccessTokenReason(
	t *testing.T,
	service *AuthContextService,
	ctx context.Context,
	accessToken string,
	want CredentialInvalidReason,
) {
	t.Helper()
	_, err := service.ResolveFirstPartyAccessToken(ctx, accessToken)
	assertCredentialValidationError(t, err, want)
}

func assertCredentialReason(
	t *testing.T,
	service *AuthContextService,
	ctx context.Context,
	credential string,
	want CredentialInvalidReason,
) {
	t.Helper()
	_, err := service.ResolveAuthContext(ctx, credential)
	assertCredentialValidationError(t, err, want)
}

func assertCredentialValidationError(t *testing.T, err error, want CredentialInvalidReason) {
	t.Helper()
	if !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("resolve credential error = %v, want unauthorized", err)
	}
	var validationError *CredentialValidationError
	if !errors.As(err, &validationError) || validationError.Reason != want {
		t.Fatalf("resolve credential reason = %#v, want %s", validationError, want)
	}
	if err.Error() != commonapi.ErrUnauthorized.Error() {
		t.Fatalf("credential error leaked internal reason: %q", err.Error())
	}
}

func insertDepartment(t *testing.T, db *gorm.DB, tenantID int64, parentID *int64, code string) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.departments (tenant_id, parent_id, code, name)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, tenantID, parentID, code, code).Scan(&id).Error; err != nil {
		t.Fatalf("insert department %s: %v", code, err)
	}
	return id
}

func insertDepartmentMembership(
	t *testing.T,
	db *gorm.DB,
	tenantID int64,
	departmentID int64,
	tenantMembershipID int64,
) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.department_memberships (
			tenant_id, department_id, tenant_membership_id, membership_type, relation_role
		)
		VALUES (?, ?, ?, 'primary', 'member')
		RETURNING id
	`, tenantID, departmentID, tenantMembershipID).Scan(&id).Error; err != nil {
		t.Fatalf("insert department membership: %v", err)
	}
	return id
}

func insertProjectGroup(t *testing.T, db *gorm.DB, tenantID int64, code string) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.project_groups (tenant_id, code, name, status)
		VALUES (?, ?, ?, 'planned')
		RETURNING id
	`, tenantID, code, code).Scan(&id).Error; err != nil {
		t.Fatalf("insert project group: %v", err)
	}
	return id
}

func insertProjectGroupMembership(
	t *testing.T,
	db *gorm.DB,
	tenantID int64,
	projectGroupID int64,
	tenantMembershipID int64,
) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.project_group_memberships (
			tenant_id, project_group_id, tenant_membership_id, relation_role
		)
		VALUES (?, ?, ?, 'member')
		RETURNING id
	`, tenantID, projectGroupID, tenantMembershipID).Scan(&id).Error; err != nil {
		t.Fatalf("insert project group membership: %v", err)
	}
	return id
}

func insertRoleAssignment(
	t *testing.T,
	db *gorm.DB,
	principalID int64,
	roleKey string,
	scopeType string,
	tenantID *int64,
	departmentID *int64,
	projectGroupID *int64,
	validFrom time.Time,
	validUntil *time.Time,
	sourceType string,
) int64 {
	t.Helper()
	var roleID int64
	if err := db.Table("system.roles").Select("id").Where("role_key = ?", roleKey).Scan(&roleID).Error; err != nil {
		t.Fatalf("find role %s: %v", roleKey, err)
	}
	if roleID == 0 {
		t.Fatalf("role %s was not seeded", roleKey)
	}
	var createdByPrincipalID *int64
	if sourceType != "bootstrap" {
		createdByPrincipalID = &principalID
	}
	var assignmentID int64
	if err := db.Raw(`
		INSERT INTO system.role_assignments (
			principal_id, role_id, scope_type, tenant_id, department_id, project_group_id,
			valid_from, valid_until, source_type, created_by_principal_id, reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'AuthContext integration test')
		RETURNING id
	`,
		principalID,
		roleID,
		scopeType,
		tenantID,
		departmentID,
		projectGroupID,
		validFrom,
		validUntil,
		sourceType,
		createdByPrincipalID,
	).Scan(&assignmentID).Error; err != nil {
		t.Fatalf("insert %s role assignment at %s scope: %v", roleKey, scopeType, err)
	}
	return assignmentID
}
