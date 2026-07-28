package iam

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRefreshTokenRotationServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(12)
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset refresh rotation test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"standard", "manager"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSelectionService: %v", err)
	}
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)
	authentication := SessionAuthentication{
		Methods:         []string{"password"},
		AssuranceLevel:  AssuranceLevelAAL1,
		AuthenticatedAt: currentTime.Add(-time.Minute),
	}

	t.Run("normal rotation and concurrent competition", func(t *testing.T) {
		initial := issueRefreshRotationSession(
			t, ctx, identityService, membershipService, selectionService,
			"rotation-normal", "rotation-normal", authentication,
		)
		initialFamily := readRefreshFamily(t, db, initial.FamilyID)
		initialAccess := readAccessTokenByHash(t, db, initial.AccessToken)
		initialRefresh := readRefreshTokenByHash(t, db, initial.RefreshToken)
		initialTickets := readResourceTickets(t, db, initial.FamilyID)
		delegatedID := insertDelegatedToken(t, db, initialAccess, currentTime, "normal")

		currentTime = currentTime.Add(time.Second)
		rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("refresh-normal")},
		})
		if err != nil {
			t.Fatalf("rotate current refresh token: %v", err)
		}
		assertRefreshRotationResult(
			t, db, initial, rotated, initialFamily, initialAccess, initialRefresh, initialTickets, delegatedID,
		)
		assertSessionSecretsAbsentFromStorageAndAudit(t, db, rotated)
		assertAuditEventCount(t, db, initial.FamilyID, "iam.refresh_token.rotated", AuditRiskMedium, 1)

		familyBeforeCompetition := readRefreshFamily(t, db, initial.FamilyID)
		principalBlocker := db.Begin()
		if principalBlocker.Error != nil {
			t.Fatalf("begin principal blocker: %v", principalBlocker.Error)
		}
		defer func() { _ = principalBlocker.Rollback().Error }()
		if err := principalBlocker.Exec(
			`SELECT id FROM system.principals WHERE id = ? FOR UPDATE`,
			initialFamily.PrincipalID,
		).Error; err != nil {
			_ = principalBlocker.Rollback().Error
			t.Fatalf("lock principal for concurrent refresh test: %v", err)
		}

		type rotationResult struct {
			session *IssuedBrowserSession
			err     error
		}
		results := make(chan rotationResult, 2)
		var waitGroup sync.WaitGroup
		for index := 0; index < 2; index++ {
			waitGroup.Add(1)
			go func(index int) {
				defer waitGroup.Done()
				session, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
					RefreshToken: rotated.RefreshToken,
					Audit: AuditMetadata{
						RequestID: stringPointer(fmt.Sprintf("refresh-concurrent-%d", index)),
					},
				})
				results <- rotationResult{session: session, err: err}
			}(index)
		}
		waitForPrincipalLockWaiters(t, db, 2)
		if err := principalBlocker.Commit().Error; err != nil {
			t.Fatalf("release principal blocker: %v", err)
		}
		waitGroup.Wait()
		close(results)

		successCount := 0
		conflictCount := 0
		var winner *IssuedBrowserSession
		for result := range results {
			switch {
			case result.err == nil && result.session != nil:
				successCount++
				winner = result.session
			case errors.Is(result.err, ErrRefreshTokenRotationConflict) && errors.Is(result.err, commonapi.ErrConflict):
				conflictCount++
			default:
				t.Fatalf("unexpected concurrent rotation result: session=%#v err=%v", result.session, result.err)
			}
		}
		if successCount != 1 || conflictCount != 1 {
			t.Fatalf("concurrent rotations = success:%d conflict:%d, want 1/1", successCount, conflictCount)
		}
		if winner == nil || winner.RefreshToken == rotated.RefreshToken {
			t.Fatalf("concurrent rotation winner = %#v", winner)
		}
		familyAfterCompetition := readRefreshFamily(t, db, initial.FamilyID)
		if familyAfterCompetition.RevokedAt != nil || !familyAfterCompetition.ExpiresAt.Equal(familyBeforeCompetition.ExpiresAt) {
			t.Fatalf("family after concurrent competition = %#v", familyAfterCompetition)
		}
		assertAuditEventCount(t, db, initial.FamilyID, "iam.refresh_token.rotated", AuditRiskMedium, 2)
		assertAuditEventCount(t, db, initial.FamilyID, "iam.refresh_token.reuse_detected", AuditRiskHigh, 0)
	})

	t.Run("authorization changes advance an active family during rotation", func(t *testing.T) {
		currentTime = time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
		initial := issueRefreshRotationSession(
			t, ctx, identityService, membershipService, selectionService,
			"rotation-authorization", "rotation-authorization", authentication,
		)
		familyBefore := readRefreshFamily(t, db, initial.FamilyID)
		principal, err := repository.GetPrincipal(ctx, familyBefore.PrincipalID)
		if err != nil {
			t.Fatalf("read authorization rotation principal: %v", err)
		}
		advancedVersion, err := repository.IncrementPrincipalAuthorizationVersion(ctx, principal.ID)
		if err != nil || advancedVersion <= familyBefore.IssuedAuthorizationVersion {
			t.Fatalf("advance principal authorization version = %d err=%v", advancedVersion, err)
		}
		authContextService, err := NewAuthContextService(repository)
		if err != nil {
			t.Fatalf("create authorization rotation AuthContext service: %v", err)
		}
		if _, err := authContextService.ResolveFirstPartyAccessToken(ctx, initial.AccessToken); err == nil {
			t.Fatal("old access token remained valid after authorization version changed")
		} else {
			var validationError *CredentialValidationError
			if !errors.As(err, &validationError) || validationError.Reason != CredentialInvalidAuthorizationVersion {
				t.Fatalf("old access token error = %v, want authorization version mismatch", err)
			}
		}

		currentTime = currentTime.Add(time.Second)
		rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("refresh-authorization-advanced")},
		})
		if err != nil {
			t.Fatalf("rotate after authorization change: %v", err)
		}
		familyAfter := readRefreshFamily(t, db, initial.FamilyID)
		if familyAfter.IssuedAuthorizationVersion != advancedVersion || familyAfter.RevokedAt != nil ||
			!familyAfter.ExpiresAt.Equal(familyBefore.ExpiresAt) {
			t.Fatalf("advanced refresh family = %#v, previous=%#v", familyAfter, familyBefore)
		}
		if _, err := authContextService.ResolveFirstPartyAccessToken(ctx, rotated.AccessToken); err != nil {
			var validationError *CredentialValidationError
			if errors.As(err, &validationError) {
				t.Fatalf("resolve rotated access token after authorization advance: %v (%s)", err, validationError.Reason)
			}
			t.Fatalf("resolve rotated access token after authorization advance: %v", err)
		}
	})

	t.Run("rotation and reuse audit failures roll back", func(t *testing.T) {
		initial := issueRefreshRotationSession(
			t, ctx, identityService, membershipService, selectionService,
			"rotation-rollback", "rotation-rollback", authentication,
		)
		initialAccess := readAccessTokenByHash(t, db, initial.AccessToken)
		initialRefresh := readRefreshTokenByHash(t, db, initial.RefreshToken)
		initialTicketCount := countActiveFamilyRows(t, db, "system.resource_access_tickets", initial.FamilyID)
		initialAuditCount := countFamilyAuditRows(t, db, initial.FamilyID)

		currentTime = currentTime.Add(time.Second)
		_, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer(" ")},
		})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("rotation with rejected audit error = %v, want bad request", err)
		}
		assertRefreshTokenStillCurrent(t, db, initialRefresh.ID)
		assertAccessTokenStillActive(t, db, initialAccess.ID)
		if got := countActiveFamilyRows(t, db, "system.resource_access_tickets", initial.FamilyID); got != initialTicketCount {
			t.Fatalf("active resource tickets after rotation rollback = %d, want %d", got, initialTicketCount)
		}
		if got := countFamilyAuditRows(t, db, initial.FamilyID); got != initialAuditCount {
			t.Fatalf("family audit rows after rotation rollback = %d, want %d", got, initialAuditCount)
		}

		rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("refresh-before-reuse")},
		})
		if err != nil {
			t.Fatalf("rotate before reuse test: %v", err)
		}
		rotatedAccess := readAccessTokenByHash(t, db, rotated.AccessToken)
		delegatedID := insertDelegatedToken(t, db, rotatedAccess, currentTime, "reuse")

		currentTime = currentTime.Add(time.Second)
		_, err = tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer(" ")},
		})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("reuse handling with rejected audit error = %v, want bad request", err)
		}
		familyAfterRollback := readRefreshFamily(t, db, initial.FamilyID)
		oldRefreshAfterRollback := readRefreshTokenByID(t, db, initialRefresh.ID)
		if familyAfterRollback.RevokedAt != nil || oldRefreshAfterRollback.ReuseDetectedAt != nil {
			t.Fatalf("reuse state after audit rollback: family=%#v token=%#v", familyAfterRollback, oldRefreshAfterRollback)
		}
		assertAccessTokenStillActive(t, db, rotatedAccess.ID)
		assertDelegatedTokenActive(t, db, delegatedID)

		_, err = tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("refresh-reuse-detected")},
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("historical refresh token reuse error = %v, want unauthorized", err)
		}
		assertFamilyRevokedForReuse(t, db, initial.FamilyID, initialRefresh.ID, delegatedID)
		assertAuditEventCount(t, db, initial.FamilyID, "iam.refresh_token.reuse_detected", AuditRiskHigh, 1)

		_, err = tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: initial.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("refresh-reuse-repeat")},
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("repeated historical refresh token error = %v, want unauthorized", err)
		}
		assertAuditEventCount(t, db, initial.FamilyID, "iam.refresh_token.reuse_detected", AuditRiskHigh, 1)
	})

	for _, token := range []string{"invalid", "addp_rt_unknown"} {
		if _, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: token,
		}); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("invalid refresh token %q error = %v, want unauthorized", token, err)
		}
	}
}

