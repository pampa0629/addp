package protection

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// A's SQL connections can fail independently of its live HTTP response and B's pool.
func TestIntegrationPostgresManagerProtectionReadDisconnect(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Manager PG gate required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var offline atomic.Bool
	open := func(blockable bool) *gorm.DB {
		cfg, err := pgx.ParseConfig(os.Getenv("MANAGER_POSTGRES_TEST_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		dial := cfg.DialFunc
		cfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if blockable && offline.Load() {
				return nil, errors.New("fixture: Manager A database link down")
			}
			return dial(ctx, network, addr)
		}
		pool := stdlib.OpenDB(*cfg)
		pool.SetMaxIdleConns(0)
		t.Cleanup(func() { _ = pool.Close() })
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	dbA, dbB := open(true), open(false)
	owner := uuid.NewString()
	tenant := time.Now().UnixNano()
	a := NewReadBoundary(&boundaryTestStore{}, dbA, owner)
	b := NewReadBoundary(&boundaryTestStore{}, dbB, uuid.NewString())
	barrier := NewAcknowledgementBarrier(dbB, b, boundaryTestIndex{})
	t.Cleanup(func() {
		if err := dbB.Where("tenant_id = ? AND process_instance_id = ?", tenant, owner).Delete(&models.ProtectionRead{}).Error; err != nil {
			t.Error(err)
		}
	})
	resume := make(chan struct{})
	resumeStream := sync.OnceFunc(func() { close(resume) })
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		end, err := a.BeginRead(r.Context(), tenant)
		if err != nil {
			http.Error(w, "register", 500)
			return
		}
		defer end()
		fmt.Fprintln(w, "first")
		w.(http.Flusher).Flush()
		select {
		case <-resume:
			fmt.Fprintln(w, "last")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer resumeStream()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := bufio.NewReader(resp.Body)
	if line, err := buf.ReadString('\n'); err != nil || line != "first\n" {
		t.Fatalf("opening chunk %q: %v", line, err)
	}
	offline.Store(true)
	if end, err := a.BeginRead(ctx, tenant); end != nil || !errors.Is(err, ErrRequired) {
		t.Fatalf("disconnected reader admitted: %v", err)
	}
	if err := barrier.ReadyToAcknowledge(ctx, tenant, "new-cursor"); err == nil {
		t.Fatal("B acknowledged while A was streaming")
	}
	if err := barrier.ReadyToAcknowledge(ctx, tenant+1, "other"); err != nil {
		t.Fatal(err)
	}
	resumeStream()
	if body, err := io.ReadAll(buf); err != nil || string(body) != "last\n" {
		t.Fatalf("stream stopped with SQL link: %q, %v", body, err)
	}
	<-done
	// Neither B nor a replacement A knows that the old request has ended.
	b.retryFinished(ctx)
	if err := barrier.ReadyToAcknowledge(ctx, tenant, "new-cursor"); err == nil {
		t.Fatal("B released A's unresolved read")
	}
	offline.Store(false)
	a.retryFinished(ctx)
	if err := barrier.ReadyToAcknowledge(ctx, tenant, "new-cursor"); err != nil {
		t.Fatal(err)
	}
}
