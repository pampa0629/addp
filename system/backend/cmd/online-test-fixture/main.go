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
	"github.com/addp/system/internal/testsupport"
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
	"security.enrollment.create", "security.enrollment.read", "security.enrollment.update", "security.finding.read", "security.finding.update",
	"security.protection_baseline.read", "security.protection_baseline.update",
	"security.policy.read", "security.policy.create", "security.policy.update", "security.policy.delete",
	"security.sensitive_data_type.create", "security.sensitive_data_type.delete", "security.sensitive_data_type.read",
}

var securityInitializerPermissions = []string{
	"security.classification.read", "security.classification.create", "security.grade.read", "security.grade.create",
	"security.sensitive_data_type.read", "security.sensitive_data_type.create", "security.detector.read", "security.detector.create",
}

// Source authorization is preparation-only; the owner consumer receives exact
// table read Grants, never the ability to issue Grants or manage delegations.
var sourceInitializerPermissions = []string{
	"system.engine_access_approval_requirement.initialize",
	"system.engine_access_delegation.create",
	"system.engine_access_grant.create",
}

var plaintextApplicantPermissions = []string{
	"manager.data_item.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"security.assessment.read", "security.enrollment.create", "security.enrollment.read",
	"security.finding.read", "security.finding.update", "security.protection_access_request.create",
	"security.protection_access_request.read",
}

var plaintextApproverPermissions = []string{
	"security.enrollment.read", "meta.catalog.read",
	"manager.data_item.read", "security.protection_access_request.update",
	"security.protection_exemption.delete", "security.protection_exemption.read",
}

var transferLineagePermissions = []string{
	"monitor.execution.read",
	"develop.task.create", "develop.task.read", "develop.task.execute", "develop.task.delete",
	"develop.data_read.execute", "develop.data_write.execute", "system.execution_authorization.create",
	"orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.execute", "orchestrator.workflow.delete",
	"manager.content.read", "manager.data_item.read", "meta.catalog.read", "meta.lineage.read",
	"meta.scan_task.execute", "meta.scan_task.read",
	"transfer.task.create", "transfer.task.delete", "transfer.task.execute", "transfer.task.read",
}

var transferContinuousPermissions = []string{
	"transfer.task.create", "transfer.task.delete", "transfer.task.execute", "transfer.task.read", "transfer.task.update",
	"monitor.execution.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"system.execution_authorization.create",
}

var transferConfigCheckPermissions = []string{"transfer.task.create", "meta.catalog.read"}

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

var orchestratorPeerPermissions = []string{"monitor.execution.read", "meta.scan_task.read"}

var managerArtifactPermissions = []string{
	"manager.content.read",
	"manager.data_item.update",
	"manager.data_item.read", "manager.derived_artifact.create", "manager.derived_artifact.delete", "manager.derived_artifact.read",
	"meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "monitor.execution.read",
}

var redisConsumerPermissions = []string{
	"system.engine_catalog.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"manager.data_item.read", "manager.content.read",
}

var publicOriginReadPermissions = []string{
	"meta.catalog.read", "manager.content.read", "manager.data_item.read",
	"transfer.task.read", "orchestrator.workflow.read",
}

var publicOriginCreatePermissions = []string{
	"meta.scan_task.create", "transfer.task.create",
	"orchestrator.workflow.create", "orchestrator.workflow.execute",
}

var rasterWorkflowPermissions = []string{
	"develop.task.read", "develop.task.execute", "develop.data_read.execute", "develop.data_write.execute",
	"system.execution_authorization.create", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"meta.lineage.read", "monitor.execution.read",
}

var readWorkflowConsumerPermissions = []string{
	"system.engine_catalog.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
	"manager.data_item.read", "manager.content.read", "develop.task.read", "develop.task.execute",
	"develop.data_read.execute", "system.execution_authorization.create",
}

