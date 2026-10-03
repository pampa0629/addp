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

var securityPermissions = []string{
	"security.assessment.read", "security.assessment.create", "security.detector.read",
	"security.enrollment.create", "security.enrollment.read", "security.finding.read", "security.finding.update",
	"security.protection_baseline.read", "security.protection_baseline.update",
	"security.policy.read", "security.policy.create", "security.policy.update", "security.policy.delete",
	"security.sensitive_data_type.create", "security.sensitive_data_type.delete", "security.sensitive_data_type.read",
}

var securityInitializerPermissions = []string{
	"security.classification.read", "security.classification.create", "security.grade.read", "security.grade.create",
	"security.sensitive_data_type.read", "security.sensitive_data_type.create", "security.detector.read", "security.detector.create",
}

var metricPermissions = []string{
	"meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"standard.metric.create", "standard.metric.read", "standard.metric.update", "standard.metric.publish",
	"model.dw_layer.create", "model.logical_model.create", "model.logical_model.read", "model.logical_model.update",
	"model.metric_implementation.create", "model.metric_implementation.read", "model.metric_implementation.update",
	"model.metric_implementation.publish", "model.metric_implementation.offline",
	"service.definition.create", "service.definition.read", "service.definition.update", "service.definition.delete",
	"service.data_read.execute", "system.execution_authorization.create",
}

var ontologyPermissions = []string{
	"ontology.revision.read", "ontology.revision.update", "ontology.revision.publish",
	"system.execution_authorization.create",
}

var orchestratorPermissions = []string{
	"orchestrator.workflow.read", "orchestrator.workflow.create", "orchestrator.workflow.delete", "orchestrator.workflow.execute",
	"meta.scan_task.read", "meta.scan_task.create", "meta.scan_task.delete", "meta.scan_task.execute",
	"monitor.execution.read", "system.execution_authorization.create",
}

var managerArtifactPermissions = []string{
	"manager.data_item.read", "manager.derived_artifact.create", "manager.derived_artifact.delete", "manager.derived_artifact.read",
	"meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "monitor.execution.read",
}

var redisConsumerPermissions = []string{
	"system.engine_catalog.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"manager.data_item.read", "manager.content.read",
}

var publicOriginReadPermissions = []string{
	"meta.catalog.read", "manager.data_item.read",
	"transfer.task.read", "orchestrator.workflow.read",
}

var publicOriginCreatePermissions = []string{
	"meta.scan_task.create", "transfer.task.create",
	"orchestrator.workflow.create", "orchestrator.workflow.execute",
}

func suitePermissions(suite string) ([]string, error) {
	switch suite {
	case "security-mysql-owner-protection":
		return append(append([]string{}, consumerPermissions...), securityPermissions...), nil
	case "opengauss-consumer-flow", "kingbase-consumer-flow":
		return consumerPermissions, nil
	case "redis-consumer-flow":
		return redisConsumerPermissions, nil
	case "metric-service-revision-lifecycle":
		return metricPermissions, nil
	case "ontology-revision-lifecycle":
		return ontologyPermissions, nil
	case "orchestrator-execution":
		return orchestratorPermissions, nil
	case "manager-internal-artifact-lineage":
		return managerArtifactPermissions, nil
	case "compose-public-origin":
		return nil, nil
	default:
		return nil, errors.New("unsupported Online identity fixture suite")
	}
}

