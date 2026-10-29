package eventfixture_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func hostAllowed(env map[string]string, platform string) bool {
	return platform == "linux" && env["GITHUB_ACTIONS"] == "true" && env["RUNNER_OS"] == "Linux" &&
		env["ADDP_ONLINE_HOSTED"] == "1" && env["ADDP_ONLINE_HOST"] == "1" && env["ADDP_ONLINE_TEST"] == "1" &&
		env["ONLINE_SUITE"] == "execution-events-pagination" && env["POSTGRES_DB"] == "addp_online" &&
		env["ADDP_ONLINE_OWNER_MANAGED"] == "" && env["ADDP_EXECUTION_TRACE_LOCAL"] == ""
}
func TestEventPaginationFixtureHostGuard(t *testing.T) {
	env := map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "ADDP_ONLINE_HOSTED": "1", "ADDP_ONLINE_HOST": "1", "ADDP_ONLINE_TEST": "1", "ONLINE_SUITE": "execution-events-pagination", "POSTGRES_DB": "addp_online"}
	if !hostAllowed(env, "linux") {
		t.Fatal("valid isolated host rejected")
	}
	for k := range env {
		bad := map[string]string{}
		for key, value := range env {
			bad[key] = value
		}
		bad[k] = "invalid"
		if hostAllowed(bad, "linux") {
			t.Fatal("invalid host accepted", k)
		}
	}
	if hostAllowed(env, "darwin") {
		t.Fatal("personal host accepted")
	}
	env["ADDP_ONLINE_OWNER_MANAGED"] = "1"
	if hostAllowed(env, "linux") {
		t.Fatal("mixed profile accepted")
	}
}
func need(t *testing.T, condition bool, code string) {
	t.Helper()
	if !condition {
		t.Fatal(code)
	}
}
func outside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func TestEventPaginationFixturePrivateDirectories(t *testing.T) {
	if outside("/private", "/private/child") || !outside("/private", "/other") {
		t.Fatal("directory boundary invalid")
	}
}
func subject(t *testing.T) authorization.AuthContext {
	t.Helper()
	u, err := url.Parse(os.Getenv("SYSTEM_URL"))
	need(t, err == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/"), "fixture_system_boundary")
	req, err := http.NewRequest("GET", strings.TrimRight(u.String(), "/")+"/api/v1/system/auth/context", nil)
	need(t, err == nil, "fixture_identity_request")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"))
	client := http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	need(t, err == nil, "fixture_identity_unavailable")
	defer response.Body.Close()
	need(t, response.StatusCode == 200, "fixture_identity_denied")
	identity, err := authorization.DecodeAuthContext(io.LimitReader(response.Body, 4<<20))
	need(t, err == nil, "fixture_identity_invalid")
	need(t, identity.Token.Type == "first_party_access_token" && identity.Delegation == nil && identity.Principal.Type == "user" && identity.Context.Type == "tenant" && identity.Context.TenantID != nil && *identity.Context.TenantID == os.Getenv("ADDP_ONLINE_TEST_TENANT_ID") && identity.Context.TenantMembershipID != nil, "fixture_identity_scope")
	permissions := map[string]bool{}
	for _, a := range identity.Authorization.RoleAssignments {
		for _, p := range a.Permissions {
			permissions[p] = true
		}
	}
	need(t, len(permissions) == 2 && permissions["monitor.execution.read"] && permissions["meta.scan_task.read"], "fixture_identity_permissions")
	return identity
}
func fixtureDB(t *testing.T, root string) *gorm.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "inspect", "addp-postgres").Output()
	need(t, err == nil, "fixture_container_unavailable")
	var containers []struct {
		ID     string `json:"Id"`
		State  struct{ Running bool }
		Config struct {
			Labels map[string]string
			Env    []string
		}
		NetworkSettings struct {
			Ports map[string][]struct {
				IP   string `json:"HostIp"`
				Port string `json:"HostPort"`
			}
		}
	}
	need(t, json.Unmarshal(output, &containers) == nil && len(containers) == 1, "fixture_container_invalid")
	c := containers[0]
	need(t, len(c.ID) == 64 && c.ID == os.Getenv("ADDP_ONLINE_RETENTION_POSTGRES_ID") && c.State.Running && c.Config.Labels["com.docker.compose.project"] == "addp-infra" && c.Config.Labels["com.docker.compose.service"] == "postgres" && c.Config.Labels["com.docker.compose.project.working_dir"] == root, "fixture_container_not_owned")
	values := map[string]string{}
	for _, entry := range c.Config.Env {
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) == 2 {
			values[kv[0]] = kv[1]
		}
	}
	ports := c.NetworkSettings.Ports["5432/tcp"]
	need(t, len(ports) == 1 && ports[0].IP == "127.0.0.1" && values["POSTGRES_DB"] == "addp_online" && values["POSTGRES_USER"] == os.Getenv("POSTGRES_USER") && values["POSTGRES_PASSWORD"] != "" && values["POSTGRES_PASSWORD"] == os.Getenv("POSTGRES_PASSWORD"), "fixture_database_boundary")
	address := url.URL{Scheme: "postgres", User: url.UserPassword(values["POSTGRES_USER"], values["POSTGRES_PASSWORD"]), Host: "127.0.0.1:" + ports[0].Port, Path: "/addp_online", RawQuery: "sslmode=disable"}
	db, err := gorm.Open(postgres.Open(address.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	need(t, err == nil, "fixture_database_open")
	connection, err := db.DB()
	need(t, err == nil, "fixture_database_connection")
	connection.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = connection.Close() })
	return db
}
func TestHostedExecutionEventPaginationFixture(t *testing.T) {
	if os.Getenv("ADDP_EXECUTION_EVENT_FIXTURE") != "1" {
		t.Skip("only the explicit isolated Hosted fixture entry may write histories")
	}
	env := map[string]string{}
	for _, v := range os.Environ() {
		kv := strings.SplitN(v, "=", 2)
		env[kv[0]] = kv[1]
	}
	need(t, hostAllowed(env, runtime.GOOS), "fixture_host_denied")
	_, file, _, ok := runtime.Caller(0)
	need(t, ok, "fixture_source_identity")
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	_, err := os.Stat(filepath.Join(root, ".env"))
	need(t, os.IsNotExist(err), "fixture_personal_environment")
	output, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	need(t, err == nil && len(output) == 0, "fixture_dirty_source")
	secret := os.Getenv("ADDP_ONLINE_SECRET_DIR")
	artifact := os.Getenv("ADDP_ONLINE_ARTIFACT_DIR")
	need(t, filepath.IsAbs(secret) && filepath.IsAbs(artifact) && outside(root, artifact) && outside(root, secret) && outside(secret, artifact) && outside(artifact, secret), "fixture_private_boundary")
	info, err := os.Lstat(secret)
	need(t, err == nil && info.IsDir() && info.Mode().Perm()&0077 == 0, "fixture_private_permissions")
	path := filepath.Join(secret, "event-pagination-fixture.json")
	_, err = os.Lstat(path)
	need(t, os.IsNotExist(err), "fixture_replay_denied")
	identity := subject(t)
	tenant, err := strconv.Atoi(*identity.Context.TenantID)
	need(t, err == nil && tenant > 1, "fixture_tenant")
	principal, err := strconv.ParseInt(identity.Principal.ID, 10, 64)
	need(t, err == nil && principal > 0, "fixture_principal")
	member, err := strconv.ParseInt(*identity.Context.TenantMembershipID, 10, 64)
	need(t, err == nil && member > 0, "fixture_membership")
	version, err := strconv.ParseInt(identity.Authorization.AuthorizationVersion, 10, 64)
	need(t, err == nil && version > 0, "fixture_authorization_version")
	db := fixtureDB(t, root)
	var count int64
	need(t, db.Model(&execution.TaskExecution{}).Count(&count).Error == nil && count == 0, "fixture_history_not_empty")
	nonce := uuid.NewString()
	ids := []string{}
	allIDs := map[string][]int64{}
	day := time.Now().UTC().Format("2006-01-02")
	for _, name := range []string{"quota", "history"} {
		id := uuid.NewString()
		trigger := int(principal)
		label := "online-events-" + name
		item := execution.TaskExecution{ExecutionID: id, TenantID: tenant, Module: "meta", TaskType: "scan", Source: "meta", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", TriggeredBy: &trigger, ActorPrincipalID: &principal, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, SourceTaskName: &label,
			ExecutionConfig: models.JSONMap{"private_fixture": nonce}, Metadata: models.JSONMap{"result": nonce, "private_fixture": nonce}}
		var lease *execution.Lease
		err = db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Create(&item).Error; e != nil {
				return e
			}
			_, l, e := execution.ClaimNext(context.Background(), tx.Where("execution_id = ?", id), execution.ClaimOptions{Module: "meta", TaskType: "scan", WorkerID: "online-events-fixture", LeaseDuration: 10 * time.Minute})
			lease = l
			return e
		})
		need(t, err == nil && lease != nil && lease.ExecutionID == id, "fixture_claim_failed")
		if name == "history" {
			past := make([]execution.Event, 600)
			for i := range past {
				past[i] = execution.Event{ExecutionID: id, TenantID: tenant, Module: "meta", TaskType: "scan", Attempt: 1, OccurredAt: time.Now().UTC().Add(-24 * time.Hour), Kind: "progress", Counters: models.JSONMap{"batch_index": i}}
			}
			need(t, db.CreateInBatches(past, 100).Error == nil, "fixture_history_prepare")
		}
		need(t, execution.AppendBoundedEvent(context.Background(), db, *lease, execution.EventInput{Kind: "started"}) == nil, "fixture_started")
		limit := 1003
		if name == "history" {
			limit = 599
		}
		for i := 1; i <= limit; i++ {
			need(t, execution.UpdateWithEvent(context.Background(), db, *lease, map[string]interface{}{"progress": 50}, execution.EventInput{Kind: "progress", Counters: map[string]int64{"records_read": int64(i)}}) == nil, "fixture_progress_write")
		}
		need(t, execution.CompleteWithEvent(context.Background(), db, *lease, "success", map[string]interface{}{"progress": 100}) == nil, "fixture_completion")
		var events []execution.Event
		need(t, db.Where("execution_id = ?", id).Order("id").Find(&events).Error == nil, "fixture_events_read")
		want := 1000
		if name == "history" {
			want = 1201
		}
		need(t, len(events) == want, "fixture_event_count")
		trunc := 0
		for _, e := range events {
			allIDs[name] = append(allIDs[name], e.ID)
			if e.Kind == "truncated" {
				trunc++
			}
		}
		if name == "quota" {
			last := events[len(events)-1]
			need(t, trunc == 1 && last.Kind == "truncated" && last.StepID == "" && len(last.Counters) == 0, "fixture_real_quota_marker")
		} else {
			need(t, trunc == 0, "fixture_history_false_truncation")
		}
		var final execution.TaskExecution
		need(t, db.Where("execution_id = ?", id).First(&final).Error == nil && final.Status == "success" && final.Progress == 100, "fixture_terminal_state")
		ids = append(ids, id)
	}
	need(t, time.Now().UTC().Format("2006-01-02") == day, "fixture_utc_day_changed")
	data, err := json.Marshal(map[string]interface{}{"execution_ids": ids, "ids": allIDs, "tenant_id": tenant, "principal_id": strconv.FormatInt(principal, 10), "nonce": nonce, "quota_write_attempts": 1005, "quota_stored": 1000, "history_stored": 1201})
	need(t, err == nil, "fixture_state_encode")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	need(t, err == nil, "fixture_state_create")
	_, err = f.Write(data)
	closeErr := f.Close()
	need(t, err == nil && closeErr == nil, "fixture_state_write")
}