func suitePermissions(suite string) ([]string, error) {
	switch suite {
	case "platform-node-metrics":
		return nil, nil
	case "raster-workflow", "raster-workflow-capacity":
		return rasterWorkflowPermissions, nil
	case "security-mysql-owner-protection":
		permissions := append(append([]string{}, consumerPermissions...), securityPermissions...)
		return append(permissions, "manager.content.read", "manager.search.execute", "manager.derived_artifact.create", "manager.derived_artifact.read", "monitor.execution.read"), nil
	case "security-plaintext-access":
		return plaintextApplicantPermissions, nil
	case "opengauss-consumer-flow", "kingbase-consumer-flow":
		return consumerPermissions, nil
	case "elasticsearch-consumer-flow":
		return readWorkflowConsumerPermissions, nil
	case "hdfs-spark-consumer-flow":
		return append(append([]string{}, readWorkflowConsumerPermissions...), "develop.data_write.execute", "develop.data_ddl.execute", "meta.lineage.read"), nil
	case "redis-consumer-flow":
		return redisConsumerPermissions, nil
	case "transfer-relational-sql-etl":
		return transferLineagePermissions, nil
	case "transfer-continuous-events":
		return transferContinuousPermissions, nil
	case "metric-service-revision-lifecycle":
		return metricPermissions, nil
	case "quality-dynamic-binding":
		return qualityPermissions, nil
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
	if suite == "quality-dynamic-binding" {
		return true
	}
	return suite == "transfer-continuous-events" || suite == "security-plaintext-access" || suite == "hdfs-spark-consumer-flow" || suite == "elasticsearch-consumer-flow" || (suite == "raster-workflow" || suite == "raster-workflow-capacity") || suite == "redis-consumer-flow" || suite == "security-mysql-owner-protection" || suite == "transfer-relational-sql-etl" || suite == "manager-internal-artifact-lineage" || suite == "opengauss-consumer-flow" || suite == "kingbase-consumer-flow" || suite == "metric-service-revision-lifecycle" || suite == "orchestrator-execution"
}

func consumerEnvironment(suite string, tenantID int64, accessToken, password string) map[string]string {
	values := map[string]string{
		"ADDP_ONLINE_TEST_TENANT_ID":         fmt.Sprintf("%d", tenantID),
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": accessToken,
	}
	switch suite {
	case "security-plaintext-access", "hdfs-spark-consumer-flow", "elasticsearch-consumer-flow", "raster-workflow", "raster-workflow-capacity", "redis-consumer-flow", "manager-internal-artifact-lineage", "transfer-relational-sql-etl", "transfer-continuous-events", "security-mysql-owner-protection":
		values["ADDP_ONLINE_TEST_USER_USERNAME"] = "external-online-consumer"
		values["ADDP_ONLINE_TEST_USER_PASSWORD"] = password
	}
	return values
}

func fixtureSessionConfig(suite string) iam.BrowserSessionConfig {
	config := iam.BrowserSessionConfig{
		AccessTokenTTL:        30 * time.Minute,
		RefreshTokenFamilyTTL: 45 * time.Minute,
		ResourceTicketOwners:  models.BrowserResourceAccessOwners,
	}
	// Full-scene capacity can outlast the ordinary 1740-second raster suite.
	// These identities exist only in the disposable Hosted platform database.
	if suite == "raster-workflow-capacity" {
		config.AccessTokenTTL = 4 * time.Hour
		config.RefreshTokenFamilyTTL = 4 * time.Hour
	}
	return config
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
	observe := flags.String("observe-catalog-grant", "", "read-only Catalog issuance observation for the dedicated Online deployment")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *observe != "" {
		if flags.NArg() != 0 || *output != "" || *suite != "enterprise-catalog-publishing" {
			return errors.New("issuance observation requires only the enterprise-catalog-publishing suite and request UUID")
		}
		return observeCatalogGrant(*observe, environment, os.Stdout)
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
	if *suite == "platform-node-metrics" {
		return preparePlatformMetricsIdentities(db, cfg, *output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repository := iam.NewRepository(db)
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		return fmt.Errorf("read fixture authority clock: %w", err)
	}
	identity := iam.NewIdentityService(repository, nil)
	tenantService := iam.NewPlatformTenantService(repository, clock)
	membershipService := iam.NewTenantMembershipService(repository, clock)
	roleService := iam.NewTenantRoleService(repository, clock)
	tokenService, err := iam.NewTokenFamilyService(repository, fixtureSessionConfig(*suite), nil, nil)
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

	consumerSession, err := issueSession(ctx, repository, selectionService, consumer.PrincipalID, "external-online-consumer-session")
	if err != nil {
		return err
	}
	values := consumerEnvironment(*suite, tenant.ID, consumerSession.AccessToken, consumerPassword)
	if *suite == "transfer-relational-sql-etl" {
		const username = "external-online-transfer-precheck"
		original, assignmentID, password, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, username, transferConfigCheckPermissions)
		if err != nil {
			return err
		}
		builtin, err := repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)
		if err != nil {
			return fmt.Errorf("resolve precheck infrastructure role: %w", err)
		}
		if _, err := grantTransferConfigCheckEngineReader(ctx, roleService, func(principalID int64) (*iam.IssuedBrowserSession, error) {
			return issueSession(ctx, repository, selectionService, principalID, username+"-current-session")
		}, original, tenant.ID, administrator.PrincipalID, assignmentID, builtin.ID); err != nil {
			return err
		}
		values["ADDP_ONLINE_TRANSFER_CHECK_USER_USERNAME"] = username
		values["ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD"] = password
	}
	if *suite == "raster-workflow" || *suite == "raster-workflow-capacity" {
		policyValues, err := rasterPolicyIdentities(ctx, repository, identity, selectionService, administrator.PrincipalID)
		if err != nil {
			return err
		}
		for key, value := range policyValues {
			values[key] = value
		}
	}
	if *suite == "compose-public-origin" {
		readerSession, _, readerPassword, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-public-reader", publicOriginReadPermissions)
		if err != nil {
			return err
		}
		creatorSession, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-public-creator", publicOriginCreatePermissions)
		if err != nil {
			return err
		}
		administratorSession, err := issueSession(ctx, repository, selectionService, administrator.PrincipalID, "external-online-administrator-session")
		if err != nil {
			return err
		}
		userType := iam.PrincipalTypeUser
		assignmentFilter := iam.TenantRoleAssignmentFilter{PrincipalType: &userType}
		ownAssignments, _, err := roleService.ListAssignments(ctx, tenant.ID, assignmentFilter, 1, 100)
		if err != nil {
			return fmt.Errorf("resolve own Tenant assignment: %w", err)
		}
		ownAssignmentID, err := tenantAdministratorAssignmentID(ownAssignments, tenant.ID, administrator.PrincipalID)
		if err != nil {
			return fmt.Errorf("resolve own Tenant administrator assignment: %w", err)
		}
		reserveAssignments, _, err := roleService.ListAssignments(ctx, reserveTenant.ID, assignmentFilter, 1, 100)
		if err != nil {
			return fmt.Errorf("resolve cross-Tenant assignment: %w", err)
		}
		reserveAssignmentID, err := tenantAdministratorAssignmentID(reserveAssignments, reserveTenant.ID, reserve.PrincipalID)
		if err != nil {
			return fmt.Errorf("resolve cross-Tenant administrator assignment: %w", err)
		}
		values["ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN"] = administratorSession.AccessToken
		values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"] = readerSession.AccessToken
		values["ADDP_ONLINE_READ_USER_USERNAME"] = "external-online-public-reader"
		values["ADDP_ONLINE_READ_USER_PASSWORD"] = readerPassword
		values["ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN"] = creatorSession.AccessToken
		values["ADDP_ONLINE_OWN_ASSIGNMENT_ID"] = fmt.Sprintf("%d", ownAssignmentID)
		values["ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID"] = fmt.Sprintf("%d", reserveAssignmentID)
	}
	if *suite == "orchestrator-execution" || *suite == "transfer-continuous-events" {
		ownerPermission := "orchestrator.workflow.read"
		readerName := "external-online-parent-reader"
		peerName := "external-online-meta-peer"
		peerPermissions := orchestratorPeerPermissions
		foreignPermissions := []string{"monitor.execution.read", "orchestrator.workflow.read", "meta.scan_task.read"}
		if *suite == "transfer-continuous-events" {
			ownerPermission = "transfer.task.read"
			readerName = "external-online-transfer-reader"
			peerName = "external-online-transfer-peer"
			peerPermissions = []string{"monitor.execution.read", "transfer.task.read"}
			foreignPermissions = peerPermissions
		}
		foreignTenant, err := createOrchestratorForeignTenant(ctx, tenantService.Create, reserve.PrincipalID, tenant.ID)
		if err != nil {
			return err
		}
		reader, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-monitor-only", []string{"monitor.execution.read"})
		if err != nil {
			return err
		}
		foreign, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			foreignTenant.ID, reserve.PrincipalID, "external-online-foreign-reader",
			foreignPermissions)
		if err != nil {
			return err
		}
		peer, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, peerName, peerPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_PEER_USER_ACCESS_TOKEN"] = peer.AccessToken
		values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"] = reader.AccessToken
		values["ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN"] = foreign.AccessToken
		parentReader, ownerAssignmentID, password, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, readerName, []string{ownerPermission})
		if err != nil {
			return err
		}
		parentReader, err = grantExecutionMonitorReader(ctx, roleService, func(principalID int64) (*iam.IssuedBrowserSession, error) {
			return issueSession(ctx, repository, selectionService, principalID, "external-online-parent-reader-complete-session")
		}, parentReader, tenant.ID, administrator.PrincipalID, ownerAssignmentID, "online."+strings.ReplaceAll(readerName, "-", "_"))
		if err != nil {
			return err
		}
		administratorSession, err := issueSession(ctx, repository, selectionService, administrator.PrincipalID, "external-online-orchestrator-iam-session")
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN"] = administratorSession.AccessToken
		values["ADDP_ONLINE_PARENT_USER_ACCESS_TOKEN"] = parentReader.AccessToken
		values["ADDP_ONLINE_PARENT_USER_USERNAME"] = readerName
		values["ADDP_ONLINE_PARENT_USER_PASSWORD"] = password
		values["ADDP_ONLINE_PARENT_ASSIGNMENT_ID"] = fmt.Sprintf("%d", ownerAssignmentID)
	}
	if *suite == "security-plaintext-access" {
		approver, _, approverPassword, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-plaintext-approver", plaintextApproverPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN"] = approver.AccessToken
		values["ADDP_ONLINE_TEST_APPROVER_USERNAME"] = "external-online-plaintext-approver"
		values["ADDP_ONLINE_TEST_APPROVER_PASSWORD"] = approverPassword
	}
	if *suite == "security-mysql-owner-protection" || *suite == "security-plaintext-access" {
		initializer, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-security-initializer", securityInitializerPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN"] = initializer.AccessToken
	}
	if *suite == "security-mysql-owner-protection" || *suite == "security-plaintext-access" || *suite == "hdfs-spark-consumer-flow" {
		sourceInitializer, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-source-initializer", sourceInitializerPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"] = sourceInitializer.AccessToken
	}
	if *suite == "quality-dynamic-binding" {
		preparer, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-quality-preparer", qualityPreparationPermissions)
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"] = preparer.AccessToken
		reader, _, _, err := createPermissionFixture(ctx, repository, identity, membershipService, roleService, selectionService,
			tenant.ID, administrator.PrincipalID, "external-online-quality-monitor-only", []string{"monitor.execution.read"})
		if err != nil {
			return err
		}
		values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"] = reader.AccessToken
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
		provisionerSession, err := issueSession(ctx, repository, selectionService, provisioner.PrincipalID, "external-online-engine-provisioner-session")
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

func tenantAdministratorAssignmentID(assignments []iam.ManagedTenantRoleAssignment, tenantID, principalID int64) (int64, error) {
	var id int64
	for _, assignment := range assignments {
		if assignment.PrincipalID != principalID || assignment.PrincipalType != iam.PrincipalTypeUser ||
			assignment.RoleKey != "tenant.administrator" || assignment.ScopeType != "tenant" ||
			assignment.TenantID == nil || *assignment.TenantID != tenantID || assignment.ID <= 0 ||
			assignment.EffectiveState != "effective" {
			continue
		}
		if id != 0 {
			return 0, errors.New("Tenant administrator assignment is ambiguous")
		}
		id = assignment.ID
	}
	if id == 0 {
		return 0, errors.New("Tenant administrator assignment is missing")
	}
	return id, nil
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
	repository *iam.Repository,
	identity *iam.IdentityService,
	memberships *iam.TenantMembershipService,
	roles *iam.TenantRoleService,
	selection *iam.ContextSelectionService,
	tenantID, actorPrincipalID int64,
	username string,
	permissions []string,
) (*iam.IssuedBrowserSession, int64, string, error) {
	user, password, err := createUserCredentials(ctx, identity, username)
	if err != nil {
		return nil, 0, "", err
	}
	membership, err := memberships.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenantID, PrincipalID: user.PrincipalID,
		SourceType:           iam.TenantMembershipSourceManual,
		CreatedByPrincipalID: &actorPrincipalID,
		Audit:                audit(username + "-membership"),
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("establish %s membership: %w", username, err)
	}
	role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenantID, RoleKey: "online." + strings.ReplaceAll(username, "-", "_"),
		Name: username, ScopeTypes: []string{"tenant"}, PermissionKeys: permissions,
		ActorPrincipalID: actorPrincipalID,
		Audit:            audit(username + "-role"),
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("create %s role: %w", username, err)
	}
	assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: membership.Membership.ID,
		RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "Disposable Online permission acceptance",
		ActorPrincipalID: actorPrincipalID,
		Audit:            audit(username + "-assignment"),
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("assign %s role: %w", username, err)
	}
	if len(assignments) != 1 || assignments[0].ID <= 0 {
		return nil, 0, "", fmt.Errorf("%s role assignment was not persisted", username)
	}
	session, err := issueSession(ctx, repository, selection, user.PrincipalID, username+"-session")
	if err != nil {
		return nil, 0, "", err
	}
	return session, assignments[0].ID, password, nil
}