func issueRefreshRotationSession(
	t *testing.T,
	ctx context.Context,
	identityService *IdentityService,
	membershipService *TenantMembershipService,
	selectionService *ContextSelectionService,
	username string,
	tenantCode string,
	authentication SessionAuthentication,
) *IssuedBrowserSession {
	t.Helper()
	audit := AuditMetadata{RequestID: stringPointer("issue-" + username)}
	user := createContextSelectionUser(t, ctx, identityService, username, audit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, tenantCode, audit)
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
	result, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    user.PrincipalID,
		Authentication: authentication,
		Audit:          audit,
	})
	if err != nil {
		t.Fatalf("issue refresh rotation session: %v", err)
	}
	if result.Session == nil {
		t.Fatalf("issued refresh rotation result = %#v", result)
	}
	return result.Session
}

func assertRefreshRotationResult(
	t *testing.T,
	db *gorm.DB,
	previous *IssuedBrowserSession,
	rotated *IssuedBrowserSession,
	initialFamily RefreshTokenFamily,
	initialAccess AccessToken,
	initialRefresh RefreshToken,
	initialTickets []ResourceAccessTicket,
	delegatedID int64,
) {
	t.Helper()
	if rotated == nil || rotated.FamilyID != previous.FamilyID ||
		rotated.AccessToken == previous.AccessToken || rotated.RefreshToken == previous.RefreshToken ||
		!rotated.RefreshTokenFamilyExpiresAt.Equal(initialFamily.ExpiresAt) {
		t.Fatalf("rotated browser session = %#v", rotated)
	}
	if !strings.HasPrefix(rotated.AccessToken, "addp_at_") || !strings.HasPrefix(rotated.RefreshToken, "addp_rt_") ||
		len(rotated.ResourceAccessTickets) != len(initialTickets) {
		t.Fatalf("rotated browser session secrets = %#v", rotated)
	}

	oldRefresh := readRefreshTokenByID(t, db, initialRefresh.ID)
	newRefresh := readRefreshTokenByHash(t, db, rotated.RefreshToken)
	if oldRefresh.UsedAt == nil || oldRefresh.ReplacedByTokenID == nil ||
		*oldRefresh.ReplacedByTokenID != newRefresh.ID || newRefresh.ParentTokenID == nil ||
		*newRefresh.ParentTokenID != oldRefresh.ID || !newRefresh.ExpiresAt.Equal(initialFamily.ExpiresAt) {
		t.Fatalf("refresh replacement chain: old=%#v new=%#v", oldRefresh, newRefresh)
	}
	oldAccess := readAccessTokenByID(t, db, initialAccess.ID)
	if oldAccess.RevokedAt == nil {
		t.Fatalf("old access token was not revoked: %#v", oldAccess)
	}
	assertDelegatedTokenRevoked(t, db, delegatedID)
	for _, ticket := range initialTickets {
		var stored ResourceAccessTicket
		if err := db.First(&stored, ticket.ID).Error; err != nil {
			t.Fatalf("read old resource ticket %d: %v", ticket.ID, err)
		}
		if stored.RevokedAt == nil {
			t.Fatalf("old resource ticket was not revoked: %#v", stored)
		}
	}
	newAccess := readAccessTokenByHash(t, db, rotated.AccessToken)
	if newRefresh.IssuedAccessTokenID != newAccess.ID || newAccess.RevokedAt != nil {
		t.Fatalf("replacement access/refresh tokens: access=%#v refresh=%#v", newAccess, newRefresh)
	}
	for owner, plainTicket := range rotated.ResourceAccessTickets {
		var ticket ResourceAccessTicket
		if err := db.Where("token_hash = ?", hashOpaqueToken(plainTicket)).Take(&ticket).Error; err != nil {
			t.Fatalf("read replacement resource ticket %s: %v", owner, err)
		}
		if ticket.Owner != owner || ticket.FamilyID != rotated.FamilyID || ticket.RevokedAt != nil {
			t.Fatalf("replacement resource ticket %s = %#v", owner, ticket)
		}
	}
}

