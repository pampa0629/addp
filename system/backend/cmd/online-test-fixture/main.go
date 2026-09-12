package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/iamcli"
	"github.com/addp/system/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const onlineDatabase = "addp_online"

const engineProvisionerRoleKey = "tenant.infrastructure_administrator"

var consumerPermissions = []string{
	"develop.data_read.execute",
	"develop.task.execute",
	"develop.task.read",
	"manager.data_item.read",
	"meta.catalog.read",
	"meta.scan_task.execute",
	"meta.scan_task.read",
	"service.data_read.execute",
	"service.definition.create",
	"service.definition.delete",
	"service.definition.read",
	"system.execution_authorization.create",
	"transfer.task.create",
	"transfer.task.delete",
	"transfer.task.execute",
	"transfer.task.read",
}

func main() {
	if err := run(os.Args[1:], os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "External Online identity fixture failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, environment []string) error {
	flags := flag.NewFlagSet("online-test-fixture", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "", "absolute path for the generated shell environment")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
		return errors.New("usage: online-test-fixture --output <absolute-path>")
	}
	if err := validateExternalEnvironment(environment, *output); err != nil {
		return err
	}

	commonconfig.LoadEnv()
	cfg := config.Load()
	if cfg.PostgresDB != onlineDatabase {
		return fmt.Errorf("POSTGRES_DB must be exactly %s", onlineDatabase)
	}
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("connect disposable PostgreSQL: %w", err)
	}
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		return fmt.Errorf("resolve current database: %w", err)
	}
	if database != onlineDatabase {
		return fmt.Errorf("connected database must be exactly %s", onlineDatabase)
	}
	if err := iamcli.RequireCurrentMigration(db); err != nil {
		return err
	}
	var users int64
	if err := db.Table("system.principals").Where("principal_type = ?", iam.PrincipalTypeUser).Count(&users).Error; err != nil {
		return fmt.Errorf("inspect disposable user principals: %w", err)
	}
	if users != 0 {
		return errors.New("disposable External Online database already contains User principals")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repository := iam.NewRepository(db)
	identity := iam.NewIdentityService(repository, nil)
	tenantService := iam.NewPlatformTenantService(repository, nil)
	membershipService := iam.NewTenantMembershipService(repository, nil)
	roleService := iam.NewTenantRoleService(repository, nil)
	tokenService, err := iam.NewTokenFamilyService(repository, iam.BrowserSessionConfig{
		AccessTokenTTL:        30 * time.Minute,
		RefreshTokenFamilyTTL: 45 * time.Minute,
		ResourceTicketOwners:  models.BrowserResourceAccessOwners,
	}, nil, nil)
	if err != nil {
		return err
	}
	selectionService, err := iam.NewContextSelectionService(repository, tokenService)
	if err != nil {
		return err
	}

	reserve, err := createUser(ctx, identity, "external-online-reserve")
	if err != nil {
		return err
	}
	if _, err := tenantService.Create(ctx, iam.CreateTenantInput{
		Code: "external-online-reserve", Name: "External Online Reserve",
		InitialAdministratorPrincipalID: reserve.PrincipalID,
		ActorPrincipalID:                reserve.PrincipalID,
		Audit:                           audit("external-online-reserve-tenant"),
	}); err != nil {
		return fmt.Errorf("create reserve tenant: %w", err)
	}

	administrator, err := createUser(ctx, identity, "external-online-administrator")
	if err != nil {
		return err
	}
	tenant, err := tenantService.Create(ctx, iam.CreateTenantInput{
		Code: "external-online", Name: "External Online",
		InitialAdministratorPrincipalID: administrator.PrincipalID,
		ActorPrincipalID:                administrator.PrincipalID,
		Audit:                           audit("external-online-tenant"),
	})
	if err != nil {
		return fmt.Errorf("create External Online tenant: %w", err)
	}
	if tenant.ID <= 1 {
		return errors.New("External Online tenant must not use the default Tenant ID")
	}

	consumer, err := createUser(ctx, identity, "external-online-consumer")
	if err != nil {
		return err
	}
	membership, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: consumer.PrincipalID,
		SourceType:           iam.TenantMembershipSourceManual,
		CreatedByPrincipalID: &administrator.PrincipalID,
		Audit:                audit("external-online-consumer-membership"),
	})
	if err != nil {
		return fmt.Errorf("establish consumer membership: %w", err)
	}
	role, err := roleService.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: "online.relational_consumer",
		Name: "Online relational consumer", Description: "Ephemeral external T4 minimum permissions",
		ScopeTypes: []string{"tenant"}, PermissionKeys: consumerPermissions,
		ActorPrincipalID: administrator.PrincipalID,
		Audit:            audit("external-online-consumer-role"),
	})
	if err != nil {
		return fmt.Errorf("create consumer role: %w", err)
	}
	if _, err := roleService.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenant.ID, MembershipID: membership.Membership.ID,
		RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "External T4 acceptance",
		ActorPrincipalID: administrator.PrincipalID,
		Audit:            audit("external-online-consumer-assignment"),
	}); err != nil {
		return fmt.Errorf("assign consumer role: %w", err)
	}

	provisioner, err := createUser(ctx, identity, "external-online-engine-provisioner")
	if err != nil {
		return err
	}
	provisionerMembership, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: provisioner.PrincipalID,
		SourceType:           iam.TenantMembershipSourceManual,
		CreatedByPrincipalID: &administrator.PrincipalID,
		Audit:                audit("external-online-engine-provisioner-membership"),
	})
	if err != nil {
		return fmt.Errorf("establish engine provisioner membership: %w", err)
	}
	provisionerRole, err := repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)
	if err != nil {
		return fmt.Errorf("resolve engine provisioner role: %w", err)
	}
	if _, err := roleService.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenant.ID, MembershipID: provisionerMembership.Membership.ID,
		RoleIDs: []int64{provisionerRole.ID}, ScopeType: "tenant", Reason: "External T4 Engine registration",
		ActorPrincipalID: administrator.PrincipalID,
		Audit:            audit("external-online-engine-provisioner-assignment"),
	}); err != nil {
		return fmt.Errorf("assign engine provisioner role: %w", err)
	}
	provisionerSession, err := issueSession(ctx, selectionService, provisioner.PrincipalID, "external-online-engine-provisioner-session")
	if err != nil {
		return err
	}
	consumerSession, err := issueSession(ctx, selectionService, consumer.PrincipalID, "external-online-consumer-session")
	if err != nil {
		return err
	}
	values := map[string]string{
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": provisionerSession.AccessToken,
		"ADDP_ONLINE_TEST_TENANT_ID":              fmt.Sprintf("%d", tenant.ID),
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN":      consumerSession.AccessToken,
	}
	if err := writeEnvironmentFile(*output, values); err != nil {
		return err
	}
	fmt.Printf("External Online identity fixture created for Tenant %d\n", tenant.ID)
	return nil
}

