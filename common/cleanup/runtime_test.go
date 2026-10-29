package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type runtimeToken struct{}

func (runtimeToken) Token(context.Context, uint) (string, error) { return "fixture-runtime-token", nil }
func (runtimeToken) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform authority prohibited")
}

type runtimeFixture struct {
	runner      *Runner
	parent      execution.TaskExecution
	child       execution.TaskExecution
	parentLease execution.Lease
	denyClaim   bool
	denyEffect  bool
	unavailable bool
	effectTTL   time.Duration
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateResults(db, "meta"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE meta.effect_probe (id integer)").Error; err != nil {
		t.Fatal(err)
	}
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: models.JSONMap{execution.DisplayLanguageKey: "en"}}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.cleanup.execute"}))
	if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "scan", TriggerType: "manual", ExpectedModules: []string{"meta"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(ctx, db, &parent, input)
	if err != nil {
		t.Fatal(err)
	}
	child := children[0]
	if err := execution.AttachPendingAuthorization(ctx, db, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: 7, Module: "meta", TaskType: "cleanup_executor", Source: "system"}, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
		t.Fatal(err)
	}
	_, pl, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &runtimeFixture{parent: parent, child: child, parentLease: *pl, effectTTL: time.Minute}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-accesses" || r.Header.Get("Authorization") != "Bearer fixture-runtime-token" {
			t.Error("wrong runtime request")
			w.WriteHeader(403)
			return
		}
		var request client.CleanupAccessRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if request.ExecutionID != child.ExecutionID || request.Scope.Module != "meta" {
			t.Error("wrong authority binding")
			w.WriteHeader(403)
			return
		}
		if f.unavailable {
			w.WriteHeader(503)
			return
		}
		if request.Stage == "claim" && f.denyClaim || request.Stage == "effect" && f.denyEffect {
			w.WriteHeader(403)
			return
		}
		if request.Stage == "effect" && (request.Attempt != 1 || request.LeaseToken == "") {
			t.Error("unbound effect")
			w.WriteHeader(403)
			return
		}
		ttl := time.Minute
		if request.Stage == "effect" {
			ttl = f.effectTTL
		}
		_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: request.ExecutionID, TenantID: "7", Stage: request.Stage, Attempt: request.Attempt, Scope: request.Scope, ExpiresAt: time.Now().Add(ttl)})
	}))
	t.Cleanup(server.Close)
	f.runner = &Runner{DB: db, Owner: "meta", System: client.NewSystemServiceClient(server.URL, runtimeToken{}, server.Client())}
	return f
}