func waitForPrincipalLockWaiters(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		err := db.Raw(`
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query ILIKE '%principals%'
			  AND query ILIKE '%FOR UPDATE%'
		`).Scan(&count).Error
		if err != nil {
			t.Fatalf("inspect PostgreSQL lock waiters: %v", err)
		}
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not observe %d concurrent principal lock waiters", want)
}

func insertDelegatedToken(
	t *testing.T,
	db *gorm.DB,
	accessToken AccessToken,
	createdAt time.Time,
	suffix string,
) int64 {
	t.Helper()
	expiresAt := earlierTime(createdAt.Add(5*time.Minute), accessToken.ExpiresAt)
	var tokenID int64
	err := db.Raw(`
		INSERT INTO system.delegated_access_tokens (
			token_hash, source_access_token_id, audience, scopes,
			agent_run_id, tool_call_id, expires_at, created_at
		)
		VALUES (?, ?, 'manager', ?, ?, ?, ?, ?)
		RETURNING id
	`,
		hashOpaqueToken("delegated-"+suffix),
		accessToken.ID,
		pq.StringArray{"data.read"},
		"run-"+suffix,
		"tool-"+suffix,
		expiresAt,
		createdAt,
	).Scan(&tokenID).Error
	if err != nil {
		t.Fatalf("insert delegated token: %v", err)
	}
	return tokenID
}

