package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRetryConnectionCheckRecoversWithinStartupWindow(t *testing.T) {
	attempts := 0
	if !retryConnectionCheck(func() bool {
		attempts++
		return attempts == 3
	}, 50*time.Millisecond, time.Millisecond) {
		t.Fatal("retryConnectionCheck() = false, want recovery")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryConnectionCheckStopsAfterStartupWindow(t *testing.T) {
	attempts := 0
	if retryConnectionCheck(func() bool {
		attempts++
		return false
	}, 5*time.Millisecond, time.Millisecond) {
		t.Fatal("retryConnectionCheck() = true, want timeout")
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want more than one probe", attempts)
	}
}

func TestHealthCheckerRetriesOfflineRuntimeUntilItIsReady(t *testing.T) {
	var attempts atomic.Int32
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer runtime.Close()

	parsed, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineRepository(db)
	engine := &models.Engine{
		Name:             "Jupyter",
		EngineType:       "jupyter",
		EngineOrigin:     "extension",
		ConnectionInfo:   models.ConnectionInfo{"protocol": parsed.Scheme, "host": parsed.Hostname(), "port": port},
		LifecycleState:   models.EngineLifecycleActive,
		ConnectionStatus: "unknown",
	}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}

	checker := NewHealthChecker(NewEngineService(repo, nil, nil))
	checker.retryWindow = 50 * time.Millisecond
	checker.retryInterval = time.Millisecond
	checker.CheckAllResourcesOnStartup()

	stored, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionStatus != "online" {
		t.Fatalf("connection_status = %q, want online; attempts=%d", stored.ConnectionStatus, attempts.Load())
	}
	if attempts.Load() < 2 {
		t.Fatalf("health probe attempts = %d, want at least 2", attempts.Load())
	}
}

func TestHealthCheckerIsolatesOfflineEngineFromOtherInstances(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer runtime.Close()
	parsed, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := repository.NewEngineRepository(db)
	engines := []*models.Engine{
		{
			Name: "offline postgres", EngineType: "postgresql", EngineOrigin: "general",
			ConnectionInfo: models.ConnectionInfo{"host": "127.0.0.1", "port": 1, "database": "offline", "user": "offline"},
			LifecycleState: models.EngineLifecycleActive, ConnectionStatus: "unknown",
		},
		{
			Name: "online runtime", EngineType: "jupyter", EngineOrigin: "extension",
			ConnectionInfo: models.ConnectionInfo{"protocol": parsed.Scheme, "host": parsed.Hostname(), "port": port},
			LifecycleState: models.EngineLifecycleActive, ConnectionStatus: "unknown",
		},
	}
	for _, engine := range engines {
		if err := repo.Create(engine); err != nil {
			t.Fatal(err)
		}
	}

	checker := NewHealthChecker(NewEngineService(repo, nil, nil))
	checker.retryWindow = time.Millisecond
	checker.retryInterval = time.Millisecond
	checker.CheckAllResourcesOnStartup()

	offline, err := repo.GetByID(engines[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	online, err := repo.GetByID(engines[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if offline.ConnectionStatus != "offline" || online.ConnectionStatus != "online" {
		t.Fatalf("statuses = offline:%q online:%q", offline.ConnectionStatus, online.ConnectionStatus)
	}
}
