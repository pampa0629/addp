package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const executionTraceLocalSuite = "execution-trace-local"

var executionTraceLocalPermissions = []string{
	"orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.delete", "orchestrator.workflow.execute",
	"develop.task.create", "develop.task.read", "develop.task.delete", "develop.task.execute", "develop.data_read.execute",
	"monitor.execution.read", "system.execution_authorization.create",
}

var executionTraceLocalEnvironmentKeys = []string{
	"ADDP_EXECUTION_TRACE_ADMIN_USERNAME", "ADDP_EXECUTION_TRACE_ADMIN_PASSWORD", "ADDP_EXECUTION_TRACE_ADMIN_TOTP",
	"ADDP_EXECUTION_TRACE_SECURITY_USERNAME", "ADDP_EXECUTION_TRACE_SECURITY_PASSWORD", "ADDP_EXECUTION_TRACE_SECURITY_TOTP",
	"ADDP_EXECUTION_TRACE_PREPARER_USERNAME", "ADDP_EXECUTION_TRACE_PREPARER_PASSWORD",
	"ADDP_EXECUTION_TRACE_USER_USERNAME", "ADDP_EXECUTION_TRACE_USER_PASSWORD", "ADDP_EXECUTION_TRACE_TENANT_ID",
}

func localTraceEnvironment(environment []string) (map[string]string, error) {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; exists {
			return nil, errors.New("execution trace local environment is ambiguous")
		}
		values[key] = value
	}
	return values, nil
}

func executionTraceClusterName(environment []string) (string, error) {
	values, err := localTraceEnvironment(environment)
	if err != nil {
		return "", err
	}
	id, err := uuid.Parse(values["ADDP_EXECUTION_TRACE_RUN_ID"])
	if err != nil || id == uuid.Nil || id.String() != values["ADDP_EXECUTION_TRACE_RUN_ID"] {
		return "", errors.New("execution trace local run identity is invalid")
	}
	return "addp-execution-trace-local-" + id.String(), nil
}

func validateExecutionTraceLocalEnvironment(environment []string, output, suite string) error {
	values, err := localTraceEnvironment(environment)
	if err != nil {
		return err
	}
	if suite != executionTraceLocalSuite || values["ADDP_EXECUTION_TRACE_LOCAL"] != "1" {
		return errors.New("execution trace fixture requires its dedicated local suite and marker")
	}
	for _, key := range []string{"GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED", "ADDP_ONLINE_LOCAL_SGM"} {
		if values[key] != "" {
			return errors.New("execution trace local fixture rejects GitHub and other Online profiles")
		}
	}
	if values["POSTGRES_DB"] != onlineDatabase || (values["POSTGRES_HOST"] != "127.0.0.1" && values["POSTGRES_HOST"] != "localhost") {
		return errors.New("execution trace local fixture requires its disposable loopback addp_online database")
	}
	if _, err := executionTraceClusterName(environment); err != nil {
		return err
	}
	return validateExecutionTraceOutput(output)
}

func validateExecutionTraceOutput(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("execution trace credential output must be an absolute private path")
	}
	info, err := os.Lstat(filepath.Dir(output))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("execution trace credential directory must already be private")
	}
	owned, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owned.Uid) != os.Geteuid() {
		return errors.New("execution trace credential directory must belong to the fixture user")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return errors.New("execution trace credential output must not already exist")
	}
	return nil
}

// The cluster marker is set only on the owned PostgreSQL container by this
// suite's lifecycle. A loopback hostname or an asserted profile alone cannot
// authorize the shared local addp-postgres (even when database names match).
func requireExecutionTraceLocalDatabase(db *gorm.DB, environment []string) error {
	expected, err := executionTraceClusterName(environment)
	if err != nil {
		return err
	}
	var identity struct {
		Database string
		Cluster  string
	}
	if err := db.Raw("SELECT current_database() AS database, current_setting('cluster_name') AS cluster").Scan(&identity).Error; err != nil {
		return errors.New("cannot verify execution trace disposable database identity")
	}
	if identity.Database != onlineDatabase || identity.Cluster != expected {
		return errors.New("execution trace fixture refuses a shared or foreign PostgreSQL cluster")
	}
	return nil
}

