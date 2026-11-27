package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/iamcli"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const k3sHATenantSuite = "k3s-ha-tenant"

func k3sHATestNamespace(namespace string) bool {
	run, ok := strings.CutPrefix(namespace, "addp-ha-test-")
	id, err := uuid.Parse(run)
	return ok && err == nil && id != uuid.Nil && id.String() == run
}

type k3sHADatabase struct {
	Cluster, Database, Role string
	LocalSocket             bool
}

func (d k3sHADatabase) matches(namespace string) bool {
	return k3sHATestNamespace(namespace) && d.Cluster == namespace && d.Database == "addp_test" && d.Role == "platform_runtime" && d.LocalSocket
}

func runK3sHATenant() error {
	const directory = "/run/addp-ha-fixture/"
	ns, err := os.ReadFile(directory + "namespace")
	if err != nil || !k3sHATestNamespace(string(ns)) {
		return errors.New("dedicated K3s HA fixture namespace required")
	}
	// The target is local to this PostgreSQL Pod. There is no site DSN option.
	db, err := gorm.Open(postgres.Open("host=/var/run/postgresql port=5432 dbname=addp_test user=platform_runtime sslmode=disable"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("dedicated local PostgreSQL fixture unavailable")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target k3sHADatabase
		if err := tx.Raw("SELECT current_setting('cluster_name') AS cluster, current_database() AS database, current_user AS role, inet_server_addr() IS NULL AS local_socket").Scan(&target).Error; err != nil || !target.matches(string(ns)) {
			return errors.New("K3s HA fixture refuses foreign database")
		}
		if err := iamcli.RequireCurrentMigration(tx); err != nil {
			return err
		}
		var empty bool
		if err := tx.Raw("SELECT NOT EXISTS (SELECT FROM system.users) AND NOT EXISTS (SELECT FROM system.tenants)").Scan(&empty).Error; err != nil || !empty {
			return errors.New("K3s HA tenant setup requires an empty identity store")
		}
		preparer, err := os.ReadFile(directory + "preparer-password")
		if err != nil {
			return errors.New("K3s HA preparer password missing")
		}
		consumer, err := os.ReadFile(directory + "consumer-password")
		if err != nil || len(preparer) < 32 || len(consumer) < 32 || string(preparer) == string(consumer) {
			return errors.New("K3s HA fixture requires two independent random passwords")
		}
		repo := iam.NewRepository(tx)
		identity := iam.NewIdentityService(repo, nil)
		create := func(name string, password []byte) (*iam.CreatedLocalUser, error) {
			return identity.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: name, DisplayName: name, Password: string(password), Audit: audit("k3s-ha-user")})
		}
		admin, err := create("ha-preparer", preparer)
		if err != nil {
			return err
		}
		reader, err := create("ha-consumer", consumer)
		if err != nil {
			return err
		}
		clock, err := testsupport.FixtureDatabaseClock(ctx, tx)
		if err != nil {
			return err
		}
		tenant, err := iam.NewPlatformTenantService(repo, clock).Create(ctx, iam.CreateTenantInput{
			Code: "ha-acceptance", Name: "K3s HA acceptance", InitialAdministratorPrincipalID: admin.PrincipalID, ActorPrincipalID: admin.PrincipalID, Audit: audit("k3s-ha-tenant"),
		})
		if err != nil {
			return err
		}
		_, err = iam.NewTenantMembershipService(repo, clock).EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
			TenantID: tenant.ID, PrincipalID: reader.PrincipalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &admin.PrincipalID, Audit: audit("k3s-ha-membership"),
		})
		return err
	})
}
