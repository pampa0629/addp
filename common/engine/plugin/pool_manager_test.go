package plugin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

type poolTestConnector struct{}
type poolTestDriver struct{}
type poolTestConn struct{}

func (poolTestConnector) Connect(context.Context) (driver.Conn, error) { return poolTestConn{}, nil }
func (poolTestConnector) Driver() driver.Driver                        { return poolTestDriver{} }
func (poolTestDriver) Open(string) (driver.Conn, error)                { return poolTestConn{}, nil }
func (poolTestConn) Prepare(string) (driver.Stmt, error)               { return nil, errors.New("unused") }
func (poolTestConn) Close() error                                      { return nil }
func (poolTestConn) Begin() (driver.Tx, error)                         { return nil, errors.New("unused") }

func poolTestDB() (*gorm.DB, *sql.DB) {
	native := sql.OpenDB(poolTestConnector{})
	return &gorm.DB{Config: &gorm.Config{ConnPool: native}}, native
}

type poolLifecyclePlugin struct {
	tabularCatalogTestPlugin
	create func(ConnectionInfo, *PoolConfig) (*gorm.DB, error)
}

func (*poolLifecyclePlugin) Type() string { return "pool_lifecycle_fixture" }
func (p *poolLifecyclePlugin) CreateConnectionPool(info ConnectionInfo, config *PoolConfig) (*gorm.DB, error) {
	return p.create(info, config)
}

func poolFixture(t *testing.T, create func(ConnectionInfo, *PoolConfig) (*gorm.DB, error)) (*PoolManager, *Engine) {
	t.Helper()
	p := &poolLifecyclePlugin{create: create}
	Register(p)
	pm := &PoolManager{pools: make(map[uint]*ManagedPool)}
	t.Cleanup(func() { pm.CloseAll(); Unregister(p.Type()) })
	return pm, &Engine{ID: 8401, EngineType: p.Type(), ConnectionInfo: ConnectionInfo{"host": "first", "password": "fixture-secret"}}
}

func TestPoolManagerCloseReleasesSQLDB(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "one", true: "all"}[all], func(t *testing.T) {
			db, native := poolTestDB()
			t.Cleanup(func() { native.Close() })
			pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { return db, nil })
			if _, err := pm.GetOrCreatePool(engine, DefaultPoolConfig()); err != nil {
				t.Fatal(err)
			}
			if err := native.Ping(); err != nil {
				t.Fatal(err)
			}
			if all {
				pm.CloseAll()
			} else if err := pm.ClosePool(engine.ID); err != nil {
				t.Fatal(err)
			}
			if err := native.Ping(); err == nil {
				t.Fatal("closed cache still retains a live sql.DB")
			}
			if len(pm.GetPoolStats()) != 0 {
				t.Fatal("closed pool remains in statistics")
			}
		})
	}
}

func TestPoolManagerRebindsChangedConfiguration(t *testing.T) {
	for _, field := range []string{"host", "port", "username", "password", "tls", "engine_type", "max_open", "max_idle", "lifetime"} {
		t.Run(field, func(t *testing.T) {
			var natives []*sql.DB
			pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
				db, native := poolTestDB()
				natives = append(natives, native)
				t.Cleanup(func() { native.Close() })
				return db, nil
			})
			config := DefaultPoolConfig()
			first, err := pm.GetOrCreatePool(engine, config)
			if err != nil {
				t.Fatal(err)
			}
			if same, err := pm.GetOrCreatePool(engine, config); err != nil || same != first {
				t.Fatal("unchanged configuration not reused")
			}
			if field == "engine_type" {
				// A second registered type must never borrow the first type's pool.
				engine.EngineType = "tabular_catalog_test"
				Register(&tabularCatalogTestPlugin{})
				t.Cleanup(func() { Unregister("tabular_catalog_test") })
			} else if field == "max_open" {
				config.MaxOpenConns++
			} else if field == "max_idle" {
				config.MaxIdleConns++
			} else if field == "lifetime" {
				config.ConnMaxLifetime += time.Minute
			} else {
				engine.ConnectionInfo[field] = "changed"
			}
			second, err := pm.GetOrCreatePool(engine, config)
			if err != nil {
				t.Fatal(err)
			}
			if first == second {
				t.Fatal("changed connection configuration reused old pool")
			}
			if err := natives[0].Ping(); err == nil {
				t.Fatal("replaced sql.DB remains open")
			}
		})
	}
}