func assertFamilyRevokedForReuse(
	t *testing.T,
	db *gorm.DB,
	familyID int64,
	reusedRefreshTokenID int64,
	delegatedID int64,
) {
	t.Helper()
	family := readRefreshFamily(t, db, familyID)
	if family.RevokedAt == nil || family.RevokedReason == nil || *family.RevokedReason != refreshTokenReuseRevocationReason {
		t.Fatalf("family was not revoked for refresh reuse: %#v", family)
	}
	reused := readRefreshTokenByID(t, db, reusedRefreshTokenID)
	if reused.ReuseDetectedAt == nil || reused.RevokedAt == nil {
		t.Fatalf("historical refresh token reuse state = %#v", reused)
	}
	for _, table := range []string{
		"system.access_tokens",
		"system.refresh_tokens",
		"system.resource_access_tickets",
	} {
		if got := countActiveFamilyRows(t, db, table, familyID); got != 0 {
			t.Fatalf("active rows in %s after family reuse revocation = %d", table, got)
		}
	}
	assertDelegatedTokenRevoked(t, db, delegatedID)
}

func assertSessionSecretsAbsentFromStorageAndAudit(t *testing.T, db *gorm.DB, session *IssuedBrowserSession) {
	t.Helper()
	secrets := []string{session.AccessToken, session.RefreshToken}
	for _, ticket := range session.ResourceAccessTickets {
		secrets = append(secrets, ticket)
	}
	for _, secret := range secrets {
		var rawTokenCount int64
		for _, table := range []string{
			"system.access_tokens",
			"system.refresh_tokens",
			"system.resource_access_tickets",
		} {
			if err := db.Table(table).Where("token_hash = ?", secret).Count(&rawTokenCount).Error; err != nil {
				t.Fatalf("scan %s for raw token: %v", table, err)
			}
			if rawTokenCount != 0 {
				t.Fatalf("raw token appeared in %s", table)
			}
		}
		var auditCount int64
		if err := db.Table("system.audit_logs").Where("details::text LIKE ?", "%"+secret+"%").Count(&auditCount).Error; err != nil {
			t.Fatalf("scan audit details for raw token: %v", err)
		}
		if auditCount != 0 {
			t.Fatalf("raw token appeared in %d audit rows", auditCount)
		}
	}
}

func assertAuditEventCount(
	t *testing.T,
	db *gorm.DB,
	familyID int64,
	eventName string,
	riskLevel AuditRiskLevel,
	want int64,
) {
	t.Helper()
	var count int64
	err := db.Table("system.audit_logs").
		Where("entity_type = 'token_family' AND entity_id = ? AND event_name = ? AND risk_level = ?",
			fmt.Sprintf("%d", familyID), eventName, riskLevel).
		Count(&count).Error
	if err != nil {
		t.Fatalf("count audit event %s: %v", eventName, err)
	}
	if count != want {
		t.Fatalf("audit event %s count = %d, want %d", eventName, count, want)
	}
}

