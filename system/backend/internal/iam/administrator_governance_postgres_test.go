package iam

import (
	"context"
	"errors"
	"fmt"
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"sync"
	"testing"
	"time"
)

func TestAdministratorGovernanceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	identity := NewIdentityService(repo, nil)
	cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	recovery := NewAdministratorCredentialRecoveryService(repo, cipher)
	service := NewAdministratorChangeService(repo).WithCredentialRecovery(recovery)
	realm := ContextTypePlatform
	audit := AuditMetadata{ContextType: &realm}
	admins := map[string]int64{}
	for _, key := range bootstrapRoleOrder {
		user := createGovernedManagementUser(t, ctx, identity, key, key, audit, db)
		admins[key] = user.PrincipalID
		createManagementMFACredential(t, ctx, repo, user.PrincipalID)
	}
	requester := func(role string) int64 {
		if role == "platform.security_administrator" {
			return admins["platform.system_administrator"]
		}
		return admins["platform.security_administrator"]
	}
	reviewer := func(role string) int64 {
		if role == "platform.audit_administrator" {
			return admins["platform.system_administrator"]
		}
		return admins["platform.audit_administrator"]
	}
	create := func(kind PrivilegedChangeType, target, actor int64) *PrivilegedChangeRequest {
		t.Helper()
		req, err := service.Create(ctx, CreateAdministratorChangeInput{ChangeType: kind, TargetPrincipalID: target, RequestedByPrincipalID: actor, Reason: "governance test", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	review := func(req *PrivilegedChangeRequest, actor int64) *PrivilegedChangeRequest {
		t.Helper()
		result, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: actor, Reason: "independent review", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, role := range bootstrapRoleOrder {
		t.Run(role+" suspension and recovery", func(t *testing.T) {
			req := create(PrivilegedChangePlatformIdentitySuspend, admins[role], requester(role))
			if _, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version + 1, ReviewerPrincipalID: reviewer(role), Reason: "stale version", Audit: audit}); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("stale request version: %v", err)
			}
			if _, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: admins[role], Reason: "self review", Audit: audit}); err == nil {
				t.Fatal("target self review allowed")
			}
			result := review(req, reviewer(role))
			if result.Status != PrivilegedChangeStatusApplied {
				t.Fatalf("approval did not apply: %s", result.Status)
			}
			p, err := repo.GetPrincipal(ctx, admins[role])
			if err != nil || p.Status != PrincipalStatusSuspended {
				t.Fatalf("target not suspended: %+v %v", p, err)
			}
			if _, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: reviewer(role), Reason: "replay", Audit: audit}); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("replay: %v", err)
			}
			req = create(PrivilegedChangePlatformIdentityReactivate, admins[role], requester(role))
			review(req, reviewer(role))
		})
	}
	t.Run("incorrect requester and ordinary account bypass", func(t *testing.T) {
		if _, err := service.Create(ctx, CreateAdministratorChangeInput{ChangeType: PrivilegedChangePlatformIdentitySuspend, TargetPrincipalID: admins["platform.security_administrator"], RequestedByPrincipalID: admins["platform.audit_administrator"], Reason: "wrong requester", Audit: audit}); err == nil {
			t.Fatal("wrong requester role accepted")
		}
		users := NewPlatformUserService(repo, identity, nil)
		if _, err := users.Suspend(ctx, ChangeManagedUserStatusInput{UserID: admins["platform.system_administrator"], Reason: "ordinary bypass", Audit: audit}); err == nil {
			t.Fatal("ordinary user lifecycle bypassed governance")
		}
	})
	t.Run("changed authorization invalidates review", func(t *testing.T) {
		req := create(PrivilegedChangePlatformIdentitySuspend, admins["platform.system_administrator"], admins["platform.security_administrator"])
		if _, err := repo.IncrementPrincipalAuthorizationVersion(ctx, req.TargetPrincipalID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: admins["platform.audit_administrator"], Reason: "changed subject", Audit: audit}); err == nil {
			t.Fatal("stale target was approved")
		}
		if err := db.Exec("UPDATE system.privileged_change_requests SET status='cancelled',decided_at=statement_timestamp() WHERE id=?", req.ID).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent approvals have one winner", func(t *testing.T) {
		req := create(PrivilegedChangePlatformIdentitySuspend, admins["platform.audit_administrator"], admins["platform.security_administrator"])
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: admins["platform.system_administrator"], Reason: "concurrent review", Audit: audit})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		wins := 0
		for err := range results {
			if err == nil {
				wins++
			} else if !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("unexpected loser: %v", err)
			}
		}
		if wins != 1 {
			t.Fatalf("winners=%d", wins)
		}
		req = create(PrivilegedChangePlatformIdentityReactivate, admins["platform.audit_administrator"], admins["platform.security_administrator"])
		review(req, admins["platform.system_administrator"])
	})
	t.Run("one time credential recovery preserves suspension", func(t *testing.T) {
		role := "platform.system_administrator"
		target := admins[role]
		review(create(PrivilegedChangePlatformIdentitySuspend, target, requester(role)), reviewer(role))
		req := create(PrivilegedChangePlatformCredentialRecovery, target, requester(role))
		approved := review(req, reviewer(role))
		if approved.Status != PrivilegedChangeStatusApproved || approved.RecoveryToken == "" {
			t.Fatalf("missing one time recovery material: status=%s", approved.Status)
		}
		stored, err := service.Get(ctx, req.ID)
		if err != nil || stored.RecoveryToken != "" {
			t.Fatal("read request exposed recovery material")
		}
		enrollment, err := recovery.Begin(ctx, approved.RecoveryToken, audit)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recovery.Begin(ctx, approved.RecoveryToken, audit); err == nil {
			t.Fatal("recovery proof reused")
		}
		code, err := totp.GenerateCode(enrollment.Secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := recovery.Complete(ctx, enrollment.EnrollmentToken, "a-new-long-admin-password", code, audit); err != nil {
			t.Fatal(err)
		}
		if err := recovery.Complete(ctx, enrollment.EnrollmentToken, "another-admin-password", code, audit); err == nil {
			t.Fatal("enrollment reused")
		}
		p, err := repo.GetPrincipal(ctx, target)
		if err != nil || p.Status != PrincipalStatusSuspended {
			t.Fatal("recovery changed principal suspension")
		}
		stored, err = service.Get(ctx, req.ID)
		if err != nil || stored.Status != PrivilegedChangeStatusApplied {
			t.Fatal("credential recovery not applied")
		}
		review(create(PrivilegedChangePlatformIdentityReactivate, target, requester(role)), reviewer(role))
	})
	t.Run("cancel and expire recovery proofs without granting authority", func(t *testing.T) {
		role := "platform.system_administrator"
		target := admins[role]
		req := create(PrivilegedChangePlatformCredentialRecovery, target, requester(role))
		approved := review(req, reviewer(role))
		if _, err := service.Cancel(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: approved.Version, ReviewerPrincipalID: requester(role), Reason: "proof delivery failed", Audit: audit}); err != nil {
			t.Fatal(err)
		}
		if _, err := recovery.Begin(ctx, approved.RecoveryToken, audit); err == nil {
			t.Fatal("cancelled recovery proof accepted")
		}
		req = create(PrivilegedChangePlatformCredentialRecovery, target, requester(role))
		approved = review(req, reviewer(role))
		if err := db.Exec("UPDATE system.administrator_credential_recoveries SET expires_at=statement_timestamp()-interval '1 second' WHERE request_id=?", req.ID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := recovery.Begin(ctx, approved.RecoveryToken, audit); err == nil {
			t.Fatal("expired recovery proof accepted")
		}
		replacement := create(PrivilegedChangePlatformCredentialRecovery, target, requester(role))
		old, err := service.Get(ctx, req.ID)
		if err != nil || old.Status != PrivilegedChangeStatusCancelled {
			t.Fatal("expired approved recovery blocked a new request")
		}
		if _, err := service.Cancel(ctx, ReviewAdministratorChangeInput{RequestID: replacement.ID, Version: replacement.Version, ReviewerPrincipalID: requester(role), Reason: "test cleanup", Audit: audit}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("failed TOTP attempts are bounded and audited without secrets", func(t *testing.T) {
		role := "platform.system_administrator"
		req := create(PrivilegedChangePlatformCredentialRecovery, admins[role], requester(role))
		approved := review(req, reviewer(role))
		enrollment, err := recovery.Begin(ctx, approved.RecoveryToken, audit)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < maxMFAFailedAttempts; i++ {
			code := ""
			for n := 0; n < 10; n++ {
				candidate := fmt.Sprintf("%06d", n)
				if _, valid := matchTOTPCode(enrollment.Secret, candidate, time.Now(), nil); !valid {
					code = candidate
					break
				}
			}
			if err := recovery.Complete(ctx, enrollment.EnrollmentToken, "a-different-long-admin-password", code, audit); !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("bad TOTP result: %v", err)
			}
		}
		code, err := totp.GenerateCode(enrollment.Secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := recovery.Complete(ctx, enrollment.EnrollmentToken, "a-different-long-admin-password", code, audit); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatal("exhausted enrollment accepted")
		}
		var leaked int64
		if err := db.Table("system.audit_logs").Where("details::text LIKE ? OR details::text LIKE ? OR details::text LIKE ?", "%"+approved.RecoveryToken+"%", "%"+enrollment.Secret+"%", "%a-different-long-admin-password%").Count(&leaked).Error; err != nil || leaked != 0 {
			t.Fatal("recovery secret leaked into audit")
		}
		if _, err := service.Cancel(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: approved.Version, ReviewerPrincipalID: requester(role), Reason: "test cleanup", Audit: audit}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("audit failure rolls back approval and target state", func(t *testing.T) {
		role := "platform.system_administrator"
		req := create(PrivilegedChangePlatformIdentitySuspend, admins[role], requester(role))
		badTenant := int64(999999)
		badAudit := audit
		badAudit.TenantID = &badTenant
		if _, err := service.Approve(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: reviewer(role), Reason: "rollback check", Audit: badAudit}); err == nil {
			t.Fatal("approval with rejected audit succeeded")
		}
		unchanged, err := service.Get(ctx, req.ID)
		if err != nil || unchanged.Status != PrivilegedChangeStatusPending || unchanged.Version != req.Version {
			t.Fatal("request transition escaped rollback")
		}
		subject, err := repo.GetPrincipal(ctx, admins[role])
		if err != nil || subject.Status != PrincipalStatusActive {
			t.Fatal("target transition escaped rollback")
		}
		if _, err := service.Cancel(ctx, ReviewAdministratorChangeInput{RequestID: req.ID, Version: req.Version, ReviewerPrincipalID: requester(role), Reason: "test cleanup", Audit: audit}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("atomic role handover", func(t *testing.T) {
		candidate, err := identity.CreateLocalUser(ctx, CreateLocalUserInput{Username: "successor", Password: "successor-password", DisplayName: "Successor", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		createManagementMFACredential(t, ctx, repo, candidate.PrincipalID)
		role := "platform.system_administrator"
		old := admins[role]
		req, err := service.Create(ctx, CreateAdministratorChangeInput{ChangeType: PrivilegedChangePlatformRoleReplace, TargetPrincipalID: old, TargetRoleKey: role, SuccessorPrincipalID: &candidate.PrincipalID, RequestedByPrincipalID: requester(role), Reason: "planned handover", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		result := review(req, reviewer(role))
		if result.Status != PrivilegedChangeStatusApplied {
			t.Fatal("handover not applied")
		}
		if has, err := repo.HasEffectivePlatformRole(ctx, old, time.Now()); err != nil || has {
			t.Fatal("old role holder retained assignment")
		}
		if has, err := repo.HasEffectivePlatformRole(ctx, candidate.PrincipalID, time.Now()); err != nil || !has {
			t.Fatal("successor did not receive role")
		}
	})
}