func TestPoolManagerLifetimeDoesNotExpirePoolCache(t *testing.T) {
	db, native := poolTestDB()
	t.Cleanup(func() { native.Close() })
	var calls int
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { calls++; return db, nil })
	config := DefaultPoolConfig()
	config.ConnMaxLifetime = 0
	if _, err := pm.GetOrCreatePool(engine, config); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.GetOrCreatePool(engine, config); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("physical connection lifetime was used as pool cache expiry")
	}
}

func TestPoolManagerConcurrentCreationAndStats(t *testing.T) {
	var calls atomic.Int32
	started, release, duplicate := make(chan struct{}), make(chan struct{}), make(chan struct{}, 64)
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		} else {
			duplicate <- struct{}{}
		}
		db, native := poolTestDB()
		t.Cleanup(func() { native.Close() })
		return db, nil
	})
	var wg sync.WaitGroup
	results := make(chan interface{}, 64)
	failures := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := pm.GetOrCreatePool(engine, DefaultPoolConfig())
			if err != nil {
				failures <- err
			}
			results <- db
			for j := 0; j < 20; j++ {
				if _, err := pm.GetOrCreatePool(engine, DefaultPoolConfig()); err != nil {
					t.Error(err)
				}
				pm.GetPoolStats()
			}
		}()
	}
	<-started
	select {
	case <-duplicate:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var first interface{}
	for result := range results {
		if first == nil {
			first = result
		}
		if result != first {
			t.Error("same engine received different pools")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("created %d pools for one engine", calls.Load())
	}
}

func TestPoolManagerCreationFailureCanRetry(t *testing.T) {
	var calls int
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("fixture creation failed")
		}
		db, native := poolTestDB()
		t.Cleanup(func() { native.Close() })
		return db, nil
	})
	if _, err := pm.GetOrCreatePool(engine, DefaultPoolConfig()); err == nil {
		t.Fatal("creation failure hidden")
	}
	if len(pm.GetPoolStats()) != 0 {
		t.Fatal("failed creation was cached")
	}
	if _, err := pm.GetOrCreatePool(engine, DefaultPoolConfig()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("failed creation was not retried")
	}
}

func TestPoolManagerReplacementFailureNeverFallsBack(t *testing.T) {
	var firstNative *sql.DB
	var calls int
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("replacement failed")
		}
		db, native := poolTestDB()
		if calls == 1 {
			firstNative = native
		}
		t.Cleanup(func() { native.Close() })
		return db, nil
	})
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	engine.ConnectionInfo["password"] = "rotated"
	if db, err := pm.GetOrCreatePool(engine, nil); err == nil || db != nil {
		t.Fatal("failed replacement returned a pool")
	}
	if firstNative.Ping() == nil {
		t.Fatal("old credentials retain a live pool")
	}
	if len(pm.GetPoolStats()) != 0 {
		t.Fatal("failed replacement left a stale cache")
	}
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("replacement could not retry")
	}
}

func TestPoolManagerPartialCreationIsClosed(t *testing.T) {
	db, native := poolTestDB()
	t.Cleanup(func() { native.Close() })
	failure := errors.New("partial initialization failed")
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { return db, failure })
	if _, err := pm.GetOrCreatePool(engine, nil); !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
	if native.Ping() == nil {
		t.Fatal("partial pool leaked")
	}
	if len(pm.GetPoolStats()) != 0 {
		t.Fatal("partial creation cached")
	}
}