func (f *runtimeFixture) readChild(t *testing.T) execution.TaskExecution {
	t.Helper()
	var e execution.TaskExecution
	if err := f.runner.DB.Where("execution_id=?", f.child.ExecutionID).First(&e).Error; err != nil {
		t.Fatal(err)
	}
	return e
}
func completedFixtureResult(t *testing.T, run *Run) execution.CleanupReceipt {
	t.Helper()
	impact, err := events.BuildCleanupImpactData(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	value := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: 7, Module: "meta", Action: "scan", Status: "success", Summary: events.CleanupResultSummary{RiskLevel: "low"}, Impact: &impact}
	receipt, err := execution.NewCleanupReceipt(run.Execution, value, "")
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestOwnerCannotRunWithoutAuthorityOrAfterParentClosure(t *testing.T) {
	f := newRuntimeFixture(t)
	f.denyClaim = true
	f.runner.Handle = func(context.Context, *Run) error { t.Fatal("unauthorized Owner ran"); return nil }
	if err := f.runner.step(t.Context(), "worker"); err != nil {
		t.Fatal(err)
	}
	if got := f.readChild(t); got.Status != "pending" || got.Attempt != 0 || got.StartedAt != nil {
		t.Fatal("denied claim acquired running facts")
	}
	if err := execution.CompleteWithLease(t.Context(), f.runner.DB, f.parentLease, "timeout", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.runner.step(t.Context(), "worker"); err != nil {
		t.Fatal(err)
	}
	if got := f.readChild(t); got.Status != "failed" || got.Attempt != 0 || got.StartedAt != nil {
		t.Fatal("closed child was not rejected unstarted")
	}
}

func TestOwnerEffectAndResultAreOneFencedTransaction(t *testing.T) {
	for _, mode := range []string{"valid", "denied", "expiry", "receipt_failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			if mode == "denied" {
				f.denyEffect = true
			}
			if mode == "expiry" {
				f.effectTTL = 50 * time.Millisecond
			}
			f.runner.Handle = func(ctx context.Context, run *Run) error {
				receipt := completedFixtureResult(t, run)
				if mode == "receipt_failure" {
					receipt.Attempt++
				}
				return run.Effect(ctx, func(tx *gorm.DB) error {
					if err := tx.Exec("INSERT INTO meta.effect_probe VALUES (1)").Error; err != nil {
						return err
					}
					if mode == "expiry" {
						time.Sleep(70 * time.Millisecond)
					}
					return Complete(ctx, tx, run.Lease, receipt, map[string]int64{"invalid_engines.count": 7})
				})
			}
			err := f.runner.step(t.Context(), "worker")
			if mode == "valid" && err != nil {
				t.Fatal(err)
			}
			if mode != "valid" && err == nil {
				t.Fatal("invalid effect passed")
			}
			var effects, results int64
			f.runner.DB.Table("meta.effect_probe").Count(&effects)
			f.runner.DB.Table("meta.cleanup_results").Count(&results)
			child := f.readChild(t)
			receipt, readErr := execution.ReadCleanupReceipt(child)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "valid" {
				if effects != 1 || results != 1 || receipt == nil || child.Status != "success" {
					t.Fatal("valid commit incomplete")
				}
			} else {
				if effects != 0 || results != 0 || receipt != nil || child.Status != "failed" {
					t.Fatal("failed transaction left effects or invented result")
				}
			}
		})
	}
}

func TestProfessionalResultRequiresBothPermissionsAndOwnTenantExecution(t *testing.T) {
	f := newRuntimeFixture(t)
	f.runner.Handle = func(ctx context.Context, run *Run) error {
		return run.Effect(ctx, func(tx *gorm.DB) error {
			return Complete(ctx, tx, run.Lease, completedFixtureResult(t, run), map[string]int64{"invalid_engines.count": 7})
		})
	}
	if err := f.runner.step(t.Context(), "worker"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, tenant, actor, token string
		permissions                []string
		id                         string
		want                       int
	}{
		{"both", "7", "7", "first_party_access_token", []string{"system.cleanup.read", "meta.cleanup_result.read"}, f.child.ExecutionID, 200},
		{"public_only", "7", "7", "first_party_access_token", []string{"system.cleanup.read"}, f.child.ExecutionID, 403},
		{"professional_only", "7", "7", "first_party_access_token", []string{"meta.cleanup_result.read"}, f.child.ExecutionID, 404},
		{"other_actor", "7", "8", "first_party_access_token", []string{"system.cleanup.read", "meta.cleanup_result.read"}, f.child.ExecutionID, 404},
		{"other_tenant", "8", "7", "first_party_access_token", []string{"system.cleanup.read", "meta.cleanup_result.read"}, f.child.ExecutionID, 404},
		{"parent", "7", "7", "first_party_access_token", []string{"system.cleanup.read", "meta.cleanup_result.read"}, f.parent.ExecutionID, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.Use(func(c *gin.Context) {
				facts := authtest.NewTenantUserAuthContext(test.tenant, test.actor, test.permissions)
				facts.Token.Type = test.token
				if err := commonauth.SetAuthContextForGin(c, facts); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			router.GET("/cleanup-executions/:execution_id/result", ReadResultHandler(f.runner.DB, "meta"))
			r := httptest.NewRequest("GET", "/cleanup-executions/"+test.id+"/result", nil)
			r.Header.Set("Accept-Language", "en")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d want %d %s", w.Code, test.want, w.Body.String())
			}
			for _, key := range []string{"lease_token", "request_digest", "actor_principal_id", "authorization_id", "execution_config"} {
				if strings.Contains(w.Body.String(), key) {
					t.Fatalf("private %s leaked", key)
				}
			}
		})
	}
}

