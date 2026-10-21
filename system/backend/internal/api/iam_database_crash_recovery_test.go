package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"
	redisv9 "github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test owns its container from creation to destruction. It never uses the
// caller's PostgreSQL DSN: crashing a shared test database would affect all its
// databases, including the developer's persisted identities.
func TestIAMDatabaseCrashRecovery(t *testing.T) {
	if os.Getenv("ADDP_SYSTEM_DATABASE_CRASH_RECOVERY_INTEGRATION") != "1" {
		t.Skip("run make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS='--test database-crash-recovery'")
	}
	owned := newIAMRecoveryPostgres(t)
	db, err := gorm.Open(postgres.Open(owned.dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open owned PostgreSQL")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := migration.NewRunner(owned.dsn).Run(ctx); err != nil {
		t.Fatal("apply migrations", err)
	}
	for _, setting := range []string{"fsync", "synchronous_commit", "full_page_writes"} {
		var value string
		if err := pool.QueryRowContext(ctx, "SELECT current_setting($1)", setting).Scan(&value); err != nil || value != "on" {
			t.Fatalf("durability setting %s must be on", setting)
		}
	}
	secrets := testBuiltinServiceClientSecrets("crash-recovery")
	provision := func(t *testing.T) {
		t.Helper()
		p, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Apply(ctx, secrets); err != nil {
			t.Fatal("provision unchanged service credentials", err)
		}
	}
	provision(t)
	runtime, router := newIAMRecoveryRuntime(t, db)
	cipher, err := iam.NewMFACredentialCipher(testIAMRuntimeConfig().IAMMFAEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapTime := time.Now().UTC().Add(-2 * time.Minute).Truncate(30 * time.Second)
	bootstrap, err := iam.NewBootstrapService(runtime.Repository, runtime.IdentityService, cipher, time.Hour, nil, func() time.Time { return bootstrapTime })
	if err != nil {
		t.Fatal(err)
	}
	proof, _, err := bootstrap.Prepare(ctx)
	if err != nil {
		t.Fatal("prepare bootstrap", err)
	}
	const totpSecret = "JBSWY3DPEHPK3PXP"
	result, err := bootstrap.Apply(ctx, iam.BootstrapApplyInput{BootstrapSecret: proof, Administrators: []iam.BootstrapAdministratorInput{
		apiBootstrapAdministrator(t, "platform.system_administrator", "crash-system", totpSecret, bootstrapTime),
		apiBootstrapAdministrator(t, "platform.security_administrator", "crash-security", "KRSXG5DSNFXGOIDB", bootstrapTime),
		apiBootstrapAdministrator(t, "platform.audit_administrator", "crash-audit", "MFRGGZDFMZTWQ2LK", bootstrapTime),
	}})
	if err != nil {
		t.Fatal("apply bootstrap", err)
	}
	user, err := runtime.IdentityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: "crash-tenant-admin", Password: "Crash-recovery-tenant-password!", DisplayName: "Retained Tenant Administrator",
	})
	if err != nil {
		t.Fatal("create tenant user", err)
	}
	tenant, err := runtime.PlatformTenantService.Create(ctx, iam.CreateTenantInput{
		Code: "crash-recovery", Name: "Crash Recovery", InitialAdministratorPrincipalID: user.PrincipalID,
		ActorPrincipalID: result.PrincipalIDs["platform.system_administrator"],
	})
	if err != nil {
		t.Fatal("create tenant and administrator assignment", err)
	}
	login := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
		"username": "crash-system", "password": "Bootstrap-password-crash-system!",
	}, nil)
	var challenge IAMBrowserLoginResponse
	decodeIAMResponse(t, login, &challenge)
	if login.Code != http.StatusOK || challenge.MFA == nil {
		t.Fatal("original password did not reach MFA")
	}
	code, err := totp.GenerateCode(totpSecret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	verification := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/mfa-verifications", map[string]any{
		"challenge_token": challenge.MFA.ChallengeToken, "code": code,
	}, nil)
	var session IAMBrowserLoginResponse
	decodeIAMResponse(t, verification, &session)
	if verification.Code != http.StatusOK || session.Session == nil {
		t.Fatal("original MFA did not issue a session")
	}
	token := session.Session.AccessToken
	beforeContext := recoveryAuthContext(t, router, token)
	if len(beforeContext.Authorization.RoleAssignments) == 0 || len(beforeContext.Authorization.RoleAssignments[0].Permissions) == 0 {
		t.Fatal("recovery fixture must contain effective permissions")
	}

	parent := t
	for _, mode := range []string{"backend_SIGQUIT", "container_SIGKILL"} {
		if !t.Run(mode, func(t *testing.T) {
			before := snapshotIAMRecoveryTables(t, pool)
			var started time.Time
			if err := pool.QueryRowContext(ctx, "SELECT pg_postmaster_start_time()").Scan(&started); err != nil {
				t.Fatal(err)
			}
			// The interrupted transaction contains both a changed account and a
			// new authorization. Neither may survive or invalidate prior sessions.
			tx, err := pool.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, `UPDATE system.users SET display_name='uncommitted account change' WHERE id=$1`, user.PrincipalID); err != nil {
				t.Fatal(err)
			}
			write, err := tx.ExecContext(ctx, `INSERT INTO system.role_assignments
			    (principal_id,role_id,scope_type,tenant_id,source_type,grant_reason,created_by_principal_id)
			    SELECT $1,id,'tenant',$2,'manual','uncommitted crash probe',$1
			    FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.source_data_authorizer'`, user.PrincipalID, tenant.ID)
			if err != nil {
				t.Fatal("prepare uncommitted role assignment", err)
			}
			if n, err := write.RowsAffected(); err != nil || n != 1 {
				t.Fatal("uncommitted authorization fixture was not written")
			}
			var backendPID int
			if err := tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil || backendPID <= 1 {
				t.Fatal("identify owned transaction backend")
			}
			owned.requireIdentity(t)
			if mode == "backend_SIGQUIT" {
				owned.docker(t, "exec", owned.id, "sh", "-c", `kill -QUIT "$1"`, "sh", strconv.Itoa(backendPID))
				owned.waitLog(t, fmt.Sprintf("server process (PID %d) exited with exit code 2", backendPID))
			} else {
				owned.docker(t, "kill", "--signal=KILL", owned.id)
				owned.requireIdentity(t)
				owned.docker(t, "start", owned.id)
				fact := owned.requireIdentity(t)
				ports := fact.NetworkSettings.Ports["5432/tcp"]
				u, err := url.Parse(owned.dsn)
				if err != nil || len(ports) != 1 || ports[0].HostPort != u.Port() {
					t.Fatal("Docker reassigned the test's automatic published port on container restart")
				}
			}
			waitIAMRecoveryReady(t, pool)
			if err := tx.Commit(); err == nil {
				t.Fatal("interrupted transaction unexpectedly committed")
			}
			var recoveredStart time.Time
			if err := pool.QueryRowContext(ctx, "SELECT pg_postmaster_start_time()").Scan(&recoveredStart); err != nil {
				t.Fatal(err)
			}
			if (mode == "backend_SIGQUIT") != started.Equal(recoveredStart) {
				t.Fatal("expected internal recovery or container restart was not observed")
			}
			if err := migration.NewRunner(owned.dsn).Run(ctx); err != nil {
				t.Fatal("restart migration runner", err)
			}
			provision(t)
			runtime, router = newIAMRecoveryRuntime(parent, db)
			after := snapshotIAMRecoveryTables(t, pool)
			if !reflect.DeepEqual(before, after) {
				changed := []string{}
				for table, digest := range before {
					if after[table] != digest {
						changed = append(changed, table)
					}
				}
				slices.Sort(changed)
				t.Fatalf("IAM facts changed across recovery: tables=%v counts=%d/%d", changed, len(before), len(after))
			}
			if !reflect.DeepEqual(beforeContext, recoveryAuthContext(t, router, token)) {
				t.Fatal("original session's identity or effective permissions changed")
			}
			for _, account := range []struct {
				username, password string
				principalID        int64
			}{
				{"crash-system", "Bootstrap-password-crash-system!", result.PrincipalIDs["platform.system_administrator"]},
				{"crash-tenant-admin", "Crash-recovery-tenant-password!", user.PrincipalID},
			} {
				authenticated, err := runtime.IdentityService.AuthenticateLocalAccount(ctx, account.username, account.password, iam.AuditMetadata{})
				if err != nil || authenticated == nil || authenticated.PrincipalID != account.principalID {
					t.Fatal("original account password or identity was not retained")
				}
			}
			if _, err := runtime.IdentityService.AuthenticateLocalAccount(ctx, "crash-tenant-admin", "wrong-password", iam.AuditMetadata{}); !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatal("recovery accepted an incorrect password")
			}
			replayed := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/mfa-verifications", map[string]any{
				"challenge_token": challenge.MFA.ChallengeToken, "code": code,
			}, nil)
			if replayed.Code != http.StatusUnauthorized {
				t.Fatal("recovery reused an already-consumed MFA challenge")
			}
			t.Logf("%d IAM tables retained; uncommitted account/role changes rolled back; original identity, password, MFA replay protection and permissions verified", len(before))
		}) {
			t.FailNow()
		}
	}
}