func TestPoolManagerIndependentEngineCreation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	pm, engine := poolFixture(t, func(info ConnectionInfo, _ *PoolConfig) (*gorm.DB, error) {
		if info["host"] == "first" {
			close(started)
			<-release
		}
		db, native := poolTestDB()
		t.Cleanup(func() { native.Close() })
		return db, nil
	})
	firstDone := make(chan error, 1)
	go func() { _, err := pm.GetOrCreatePool(engine, nil); firstDone <- err }()
	<-started
	other := &Engine{ID: engine.ID + 1, EngineType: engine.EngineType, ConnectionInfo: ConnectionInfo{"host": "second"}}
	otherDone := make(chan error, 1)
	go func() { _, err := pm.GetOrCreatePool(other, nil); otherDone <- err }()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(3 * time.Second):
		once.Do(func() { close(release) })
		<-firstDone
		t.Fatal("unrelated engine blocked behind connection creation")
	}
	once.Do(func() { close(release) })
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestPoolManagerCloseDuringCreation(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "one", true: "all"}[all], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			var natives []*sql.DB
			pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
				if len(natives) == 0 {
					close(started)
					<-release
				}
				db, native := poolTestDB()
				natives = append(natives, native)
				t.Cleanup(func() { native.Close() })
				return db, nil
			})
			created := make(chan error, 1)
			go func() { _, err := pm.GetOrCreatePool(engine, nil); created <- err }()
			<-started
			closed := make(chan error, 1)
			go func() {
				if all {
					pm.CloseAll()
					closed <- nil
				} else {
					closed <- pm.ClosePool(engine.ID)
				}
			}()
			if all {
				// Wait until CloseAll is accepting no new lifecycle operations.
				deadline := time.NewTimer(3 * time.Second)
				defer deadline.Stop()
				for {
					pm.mu.RLock()
					closing := pm.closing
					pm.mu.RUnlock()
					if closing != nil {
						break
					}
					select {
					case <-deadline.C:
						once.Do(func() { close(release) })
						<-created
						<-closed
						t.Fatal("CloseAll did not start")
					default:
						runtime.Gosched()
					}
				}
			}
			select {
			case <-closed:
				t.Fatal("close returned before creation finished")
			default:
			}
			once.Do(func() { close(release) })
			if err := <-created; err != nil {
				t.Fatal(err)
			}
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			if len(pm.GetPoolStats()) != 0 || natives[0].Ping() == nil {
				t.Fatal("creation republished a live pool after close")
			}
			if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
				t.Fatal(err)
			}
			if len(natives) != 2 {
				t.Fatal("closed manager cannot create again")
			}
		})
	}
}

func TestPoolManagerInputValidationAndIdentity(t *testing.T) {
	var calls int
	pm, engine := poolFixture(t, func(_ ConnectionInfo, config *PoolConfig) (*gorm.DB, error) {
		calls++
		if *config != *DefaultPoolConfig() {
			t.Error("nil config did not use defaults")
		}
		db, native := poolTestDB()
		t.Cleanup(func() { native.Close() })
		return db, nil
	})
	if _, err := pm.GetOrCreatePool(nil, nil); err == nil {
		t.Fatal("nil engine accepted")
	}
	first, err := pm.GetOrCreatePool(engine, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.ConnectionInfo = ConnectionInfo{"password": "fixture-secret", "host": "first"}
	same, err := pm.GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil || same != first || calls != 1 {
		t.Fatal("map insertion order changed identity")
	}
	engine.ConnectionInfo["bad"] = func() {}
	if _, err := pm.GetOrCreatePool(engine, nil); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("invalid configuration accepted or sensitive configuration exposed")
	}
}

func TestPoolManagerInvalidPoolIsNotCached(t *testing.T) {
	for _, db := range []*gorm.DB{nil, {}, {Config: &gorm.Config{}}} {
		pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { return db, nil })
		if _, err := pm.GetOrCreatePool(engine, nil); err == nil {
			t.Fatal("invalid pool accepted")
		}
		if len(pm.GetPoolStats()) != 0 {
			t.Fatal("invalid pool cached")
		}
	}
}

type poolCloseErrorConnector struct {
	poolTestConnector
	failure error
}
type poolCloseErrorConn struct {
	poolTestConn
	failure error
}

