package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	ms "github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationMySQLManagerSearchMaintenance(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_MANAGER_STORAGE") != "1" {
		t.Skip("requires disposable Manager MySQL gate")
	}
	dialect, err := platformmysql.Dialector(database.Config{Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"), User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	verifyIndexMaintenance(t, dialect)
}
func TestIntegrationPostgresManagerSearchMaintenance(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires disposable Manager PostgreSQL gate")
	}
	verifyIndexMaintenance(t, postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")))
}

func verifyIndexMaintenance(t *testing.T, dialect gorm.Dialector) {
	t.Helper()
	ctx := t.Context()
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := repository.MigrateContentIndexDeliverySchema(db); err != nil {
		t.Fatal(err)
	}
	if dialect.Name() == "postgres" {
		if err := db.AutoMigrate(&models.Embedding{}); err != nil {
			t.Fatal(err)
		}
		if err := repository.MigrateSearchSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{MeilisearchURL: os.Getenv("MANAGER_MEILISEARCH_TEST_URL"), MeilisearchMasterKey: os.Getenv("MANAGER_MEILISEARCH_TEST_KEY"), MeilisearchManagerContentIndex: "maintenance-" + uuid.NewString()}
	// Return a gateway error after the real engine has accepted the clear.
	// Resuming must recover that exact task without dispatching it twice.
	upstream, err := url.Parse(cfg.MeilisearchURL)
	if err != nil {
		t.Fatal(err)
	}
	var loseClear atomic.Bool
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ModifyResponse = func(res *http.Response) error {
		if res.StatusCode == http.StatusAccepted && res.Request.Method == http.MethodDelete && res.Request.URL.Path == "/indexes/"+cfg.MeilisearchManagerContentIndex+"/documents" && loseClear.CompareAndSwap(true, false) {
			res.Body.Close()
			res.StatusCode = http.StatusBadGateway
			res.Status = "502 Bad Gateway"
			res.Body = io.NopCloser(strings.NewReader(""))
			res.ContentLength = 0
			res.Header.Del("Content-Length")
		}
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	cfg.MeilisearchURL = server.URL
	c := ms.New(cfg.MeilisearchURL, ms.WithAPIKey(cfg.MeilisearchMasterKey), ms.DisableRetries())
	wait := func(info *ms.TaskInfo, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		task, err := c.WaitForTaskWithContext(ctx, info.TaskUID, 30*time.Millisecond)
		if err != nil || task.Status != ms.TaskStatusSucceeded {
			t.Fatalf("fixture task: %+v %v", task, err)
		}
	}
	index := cfg.MeilisearchManagerContentIndex
	sentinel := "neighbor-" + uuid.NewString()
	for _, name := range []string{index, sentinel} {
		wait(c.CreateIndexWithContext(ctx, &ms.IndexConfig{Uid: name, PrimaryKey: "id"}))
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			task, err := c.DeleteIndexWithContext(cleanupCtx, name)
			if err != nil {
				t.Error(err)
				return
			}
			done, err := c.WaitForTaskWithContext(cleanupCtx, task.TaskUID, time.Millisecond*20)
			if err != nil || done.Status != ms.TaskStatusSucceeded {
				t.Errorf("fixture index cleanup: %+v %v", done, err)
			}
		})
	}
	repo := repository.NewContentIndexDeliveryRepository(db, index)
	endpoint := config.SearchEndpointID(cfg.MeilisearchURL)
	epoch, err := repo.ConfirmTarget(ctx, endpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateIfSettled(ctx, epoch); err != nil {
		t.Fatal(err)
	}
	var ordinal int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		ids, err := repo.ReserveSearchOrdinals(ctx, tx, epoch, []string{"orders"})
		ordinal = ids["orders"]
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{index, sentinel} {
		wait(c.Index(name).AddDocumentsWithContext(ctx, []map[string]any{{"id": "orders", "document_id": "orders", "search_ordinal": ordinal, "name": "order fixture"}}, nil))
	}
	before, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil {
		t.Fatal(err)
	}
	run := func(action, id string, wantSuccess bool) {
		t.Helper()
		cmd := exec.CommandContext(ctx, os.Getenv("MANAGER_MAINTENANCE_TEST_BINARY"), "maintain-search", action, id)
		cmd.Env = append(os.Environ(), "ADDP_INFRA_DATABASE_PROVIDER="+dialect.Name(), "MEILISEARCH_URL="+cfg.MeilisearchURL,
			"MEILISEARCH_MASTER_KEY="+cfg.MeilisearchMasterKey, "MEILISEARCH_MANAGER_CONTENT_INDEX="+index)
		if dialect.Name() == "mysql" {
			cmd.Env = append(cmd.Env, "ADDP_MYSQL_HOST="+os.Getenv("ADDP_TEST_MYSQL_HOST"), "ADDP_MYSQL_PORT="+os.Getenv("ADDP_TEST_MYSQL_PORT"),
				"ADDP_DATABASE_DEPLOY_USER="+os.Getenv("ADDP_TEST_MYSQL_USER"), "ADDP_DATABASE_DEPLOY_PASSWORD="+os.Getenv("ADDP_TEST_MYSQL_PASSWORD"))
		} else {
			pg, err := pgx.ParseConfig(os.Getenv("MANAGER_POSTGRES_TEST_DSN"))
			if err != nil {
				t.Fatal(err)
			}
			cmd.Env = append(cmd.Env, "POSTGRES_HOST="+pg.Host, "POSTGRES_PORT="+strconv.Itoa(int(pg.Port)), "POSTGRES_DB="+pg.Database,
				"ADDP_DATABASE_DEPLOY_USER="+pg.User, "ADDP_DATABASE_DEPLOY_PASSWORD="+pg.Password)
		}
		if b, err := cmd.CombinedOutput(); (err == nil) != wantSuccess {
			t.Fatalf("maintenance command exit: %v: %s", err, b)
		}
	}
	id := uuid.NewString()
	if err := repository.PrepareSearchMaintenance(db); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSearchMaintenance(ctx, id, endpoint, "rebuild"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SearchMaterial(ctx, epoch, 7); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatalf("maintenance did not fence reads: %v", err)
	}
	if err := repo.ActivateIfSettled(ctx, epoch); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatalf("recovery reopened unfinished maintenance: %v", err)
	}
	run("rebuild", id, true)
	after, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil || after.SearchRevision <= before.SearchRevision || after.NextSearchOrdinal != ordinal {
		t.Fatalf("rebuild material: %+v %v", after, err)
	}
	var doc struct {
		Name    string
		Ordinal int64 `json:"search_ordinal"`
	}
	if err := c.Index(index).GetDocumentWithContext(ctx, "orders", nil, &doc); err != nil || doc.Name != "order fixture" || doc.Ordinal != ordinal {
		t.Fatalf("rebuilt document: %+v %v", doc, err)
	}
	run("rebuild", id, true)
	replay, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil || replay != after {
		t.Fatalf("operation replay changed material: %+v %v", replay, err)
	}
	clearID := uuid.NewString()
	loseClear.Store(true)
	run("clear", clearID, false)
	if loseClear.Load() {
		t.Fatal("fixture did not lose the accepted response")
	}
	if err := repo.ActivateIfSettled(ctx, epoch); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatalf("lost response reopened index: %v", err)
	}
	run("clear", clearID, true)
	tasks, err := c.GetTasksWithContext(ctx, &ms.TasksQuery{Types: []ms.TaskType{ms.TaskTypeDocumentDeletion}, IndexUIDS: []string{index}, Limit: 2})
	if err != nil || tasks.Total != 1 || tasks.Results[0].CustomMetadata != clearID {
		t.Fatalf("clear task count or identity: %+v %v", tasks, err)
	}
	for _, check := range []struct {
		name     string
		expected int64
	}{{index, 0}, {sentinel, 1}} {
		stats, err := c.Index(check.name).GetStatsWithContext(ctx, nil)
		if err != nil || stats.NumberOfDocuments != check.expected {
			t.Fatalf("post-clear %s stats %+v %v", check.name, stats, err)
		}
	}
	if err := repo.CheckSearchOrdinals(ctx, map[string]int64{"orders": ordinal}); err != nil {
		t.Fatal(err)
	}
}