func countFamilyAuditRows(t *testing.T, db *gorm.DB, familyID int64) int64 {
	t.Helper()
	var count int64
	if err := db.Table("system.audit_logs").
		Where("entity_type = 'token_family' AND entity_id = ?", fmt.Sprintf("%d", familyID)).
		Count(&count).Error; err != nil {
		t.Fatalf("count family audit rows: %v", err)
	}
	return count
}

func countActiveFamilyRows(t *testing.T, db *gorm.DB, table string, familyID int64) int64 {
	t.Helper()
	var count int64
	if err := db.Table(table).Where("family_id = ? AND revoked_at IS NULL", familyID).Count(&count).Error; err != nil {
		t.Fatalf("count active family rows in %s: %v", table, err)
	}
	return count
}

func readRefreshFamily(t *testing.T, db *gorm.DB, familyID int64) RefreshTokenFamily {
	t.Helper()
	var family RefreshTokenFamily
	if err := db.First(&family, familyID).Error; err != nil {
		t.Fatalf("read refresh token family %d: %v", familyID, err)
	}
	return family
}

func readAccessTokenByHash(t *testing.T, db *gorm.DB, plainToken string) AccessToken {
	t.Helper()
	var token AccessToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(plainToken)).Take(&token).Error; err != nil {
		t.Fatalf("read access token by hash: %v", err)
	}
	return token
}

func readAccessTokenByID(t *testing.T, db *gorm.DB, tokenID int64) AccessToken {
	t.Helper()
	var token AccessToken
	if err := db.First(&token, tokenID).Error; err != nil {
		t.Fatalf("read access token %d: %v", tokenID, err)
	}
	return token
}

func readRefreshTokenByHash(t *testing.T, db *gorm.DB, plainToken string) RefreshToken {
	t.Helper()
	var token RefreshToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(plainToken)).Take(&token).Error; err != nil {
		t.Fatalf("read refresh token by hash: %v", err)
	}
	return token
}

func readRefreshTokenByID(t *testing.T, db *gorm.DB, tokenID int64) RefreshToken {
	t.Helper()
	var token RefreshToken
	if err := db.First(&token, tokenID).Error; err != nil {
		t.Fatalf("read refresh token %d: %v", tokenID, err)
	}
	return token
}

func readResourceTickets(t *testing.T, db *gorm.DB, familyID int64) []ResourceAccessTicket {
	t.Helper()
	var tickets []ResourceAccessTicket
	if err := db.Where("family_id = ? AND revoked_at IS NULL", familyID).Order("id ASC").Find(&tickets).Error; err != nil {
		t.Fatalf("read resource access tickets: %v", err)
	}
	return tickets
}

func assertRefreshTokenStillCurrent(t *testing.T, db *gorm.DB, tokenID int64) {
	t.Helper()
	token := readRefreshTokenByID(t, db, tokenID)
	if token.UsedAt != nil || token.ReplacedByTokenID != nil || token.ReuseDetectedAt != nil || token.RevokedAt != nil {
		t.Fatalf("refresh token was modified despite rollback: %#v", token)
	}
}

func assertAccessTokenStillActive(t *testing.T, db *gorm.DB, tokenID int64) {
	t.Helper()
	token := readAccessTokenByID(t, db, tokenID)
	if token.RevokedAt != nil {
		t.Fatalf("access token is revoked: %#v", token)
	}
}

func assertDelegatedTokenActive(t *testing.T, db *gorm.DB, tokenID int64) {
	t.Helper()
	var token struct {
		RevokedAt *time.Time
	}
	if err := db.Table("system.delegated_access_tokens").Select("revoked_at").Where("id = ?", tokenID).Take(&token).Error; err != nil {
		t.Fatalf("read delegated token %d: %v", tokenID, err)
	}
	if token.RevokedAt != nil {
		t.Fatalf("delegated token %d is revoked", tokenID)
	}
}

func assertDelegatedTokenRevoked(t *testing.T, db *gorm.DB, tokenID int64) {
	t.Helper()
	var token struct {
		RevokedAt *time.Time
	}
	if err := db.Table("system.delegated_access_tokens").Select("revoked_at").Where("id = ?", tokenID).Take(&token).Error; err != nil {
		t.Fatalf("read delegated token %d: %v", tokenID, err)
	}
	if token.RevokedAt == nil {
		t.Fatalf("delegated token %d was not revoked", tokenID)
	}
}
