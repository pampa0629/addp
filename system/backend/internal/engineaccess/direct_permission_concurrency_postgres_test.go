package engineaccess

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Extend the standard disposable fixture with actual overlapping transactions.
// Pinned backend PIDs prove the second transaction waits on the first; merely
// observing a delayed goroutine is not evidence of authorization ordering.
func exerciseDirectPermissionIssuanceOrdering(t *testing.T, db *gorm.DB, actor Actor, receiver userProvenance, template CreateIndependentGrantInput, adminID int64) {
	for _, source := range []string{directPermissionExplicit, directPermissionDeployment} {
		for _, changeFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("permission ordering %s change_first=%t", source, changeFirst), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := template
				command.RequestID = uuid.New()
				command.CatalogPath = engineplugin.TabularItemPath(uint(command.EngineID), "schema", "public", fmt.Sprintf("permit_order_%s_%t", source, changeFirst))
				binding := factstest.Binding(command.CatalogPath)
				command.ItemFingerprint, command.ExpectedBoundaryHash = binding.ItemFingerprint, binding.BoundaryHash
				command.DirectPermissionSource = source
				if source == directPermissionExplicit {
					permit, err := installFixturePermit(ctx, NewRepository(db), actor.TenantID, adminID, command.CatalogPath)
					if err != nil {
						t.Fatal(err)
					}
					command.DirectPermissionVersion = permit.Version
				} else {
					command.DirectPermissionVersion = fixtureDisabledCatalogDeclaration(t, ctx, db)
				}
				service := func(connection *gorm.DB) *Service {
					return NewService(NewRepository(connection), func(*models.Engine) bool { return true }).
						WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{command.CatalogPath}}).
						WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, tenant int64, path engineplugin.EngineCatalogPath) (int64, error) {
							if tenant != actor.TenantID || path.EngineID != command.CatalogPath.EngineID {
								return 0, fmt.Errorf("unexpected isolated target")
							}
							engine, err := NewRepository(db).engine(c, tenant, command.EngineID, false)
							if err != nil {
								return 0, err
							}
							return engine.Version, nil
						}))
				}
				t.Cleanup(func() {
					cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
					defer done()
					repo := NewRepository(db)
					if _, err := repo.findSourceGrant(cleanup, command.RequestID); errors.Is(err, gorm.ErrRecordNotFound) {
						return
					} else if err != nil {
						t.Errorf("inspect ordering fixture Grant cleanup: %v", err)
						return
					}
					if _, err := repo.findGrantRevocation(cleanup, command.RequestID); err == nil {
						return
					} else if !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Errorf("inspect ordering fixture revocation cleanup: %v", err)
						return
					}
					if _, err := service(db).RevokeGrant(cleanup, RevokeGrantInput{Actor: actor, EngineID: command.EngineID, RequestID: command.RequestID, Reason: "Clean up interrupted permission ordering verification"}); err != nil {
						t.Errorf("withdraw ordering fixture Grant: %v", err)
					}
				})
				begin := func() (*gorm.DB, int) {
					tx := db.WithContext(ctx).Begin()
					if tx.Error != nil {
						t.Fatal(tx.Error)
					}
					var pid int
					if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					return tx, pid
				}
				grantTx, grantPID := begin()
				defer grantTx.Rollback()
				changeTx, changePID := begin()
				defer changeTx.Rollback()
				var granted *SourceGrantView
				issue := func() error {
					var err error
					granted, err = service(grantTx).CreateIndependentGrant(ctx, command)
					return err
				}
				change := func() error {
					if source == directPermissionDeployment {
						return changeTx.Exec("UPDATE system.module_definitions SET enabled=true,version=version+1 WHERE module_name='catalog'").Error
					}
					repo := NewRepository(changeTx)
					input := fixturePermitInput(ctx, repo, actor.TenantID, adminID, command.CatalogPath, false, command.DirectPermissionVersion)
					_, err := fixtureSetPermit(ctx, repo, input, func(*Repository) error { return nil })
					return err
				}
				first, second := issue, change
				holder, holderPID, waiterPID := grantTx, grantPID, changePID
				if changeFirst {
					first, second = change, issue
					holder, holderPID, waiterPID = changeTx, changePID, grantPID
				}
				if err := first(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- second() }()
				if err := waitDirectPermissionBlock(ctx, db, waiterPID, holderPID, source, done); err != nil {
					cancel()
					holder.Rollback()
					t.Fatal(err)
				}
				if err := holder.Commit().Error; err != nil {
					cancel()
					t.Fatal(err)
				}
				var secondErr error
				select {
				case secondErr = <-done:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if changeFirst {
					if granted != nil || !errors.Is(secondErr, ErrIndependentGrantBasis) {
						t.Fatalf("stale issuance survived earlier denial: %+v %v", granted, secondErr)
					}
					grantTx.Rollback()
					var count int64
					if err := db.WithContext(ctx).Table("system.engine_access_grants").Where("request_id=?", command.RequestID).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("denied issuance left Grant: count=%d err=%v", count, err)
					}
					if err := db.WithContext(ctx).Table("system.audit_logs").Where("event_name='system.engine_access_grant.issued' AND entity_id=?", command.RequestID.String()).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("denied issuance left success audit: count=%d err=%v", count, err)
					}
					return
				}
				if secondErr != nil || granted == nil {
					t.Fatalf("ordered change failed: grant=%+v err=%v", granted, secondErr)
				}
				if err := changeTx.Commit().Error; err != nil {
					t.Fatal(err)
				}
				rules, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: actor.TenantID, Source: receiver, Targets: []engineplugin.EngineCatalogPath{command.CatalogPath}, Bindings: testReadBindings(command.CatalogPath)})
				if err != nil || rules == nil || !rules.Covered {
					t.Fatalf("later denial revoked already committed Grant: %+v %v", rules, err)
				}
				replay, err := service(db).CreateIndependentGrant(ctx, command)
				if err != nil || replay == nil || !replay.GrantedAt.Equal(granted.GrantedAt) {
					t.Fatalf("later denial changed same-command history: %+v %v", replay, err)
				}
				fresh := command
				fresh.RequestID = uuid.New()
				if row, err := service(db).CreateIndependentGrant(ctx, fresh); row != nil || !errors.Is(err, ErrIndependentGrantBasis) {
					t.Fatalf("later new issuance ignored denial: %+v %v", row, err)
				}
				if _, err := service(db).RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: command.EngineID, RequestID: command.RequestID, Reason: "Finish isolated permission ordering verification"}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func waitDirectPermissionBlock(ctx context.Context, db *gorm.DB, waiter, holder int, source string, done <-chan error) error {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		lock := "advisory"
		if source == directPermissionDeployment {
			lock = "transactionid"
		}
		if err := db.WithContext(probe).Raw(`SELECT ? = ANY(pg_blocking_pids(?)) AND EXISTS
            (SELECT 1 FROM pg_locks WHERE pid=? AND NOT granted AND locktype=?)`, holder, waiter, waiter, lock).Scan(&blocked).Error; err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case err := <-done:
			return fmt.Errorf("operation did not block on its predecessor: %v", err)
		case <-probe.Done():
			return fmt.Errorf("no database wait from %d to %d: %w", waiter, holder, probe.Err())
		case <-ticker.C:
		}
	}
}