func newIAMRecoveryRuntime(t *testing.T, db *gorm.DB) (*IAMRuntime, http.Handler) {
	t.Helper()
	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	runtime.ExecutionAuthorizationHandler = &IAMExecutionAuthorizationHandler{}
	runtime.NotebookSessionAuthorizationHandler = &IAMNotebookSessionAuthorizationHandler{}
	runtime.TaskAuthorizationSubjectHandler = &IAMTaskAuthorizationSubjectHandler{}
	redisServer := miniredis.RunT(t)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := RegisterIAMRoutes(router.Group("/api/v1/system"), runtime, redisClient); err != nil {
		t.Fatal(err)
	}
	return runtime, router
}

func recoveryAuthContext(t *testing.T, router http.Handler, token string) commonauth.AuthContext {
	t.Helper()
	response := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/auth/context", nil, map[string]string{"Authorization": "Bearer " + token})
	if response.Code != http.StatusOK {
		t.Fatal("original access token cannot resolve after recovery")
	}
	var value commonauth.AuthContext
	decodeIAMResponse(t, response, &value)
	return value
}

func snapshotIAMRecoveryTables(t *testing.T, db *sql.DB) map[string][32]byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT c.relname,c.relpersistence FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='system' AND c.relkind IN ('r','p') ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name, persistence string
		if err := rows.Scan(&name, &persistence); err != nil {
			t.Fatal(err)
		}
		if persistence != "p" {
			t.Fatalf("IAM table %s is not WAL logged", name)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	digests := make(map[string][32]byte, len(names))
	for _, name := range names {
		var content string
		query := `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text COLLATE "C"),'[]'::jsonb)::text FROM ` + pgx.Identifier{"system", name}.Sanitize() + ` r`
		if err := db.QueryRowContext(ctx, query).Scan(&content); err != nil {
			t.Fatal("snapshot IAM table", name, err)
		}
		// Password hashes, encrypted MFA material and token digests remain in
		// memory; failure output contains only the names of changed tables.
		digests[name] = sha256.Sum256([]byte(content))
	}
	if len(digests) < 8 {
		t.Fatal("IAM snapshot unexpectedly empty")
	}
	return digests
}

