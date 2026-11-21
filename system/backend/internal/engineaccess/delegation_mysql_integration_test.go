package engineaccess

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestEngineDelegationAgainstMySQL(t *testing.T) {
	repo := mysqlSourceGrantRepository(t)
	ctx := t.Context()
	svc := NewService(repo, func(e *models.Engine) bool { return e.EngineType == "postgresql" })
	actor := Actor{TenantID: 7, PrincipalID: 40001, MembershipID: 50001, TokenExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.db.Raw("SELECT authorization_version FROM system.principals WHERE id=?", actor.PrincipalID).Row().Scan(&actor.AuthorizationVersion); err != nil {
		t.Fatal(err)
	}
	const member int64 = 50002
	input := CreateInput{Actor: actor, EngineID: 12, TenantMembershipID: member, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), Reason: "Catalog management"}
	created, err := svc.Create(ctx, input)
	if err != nil || created.ID == 0 || created.EffectiveState != "effective" || created.Version != 1 {
		t.Fatalf("create: %+v, %v", created, err)
	}
	if _, err = svc.Create(ctx, input); !errors.Is(err, ErrOverlap) {
		t.Fatalf("overlap: %v", err)
	}
	checkScope := func(tx *Repository) error {
		s, err := tx.lockManagementScope(ctx, 7, 12, member)
		if err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		return s.check(now)
	}
	if err := repo.transaction(ctx, checkScope); err != nil {
		t.Fatalf("current delegated scope: %v", err)
	}
	revoke := RevokeInput{Actor: actor, EngineID: 12, ID: created.ID, Version: 1, Reason: "management complete"}
	revoked, err := svc.Revoke(ctx, revoke)
	if err != nil || revoked.Version != 2 || revoked.EffectiveState != "revoked" {
		t.Fatalf("revoke: %+v, %v", revoked, err)
	}
	if _, err = svc.Revoke(ctx, revoke); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale revoke: %v", err)
	}
	if err := repo.transaction(ctx, checkScope); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("scope after revocation: %v", err)
	}
	history, total, err := svc.List(ctx, 7, 12, 1, 10)
	if err != nil || total != 1 || len(history) != 1 || history[0].ID != created.ID || history[0].EffectiveState != "revoked" {
		t.Fatalf("history: %+v, total=%d, %v", history, total, err)
	}
	// Keep the first insert uncommitted while a separate connection tries the
	// same interval. A bounded InnoDB lock wait provides a deterministic witness.
	first := repo.db.Begin()
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	defer first.Rollback()
	row := created.Delegation
	row.ID = 0
	if err := NewRepository(first).insert(ctx, &row); err != nil {
		t.Fatal(err)
	}
	err = repo.db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SET SESSION innodb_lock_wait_timeout=1").Error; err != nil {
			return err
		}
		defer conn.Exec("SET SESSION innodb_lock_wait_timeout=DEFAULT")
		competing := created.Delegation
		competing.ID = 0
		return NewRepository(conn).insert(ctx, &competing)
	})
	var wait *drivermysql.MySQLError
	if !errors.As(err, &wait) || wait.Number != 1205 {
		t.Fatalf("competing insert: %v", err)
	}
	if err := first.Commit().Error; err != nil {
		t.Fatal(err)
	}
	competing := created.Delegation
	competing.ID = 0
	if err := repo.insert(ctx, &competing); !errors.Is(err, ErrOverlap) {
		t.Fatalf("committed overlap: %v", err)
	}
}
