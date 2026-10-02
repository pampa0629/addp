package engineaccess

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

type qualificationRaceContextKey struct{}

// Exercise real IAM services and the same qualification repository consumed by
// fulfillment. Hooks only arrange the interleaving; they do not replace SQL or
// authority checks. A writer must wait on the principal before holding a lower
// tenant/organization lock that the qualification reader will need.
func assertIAMQualificationRace(t *testing.T, db *gorm.DB, tenantID int64, source userProvenance,
	write func(context.Context) error,
	firstWrite ...func(context.Context, *gorm.DB) error,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	readerHeld, writerPID, lowerHeld := make(chan struct{}, 1), make(chan int, 1), make(chan struct{}, 1)
	resume := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(resume) })
	var readerOnce, writerOnce, lowerOnce sync.Once
	name := "qualification_race_" + strings.ReplaceAll(t.Name(), "/", "_")
	if err := db.Callback().Query().Before("gorm:query").Register(name+"_pid", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(qualificationRaceContextKey{}) == "writer" {
			writerOnce.Do(func() {
				var pid int
				if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
					tx.AddError(err)
					return
				}
				writerPID <- pid
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register(name+"_pause", func(tx *gorm.DB) {
		if tx.Error != nil {
			return
		}
		sql := tx.Statement.SQL.String()
		principal, isPrincipal := tx.Statement.Dest.(*iam.Principal)
		if tx.Statement.Context.Value(qualificationRaceContextKey{}) == "reader" && tx.Statement.Table == "principals" &&
			isPrincipal && principal.ID == source.PrincipalID &&
			(strings.Contains(sql, "FOR SHARE") || (len(firstWrite) > 0 && strings.Contains(sql, "FOR UPDATE"))) {
			readerOnce.Do(func() {
				readerHeld <- struct{}{}
				select {
				case <-resume:
				case <-ctx.Done():
				}
			})
		}
		if tx.Statement.Context.Value(qualificationRaceContextKey{}) == "writer" &&
			(tx.Statement.Table == "tenants" || tx.Statement.Table == "departments" || tx.Statement.Table == "project_groups") && strings.Contains(sql, "FOR UPDATE") {
			lowerOnce.Do(func() {
				lowerHeld <- struct{}{}
				select {
				case <-resume:
				case <-ctx.Done():
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name + "_pid")
	defer db.Callback().Query().Remove(name + "_pause")
	readerDone, writerDone := make(chan error, 1), make(chan error, 1)
	readerStarted, writerStarted, readerJoined, writerJoined := false, false, false, false
	defer func() {
		cancel()
		release.Do(func() { close(resume) })
		for _, pending := range []struct {
			started, joined bool
			done            <-chan error
		}{{readerStarted, readerJoined, readerDone}, {writerStarted, writerJoined, writerDone}} {
			if pending.started && !pending.joined {
				select {
				case <-pending.done:
				case <-time.After(2 * time.Second):
					t.Error("IAM race worker did not stop after cancellation")
				}
			}
		}
	}()
	readerStarted = true
	go func() {
		readerCtx := context.WithValue(ctx, qualificationRaceContextKey{}, "reader")
		readerDone <- db.WithContext(readerCtx).Transaction(func(tx *gorm.DB) error {
			if len(firstWrite) > 0 {
				return firstWrite[0](readerCtx, tx)
			}
			_, _, _, err := iam.NewRepository(tx).LockUserAuthorizationSource(readerCtx, source.PrincipalID, source.MembershipID, tenantID)
			return err
		})
	}()
	select {
	case <-readerHeld:
	case err := <-readerDone:
		readerJoined = true
		t.Fatalf("qualification did not hold principal: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	writerStarted = true
	go func() { writerDone <- write(context.WithValue(ctx, qualificationRaceContextKey{}, "writer")) }()
	var pid int
	select {
	case pid = <-writerPID:
	case err := <-writerDone:
		writerJoined = true
		t.Fatalf("writer did not start: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Release on an observed row-lock wait (fixed order), or once the old writer
	// has already taken a lower lock (the deterministic deadlock reproduction).
	for {
		select {
		case <-lowerHeld:
			t.Error("IAM writer took a lower authorization lock before the affected principal")
			goto ready
		case err := <-writerDone:
			writerJoined = true
			t.Fatalf("writer ended before contention: %v", err)
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
ready:
	release.Do(func() { close(resume) })
	if err := <-readerDone; err != nil {
		t.Errorf("qualification failed during IAM write: %v", err)
	}
	readerJoined = true
	if err := <-writerDone; err != nil {
		t.Errorf("real IAM write failed during qualification: %v", err)
	}
	writerJoined = true
}

// Add a previously undiscovered principal after the writer has locked its
// snapshot, but before it locks the aggregate. The write must conflict rather
// than acquiring the new account in the wrong order or omitting its version.
func assertMutationSetChange(t *testing.T, db *gorm.DB, table string, join func(context.Context) error, mutate func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	name := "mutation_set_" + strings.ReplaceAll(t.Name(), "/", "_")
	var once sync.Once
	called := false
	var joinErr error
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(qualificationRaceContextKey{}) == "batch" && tx.Statement.Table == table {
			once.Do(func() {
				called = true
				joinErr = join(ctx)
				if joinErr != nil {
					tx.AddError(joinErr)
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	err := mutate(context.WithValue(ctx, qualificationRaceContextKey{}, "batch"))
	if !called || joinErr != nil || !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("changed account set: reached=%v join=%v mutation=%v", called, joinErr, err)
	}
}

func exerciseIAMMutationSetChanges(t *testing.T, db *gorm.DB, identity *iam.Repository, tenantID, actorID int64,
	newOperator func(*testing.T, time.Duration) (userProvenance, time.Time),
) {
	ctx := context.Background()
	organizations := iam.NewOrganizationService(identity, time.Now)
	for _, department := range []bool{true, false} {
		kind := "group"
		if department {
			kind = "department"
		}
		t.Run(kind, func(t *testing.T) {
			holder, _ := newOperator(t, time.Hour)
			entrant, _ := newOperator(t, time.Hour)
			var id, version int64
			table := "project_groups"
			if department {
				row, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID,
					ActorPrincipalID: actorID, Code: "set_guard_dep", Name: "Set guard"})
				if err != nil {
					t.Fatal(err)
				}
				id, version, table = row.ID, row.Version, "departments"
			} else {
				row, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID,
					ActorPrincipalID: actorID, Code: "set_guard_group", Name: "Set guard"})
				if err != nil {
					t.Fatal(err)
				}
				id, version = row.ID, row.Version
			}
			join := func(writeCtx context.Context, source userProvenance) error {
				// Entrant is deliberately outside the batch's locked account set.
				if department {
					_, err := organizations.CreateDepartmentMembership(writeCtx, iam.CreateDepartmentMembershipInput{TenantID: tenantID,
						DepartmentID: id, TenantMembershipID: source.MembershipID, ActorPrincipalID: source.PrincipalID,
						MembershipType: iam.DepartmentMembershipTypePrimary, RelationRole: iam.DepartmentRelationRoleMember})
					return err
				}
				_, err := organizations.CreateProjectGroupMembership(writeCtx, iam.CreateProjectGroupMembershipInput{TenantID: tenantID,
					ProjectGroupID: id, TenantMembershipID: source.MembershipID, ActorPrincipalID: source.PrincipalID,
					RelationRole: iam.ProjectGroupRelationRoleMember})
				return err
			}
			if err := join(ctx, holder); err != nil {
				t.Fatal(err)
			}
			before, err := identity.GetPrincipal(ctx, holder.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			assertMutationSetChange(t, db, table, func(writeCtx context.Context) error { return join(writeCtx, entrant) }, func(writeCtx context.Context) error {
				if department {
					_, err := organizations.DisableDepartment(writeCtx, iam.ChangeDepartmentStatusInput{TenantID: tenantID,
						DepartmentID: id, Version: version, ActorPrincipalID: actorID, Reason: "Set guard"})
					return err
				}
				_, err := organizations.CloseProjectGroup(writeCtx, iam.CloseProjectGroupInput{TenantID: tenantID,
					ProjectGroupID: id, Version: version, ActorPrincipalID: actorID, Reason: "Set guard"})
				return err
			})
			var active int64
			if err := db.Table("system."+table).Where("id = ? AND status = 'active' AND version = ?", id, version).Count(&active).Error; err != nil || active != 1 {
				t.Fatalf("conflicted lifecycle mutated aggregate: count=%d err=%v", active, err)
			}
			after, err := identity.GetPrincipal(ctx, holder.PrincipalID)
			if err != nil || after.AuthorizationVersion != before.AuthorizationVersion {
				t.Fatalf("conflict advanced holder version: %+v err=%v", after, err)
			}
		})
	}
	t.Run("role", func(t *testing.T) {
		holder, _ := newOperator(t, time.Hour)
		entrant, _ := newOperator(t, time.Hour)
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.set_guard",
			Name: "Set guard", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: actorID})
		if err != nil {
			t.Fatal(err)
		}
		join := func(writeCtx context.Context, source userProvenance) error {
			_, err := roles.CreateAssignments(writeCtx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID,
				MembershipID: source.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: source.PrincipalID, Reason: "Set guard"})
			return err
		}
		if err := join(ctx, holder); err != nil {
			t.Fatal(err)
		}
		assertMutationSetChange(t, db, "tenants", func(writeCtx context.Context) error { return join(writeCtx, entrant) }, func(writeCtx context.Context) error {
			_, err := roles.UpdateRole(writeCtx, iam.UpdateTenantRoleInput{TenantID: tenantID, RoleID: role.ID, ActorPrincipalID: actorID,
				Name: "Must not commit", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read"}})
			return err
		})
		current, err := identity.GetTenantRole(ctx, tenantID, role.ID)
		if err != nil || current.Name == nil || *current.Name != "Set guard" {
			t.Fatalf("conflicted role update committed: %+v err=%v", current, err)
		}
	})
	t.Run("tenant", func(t *testing.T) {
		holder, _ := newOperator(t, time.Hour)
		entrant, _ := newOperator(t, time.Hour)
		tenants := iam.NewPlatformTenantService(identity, time.Now)
		tenant, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "set_guard_tenant", Name: "Set guard",
			ActorPrincipalID: actorID, InitialAdministratorPrincipalID: holder.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		assertMutationSetChange(t, db, "tenants", func(writeCtx context.Context) error {
			_, err := iam.NewTenantMembershipService(identity, time.Now).EstablishMembership(writeCtx, iam.EstablishTenantMembershipInput{
				TenantID: tenant.ID, PrincipalID: entrant.PrincipalID, SourceType: iam.TenantMembershipSourceManual})
			return err
		}, func(writeCtx context.Context) error {
			_, err := tenants.Suspend(writeCtx, iam.ChangeTenantStatusInput{TenantID: tenant.ID, Reason: "Set guard"})
			return err
		})
		current, err := tenants.Get(ctx, tenant.ID)
		if err != nil || current.Status != iam.TenantStatusActive {
			t.Fatalf("conflicted tenant lifecycle committed: %+v err=%v", current, err)
		}
	})
}

func exerciseIAMQualificationWriters(t *testing.T, db *gorm.DB, identity *iam.Repository, tenantID, actorID int64,
	newOperator func(*testing.T, time.Duration) (userProvenance, time.Time),
) {
	ctx := context.Background()
	roles := iam.NewTenantRoleService(identity, time.Now)
	organizations := iam.NewOrganizationService(identity, time.Now)
	t.Run("two accounts assign roles to one another", func(t *testing.T) {
		a, _ := newOperator(t, time.Hour)
		b, _ := newOperator(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.mutual",
			Name: "Mutual fixture", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: actorID})
		if err != nil {
			t.Fatal(err)
		}
		assign := func(writeCtx context.Context, repo *iam.Repository, actor, target userProvenance) error {
			_, err := iam.NewTenantRoleService(repo, time.Now).CreateAssignments(writeCtx, iam.CreateTenantRoleAssignmentsInput{
				TenantID: tenantID, MembershipID: target.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: actor.PrincipalID, Reason: "Mutual fixture"})
			return err
		}
		assertIAMQualificationRace(t, db, tenantID, a, func(writeCtx context.Context) error { return assign(writeCtx, identity, b, a) },
			func(writeCtx context.Context, tx *gorm.DB) error {
				return assign(writeCtx, iam.NewRepository(tx), a, b)
			})
	})
	for i, operation := range []string{"revoke", "update role", "delete role"} {
		t.Run(operation, func(t *testing.T) {
			operator, _ := newOperator(t, time.Hour)
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID,
				RoleKey: "custom.race_" + string(rune('a'+i)), Name: "Race fixture", ScopeTypes: []string{"tenant"},
				PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: actorID})
			if err != nil {
				t.Fatal(err)
			}
			assigned, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID,
				MembershipID: operator.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: actorID, Reason: "Race fixture"})
			if err != nil {
				t.Fatal(err)
			}
			assertIAMQualificationRace(t, db, tenantID, operator, func(raceCtx context.Context) error {
				switch operation {
				case "revoke":
					_, err := roles.RevokeAssignment(raceCtx, iam.RevokeTenantRoleAssignmentInput{TenantID: tenantID,
						AssignmentID: assigned[0].ID, ActorPrincipalID: actorID, Reason: "Race fixture"})
					return err
				case "update role":
					_, err := roles.UpdateRole(raceCtx, iam.UpdateTenantRoleInput{TenantID: tenantID, RoleID: role.ID,
						Name: "Updated fixture", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: actorID})
					return err
				default:
					return roles.DeleteRole(raceCtx, iam.DeleteTenantRoleInput{TenantID: tenantID, RoleID: role.ID,
						ActorPrincipalID: actorID, Reason: "Race fixture"})
				}
			})
		})
	}
	for _, department := range []bool{true, false} {
		kind := "group"
		if department {
			kind = "department"
		}
		for i, operation := range []string{"create member", "update member", "close member", "close organization"} {
			t.Run(kind+" "+operation, func(t *testing.T) {
				operator, _ := newOperator(t, time.Hour)
				var organizationID, version int64
				if department {
					row, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID,
						ActorPrincipalID: actorID, Code: "race_dep_" + string(rune('a'+i)), Name: "Race department"})
					if err != nil {
						t.Fatal(err)
					}
					organizationID, version = row.ID, row.Version
				} else {
					row, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID,
						ActorPrincipalID: actorID, Code: "race_group_" + string(rune('a'+i)), Name: "Race group"})
					if err != nil {
						t.Fatal(err)
					}
					organizationID, version = row.ID, row.Version
				}
				create := func(writeCtx context.Context) (*iam.ManagedOrganizationMembership, error) {
					if department {
						return organizations.CreateDepartmentMembership(writeCtx, iam.CreateDepartmentMembershipInput{TenantID: tenantID,
							DepartmentID: organizationID, TenantMembershipID: operator.MembershipID, ActorPrincipalID: actorID,
							MembershipType: iam.DepartmentMembershipTypePrimary, RelationRole: iam.DepartmentRelationRoleMember})
					}
					return organizations.CreateProjectGroupMembership(writeCtx, iam.CreateProjectGroupMembershipInput{TenantID: tenantID,
						ProjectGroupID: organizationID, TenantMembershipID: operator.MembershipID, ActorPrincipalID: actorID,
						RelationRole: iam.ProjectGroupRelationRoleMember})
				}
				var member *iam.ManagedOrganizationMembership
				if operation != "create member" {
					var err error
					member, err = create(ctx)
					if err != nil {
						t.Fatal(err)
					}
				}
				assertIAMQualificationRace(t, db, tenantID, operator, func(raceCtx context.Context) error {
					switch operation {
					case "create member":
						_, err := create(raceCtx)
						return err
					case "update member":
						if department {
							_, err := organizations.UpdateDepartmentMembership(raceCtx, iam.UpdateDepartmentMembershipInput{TenantID: tenantID,
								DepartmentID: organizationID, MembershipID: member.ID, Version: member.Version, ActorPrincipalID: actorID,
								MembershipType: iam.DepartmentMembershipTypePrimary, RelationRole: iam.DepartmentRelationRoleLeader})
							return err
						}
						_, err := organizations.UpdateProjectGroupMembership(raceCtx, iam.UpdateProjectGroupMembershipInput{TenantID: tenantID,
							ProjectGroupID: organizationID, MembershipID: member.ID, Version: member.Version, ActorPrincipalID: actorID,
							RelationRole: iam.ProjectGroupRelationRoleCoordinator})
						return err
					case "close member":
						input := iam.CloseOrganizationMembershipInput{TenantID: tenantID, OrganizationID: organizationID,
							MembershipID: member.ID, Version: member.Version, ActorPrincipalID: actorID, Reason: "Race fixture"}
						if department {
							_, err := organizations.CloseDepartmentMembership(raceCtx, input)
							return err
						}
						_, err := organizations.CloseProjectGroupMembership(raceCtx, input)
						return err
					default:
						if department {
							_, err := organizations.DisableDepartment(raceCtx, iam.ChangeDepartmentStatusInput{TenantID: tenantID,
								DepartmentID: organizationID, Version: version, ActorPrincipalID: actorID, Reason: "Race fixture"})
							return err
						}
						_, err := organizations.CloseProjectGroup(raceCtx, iam.CloseProjectGroupInput{TenantID: tenantID,
							ProjectGroupID: organizationID, Version: version, ActorPrincipalID: actorID, Reason: "Race fixture"})
						return err
					}
				})
			})
		}
	}
	t.Run("tenant membership expiry maintenance", func(t *testing.T) {
		operator, _ := newOperator(t, time.Hour)
		expires := time.Now().Add(2 * time.Hour)
		assertIAMQualificationRace(t, db, tenantID, operator, func(raceCtx context.Context) error {
			_, err := iam.NewTenantMembershipService(identity, time.Now).UpdateManagedMembership(raceCtx,
				iam.UpdateTenantMembershipInput{TenantID: tenantID, MembershipID: operator.MembershipID, ExpiresAt: &expires})
			return err
		})
	})
	t.Run("tenant lifecycle", func(t *testing.T) {
		// A separate tenant keeps later fulfillment fixtures active.
		operator, _ := newOperator(t, time.Hour)
		other, err := iam.NewPlatformTenantService(identity, time.Now).Create(ctx, iam.CreateTenantInput{Code: "race_tenant",
			Name: "Race tenant", ActorPrincipalID: actorID, InitialAdministratorPrincipalID: operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		var member iam.TenantMembership
		if err := db.Where("tenant_id = ? AND principal_id = ?", other.ID, operator.PrincipalID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		source := userProvenance{PrincipalID: operator.PrincipalID, MembershipID: member.ID}
		assertIAMQualificationRace(t, db, other.ID, source, func(raceCtx context.Context) error {
			_, err := iam.NewPlatformTenantService(identity, time.Now).Suspend(raceCtx,
				iam.ChangeTenantStatusInput{TenantID: other.ID, Reason: "Race fixture"})
			return err
		})
	})
}