const iamRecoveryPostgresImage = "postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c"
const iamRecoveryOwnerLabel = "addp.test.iam-crash-recovery"

type iamRecoveryPostgres struct {
	id, nonce, dsn string
	volumes        []string
}
type iamRecoveryContainer struct {
	ID         string `json:"Id"`
	Config     struct{ Labels map[string]string }
	Mounts     []struct{ Type, Name, Destination string }
	HostConfig struct {
		PortBindings map[string][]struct{ HostIP, HostPort string }
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostIP, HostPort string }
	}
}

func (p *iamRecoveryPostgres) docker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", args...)
	var output []byte
	var err error
	if args[0] == "run" {
		// Image-pull progress goes to stderr; stdout must contain only the ID.
		output, err = command.Output()
	} else {
		output, err = command.CombinedOutput()
	}
	if err != nil {
		t.Fatalf("owned PostgreSQL Docker %s failed: %v", args[0], err)
	}
	return strings.TrimSpace(string(output))
}

func (p *iamRecoveryPostgres) requireIdentity(t *testing.T) iamRecoveryContainer {
	t.Helper()
	var fact iamRecoveryContainer
	if err := json.Unmarshal([]byte(p.docker(t, "inspect", "--format", "{{json .}}", p.id)), &fact); err != nil {
		t.Fatal("decode owned PostgreSQL identity")
	}
	if fact.ID != p.id || fact.Config.Labels[iamRecoveryOwnerLabel] != p.nonce {
		t.Fatal("refusing foreign PostgreSQL container")
	}
	bindings := fact.HostConfig.PortBindings["5432/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
		t.Fatal("owned PostgreSQL must bind only to loopback")
	}
	volumes := []string{}
	for _, mount := range fact.Mounts {
		if mount.Type == "volume" && mount.Destination == "/var/lib/postgresql/data" {
			volumes = append(volumes, mount.Name)
		} else {
			t.Fatal("unexpected owned PostgreSQL mount")
		}
	}
	if len(volumes) != 1 || len(p.volumes) != 0 && !slices.Equal(p.volumes, volumes) {
		t.Fatal("owned PostgreSQL volume identity changed")
	}
	p.volumes = volumes
	return fact
}

