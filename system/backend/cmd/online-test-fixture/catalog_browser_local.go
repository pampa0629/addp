package main

// Local browser prerequisites belong to System. No session/token is issued here.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/iamcli"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const catalogBrowserLocalSuite = "catalog-browser-local"

var catalogBrowserMinimum = []string{"catalog.entry.read", "catalog.inventory.read", "standard.domain.read", "standard.element.read"}

func catalogBrowserExtended() []string {
	return append(append([]string{}, catalogBrowserMinimum...), "model.entity.read", "standard.unit.read", "standard.document.read")
}
func catalogBrowserPreparation() []string {
	return append(catalogBrowserExtended(), "standard.domain.create", "standard.element.create", "standard.element.update", "standard.element.publish", "standard.document.create", "standard.document.update", "model.entity.create", "model.entity.update")
}

type catalogBrowserUser struct {
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	PrincipalID  int64    `json:"principal_id"`
	MembershipID int64    `json:"membership_id"`
	Permissions  []string `json:"permissions"`
}
type catalogBrowserCredentials struct {
	RunID     string                        `json:"run_id"`
	TenantID  int64                         `json:"tenant_id"`
	ExpiresAt time.Time                     `json:"expires_at"`
	Users     map[string]catalogBrowserUser `json:"users"`
}

func catalogBrowserEnvironment(environment []string, suite string) (map[string]string, error) {
	values, err := localTraceEnvironment(environment)
	if err != nil {
		return nil, err
	}
	if suite != catalogBrowserLocalSuite || values["ADDP_CATALOG_BROWSER_LOCAL"] != "1" {
		return nil, errors.New("Catalog browser requires its dedicated local profile")
	}
	for _, key := range []string{"GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED", "ADDP_ONLINE_LOCAL_SGM", "ADDP_EXECUTION_TRACE_LOCAL", "ADDP_ONLINE_HOST"} {
		if values[key] != "" {
			return nil, errors.New("Catalog browser refuses other execution profiles")
		}
	}
	id, err := uuid.Parse(values["ADDP_CATALOG_BROWSER_RUN_ID"])
	if err != nil || id == uuid.Nil || id.String() != values["ADDP_CATALOG_BROWSER_RUN_ID"] {
		return nil, errors.New("Catalog browser run ID must be a canonical nonzero UUID")
	}
	if values["POSTGRES_DB"] != "addp_test" || values["POSTGRES_HOST"] != "127.0.0.1" {
		return nil, errors.New("Catalog browser requires its disposable loopback addp_test")
	}
	ttl, err := strconv.Atoi(values["ADDP_CATALOG_BROWSER_MINUTES"])
	if err != nil || ttl < 1 || ttl > 120 {
		return nil, errors.New("Catalog browser lease must be 1..120 minutes")
	}
	return values, nil
}

func requireCatalogBrowserDatabase(db *gorm.DB, runID string) error {
	var found struct {
		Database string
		Cluster  string
	}
	if err := db.Raw("SELECT current_database() AS database, current_setting('cluster_name') AS cluster").Scan(&found).Error; err != nil {
		return errors.New("cannot verify Catalog browser database identity")
	}
	if found.Database != "addp_test" || found.Cluster != "addp-catalog-browser-"+runID {
		return errors.New("Catalog browser refuses shared or foreign PostgreSQL")
	}
	return nil
}

func runCatalogBrowserLocal(environment []string, suite, output, release string) error {
	values, err := catalogBrowserEnvironment(environment, suite)
	if err != nil {
		return err
	}
	if release != "" && release != "preparer" && release != "all" {
		return errors.New("invalid Catalog browser release scope")
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("Catalog browser credential path must be absolute")
	}
	parent, err := os.Lstat(filepath.Dir(output))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return errors.New("Catalog browser credential directory must be private")
	}
	owner, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() {
		return errors.New("Catalog browser credential directory owner differs")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil || resolved != filepath.Dir(output) {
		return errors.New("Catalog browser refuses symlinked credential directory")
	}
	if release == "" {
		if err := validateExecutionTraceOutput(output); err != nil {
			return err
		}
	} else {
		info, err := os.Lstat(output)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return errors.New("Catalog browser requires its private credential file")
		}
	}
	// Explicit process environment only. Never load the working tree .env.
	cfg := config.Load()
	if cfg.PostgresDB != "addp_test" || cfg.PostgresHost != "127.0.0.1" {
		return errors.New("Catalog browser process configuration differs from admitted database")
	}
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("connect Catalog browser database failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := requireCatalogBrowserDatabase(db, values["ADDP_CATALOG_BROWSER_RUN_ID"]); err != nil {
		return err
	}
	if err := iamcli.RequireCurrentMigration(db); err != nil {
		return err
	}
	if release != "" {
		return releaseCatalogBrowserUsers(db, output, values["ADDP_CATALOG_BROWSER_RUN_ID"], release)
	}
	var count int64
	if err := db.Table("system.principals").Where("principal_type = ?", iam.PrincipalTypeUser).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("Catalog browser fixture requires a fresh database without users")
	}
	minutes, _ := strconv.Atoi(values["ADDP_CATALOG_BROWSER_MINUTES"])
	return prepareCatalogBrowserUsers(db, output, values["ADDP_CATALOG_BROWSER_RUN_ID"], time.Duration(minutes)*time.Minute)
}