// Keep Monitor access in a separate assignment so revoking Owner access still
// exercises Monitor's per-execution authorization with a newly logged-in User.
type executionReaderRoles interface {
	GetAssignment(context.Context, int64, int64) (*iam.ManagedTenantRoleAssignment, error)
	CreateRole(context.Context, iam.CreateTenantRoleInput) (*iam.TenantRole, error)
	CreateAssignments(context.Context, iam.CreateTenantRoleAssignmentsInput) ([]iam.ManagedTenantRoleAssignment, error)
}

type transferConfigCheckRoles interface {
	GetAssignment(context.Context, int64, int64) (*iam.ManagedTenantRoleAssignment, error)
	CreateAssignments(context.Context, iam.CreateTenantRoleAssignmentsInput) ([]iam.ManagedTenantRoleAssignment, error)
}

func grantTransferConfigCheckEngineReader(ctx context.Context, roles transferConfigCheckRoles, issue func(int64) (*iam.IssuedBrowserSession, error), original *iam.IssuedBrowserSession, tenantID, actorID, assignmentID, builtinRoleID int64) (*iam.IssuedBrowserSession, error) {
	if original == nil || original.Context.Type != iam.ContextTypeTenant || original.Context.TenantID == nil || *original.Context.TenantID != tenantID ||
		original.Context.TenantMembershipID == nil || *original.Context.TenantMembershipID <= 0 || tenantID <= 1 || assignmentID <= 0 || builtinRoleID <= 0 {
		return nil, errors.New("precheck reader requires its dedicated Tenant session and role")
	}
	assignment, err := roles.GetAssignment(ctx, tenantID, assignmentID)
	if err != nil {
		return nil, fmt.Errorf("resolve precheck role assignment: %w", err)
	}
	if assignment == nil || assignment.ID != assignmentID || assignment.PrincipalID <= 0 || assignment.MembershipID != *original.Context.TenantMembershipID ||
		assignment.RoleKey != "online.external_online_transfer_precheck" || assignment.PrincipalType != iam.PrincipalTypeUser {
		return nil, errors.New("precheck role assignment does not match its dedicated user")
	}
	grants, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: assignment.MembershipID, RoleIDs: []int64{builtinRoleID}, ScopeType: "tenant",
		Reason: "Disposable Online read-only configuration check", ActorPrincipalID: actorID,
		Audit: audit("external-online-precheck-engine-reader-assignment"),
	})
	if err != nil {
		return nil, fmt.Errorf("assign precheck infrastructure role: %w", err)
	}
	if len(grants) != 1 || grants[0].ID <= 0 || grants[0].ID == assignmentID || grants[0].PrincipalID != assignment.PrincipalID || grants[0].MembershipID != assignment.MembershipID {
		return nil, errors.New("precheck infrastructure assignment was not persisted separately")
	}
	// The additional role changes authorization_version; never reuse the old token.
	return issue(assignment.PrincipalID)
}