func newIAMRecoveryPostgres(t *testing.T) *iamRecoveryPostgres {
	t.Helper()
	p := &iamRecoveryPostgres{nonce: uuid.NewString()}
	name := "addp-iam-crash-test-" + p.nonce
	state := os.Getenv("ADDP_SYSTEM_DATABASE_CRASH_RECOVERY_STATE_DIR")
	if state == "" {
		t.Fatal("crash recovery requires the standard gate's cleanup directory")
	}
	if err := os.WriteFile(filepath.Join(state, "crash-recovery-owner"), []byte(p.nonce), 0600); err != nil {
		t.Fatal("record crash-recovery cleanup ownership", err)
	}
	// Register cleanup before creation, including timeout/partial-run failures.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if p.id == "" {
			out, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--no-trunc", "--filter", "label="+iamRecoveryOwnerLabel+"="+p.nonce).Output()
			if err != nil {
				t.Error("cannot verify partial container creation")
				return
			}
			p.id = strings.TrimSpace(string(out))
			if p.id == "" {
				return
			}
		}
		p.requireIdentity(t)
		p.docker(t, "rm", "--force", "--volumes", p.id)
		if p.docker(t, "ps", "-aq", "--no-trunc", "--filter", "label="+iamRecoveryOwnerLabel+"="+p.nonce) != "" {
			t.Error("owned container cleanup left residuals")
		}
		remaining := strings.Fields(p.docker(t, "volume", "ls", "--format", "{{.Name}}"))
		for _, volume := range p.volumes {
			if slices.Contains(remaining, volume) {
				t.Error("owned volume cleanup left residuals")
			}
		}
		t.Log("owned PostgreSQL container and volume cleanup verified")
	})
	// Docker can reassign an automatically published port on container start.
	// Choose an OS-assigned ephemeral port, then bind it explicitly for this
	// container's lifetime. A competing bind fails creation; no existing server
	// is reused or stopped.
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve owned PostgreSQL port", err)
	}
	listenAddress := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	p.id = p.docker(t, "run", "--detach", "--name", name, "--label", iamRecoveryOwnerLabel+"="+p.nonce,
		"--publish", listenAddress+":5432", "--volume", "/var/lib/postgresql/data", "--memory", "512m", "--cpus", "1", "--pids-limit", "128",
		"--env", "POSTGRES_DB=addp_iam_test", "--env", "POSTGRES_USER=iam_crash_test", "--env", "POSTGRES_PASSWORD=disposable-crash-test-password",
		iamRecoveryPostgresImage)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.id) {
		p.id = ""
		t.Fatal("Docker did not return one container ID")
	}
	fact := p.requireIdentity(t)
	ports := fact.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 || ports[0].HostIP != "127.0.0.1" {
		t.Fatal("invalid owned PostgreSQL port")
	}
	port, err := strconv.Atoi(ports[0].HostPort)
	if err != nil || port <= 0 {
		t.Fatal("invalid owned PostgreSQL port")
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword("iam_crash_test", "disposable-crash-test-password"), Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Path: "/addp_iam_test", RawQuery: "sslmode=disable&connect_timeout=2"}
	p.dsn = u.String()
	pool, err := sql.Open("pgx", p.dsn)
	if err != nil {
		t.Fatal("open owned readiness connection")
	}
	defer pool.Close()
	waitIAMRecoveryReady(t, pool)
	return p
}

func waitIAMRecoveryReady(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := db.PingContext(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("owned PostgreSQL did not become ready")
}

func (p *iamRecoveryPostgres) waitLog(t *testing.T, expected string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.docker(t, "logs", p.id), expected) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected PostgreSQL backend crash was not recorded")
}
