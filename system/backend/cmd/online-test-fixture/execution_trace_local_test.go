package main

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func privateTraceTestDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func localTraceAdmissionEnvironment() []string {
	return []string{"ADDP_EXECUTION_TRACE_LOCAL=1", "ADDP_EXECUTION_TRACE_RUN_ID=11111111-1111-4111-8111-111111111111", "POSTGRES_DB=addp_online", "POSTGRES_HOST=127.0.0.1"}
}

func TestExecutionTraceLocalAdmissionRejectsOtherProfilesAndDatabaseScopes(t *testing.T) {
	output := filepath.Join(privateTraceTestDir(t), "identities.env")
	base := localTraceAdmissionEnvironment()
	if err := validateExternalEnvironment(base, output, executionTraceLocalSuite); err != nil {
		t.Fatal("dedicated local admission", err)
	}
	for _, suite := range []string{"platform-node-metrics", "manager-internal-artifact-lineage", "orchestrator-execution", ""} {
		if err := validateExternalEnvironment(base, output, suite); err == nil {
			t.Fatal("local marker opened another suite", suite)
		}
	}
	for _, extra := range []string{"GITHUB_ACTIONS=true", "GITHUB_ACTIONS=false", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_HOSTED=0", "ADDP_ONLINE_OWNER_MANAGED=1", "ADDP_ONLINE_LOCAL_SGM=1", "POSTGRES_HOST=remote.example", "POSTGRES_DB=addp_test", "ADDP_EXECUTION_TRACE_LOCAL=0", "ADDP_EXECUTION_TRACE_RUN_ID=00000000-0000-0000-0000-000000000000"} {
		env := append(append([]string(nil), base...), extra)
		if err := validateExternalEnvironment(env, output, executionTraceLocalSuite); err == nil {
			t.Fatal("accepted foreign/ambiguous input", extra)
		}
	}
	if err := validateExternalEnvironment(nil, output, executionTraceLocalSuite); err == nil {
		t.Fatal("suite alone authorized local fixture")
	}
	for _, bad := range []string{"not-a-uuid", "11111111111141118111111111111111", "00000000-0000-0000-0000-000000000000"} {
		if _, err := executionTraceClusterName([]string{"ADDP_EXECUTION_TRACE_RUN_ID=" + bad}); err == nil {
			t.Fatal("noncanonical run UUID")
		}
	}
}

func TestExecutionTraceLocalDatabaseRequiresActualClusterIdentity(t *testing.T) {
	expected, err := executionTraceClusterName(localTraceAdmissionEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != 63 {
		t.Fatal("cluster identity must fit PostgreSQL identifier budget")
	}
	for _, test := range []struct {
		name, database, cluster string
		accepted                bool
	}{
		{"owned", "addp_online", expected, true}, {"shared", "addp_online", "", false}, {"foreign", "addp_online", "other-run", false}, {"shared-database", "addp_test", expected, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			driverName := "execution_trace_local_identity_" + uuid.NewString()
			sql.Register(driverName, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				if err := conn.RegisterFunc("current_database", func() string { return test.database }, true); err != nil {
					return err
				}
				return conn.RegisterFunc("current_setting", func(name string) string {
					if name != "cluster_name" {
						return ""
					}
					return test.cluster
				}, true)
			}})
			db, err := gorm.Open(sqlite.Dialector{DriverName: driverName, DSN: ":memory:"}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := db.DB()
			defer raw.Close()
			err = requireExecutionTraceLocalDatabase(db, localTraceAdmissionEnvironment())
			if (err == nil) != test.accepted {
				t.Fatalf("actual cluster acceptance=%v want=%v", err, test.accepted)
			}
		})
	}
}

func TestExecutionTraceBusinessUserPermissionsStayExact(t *testing.T) {
	want := []string{"orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.delete", "orchestrator.workflow.execute", "develop.task.create", "develop.task.read", "develop.task.delete", "develop.task.execute", "develop.data_read.execute", "monitor.execution.read", "system.execution_authorization.create"}
	got, err := suitePermissions(executionTraceLocalSuite)
	if err != nil || !reflect.DeepEqual(got, want) || needsEngineProvisioner(executionTraceLocalSuite) {
		t.Fatal("local User role drift", err)
	}
	got[0] = "platform.trace.read"
	if executionTraceLocalPermissions[0] != want[0] {
		t.Fatal("suite permission result aliases source")
	}
}

func TestExecutionTraceCredentialsPrivateExactAndNeverContainSessions(t *testing.T) {
	directory := privateTraceTestDir(t)
	output := filepath.Join(directory, "identity.env")
	values := map[string]string{}
	for _, key := range executionTraceLocalEnvironmentKeys {
		values[key] = "private'quoted"
	}
	if err := writeExecutionTraceLocalEnvironment(output, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output not owner-only")
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ACCESS_TOKEN") || strings.Contains(string(raw), "SESSION") || len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 11 || !strings.Contains(string(raw), "'private'\"'\"'quoted'") {
		t.Fatal("credentials content drift")
	}
	if err := writeExecutionTraceLocalEnvironment(output, values); err == nil {
		t.Fatal("replaced existing credential file")
	}
	values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"] = "forbidden"
	if err := writeExecutionTraceLocalEnvironment(filepath.Join(directory, "other.env"), values); err == nil {
		t.Fatal("session token exported")
	}
	// A dedicated fixture can establish IAM facts, never manufacture a session.
	source, err := parser.ParseFile(token.NewFileSet(), "execution_trace_local.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch target := call.Fun.(type) {
		case *ast.Ident:
			name = target.Name
		case *ast.SelectorExpr:
			name = target.Sel.Name
		}
		for _, forbidden := range []string{"issueSession", "createPermissionFixture", "NewTokenFamilyService", "NewContextSelectionService", "IssueBrowserSession", "BeginContextSelection"} {
			if name == forbidden {
				t.Errorf("local identity preparation manufactured authentication through %s", name)
			}
		}
		return true
	})
}

func TestExecutionTraceOutputRejectsAliasedOrPublicPaths(t *testing.T) {
	directory := privateTraceTestDir(t)
	output := filepath.Join(directory, "identity.env")
	if err := validateExecutionTraceOutput("identity.env"); err == nil {
		t.Fatal("relative output")
	}
	public := filepath.Join(directory, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutionTraceOutput(filepath.Join(public, "identity.env")); err == nil {
		t.Fatal("public credential directory")
	}
	alias := filepath.Join(directory, "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutionTraceOutput(filepath.Join(alias, "identity.env")); err == nil {
		t.Fatal("symlink credential directory")
	}
	if err := os.Symlink(filepath.Join(directory, "target"), output); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutionTraceOutput(output); err == nil {
		t.Fatal("symlink credential file")
	}
}