func grantExecutionMonitorReader(ctx context.Context, roles executionReaderRoles, issue func(int64) (*iam.IssuedBrowserSession, error), original *iam.IssuedBrowserSession, tenantID, actorID, ownerAssignmentID int64, ownerRoleKey string) (*iam.IssuedBrowserSession, error) {
	if original == nil || original.Context.Type != iam.ContextTypeTenant || original.Context.TenantID == nil || *original.Context.TenantID != tenantID ||
		original.Context.TenantMembershipID == nil || *original.Context.TenantMembershipID <= 0 || tenantID <= 1 || ownerAssignmentID <= 0 {
		return nil, errors.New("execution reader requires its dedicated Tenant session and Owner assignment")
	}
	assignment, err := roles.GetAssignment(ctx, tenantID, ownerAssignmentID)
	if err != nil {
		return nil, fmt.Errorf("resolve execution reader Owner assignment: %w", err)
	}
	if assignment == nil || assignment.ID != ownerAssignmentID || assignment.PrincipalID <= 0 || assignment.MembershipID != *original.Context.TenantMembershipID ||
		assignment.RoleKey != ownerRoleKey || assignment.PrincipalType != iam.PrincipalTypeUser {
		return nil, errors.New("execution reader Owner assignment does not match the session")
	}
	role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenantID, RoleKey: "online.execution_monitor_reader", Name: "Execution Monitor reader",
		ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"monitor.execution.read"},
		ActorPrincipalID: actorID, Audit: audit("external-online-parent-monitor-role"),
	})
	if err != nil {
		return nil, fmt.Errorf("create execution reader Monitor role: %w", err)
	}
	if role == nil || role.ID <= 0 {
		return nil, errors.New("execution reader Monitor role was not persisted")
	}
	assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: assignment.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant",
		Reason: "Disposable Online independent Monitor access", ActorPrincipalID: actorID,
		Audit: audit("external-online-parent-monitor-assignment"),
	})
	if err != nil {
		return nil, fmt.Errorf("assign execution reader Monitor role: %w", err)
	}
	if len(assignments) != 1 || assignments[0].ID <= 0 || assignments[0].ID == ownerAssignmentID || assignments[0].PrincipalID != assignment.PrincipalID || assignments[0].MembershipID != assignment.MembershipID {
		return nil, errors.New("execution reader Monitor assignment was not persisted separately")
	}
	// Role changes invalidate the previous authorization-version-bound session.
	return issue(assignment.PrincipalID)
}