func TestAuthorizationDiagnosticsDoNotInventReceiptOrRuntime(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.denyClaim = !unavailable
			f.unavailable = unavailable
			f.runner.Handle = func(context.Context, *Run) error { t.Fatal("unclaimed runtime"); return nil }
			if err := f.runner.step(t.Context(), "worker"); err != nil {
				t.Fatal(err)
			}
			code := "cleanup_authorization_denied"
			if unavailable {
				code = "cleanup_authorization_unavailable"
			}
			child := f.readChild(t)
			if child.ErrorDetails[code] == nil || child.StartedAt != nil || child.Attempt != 0 || child.Status != "pending" {
				t.Fatal("missing safe pending fact")
			}
			if err := execution.CompleteWithLease(t.Context(), f.runner.DB, f.parentLease, "timeout", nil); err != nil {
				t.Fatal(err)
			}
			if err := f.runner.step(t.Context(), "worker"); err != nil {
				t.Fatal(err)
			}
			child = f.readChild(t)
			if child.ErrorDetails[code] == nil || child.StartedAt != nil || child.Attempt != 0 {
				t.Fatal("closure lost authority fact or forged start")
			}
		})
	}
	for _, kind := range []string{"denied", "unavailable", "domain"} {
		t.Run(kind, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.runner.Handle = func(ctx context.Context, run *Run) error {
				if kind == "domain" {
					return errors.New("private domain text")
				}
				f.denyEffect = kind == "denied"
				f.unavailable = kind == "unavailable"
				return run.Effect(ctx, func(*gorm.DB) error { t.Fatal("unauthorized effect"); return nil })
			}
			if err := f.runner.step(t.Context(), "worker"); err == nil {
				t.Fatal("failure disappeared")
			}
			child := f.readChild(t)
			want := "cleanup_owner_stopped_without_receipt"
			if kind != "domain" {
				want = "cleanup_authorization_" + kind
			}
			if child.Status != "failed" || child.ErrorDetails["code"] != want {
				t.Fatal("cause flattened", child.ErrorDetails)
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil || receipt != nil {
				t.Fatal("invented result")
			}
		})
	}
}

func TestPrepareExternalCommitsBeforeProviderAndBoundsDecision(t *testing.T) {
	for _, mode := range []string{"valid", "denied", "expired_in_prepare", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.effectTTL = 120 * time.Millisecond
			f.denyEffect = mode == "denied"
			called := false
			f.runner.Handle = func(ctx context.Context, run *Run) error {
				effectCtx, cancel, err := run.PrepareExternal(ctx, func(tx *gorm.DB) error {
					if err := tx.Exec("INSERT INTO meta.effect_probe VALUES (1)").Error; err != nil {
						return err
					}
					if mode == "expired_in_prepare" {
						time.Sleep(150 * time.Millisecond)
					}
					if mode == "rollback" {
						return errors.New("registration rejected")
					}
					return nil
				})
				if err != nil {
					return err
				}
				defer cancel()
				called = true
				// One connection: a held PG transaction would prevent this provider-side
				// observation from completing before the decision deadline.
				var count int64
				if err := f.runner.DB.WithContext(effectCtx).Table("meta.effect_probe").Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("registration not committed before call: %d %v", count, err)
				}
				deadline, ok := effectCtx.Deadline()
				if !ok || time.Until(deadline) > 120*time.Millisecond {
					t.Fatal("external call outlives decision")
				}
				<-effectCtx.Done()
				if !errors.Is(effectCtx.Err(), context.DeadlineExceeded) {
					t.Fatal(effectCtx.Err())
				}
				return effectCtx.Err()
			}
			if err := f.runner.step(t.Context(), "worker"); err == nil {
				t.Fatal("external uncertainty must not succeed")
			}
			var count int64
			f.runner.DB.Table("meta.effect_probe").Count(&count)
			if mode == "valid" {
				if !called || count != 1 {
					t.Fatal("committed operation lost")
				}
			} else if called || count != 0 {
				t.Fatal("rejected registration dispatched or committed")
			}
			child := f.readChild(t)
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil || receipt != nil || child.Status != "failed" {
				t.Fatal("external uncertainty invented completion", err)
			}
		})
	}
}