// This creates prerequisites through formal IAM services, without issuing any
// session. The harness must use public password login/MFA/AuthContext for every
// User. The separate Tenant initial administrator only prepares this run's
// engine and grants; it never serves as business or Platform Trace identity.
func prepareExecutionTraceLocalIdentities(db *gorm.DB, cfg *config.Config, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repo := iam.NewRepository(db)
	identity := iam.NewIdentityService(repo, nil)
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return err
	}
	bootstrap, err := iam.NewBootstrapService(repo, identity, cipher, time.Hour, nil, nil)
	if err != nil {
		return err
	}
	secret, _, err := bootstrap.Prepare(ctx)
	if err != nil {
		return err
	}
	inputs, err := platformBootstrapInputs(time.Now(), "local-execution-trace", "Local execution trace")
	if err != nil {
		return err
	}
	if _, err := bootstrap.Apply(ctx, iam.BootstrapApplyInput{BootstrapSecret: secret, Administrators: inputs}); err != nil {
		return fmt.Errorf("bootstrap execution trace identities: %w", err)
	}
	values := map[string]string{}
	for i, prefix := range []string{"ADDP_EXECUTION_TRACE_ADMIN", "ADDP_EXECUTION_TRACE_SECURITY"} {
		values[prefix+"_USERNAME"], values[prefix+"_PASSWORD"], values[prefix+"_TOTP"] = inputs[i].Username, inputs[i].Password, inputs[i].TOTPSecret
	}
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		return err
	}
	preparer, password, err := createUserCredentials(ctx, identity, "local-execution-trace-preparer")
	if err != nil {
		return err
	}
	tenants := iam.NewPlatformTenantService(repo, clock)
	tenant, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "local-execution-trace", Name: "Local execution trace", InitialAdministratorPrincipalID: preparer.PrincipalID, ActorPrincipalID: preparer.PrincipalID, Audit: audit("local-execution-trace-tenant")})
	if err != nil {
		return err
	}
	values["ADDP_EXECUTION_TRACE_PREPARER_USERNAME"], values["ADDP_EXECUTION_TRACE_PREPARER_PASSWORD"] = "local-execution-trace-preparer", password
	consumer, password, err := createUserCredentials(ctx, identity, "local-execution-trace-user")
	if err != nil {
		return err
	}
	memberships := iam.NewTenantMembershipService(repo, clock)
	membership, err := memberships.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: consumer.PrincipalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &preparer.PrincipalID, Audit: audit("local-execution-trace-membership")})
	if err != nil {
		return err
	}
	roles := iam.NewTenantRoleService(repo, clock)
	role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant.ID, RoleKey: "local.execution_trace", Name: "Local execution trace business user", PermissionKeys: append([]string(nil), executionTraceLocalPermissions...), ActorPrincipalID: preparer.PrincipalID, Audit: audit("local-execution-trace-role")})
	if err != nil {
		return err
	}
	if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleIDs: []int64{role.ID}, Reason: "Disposable local execution trace acceptance", ActorPrincipalID: preparer.PrincipalID, Audit: audit("local-execution-trace-assignment")}); err != nil {
		return err
	}
	values["ADDP_EXECUTION_TRACE_USER_USERNAME"], values["ADDP_EXECUTION_TRACE_USER_PASSWORD"], values["ADDP_EXECUTION_TRACE_TENANT_ID"] = "local-execution-trace-user", password, fmt.Sprint(tenant.ID)
	return writeExecutionTraceLocalEnvironment(output, values)
}

func writeExecutionTraceLocalEnvironment(output string, values map[string]string) error {
	if err := validateExecutionTraceOutput(output); err != nil {
		return err
	}
	if len(values) != len(executionTraceLocalEnvironmentKeys) {
		return errors.New("execution trace credentials have an unexpected field set")
	}
	keys := append([]string(nil), executionTraceLocalEnvironmentKeys...)
	sort.Strings(keys)
	for _, key := range keys {
		if values[key] == "" || strings.ContainsAny(values[key], "\x00\r\n") {
			return errors.New("execution trace credentials are incomplete")
		}
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private execution trace credential file")
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			_ = os.Remove(output)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return errors.New("cannot protect execution trace credential file")
	}
	for _, key := range keys {
		quoted := strings.ReplaceAll(values[key], "'", "'\"'\"'")
		if _, err := fmt.Fprintf(file, "export %s='%s'\n", key, quoted); err != nil {
			return errors.New("cannot write execution trace credentials")
		}
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close execution trace credential file")
	}
	success = true
	return nil
}
