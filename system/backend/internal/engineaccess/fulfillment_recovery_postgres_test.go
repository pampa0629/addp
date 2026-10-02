package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func exerciseFulfillmentRuntimeRecovery(t *testing.T, db *gorm.DB, request fulfillmentRequest) {
	t.Run("production runtime recovery service", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		hash, err := bcrypt.GenerateFromPassword([]byte(strings.Repeat("recovery-fixture-", 3)), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id = 'addp-catalog'").Updates(map[string]any{"status": "active", "client_secret_hash": string(hash)}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id = 'addp-catalog'").Update("status", "disabled")
		var principal iam.Principal
		var member iam.TenantMembership
		if err := db.First(&principal, request.CallerPrincipalID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id = ? AND principal_id = ?", request.TenantID, principal.ID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		actor := FulfillmentRuntimeActor{Actor: Actor{TenantID: request.TenantID, PrincipalID: principal.ID, MembershipID: member.ID, AuthorizationVersion: principal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-catalog"}
		binding := shared.SharingFulfillmentBinding{CallerPrincipalID: request.CallerPrincipalID,
			Operator: shared.SharingFulfillmentOperator{PrincipalID: request.Operator.PrincipalID, MembershipID: request.Operator.MembershipID, AuthorizationVersion: request.Operator.AuthorizationVersion},
			Path:     request.Path, DecisionID: request.DecisionID, RequirementVersion: request.RequirementVersion,
			RecipientType: request.RecipientType, RecipientID: request.RecipientID, Action: request.Action, ExpiryMode: request.ExpiryMode, ExpiresAt: request.ExpiresAt}
		service := NewService(NewRepository(db), nil)
		lookup, err := service.ResolveFulfillment(ctx, actor, request.RequestID, binding)
		if err != nil || lookup.Found || lookup.Resolution != nil {
			t.Fatalf("lookup=%+v err=%v", lookup, err)
		}
		var count int64
		if err := db.Model(&fulfillmentOutcome{}).Where("request_id=?", request.RequestID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("lookup wrote outcome: %d %v", count, err)
		}
		// Original identities/expiry need not be current: closing is not approval.
		binding.Operator.PrincipalID = 9007199254740993
		binding.RecipientID = 9007199254740994
		past := time.Now().Add(-time.Hour)
		binding.ExpiresAt = &past
		closed, err := service.CloseFulfillment(ctx, actor, request.RequestID, binding)
		if err != nil || closed.Outcome != "closed" || closed.Deadline != nil {
			t.Fatalf("close=%+v err=%v", closed, err)
		}
		again, err := service.CloseFulfillment(ctx, actor, request.RequestID, binding)
		if err != nil || !again.RecordedAt.Equal(closed.RecordedAt) {
			t.Fatalf("repeat=%+v err=%v", again, err)
		}
		lookup, err = service.ResolveFulfillment(ctx, actor, request.RequestID, binding)
		if err != nil || !lookup.Found || lookup.Resolution.Outcome != "closed" {
			t.Fatalf("history=%+v err=%v", lookup, err)
		}
		changed := binding
		changed.RequirementVersion++
		if _, err := service.ResolveFulfillment(ctx, actor, request.RequestID, changed); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("binding conflict=%v", err)
		}
		for _, change := range []func(*FulfillmentRuntimeActor){
			func(a *FulfillmentRuntimeActor) { a.ClientID = "addp-meta" }, func(a *FulfillmentRuntimeActor) { a.AuthorizationVersion++ },
			func(a *FulfillmentRuntimeActor) { a.MembershipID++ }, func(a *FulfillmentRuntimeActor) { a.TenantID++ },
			func(a *FulfillmentRuntimeActor) { a.TokenExpiresAt = time.Now().Add(-time.Second) },
		} {
			bad := actor
			change(&bad)
			if _, err := service.CloseFulfillment(ctx, bad, uuid.New(), binding); err == nil {
				t.Fatalf("invalid service accepted: %+v", bad)
			}
			if _, err := service.ResolveFulfillment(ctx, bad, request.RequestID, changed); err == nil || errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("invalid service learned a historical binding conflict: %v", err)
			}
		}
		if err := db.Table("system.audit_logs").Where("entity_type='engine_access_fulfillment' AND entity_id=?", request.RequestID.String()).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("audit=%d err=%v", count, err)
		}
		t.Run("token expiry during target wait rolls back closure", func(t *testing.T) {
			locked := db.Begin()
			if locked.Error != nil {
				t.Fatal(locked.Error)
			}
			defer locked.Rollback()
			path, err := shared.EncodeSharingTarget(binding.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewRepository(locked).lockFulfillmentTarget(ctx, actor.TenantID, path); err != nil {
				t.Fatal(err)
			}
			var now time.Time
			if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				t.Fatal(err)
			}
			waiting := actor
			waiting.TokenExpiresAt = now.Add(time.Second)
			id := uuid.New()
			done := make(chan error, 1)
			go func() {
				_, err := service.CloseFulfillment(ctx, waiting, id, binding)
				done <- err
			}()
			blocked := false
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE NOT granted AND locktype='advisory'
					AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&blocked).Error; err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("runtime closure did not wait on target arbitration")
			}
			if err := db.Exec("SELECT pg_sleep(1.1)").Error; err != nil {
				t.Fatal(err)
			}
			if err := locked.Commit().Error; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, commonapi.ErrUnauthorized) {
					t.Fatalf("expired waiting token=%v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := db.Model(&fulfillmentOutcome{}).Where("request_id=?", id).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("expired token left closure=%d err=%v", count, err)
			}
			if err := db.Table("system.audit_logs").Where("entity_type='engine_access_fulfillment' AND entity_id=?", id.String()).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("expired token left audit=%d err=%v", count, err)
			}
		})
	})
}
