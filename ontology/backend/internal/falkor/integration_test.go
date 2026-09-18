package falkor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestFalkorIntegration(t *testing.T) {
	if os.Getenv("ADDP_ONTOLOGY_FALKOR_INTEGRATION") != "1" {
		t.Skip("requires owner disposable FalkorDB gate")
	}
	address, password := os.Getenv("ONTOLOGY_FALKOR_TEST_ADDRESS"), os.Getenv("ONTOLOGY_FALKOR_TEST_PASSWORD")
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "6379" || port == "16379" || len(password) != 32 {
		t.Fatal("invalid disposable endpoint")
	}
	if _, err := uuid.Parse(password); err != nil {
		t.Fatal("invalid owner run identity")
	}
	c, err := New(Config{Address: address, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		if _, err = c.command(ctx, "PING"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("FalkorDB not ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	observer := redis.NewClient(&redis.Options{Addr: address, Password: password, Protocol: 2, DisableIdentity: true, MaxRetries: -1, ReadTimeout: 3 * time.Second})
	if err := c.Health(ctx); err != nil {
		value, probeErr := observer.Do(ctx, "GRAPH.CONFIG", "GET", "TIMEOUT_MAX").Result()
		t.Fatalf("graph readiness contract: %v; timeout configuration=%#v probe=%v", err, value, probeErr)
	}
	t.Cleanup(func() { _ = observer.Close() })
	for _, key := range []string{"TIMEOUT_MAX", "TIMEOUT_DEFAULT"} {
		value, err := observer.Do(ctx, "GRAPH.CONFIG", "GET", key).Result()
		if err != nil || !strings.Contains(fmt.Sprint(value), "2000") {
			t.Fatalf("missing server timeout %s: %v %v", key, value, err)
		}
	}
	keys := []string{}
	newKey := func() string {
		p, _ := Plan(fixture(t), uuid.NewString())
		keys = append(keys, p.key)
		return p.key
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		for _, key := range keys {
			if err := observer.Do(cleanupCtx, "GRAPH.DELETE", key).Err(); err != nil {
				t.Errorf("graph cleanup failed: %v", err)
			}
		}
		remaining, err := observer.DBSize(cleanupCtx).Result()
		if err != nil || remaining != 0 {
			t.Errorf("disposable graph residual: %d %v", remaining, err)
		}
	})
	t.Run("projection_round_trip_and_no_overwrite", func(t *testing.T) {
		p, _ := Plan(fixture(t), uuid.NewString())
		keys = append(keys, p.key)
		if err := c.Build(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := c.Build(ctx, p); !errors.Is(err, ErrRejected) {
			t.Fatalf("overwritten: %v", err)
		}
		if err := c.Verify(ctx, p); err != nil {
			t.Fatal(err)
		}
		_, err := c.query(ctx, p.key, "MATCH (n:Definition {id:'hike'}) SET n.name='changed' RETURN 1", nil, false, MaxQueryTime)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(ctx, p); !errors.Is(err, ErrProtocol) {
			t.Fatalf("tampering accepted: %v", err)
		}
	})
	t.Run("maximum_definition_size", func(t *testing.T) {
		d := semantic.Definition{Scope: semantic.Scope{TenantID: 101, OntologyID: "limits", Revision: 1}}
		for i := 0; i < 64; i++ {
			d.Classes = append(d.Classes, semantic.Class{ID: fmt.Sprintf("class_%d", i), Name: "类"})
		}
		for i := 0; i < 256; i++ {
			d.Properties = append(d.Properties, semantic.Property{ID: fmt.Sprintf("property_%d", i), ClassID: "class_0", Key: fmt.Sprintf("field_%d", i), Name: "属性", Kind: semantic.Boolean})
		}
		for i := 0; i < 128; i++ {
			d.Relations = append(d.Relations, semantic.Relation{ID: fmt.Sprintf("relation_%d", i), Name: "关系", From: "class_0", To: "class_1"})
		}
		for i := 0; i < 64; i++ {
			r := semantic.Rule{ID: fmt.Sprintf("rule_%d", i), ClassID: "class_0", Basis: "测试"}
			variables := []string{}
			for j := 0; j < 16; j++ {
				v := fmt.Sprintf("v_%d", j)
				variables = append(variables, v)
				r.Inputs = append(r.Inputs, semantic.Input{Variable: v, PropertyID: fmt.Sprintf("property_%d", j), OnAbsent: semantic.AbsenceUnknown})
			}
			r.Expression = strings.Join(variables, " && ")
			d.Rules = append(d.Rules, r)
		}
		snapshot, err := semantic.Freeze(d)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Plan(snapshot, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, p.key)
		if err := c.Build(ctx, p); err != nil {
			t.Fatal(err)
		}
		if len(p.nodeRows) != 512 || len(p.edgeRows) <= 1024 {
			t.Fatal("fixture does not cover projection limits")
		}
	})
	t.Run("cancelled_build_does_not_create_graph", func(t *testing.T) {
		p, _ := Plan(fixture(t), uuid.NewString())
		request, stop := context.WithCancel(ctx)
		stop()
		if err := c.Build(request, p); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if n, err := observer.Exists(ctx, p.key).Result(); err != nil || n != 0 {
			t.Fatal("cancelled build touched graph")
		}
	})
	t.Run("parameters_and_error_categories", func(t *testing.T) {
		key := newKey()
		name := "北京\"\\\n' }) MATCH (n) DELETE n //"
		rows, err := c.query(ctx, key, "CREATE (n:Probe {name:$name}) RETURN n.name, $number, $flag", map[string]any{"name": name, "number": int64(9007199254740993), "flag": true}, false, MaxQueryTime)
		if err != nil || len(rows) != 1 || rows[0][0] != name || rows[0][1] != int64(9007199254740993) {
			t.Fatalf("binding: %v %v", rows, err)
		}
		if _, err := c.query(ctx, key, "not a query", nil, true, MaxQueryTime); !errors.Is(err, ErrRejected) {
			t.Fatal(err)
		}
		bad, _ := New(Config{Address: address, Password: "wrong"})
		if _, err := bad.command(ctx, "PING"); !errors.Is(err, ErrRejected) {
			t.Fatal(err)
		}
	})
	const expensive = "UNWIND range(1,1000) AS a UNWIND range(1,1000) AS b UNWIND range(1,1000) AS d WITH a+b+d AS s WHERE s>0 RETURN sum(s)"
	t.Run("server_timeout_and_write_rollback", func(t *testing.T) {
		key := newKey()
		if _, err := c.query(ctx, key, "CREATE (:Probe) RETURN 1", nil, false, MaxQueryTime); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := c.query(ctx, key, expensive, nil, true, 25*time.Millisecond); !errors.Is(err, ErrTimeout) {
			t.Fatalf("read timeout: %v", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("read exceeded timeout tolerance")
		}
		if _, err := c.query(ctx, key, "UNWIND range(1,1000000) AS i CREATE (:Rollback {id:i}) RETURN count(*)", nil, false, 10*time.Millisecond); !errors.Is(err, ErrTimeout) {
			t.Fatalf("write timeout: %v", err)
		}
		rows, err := c.query(ctx, key, "MATCH (n:Rollback) RETURN count(n)", nil, true, MaxQueryTime)
		if err != nil || !reflect.DeepEqual(rows, [][]any{{int64(0)}}) {
			t.Fatalf("rollback failed: %v %v", rows, err)
		}
	})
	t.Run("cancel_closes_socket_and_discards_result", func(t *testing.T) {
		for iteration := 0; iteration < 10; iteration++ {
			key := newKey()
			if _, err := c.query(ctx, key, "CREATE (:Probe) RETURN 1", nil, false, MaxQueryTime); err != nil {
				t.Fatal(err)
			}
			request, stop := context.WithCancel(ctx)
			defer stop()
			done := make(chan error, 1)
			go func() {
				rows, err := c.query(request, key, expensive, nil, true, 500*time.Millisecond)
				if rows != nil {
					done <- fmt.Errorf("cancelled result leaked")
					return
				}
				done <- err
			}()
			// Observe actual submission, not an arbitrary sleep before cancellation.
			deadline := time.Now().Add(time.Second)
			for {
				clients, err := observer.ClientList(ctx).Result()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(strings.ToLower(clients), "cmd=graph.ro_query") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("query not observed in server")
				}
				time.Sleep(5 * time.Millisecond)
			}
			start := time.Now()
			stop()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if time.Since(start) > 300*time.Millisecond {
				t.Fatal("local connection not released promptly")
			}
			// The server's query budget, not closing the socket, ends remote work.
			// A write on the same graph must become possible again within tolerance.
			if _, err := c.query(ctx, key, "CREATE (:AfterCancel) RETURN 1", nil, false, MaxQueryTime); err != nil {
				t.Fatal(err)
			}
			// TCP close is locally synchronous but its FIN is consumed by the
			// server event loop asynchronously, including the probe connection.
			deadline = time.Now().Add(time.Second)
			for {
				clients, err := observer.ClientList(ctx).Result()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(strings.TrimSpace(clients), "\n") == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("connection residual after recovery deadline")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	})
	t.Run("concurrent_connections_are_isolated", func(t *testing.T) {
		key := newKey()
		if _, err := c.query(ctx, key, "CREATE (:Probe) RETURN 1", nil, false, MaxQueryTime); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rows, err := c.query(ctx, key, "RETURN $id", map[string]any{"id": int64(i)}, true, MaxQueryTime)
				if err != nil || !reflect.DeepEqual(rows, [][]any{{int64(i)}}) {
					t.Errorf("crossed reply: %v %v", rows, err)
				}
			}(i)
		}
		wg.Wait()
	})
}