func validateExternalEnvironment(environment []string, output string) error {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	profileCount := 0
	for _, key := range []string{"ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED"} {
		if values[key] == "1" {
			profileCount++
		}
	}
	if values["GITHUB_ACTIONS"] != "true" || values["RUNNER_OS"] != "Linux" || profileCount != 1 {
		return errors.New("fixture requires exactly one GitHub Hosted or owner-managed Linux Online profile")
	}
	if !filepath.IsAbs(output) {
		return errors.New("output path must be absolute")
	}
	return nil
}

func createUser(ctx context.Context, service *iam.IdentityService, username string) (*iam.CreatedLocalUser, error) {
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, fmt.Errorf("generate disposable password: %w", err)
	}
	password := "External-" + base64.RawURLEncoding.EncodeToString(passwordBytes)
	created, err := service.CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: username, Password: password, DisplayName: username,
		Audit: audit(username),
	})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", username, err)
	}
	return created, nil
}

func issueSession(ctx context.Context, service *iam.ContextSelectionService, principalID int64, requestID string) (*iam.IssuedBrowserSession, error) {
	result, err := service.BeginContextSelection(ctx, iam.BeginContextSelectionInput{
		PrincipalID: principalID,
		Authentication: iam.SessionAuthentication{
			Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1,
			AuthenticatedAt: time.Now().UTC(),
		},
		Audit: audit(requestID),
	})
	if err != nil {
		return nil, fmt.Errorf("issue %s: %w", requestID, err)
	}
	if result.NextAction != iam.ContextSelectionNextActionSessionIssued || result.Session == nil {
		return nil, fmt.Errorf("%s did not resolve to one Tenant Context", requestID)
	}
	return result.Session, nil
}

func audit(requestID string) iam.AuditMetadata {
	return iam.AuditMetadata{RequestID: &requestID}
}

func writeEnvironmentFile(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create fixture output directory: %w", err)
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create fixture environment: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect fixture environment: %w", err)
	}
	for _, key := range []string{
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN",
		"ADDP_ONLINE_TEST_TENANT_ID",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
	} {
		value := strings.ReplaceAll(values[key], "'", "'\"'\"'")
		if _, err := fmt.Fprintf(file, "export %s='%s'\n", key, value); err != nil {
			file.Close()
			return fmt.Errorf("write fixture environment: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close fixture environment: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish fixture environment: %w", err)
	}
	return nil
}
