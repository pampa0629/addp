package iam

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskAuthorizationSubjectAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System IAM PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	svc, _ := NewTaskAuthorizationSubjectService(repo)
	audit := AuditMetadata{RequestID: stringPointer("transfer-task-authorization")}
	identities := NewIdentityService(repo, nil)
	members := NewTenantMembershipService(repo, testFixtureDatabaseClock(t, db))
	tenant := createContextSelectionTenant(t, ctx, members, "schedule-subject", audit)
	user := createContextSelectionUser(t, ctx, identities, "schedule-user", audit)
	membership := establishContextSelectionMembership(t, ctx, members, tenant.ID, user.PrincipalID, audit)
	role, err := NewTenantRoleService(repo, time.Now).CreateRole(ctx, CreateTenantRoleInput{TenantID: tenant.ID, RoleKey: "tenant.schedule_transfer_test", Name: "Transfer schedule", PermissionKeys: []string{"transfer.task.execute", "system.execution_authorization.create"}, ActorPrincipalID: user.PrincipalID, Audit: audit})
	if err != nil {
		t.Fatal(err)
	}
	insertRoleAssignment(t, db, user.PrincipalID, role.RoleKey, "tenant", &tenant.ID, nil, nil, time.Now().Add(-time.Minute), nil, "manual")
	var principal Principal
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	input := AuthorizeTaskSubjectInput{OwnerModule: "transfer", TaskType: "sync", TaskRef: uuid.New(), DefinitionHash: strings.Repeat("a", 64), TenantID: tenant.ID, PrincipalID: user.PrincipalID, TenantMembershipID: membership.Membership.ID, AuthorizationVersion: principal.AuthorizationVersion, Audit: audit}
	subject, err := svc.Authorize(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-transfer'").Scan(&runtimeID).Error; err != nil {
		t.Fatal(err)
	}
	resolve := ResolveTaskSubjectInput{SubjectID: subject.ID, OwnerModule: "transfer", TaskType: "sync", TaskRef: input.TaskRef, DefinitionHash: input.DefinitionHash, TenantID: tenant.ID, ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", Audit: audit}
	current, err := svc.Resolve(ctx, resolve)
	if err != nil || current.PrincipalID != user.PrincipalID || current.TenantMembershipID != membership.Membership.ID || current.AuthorizationVersion != principal.AuthorizationVersion {
		t.Fatalf("resolve: %#v %v", current, err)
	}
	for _, scenario := range []string{"client", "tenant", "hash", "owner_type", "version", "membership", "principal", "expired_permission"} {
		t.Run(scenario, func(t *testing.T) {
			r := resolve
			switch scenario {
			case "client":
				r.ServiceClientID = "addp-orchestrator"
			case "tenant":
				r.TenantID++
			case "hash":
				r.DefinitionHash = strings.Repeat("b", 64)
			case "owner_type":
				r.TaskType = "orchestration"
			case "version":
				if err := db.Model(&Principal{}).Where("id=?", user.PrincipalID).Update("authorization_version", principal.AuthorizationVersion+1).Error; err != nil {
					t.Fatal(err)
				}
				defer db.Model(&Principal{}).Where("id=?", user.PrincipalID).Update("authorization_version", principal.AuthorizationVersion)
			case "membership":
				if err := db.Model(&TenantMembership{}).Where("id=?", membership.Membership.ID).Update("status", "suspended").Error; err != nil {
					t.Fatal(err)
				}
				defer db.Model(&TenantMembership{}).Where("id=?", membership.Membership.ID).Update("status", "active")
			case "principal":
				if err := db.Model(&Principal{}).Where("id=?", user.PrincipalID).Update("status", "suspended").Error; err != nil {
					t.Fatal(err)
				}
				defer db.Model(&Principal{}).Where("id=?", user.PrincipalID).Update("status", "active")
			case "expired_permission":
				expiringUser := createContextSelectionUser(t, ctx, identities, "expiring-schedule-user", audit)
				expiringMembership := establishContextSelectionMembership(t, ctx, members, tenant.ID, expiringUser.PrincipalID, audit)
				now, err := repo.CurrentDatabaseTime(ctx)
				if err != nil {
					t.Fatal(err)
				}
				expires := now.Add(2 * time.Second)
				insertRoleAssignment(t, db, expiringUser.PrincipalID, role.RoleKey, "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), &expires, "manual")
				var actor Principal
				if err := db.First(&actor, expiringUser.PrincipalID).Error; err != nil {
					t.Fatal(err)
				}
				expiringInput := input
				expiringInput.TaskRef = uuid.New()
				expiringInput.PrincipalID = actor.ID
				expiringInput.TenantMembershipID = expiringMembership.Membership.ID
				expiringInput.AuthorizationVersion = actor.AuthorizationVersion
				expiring, err := svc.Authorize(ctx, expiringInput)
				if err != nil {
					t.Fatal(err)
				}
				r.SubjectID = expiring.ID
				r.TaskRef = expiring.TaskRef
				for {
					now, err = repo.CurrentDatabaseTime(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if !now.Before(expires) {
						break
					}
					time.Sleep(25 * time.Millisecond)
				}

			}
			if _, err := svc.Resolve(ctx, r); err == nil {
				t.Fatal("invalid current task authorization resolved")
			}
		})
	}
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	input.AuthorizationVersion = principal.AuthorizationVersion
	// A user with only Transfer execute cannot authorize an orchestration.
	other := input
	other.OwnerModule = "orchestrator"
	other.TaskType = "orchestration"
	other.TaskRef = uuid.New()
	if _, err := svc.Authorize(ctx, other); err == nil {
		t.Fatal("Transfer execution permission authorized Orchestrator")
	}
	// Owner/type constraints are enforced by PostgreSQL as well as the service.
	invalid := TaskAuthorizationSubject{OwnerModule: "transfer", TaskType: "orchestration", TaskRef: uuid.New(), DefinitionHash: input.DefinitionHash, TenantID: tenant.ID, PrincipalID: user.PrincipalID, TenantMembershipID: membership.Membership.ID, AuthorizationVersion: principal.AuthorizationVersion, AuthorizedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("database admitted unsupported owner/type pair")
	}
	// Reauthorization binds the updated definition; the old hash cannot resolve.
	input.DefinitionHash = strings.Repeat("c", 64)
	newer, err := svc.Authorize(ctx, input)
	if err != nil || newer.ID != subject.ID {
		t.Fatalf("replace subject: %#v %v", newer, err)
	}
	if _, err := svc.Resolve(ctx, resolve); err == nil {
		t.Fatal("old definition hash still resolved")
	}
	resolve.DefinitionHash = input.DefinitionHash
	if _, err := svc.Resolve(ctx, resolve); err != nil {
		t.Fatal(err)
	}
	var denies int64
	if err := db.Model(&AuditLog{}).Where("event_name=?", "iam.task_authorization.resolve_denied").Count(&denies).Error; err != nil || denies < 6 {
		t.Fatalf("missing durable denial audits: %d %v", denies, err)
	}
	var grants int64
	if err := db.Table("system.role_permissions rp").Joins("JOIN system.permissions p ON p.id=rp.permission_id").Joins("JOIN system.roles r ON r.id=rp.role_id").Where("p.permission_key=? AND r.role_key=?", "system.task_authorization.execute", "tenant.transfer_runtime").Count(&grants).Error; err != nil || grants != 1 {
		t.Fatalf("runtime resolve permission=%d %v", grants, err)
	}
}