func prepareCatalogBrowserUsers(db *gorm.DB, output, runID string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repo := iam.NewRepository(db)
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		return err
	}
	identity := iam.NewIdentityService(repo, nil)
	administrator, err := createUser(ctx, identity, "catalog-bootstrap-"+runID[:8])
	if err != nil {
		return err
	}
	tenant, err := iam.NewPlatformTenantService(repo, clock).Create(ctx, iam.CreateTenantInput{Code: "catalog-browser-" + runID[:8], Name: "Catalog browser acceptance", InitialAdministratorPrincipalID: administrator.PrincipalID, ActorPrincipalID: administrator.PrincipalID, Audit: audit("catalog-browser-tenant")})
	if err != nil {
		return err
	}
	expires := clock().Add(ttl)
	credentials := catalogBrowserCredentials{RunID: runID, TenantID: tenant.ID, ExpiresAt: expires, Users: map[string]catalogBrowserUser{}}
	memberships := iam.NewTenantMembershipService(repo, clock)
	roles := iam.NewTenantRoleService(repo, clock)
	for _, fixture := range []struct {
		name        string
		permissions []string
	}{{"preparer", catalogBrowserPreparation()}, {"minimum", catalogBrowserMinimum}, {"extended", catalogBrowserExtended()}} {
		username := "catalog-" + fixture.name + "-" + runID[:8]
		user, password, err := createUserCredentials(ctx, identity, username)
		if err != nil {
			return err
		}
		member, err := memberships.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: user.PrincipalID, SourceType: iam.TenantMembershipSourceManual, ExpiresAt: &expires, CreatedByPrincipalID: &administrator.PrincipalID, Audit: audit("catalog-browser-member")})
		if err != nil {
			return err
		}
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant.ID, RoleKey: "local.catalog_browser_" + fixture.name, Name: "Catalog browser " + fixture.name, PermissionKeys: fixture.permissions, ActorPrincipalID: administrator.PrincipalID, Audit: audit("catalog-browser-role")})
		if err != nil {
			return err
		}
		_, err = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant.ID, MembershipID: member.Membership.ID, RoleIDs: []int64{role.ID}, ValidUntil: &expires, Reason: "Disposable Catalog browser acceptance", ActorPrincipalID: administrator.PrincipalID, Audit: audit("catalog-browser-assignment")})
		if err != nil {
			return err
		}
		credentials.Users[fixture.name] = catalogBrowserUser{username, password, user.PrincipalID, member.Membership.ID, fixture.permissions}
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func releaseCatalogBrowserUsers(db *gorm.DB, output, runID, scope string) error {
	data, err := os.ReadFile(output)
	if err != nil {
		return err
	}
	var credentials catalogBrowserCredentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return errors.New("invalid Catalog browser credential manifest")
	}
	if credentials.RunID != runID || credentials.TenantID <= 0 || len(credentials.Users) != 3 {
		return errors.New("foreign Catalog browser manifest")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := iam.NewRepository(db)
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		return err
	}
	service := iam.NewTenantMembershipService(repo, clock)
	for name, user := range credentials.Users {
		if scope != "all" && name != scope {
			continue
		}
		if user.Username != "catalog-"+name+"-"+runID[:8] {
			return errors.New("foreign Catalog browser user")
		}
		member, err := service.GetManagedMembership(ctx, credentials.TenantID, user.MembershipID)
		if err != nil {
			return err
		}
		if member.PrincipalID != user.PrincipalID {
			return errors.New("Catalog browser membership identity changed")
		}
		if member.Status == iam.TenantMembershipStatusEnded {
			continue
		}
		if _, err := service.EndMembership(ctx, iam.ChangeTenantMembershipInput{TenantID: credentials.TenantID, PrincipalID: user.PrincipalID, Reason: "Catalog browser acceptance release", Audit: audit("catalog-browser-release")}); err != nil {
			return fmt.Errorf("release Catalog browser membership: %w", err)
		}
	}
	return nil
}