// Only the disposable gate database is changed. Restore the prior declaration
// even when an assertion fails; no development module or runtime is contacted.
func fixtureDisabledCatalogDeclaration(t *testing.T, ctx context.Context, db *gorm.DB) int64 {
	t.Helper()
	var before struct {
		Enabled   bool
		Version   int64
		UpdatedAt time.Time
	}
	found := db.WithContext(ctx).Table("system.module_definitions").Select("enabled,version,updated_at").Where("module_name='catalog'").Take(&before).Error
	if found != nil && !errors.Is(found, gorm.ErrRecordNotFound) {
		t.Fatal(found)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		if errors.Is(found, gorm.ErrRecordNotFound) {
			err = db.WithContext(cleanup).Exec("DELETE FROM system.module_definitions WHERE module_name='catalog'").Error
		} else {
			err = db.WithContext(cleanup).Exec("UPDATE system.module_definitions SET enabled=?,version=?,updated_at=? WHERE module_name='catalog'", before.Enabled, before.Version, before.UpdatedAt).Error
		}
		if err != nil {
			t.Errorf("restore isolated Catalog declaration: %v", err)
		}
	})
	if err := db.WithContext(ctx).Exec(`INSERT INTO system.module_definitions(module_name,route_prefix,enabled,version,created_at,updated_at)
        VALUES('catalog','/catalog',false,1,now(),now()) ON CONFLICT(module_name) DO UPDATE SET enabled=false,version=system.module_definitions.version+1`).Error; err != nil {
		t.Fatal(err)
	}
	declaration, err := NewRepository(db).catalogDeployment(ctx, false)
	if err != nil || declaration == nil {
		t.Fatal("isolated declaration missing", err)
	}
	return declaration.Version
}
