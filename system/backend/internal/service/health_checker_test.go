package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
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
	t.Cleanup(runtime.Close)

	parsed, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t)
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
	t.Cleanup(runtime.Close)
	parsed, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t)
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
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

func TestHealthCheckerRunRecoversEngineStartedAfterSystem(t *testing.T) {
	var ready atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(runtime.Close)

	parsed, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineRepository(db)
	engine := &models.Engine{
		Name: "late runtime", EngineType: "jupyter", EngineOrigin: "extension",
		ConnectionInfo: models.ConnectionInfo{"protocol": parsed.Scheme, "host": parsed.Hostname(), "port": port},
		LifecycleState: models.EngineLifecycleActive, ConnectionStatus: models.EngineConnectionUnknown,
	}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}

	checker := NewHealthChecker(NewEngineService(repo, nil, nil))
	checker.retryWindow = time.Millisecond
	checker.retryInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("HealthChecker.Run did not stop after cancellation")
		}
	})
	go func() {
		defer close(done)
		checker.Run(ctx, 5*time.Millisecond)
	}()

	waitForEngineConnectionStatus(t, repo, engine.ID, models.EngineConnectionOffline)
	ready.Store(true)
	waitForEngineConnectionStatus(t, repo, engine.ID, models.EngineConnectionOnline)
}

func waitForEngineConnectionStatus(t *testing.T, repo *repository.EngineRepository, engineID uint, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		engine, err := repo.GetByID(engineID)
		if err != nil {
			t.Fatalf("read connection observation: %v", err)
		}
		if engine.ConnectionStatus == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	engine, err := repo.GetByID(engineID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("connection_status = %q, want %q", engine.ConnectionStatus, want)
}

func TestHealthCheckerReadDuringConnectionObservation(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineRepository(db)
	engine := &models.Engine{
		Name: "offline", EngineType: "custom_runtime", EngineOrigin: "extension",
		LifecycleState: models.EngineLifecycleActive, ConnectionInfo: models.ConnectionInfo{},
	}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	held, releaseWrite := make(chan struct{}), make(chan struct{})
	var holdOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWrite) }) }
	if err := db.Callback().Update().After("gorm:update").Before("gorm:commit_or_rollback_transaction").Register("test:hold_health_observation", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if tx.Statement.Table == "engines" && ok && values["last_check_at"] != nil && tx.Error == nil {
			holdOnce.Do(func() {
				close(held)
				<-releaseWrite
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	checker := NewHealthChecker(NewEngineService(repo, nil, nil))
	checker.retryWindow, checker.retryInterval = time.Millisecond, time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	t.Cleanup(func() {
		release()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("health checker did not finish")
		}
	})
	go func() {
		defer close(done)
		checker.Run(ctx, time.Hour)
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("health checker did not reach the uncommitted observation")
	}
	readDone := make(chan error, 1)
	readFinished := make(chan struct{})
	t.Cleanup(func() {
		release()
		cancel()
		select {
		case <-readFinished:
		case <-time.After(5 * time.Second):
			t.Error("concurrent observation reader did not finish")
		}
	})
	var stored models.Engine
	go func() {
		defer close(readFinished)
		readDone <- db.WithContext(ctx).First(&stored, engine.ID).Error
	}()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-readDone:
			if err != nil {
				t.Fatalf("read during health observation: %v", err)
			}
			if stored.LastCheckAt == nil {
				t.Fatal("reader did not see the committed health observation")
			}
			return
		case <-ticker.C:
			if sqlDB.Stats().WaitCount > 0 {
				release()
			}
		case <-ctx.Done():
			t.Fatal("concurrent health observation read did not finish")
		}
	}
}
