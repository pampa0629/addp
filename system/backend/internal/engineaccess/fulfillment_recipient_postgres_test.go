package engineaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The real lifecycle service must wait until the acceptance transaction has
// committed. An independent actor ensures organization cases contend on the
// recipient organization, not incidentally on the operator's account.
func assertQualificationMutationWaitsForAcceptance(t *testing.T, db *gorm.DB, request fulfillmentRequest, confirmation userProvenance, write func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	ready, resume, pidCh := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	var release, once sync.Once
	name := "recipient_lifecycle_" + strings.ReplaceAll(t.Name(), "/", "_")
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(qualificationRaceContextKey{}) == "recipient_writer" {
			once.Do(func() {
				var pid int
				if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
					tx.AddError(err)
					return
				}
				pidCh <- pid
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	acceptDone, writeDone := make(chan error, 1), make(chan error, 1)
	acceptJoined, writeStarted, writeJoined := false, false, false
	defer func() {
		cancel()
		release.Do(func() { close(resume) })
		for _, pending := range []struct {
			started, joined bool
			done            <-chan error
		}{{true, acceptJoined, acceptDone}, {writeStarted, writeJoined, writeDone}} {
			if pending.started && !pending.joined {
				select {
				case <-pending.done:
				case <-time.After(2 * time.Second):
					t.Error("recipient race worker did not stop")
				}
			}
		}
	}()
	go func() {
		acceptDone <- NewRepository(db).transaction(ctx, func(tx *Repository) error {
			_, err := tx.settleFulfillment(ctx, request, true, confirmation, func(*Repository) error {
				close(ready)
				select {
				case <-resume:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			return err
		})
	}()
	select {
	case <-ready:
	case err := <-acceptDone:
		acceptJoined = true
		t.Fatalf("acceptance did not reach verification: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	writeStarted = true
	go func() { writeDone <- write(context.WithValue(ctx, qualificationRaceContextKey{}, "recipient_writer")) }()
	var pid int
	select {
	case pid = <-pidCh:
	case err := <-writeDone:
		writeJoined = true
		t.Fatalf("lifecycle writer did not start: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		select {
		case err := <-writeDone:
			writeJoined = true
			t.Fatalf("recipient lifecycle changed before acceptance committed: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
		var waiting bool
		if err := db.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid = ? AND NOT granted AND locktype IN ('transactionid', 'tuple'))", pid).Scan(&waiting).Error; err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	release.Do(func() { close(resume) })
	if err := <-acceptDone; err != nil {
		t.Errorf("acceptance during lifecycle mutation: %v", err)
	}
	acceptJoined = true
	if err := <-writeDone; err != nil {
		t.Errorf("lifecycle after acceptance: %v", err)
	}
	writeJoined = true
	if result, err := NewRepository(db).readFulfillment(ctx, request); err != nil || result.Outcome != "accepted" {
		t.Fatalf("later invalidation lost committed outcome: %+v %v", result, err)
	}
}

func exerciseFulfillmentRecipients(t *testing.T, db *gorm.DB, identity *iam.Repository,
	newRequest func() fulfillmentRequest, newUser func(*testing.T, time.Duration) (userProvenance, time.Time),
	seedDelegation func(*testing.T, int64, time.Time) *Delegation,
	confirmation userProvenance,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	base := newRequest()
	repo := NewRepository(db)
	settle := func(request fulfillmentRequest, accept bool, verify func(*Repository) error) (*fulfillmentOutcome, error) {
		var outcome *fulfillmentOutcome
		err := repo.transaction(ctx, func(tx *Repository) error {
			var err error
			outcome, err = tx.settleFulfillment(ctx, request, accept, confirmation, verify)
			return err
		})
		return outcome, err
	}
	verified := func(*Repository) error { return nil }
	t.Run("lower recipient account precedes operator during real role write", func(t *testing.T) {
		receiver, _ := newUser(t, time.Hour)
		operator, membershipExpiry := newUser(t, time.Hour)
		seedDelegation(t, operator.MembershipID, membershipExpiry.Add(-time.Second))
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: base.TenantID,
			RoleKey: "custom.recipient_lock_order", Name: "Recipient lock order", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		request := newRequest()
		request.Operator, request.RecipientID = operator, receiver.PrincipalID
		assertIAMQualificationRace(t, db, base.TenantID, receiver, func(writeCtx context.Context) error {
			_, err := roles.CreateAssignments(writeCtx, iam.CreateTenantRoleAssignmentsInput{TenantID: base.TenantID,
				MembershipID: receiver.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant",
				ActorPrincipalID: operator.PrincipalID, Reason: "Concurrency fixture"})
			return err
		}, func(readCtx context.Context, tx *gorm.DB) error {
			_, err := NewRepository(tx).settleFulfillment(readCtx, request, true, confirmation, verified)
			return err
		})
	})
	reject := func(t *testing.T, request fulfillmentRequest, verify func(*Repository) error) {
		t.Helper()
		if _, err := settle(request, true, verify); err == nil {
			t.Fatal("invalid recipient produced acceptance")
		}
		for _, table := range []string{"system.engine_access_fulfillment_outcomes", "system.audit_logs"} {
			field, id := "request_id", any(request.RequestID)
			if table == "system.audit_logs" {
				field, id = "entity_id", request.RequestID.String()
			}
			var count int64
			if err := db.Table(table).Where(field+" = ?", id).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rejected acceptance left %s count=%d err=%v", table, count, err)
			}
		}
		if _, err := repo.readFulfillment(ctx, request); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("failed recipient became a durable outcome: %v", err)
		}
		// Invalid recipients must not prevent releasing pending business facts.
		closed, err := settle(request, false, nil)
		if err != nil || closed.Outcome != "closed" {
			t.Fatalf("invalid recipient stranded close: %+v %v", closed, err)
		}
	}
	newGlobal := func() *iam.Principal {
		p := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
		if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
			if err := tx.CreatePrincipal(ctx, p); err != nil {
				return err
			}
			return tx.CreateUser(ctx, &iam.User{ID: p.ID, DisplayName: "Recipient fixture"})
		}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	foreignUser := newGlobal()
	foreign, err := iam.NewPlatformTenantService(identity, time.Now).Create(ctx, iam.CreateTenantInput{
		Code: "recipient_foreign", Name: "Recipient foreign", InitialAdministratorPrincipalID: foreignUser.ID,
		ActorPrincipalID: base.Operator.PrincipalID})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		id    int64
		state string
	}{
		{"missing account", 9000000000, ""},
		{"machine identity", base.CallerPrincipalID, ""},
		{"no tenant membership", newGlobal().ID, ""},
		{"foreign tenant user", foreignUser.ID, ""},
		{"suspended account", 0, "principal"},
		{"suspended membership", 0, "member"},
		{"ended membership", 0, "ended"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := newRequest()
			request.RecipientID = test.id
			if test.state != "" {
				source, _ := newUser(t, time.Hour)
				request.RecipientID = source.PrincipalID
				table, id, status := "system.tenant_memberships", source.MembershipID, "suspended"
				if test.state == "principal" {
					table, id = "system.principals", source.PrincipalID
				} else if test.state == "ended" {
					_, err := iam.NewTenantMembershipService(identity, time.Now).EndMembership(ctx,
						iam.ChangeTenantMembershipInput{TenantID: base.TenantID, PrincipalID: source.PrincipalID, Reason: "Fixture only"})
					if err != nil {
						t.Fatal(err)
					}
					reject(t, request, verified)
					return
				}
				if err := db.Table(table).Where("id = ?", id).Update("status", status).Error; err != nil {
					t.Fatal(err)
				}
			}
			reject(t, request, verified)
		})
	}
	t.Run("recipient locks coordinate real IAM lifecycle writes", func(t *testing.T) {
		for _, kind := range []string{"user", "department", "project_group"} {
			t.Run(kind, func(t *testing.T) {
				request := newRequest()
				request.RecipientType = kind
				actor, _ := newUser(t, time.Hour)
				organizations := iam.NewOrganizationService(identity, time.Now)
				var write func(context.Context) error
				switch kind {
				case "user":
					source, _ := newUser(t, time.Hour)
					request.RecipientID = source.PrincipalID
					write = func(writeCtx context.Context) error {
						principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
						_, err := iam.NewTenantMembershipService(identity, time.Now).SuspendMembership(writeCtx,
							iam.ChangeTenantMembershipInput{TenantID: base.TenantID, PrincipalID: source.PrincipalID,
								Reason: "Concurrency fixture", Audit: iam.AuditMetadata{PrincipalID: &actor.PrincipalID,
									PrincipalType: &principalType, ContextType: &contextType, TenantID: &base.TenantID}})
						return err
					}
				case "department":
					row, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: base.TenantID,
						ActorPrincipalID: actor.PrincipalID, Code: "recipient_race_department", Name: "Recipient race"})
					if err != nil {
						t.Fatal(err)
					}
					request.RecipientID = row.ID
					write = func(writeCtx context.Context) error {
						_, err := organizations.DisableDepartment(writeCtx, iam.ChangeDepartmentStatusInput{TenantID: base.TenantID,
							DepartmentID: row.ID, Version: row.Version, ActorPrincipalID: actor.PrincipalID, Reason: "Concurrency fixture"})
						return err
					}
				case "project_group":
					row, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: base.TenantID,
						ActorPrincipalID: actor.PrincipalID, Code: "recipient_race_group", Name: "Recipient race"})
					if err != nil {
						t.Fatal(err)
					}
					request.RecipientID = row.ID
					write = func(writeCtx context.Context) error {
						_, err := organizations.CloseProjectGroup(writeCtx, iam.CloseProjectGroupInput{TenantID: base.TenantID,
							ProjectGroupID: row.ID, Version: row.Version, ActorPrincipalID: actor.PrincipalID, Reason: "Concurrency fixture"})
						return err
					}
				}
				assertQualificationMutationWaitsForAcceptance(t, db, request, confirmation, write)
				request.RequestID = uuid.New()
				reject(t, request, verified)
			})
		}
	})
	for _, kind := range []string{"department", "project_group"} {
		for _, state := range []string{"active", "missing", "foreign", "unavailable"} {
			t.Run(kind+" "+state, func(t *testing.T) {
				request := newRequest()
				request.RecipientType = kind
				tenantID := base.TenantID
				if state == "foreign" {
					tenantID = foreign.ID
				}
				organizations := iam.NewOrganizationService(identity, time.Now)
				code := fmt.Sprintf("recipient_%s_%s", kind, state)
				var invalidate func() error
				if kind == "department" {
					row, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID,
						Code: code, Name: code, ActorPrincipalID: base.Operator.PrincipalID})
					if err != nil {
						t.Fatal(err)
					}
					request.RecipientID = row.ID
					invalidate = func() error {
						_, err := organizations.DisableDepartment(ctx, iam.ChangeDepartmentStatusInput{TenantID: tenantID,
							DepartmentID: row.ID, Version: row.Version, ActorPrincipalID: base.Operator.PrincipalID, Reason: "Fixture only"})
						return err
					}
				} else {
					row, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID,
						Code: code, Name: code, ActorPrincipalID: base.Operator.PrincipalID})
					if err != nil {
						t.Fatal(err)
					}
					request.RecipientID = row.ID
					invalidate = func() error {
						_, err := organizations.CloseProjectGroup(ctx, iam.CloseProjectGroupInput{TenantID: tenantID,
							ProjectGroupID: row.ID, Version: row.Version, ActorPrincipalID: base.Operator.PrincipalID, Reason: "Fixture only"})
						return err
					}
				}
				if state == "active" {
					original, err := settle(request, true, verified)
					if err != nil || original.Outcome != "accepted" {
						t.Fatalf("empty active organization rejected: %+v %v", original, err)
					}
					if err := invalidate(); err != nil {
						t.Fatal(err)
					}
					recovered, err := settle(request, true, nil)
					if err != nil || !recovered.RecordedAt.Equal(original.RecordedAt) {
						t.Fatalf("organization invalidation stranded historical recovery: %+v %v", recovered, err)
					}
					request.RequestID = uuid.New()
				} else if state == "missing" {
					request.RecipientID = 9000000000
				} else if state == "unavailable" {
					if err := invalidate(); err != nil {
						t.Fatal(err)
					}
				}
				reject(t, request, verified)
			})
		}
	}
	t.Run("member expiry during business verification", func(t *testing.T) {
		source, expires := newUser(t, time.Second)
		request := newRequest()
		request.RecipientID = source.PrincipalID
		reject(t, request, func(tx *Repository) error {
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			time.Sleep(expires.Sub(now) + 30*time.Millisecond)
			return nil
		})
	})
	t.Run("user versions do not become a recipient snapshot", func(t *testing.T) {
		source, _ := newUser(t, time.Hour)
		request := newRequest()
		request.RecipientID = source.PrincipalID
		if err := db.Table("system.principals").Where("id = ?", source.PrincipalID).
			Update("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
			t.Fatal(err)
		}
		original, err := settle(request, true, verified)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.principals").Where("id = ?", source.PrincipalID).Update("status", "suspended").Error; err != nil {
			t.Fatal(err)
		}
		read, err := repo.readFulfillment(ctx, request)
		if err != nil || !read.RecordedAt.Equal(original.RecordedAt) {
			t.Fatalf("user invalidation stranded read: %+v %v", read, err)
		}
		recovered, err := settle(request, true, nil)
		if err != nil || !recovered.RecordedAt.Equal(original.RecordedAt) {
			t.Fatalf("user invalidation stranded arbitration retry: %+v %v", recovered, err)
		}
		request.RequestID = uuid.New()
		reject(t, request, verified)
	})
}