func needsEngineProvisioner(suite string) bool {
	return suite == "security-mysql-owner-protection" || suite == "redis-consumer-flow" || suite == "manager-internal-artifact-lineage" || suite == "opengauss-consumer-flow" || suite == "kingbase-consumer-flow" || suite == "metric-service-revision-lifecycle" || suite == "orchestrator-execution"
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
	suite := flags.String("suite", "", "registered Online suite")
	output := flags.String("output", "", "absolute path for the generated shell environment")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
		return errors.New("usage: online-test-fixture --suite <suite> --output <absolute-path>")
	}
	permissions, err := suitePermissions(*suite)
	if err != nil {
		return err
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
	reserveTenant, err := tenantService.Create(ctx, iam.CreateTenantInput{
		Code: "external-online-reserve", Name: "External Online Reserve",
		InitialAdministratorPrincipalID: reserve.PrincipalID,
		ActorPrincipalID:                reserve.PrincipalID,
		Audit:                           audit("external-online-reserve-tenant"),
	})
	if err != nil {
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

	consumer, consumerPassword, err := createUserCredentials(ctx, identity, "external-online-consumer")
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
	if len(permissions) > 0 {
		role, err := roleService.CreateRole(ctx, iam.CreateTenantRoleInput{
			TenantID: tenant.ID, RoleKey: "online." + strings.ReplaceAll(*suite, "-", "_"),
			Name: "Online suite consumer", Description: "Ephemeral external T4 minimum permissions",
			ScopeTypes: []string{"tenant"}, PermissionKeys: permissions,
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
	}

	consumerSession, err := issueSession(ctx, selectionService, consumer.PrincipalID, "external-online-consumer-session")
	if err != nil {
		return err
	}
	values := map[string]string{
		"ADDP_ONLINE_TEST_TENANT_ID":         fmt.Sprintf("%d", tenant.ID),
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": consumerSession.AccessToken,
	}
	if *suite == "redis-consumer-flow" || *suite == "manager-internal-artifact-lineage" {
		values["ADDP_ONLINE_TEST_USER_USERNAME"] = "external-online-consumer"
		values["ADDP_ONLINE_TEST_USER_PASSWORD"] = consumerPassword
	}
	if *suite == "compose-public-origin" {
		readerSession, _, err := createPermissionFixture(ctx, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-public-reader", publicOriginReadPermissions)
		if err != nil {
			return err
		}
		creatorSession, _, err := createPermissionFixture(ctx, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-public-creator", publicOriginCreatePermissions)
		if err != nil {
			return err
		}
		administratorSession, err := issueSession(ctx, selectionService, administrator.PrincipalID, "external-online-administrator-session")
		if err != nil {
			return err
		}
		ownAssignments, _, err := roleService.ListAssignments(ctx, tenant.ID, iam.TenantRoleAssignmentFilter{}, 1, 100)
		if err != nil {
			return fmt.Errorf("resolve own Tenant assignment: %w", err)
		}
		var ownAssignmentID int64
		for _, assignment := range ownAssignments {
			if assignment.PrincipalID == administrator.PrincipalID {
				ownAssignmentID = assignment.ID
				break
			}
		}
		if ownAssignmentID <= 0 {
			return errors.New("own Tenant administrator assignment is missing")
		}
		reserveAssignments, _, err := roleService.ListAssignments(ctx, reserveTenant.ID, iam.TenantRoleAssignmentFilter{}, 1, 20)
		if err != nil {
			return fmt.Errorf("resolve cross-Tenant assignment: %w", err)
		}
		if len(reserveAssignments) == 0 || reserveAssignments[0].PrincipalID != reserve.PrincipalID {
			return errors.New("reserve Tenant administrator assignment is missing")
		}
		values["ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN"] = administratorSession.AccessToken
		values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"] = readerSession.AccessToken
		values["ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN"] = creatorSession.AccessToken
		values["ADDP_ONLINE_OWN_ASSIGNMENT_ID"] = fmt.Sprintf("%d", ownAssignmentID)
		values["ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID"] = fmt.Sprintf("%d", reserveAssignments[0].ID)
	}
	if *suite == "orchestrator-execution" {
		foreignTenant, err := createOrchestratorForeignTenant(ctx, tenantService.Create, reserve.PrincipalID, tenant.ID)
		if err != nil {
			return err
		}
		reader, _, err := createPermissionFixture(ctx, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-monitor-only", []string{"monitor.execution.read"})
		if err != nil {
			return err
		}
		foreign, _, err := createPermissionFixture(ctx, identity, membershipService, roleService, selectionService,
			foreignTenant.ID, reserve.PrincipalID, "external-online-foreign-reader",
			[]string{"monitor.execution.read", "orchestrator.workflow.read", "meta.scan_task.read"})
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"] = reader.AccessToken
		values["ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN"] = foreign.AccessToken
	}
	if *suite == "security-mysql-owner-protection" {
		initializer, _, err := createPermissionFixture(ctx, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-security-initializer", securityInitializerPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN"] = initializer.AccessToken
	}
	if needsEngineProvisioner(*suite) {
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
		values["ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"] = provisionerSession.AccessToken
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

// The reserve Tenant occupies ID 1 on a fresh database; it cannot serve as
// the nondefault foreign Tenant required by execution isolation acceptance.
func createOrchestratorForeignTenant(ctx context.Context, create func(context.Context, iam.CreateTenantInput) (*iam.ManagedTenant, error), administratorID, consumerTenantID int64) (*iam.ManagedTenant, error) {
	tenant, err := create(ctx, iam.CreateTenantInput{
		Code: "external-online-foreign", Name: "External Online Foreign",
		InitialAdministratorPrincipalID: administratorID,
		ActorPrincipalID:                administratorID,
		Audit:                           audit("external-online-foreign-tenant"),
	})
	if err != nil {
		return nil, fmt.Errorf("create foreign execution Tenant: %w", err)
	}
	if tenant == nil || tenant.ID <= 1 || tenant.ID == consumerTenantID {
		return nil, errors.New("foreign execution Tenant must be distinct and nondefault")
	}
	return tenant, nil
}

func createUser(ctx context.Context, service *iam.IdentityService, username string) (*iam.CreatedLocalUser, error) {
	created, _, err := createUserCredentials(ctx, service, username)
	return created, err
}

func createUserCredentials(ctx context.Context, service *iam.IdentityService, username string) (*iam.CreatedLocalUser, string, error) {
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, "", fmt.Errorf("generate disposable password: %w", err)
	}
	password := "External-" + base64.RawURLEncoding.EncodeToString(passwordBytes)
	created, err := service.CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: username, Password: password, DisplayName: username,
		Audit: audit(username),
	})
	if err != nil {
		return nil, "", fmt.Errorf("create %s: %w", username, err)
	}
	return created, password, nil
}

func createPermissionFixture(
	ctx context.Context,
	identity *iam.IdentityService,
	memberships *iam.TenantMembershipService,
	roles *iam.TenantRoleService,
	selection *iam.ContextSelectionService,
	tenantID, actorPrincipalID int64,
	username string,
	permissions []string,
) (*iam.IssuedBrowserSession, int64, error) {
	user, err := createUser(ctx, identity, username)
	if err != nil {
		return nil, 0, err
	}
	membership, err := memberships.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenantID, PrincipalID: user.PrincipalID,
		SourceType:           iam.TenantMembershipSourceManual,
		CreatedByPrincipalID: &actorPrincipalID,
		Audit:                audit(username + "-membership"),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("establish %s membership: %w", username, err)
	}
	role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenantID, RoleKey: "online." + strings.ReplaceAll(username, "-", "_"),
		Name: username, ScopeTypes: []string{"tenant"}, PermissionKeys: permissions,
		ActorPrincipalID: actorPrincipalID,
		Audit:            audit(username + "-role"),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("create %s role: %w", username, err)
	}
	assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: membership.Membership.ID,
		RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "Disposable Online permission acceptance",
		ActorPrincipalID: actorPrincipalID,
		Audit:            audit(username + "-assignment"),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("assign %s role: %w", username, err)
	}
	if len(assignments) != 1 || assignments[0].ID <= 0 {
		return nil, 0, fmt.Errorf("%s role assignment was not persisted", username)
	}
	session, err := issueSession(ctx, selection, user.PrincipalID, username+"-session")
	if err != nil {
		return nil, 0, err
	}
	return session, assignments[0].ID, nil
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
		"ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID",
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN",
		"ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_OWN_ASSIGNMENT_ID",
		"ADDP_ONLINE_READ_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_TEST_TENANT_ID",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_TEST_USER_USERNAME",
		"ADDP_ONLINE_TEST_USER_PASSWORD",
	} {
		raw, exists := values[key]
		if !exists {
			continue
		}
		value := strings.ReplaceAll(raw, "'", "'\"'\"'")
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