func issueSession(ctx context.Context, repository *iam.Repository, service *iam.ContextSelectionService, principalID int64, requestID string) (*iam.IssuedBrowserSession, error) {
	// Session authentication and validation must use the same IAM authority clock.
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, fmt.Errorf("read %s authentication time: %w", requestID, err)
	}
	result, err := service.BeginContextSelection(ctx, iam.BeginContextSelectionInput{
		PrincipalID: principalID,
		Authentication: iam.SessionAuthentication{
			Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1,
			AuthenticatedAt: now,
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
		"ADDP_ONLINE_METRICS_ADMIN_USERNAME",
		"ADDP_ONLINE_METRICS_ADMIN_PASSWORD",
		"ADDP_ONLINE_METRICS_ADMIN_TOTP",
		"ADDP_ONLINE_METRICS_SECURITY_USERNAME",
		"ADDP_ONLINE_METRICS_SECURITY_PASSWORD",
		"ADDP_ONLINE_METRICS_SECURITY_TOTP",
		"ADDP_ONLINE_METRICS_FOREIGN_TENANT_ID",
		"ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID",
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN",
		"ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN",
		"ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN",
		"ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN",
		"ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_OWN_ASSIGNMENT_ID",
		"ADDP_ONLINE_PARENT_ASSIGNMENT_ID",
		"ADDP_ONLINE_PARENT_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_PARENT_USER_USERNAME",
		"ADDP_ONLINE_PARENT_USER_PASSWORD",
		"ADDP_ONLINE_PEER_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN",
		"ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN",
		"ADDP_ONLINE_READ_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_READ_USER_USERNAME",
		"ADDP_ONLINE_READ_USER_PASSWORD",
		"ADDP_ONLINE_TEST_TENANT_ID",
		"ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN",
		"ADDP_ONLINE_TEST_APPROVER_USERNAME",
		"ADDP_ONLINE_TEST_APPROVER_PASSWORD",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
		"ADDP_ONLINE_TEST_USER_USERNAME",
		"ADDP_ONLINE_TEST_USER_PASSWORD",
		"ADDP_ONLINE_TRANSFER_CHECK_USER_USERNAME",
		"ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD",
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