func (c poolCloseErrorConnector) Connect(context.Context) (driver.Conn, error) {
	return poolCloseErrorConn{failure: c.failure}, nil
}
func (c poolCloseErrorConn) Close() error { return c.failure }

func TestPoolManagerCloseErrorStillDetachesPool(t *testing.T) {
	failure := errors.New("fixture close error")
	native := sql.OpenDB(poolCloseErrorConnector{failure: failure})
	t.Cleanup(func() { native.Close() })
	db := &gorm.DB{Config: &gorm.Config{ConnPool: native}}
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { return db, nil })
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	if err := native.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := pm.ClosePool(engine.ID); !errors.Is(err, failure) {
		t.Fatalf("close error lost: %v", err)
	}
	if len(pm.GetPoolStats()) != 0 || native.Ping() == nil {
		t.Fatal("close error retained old pool")
	}
}

func TestPoolManagerCloseAllClosesEveryPoolDespiteError(t *testing.T) {
	failure := errors.New("fixture close error")
	var natives []*sql.DB
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
		native := sql.OpenDB(poolCloseErrorConnector{failure: failure})
		t.Cleanup(func() { native.Close() })
		natives = append(natives, native)
		if err := native.Ping(); err != nil {
			t.Fatal(err)
		}
		return &gorm.DB{Config: &gorm.Config{ConnPool: native}}, nil
	})
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	other := *engine
	other.ID++
	if _, err := pm.GetOrCreatePool(&other, nil); err != nil {
		t.Fatal(err)
	}
	pm.CloseAll()
	if len(pm.GetPoolStats()) != 0 {
		t.Fatal("close error stopped cache removal")
	}
	for _, native := range natives {
		if native.Ping() == nil {
			t.Fatal("close error prevented another pool closing")
		}
	}
}

func TestPoolManagerOldAccessTimeDoesNotExpirePool(t *testing.T) {
	db, native := poolTestDB()
	t.Cleanup(func() { native.Close() })
	var calls int
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) { calls++; return db, nil })
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	pm.mu.Lock()
	pm.pools[engine.ID].LastAccess = time.Now().Add(-2 * time.Hour)
	pm.mu.Unlock()
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("idle pool incorrectly expired on physical lifetime")
	}
	if pm.GetPoolStats()[engine.ID].IdleTime >= time.Hour {
		t.Fatal("access time was not refreshed")
	}
}

type poolBlockingCloseConnector struct {
	poolTestConnector
	started chan struct{}
	release chan struct{}
}
type poolBlockingCloseConn struct {
	poolTestConn
	started chan struct{}
	release chan struct{}
}

func (c poolBlockingCloseConnector) Connect(context.Context) (driver.Conn, error) {
	return poolBlockingCloseConn{started: c.started, release: c.release}, nil
}
func (c poolBlockingCloseConn) Close() error { close(c.started); <-c.release; return nil }

func TestPoolManagerCloseAllBlocksNewCreationUntilSQLClose(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	recreated := make(chan struct{})
	pm, engine := poolFixture(t, func(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
		var native *sql.DB
		if calls.Add(1) == 1 {
			native = sql.OpenDB(poolBlockingCloseConnector{started: started, release: release})
		} else {
			native = sql.OpenDB(poolTestConnector{})
			close(recreated)
		}
		t.Cleanup(func() { once.Do(func() { close(release) }); native.Close() })
		if err := native.Ping(); err != nil {
			t.Error(err)
		}
		return &gorm.DB{Config: &gorm.Config{ConnPool: native}}, nil
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if _, err := pm.GetOrCreatePool(engine, nil); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { pm.CloseAll(); close(closed) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("CloseAll never closed the underlying SQL pool")
	}
	result := make(chan error, 1)
	go func() { _, err := pm.GetOrCreatePool(engine, nil); result <- err }()
	select {
	case <-recreated:
		once.Do(func() { close(release) })
		<-closed
		<-result
		t.Fatal("new pool created before old pools finished closing")
	case <-time.After(100 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	<-closed
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(pm.GetPoolStats()) != 1 {
		t.Fatal("waiting request did not resume after close")
	}
}
