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
	observer := redis.NewClient(&redis.Options{Addr: address, Password: password, Protocol: 2, DisableIdentity: true, MaxRetries: -1, ReadTimeout: 3 * time.Second, ContextTimeoutEnabled: true})
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
		if err := cleanupDisposableGraphs(cleanupCtx, observer, keys); err != nil {
			t.Error(err)
		}
	})
	const expensive = "UNWIND range(1,1000) AS a UNWIND range(1,1000) AS b UNWIND range(1,1000) AS d WITH a+b+d AS s WHERE s>0 RETURN sum(s)"
	t.Run("async_delete_reclaims_owned_telemetry", func(t *testing.T) {
		// Occupy both configured query workers so graph reclamation cannot run
		// before GRAPH.DELETE replies. This exposes the cleanup race reliably.
		var localKeys []string
		var queries []chan error
		t.Cleanup(func() {
			cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			if err := cleanupDisposableGraphs(cleanupCtx, observer, localKeys); err != nil {
				t.Error(err)
			} else {
				t.Log("graph keys and server-owned telemetry reclaimed before the cleanup deadline")
			}
			for _, result := range queries {
				select {
				case err := <-result:
					if !errors.Is(err, ErrTimeout) {
						t.Errorf("blocking read must end at its server budget: %v", err)
					}
				case <-cleanupCtx.Done():
					t.Fatal("blocking read did not finish")
				}
			}
		})
		for i := 0; i < 2; i++ {
			p, _ := Plan(fixture(t), uuid.NewString())
			localKeys = append(localKeys, p.key)
			if _, err := c.query(ctx, p.key, "CREATE (:Probe) RETURN 1", nil, false, MaxQueryTime); err != nil {
				t.Fatal(err)
			}
		}
		// Query telemetry is flushed by the server cron (at most 3 seconds),
		// independently of command completion. Observe it before blocking work.
		for _, key := range localKeys {
			deadline := time.Now().Add(5 * time.Second)
			for {
				kind, err := observer.Type(ctx, "telemetry{"+key+"}").Result()
				if err != nil || (kind != "none" && kind != "stream") {
					t.Fatalf("unexpected telemetry type: %s %v", kind, err)
				}
				if kind == "stream" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server did not flush query telemetry")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		for _, key := range localKeys {
			result := make(chan error, 1)
			queries = append(queries, result)
			go func() {
				_, err := c.query(ctx, key, expensive, nil, true, MaxQueryTime)
				result <- err
			}()
		}
		deadline := time.Now().Add(time.Second)
		for {
			clients, err := observer.ClientList(ctx).Result()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(strings.ToLower(clients), "cmd=graph.ro_query") == len(queries) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("blocking reads not observed in server")
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	t.Run("cleanup_rejects_unexpected_and_permanent_residuals", func(t *testing.T) {
		for _, scenario := range []string{"foreign_key", "foreign_stream", "owned_graph", "wrong_telemetry_type", "permanent_telemetry"} {
			t.Run(scenario, func(t *testing.T) {
				p, _ := Plan(fixture(t), uuid.NewString())
				key := "telemetry{" + p.key + "}"
				switch scenario {
				case "foreign_key", "foreign_stream":
					key = "foreign:" + uuid.NewString()
				case "owned_graph":
					key = p.key
				}
				t.Cleanup(func() {
					cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
					defer done()
					if scenario == "owned_graph" {
						if err := cleanupDisposableGraphs(cleanupCtx, observer, []string{p.key}); err != nil {
							t.Error(err)
						}
						return
					}
					if err := observer.Del(cleanupCtx, key).Err(); err != nil {
						t.Error(err)
					}
					if err := waitDisposableGraphCleanup(cleanupCtx, observer, nil); err != nil {
						t.Error(err)
					}
				})
				var err error
				switch scenario {
				case "owned_graph":
					_, err = c.query(ctx, key, "CREATE (:Probe) RETURN 1", nil, false, MaxQueryTime)
				case "foreign_stream", "permanent_telemetry":
					err = observer.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]any{"probe": "test"}}).Err()
				default:
					err = observer.Set(ctx, key, "test", 0).Err()
				}
				if err != nil {
					t.Fatal(err)
				}
				budget := time.Second
				if scenario == "permanent_telemetry" {
					budget = 50 * time.Millisecond
				}
				request, stop := context.WithTimeout(ctx, budget)
				defer stop()
				deadline, _ := request.Deadline()
				err = waitDisposableGraphCleanup(request, observer, []string{p.key})
				// A Redis command at the context deadline may return a socket
				// i/o timeout before the context timer callback runs. Both are
				// failures; compare against the actual monotonic caller deadline.
				if err == nil || !time.Now().Before(deadline) != (scenario == "permanent_telemetry") {
					t.Fatalf("cleanup accepted residual or waited for an unexpected key: %v", err)
				}
				if n, err := observer.Exists(ctx, key).Result(); err != nil || n != 1 {
					t.Fatalf("residual verification must not delete keys: %d %v", n, err)
				}
			})
		}
		closed := redis.NewClient(&redis.Options{Addr: address, Password: password, MaxRetries: -1})
		_ = closed.Close()
		if err := waitDisposableGraphCleanup(ctx, closed, nil); err == nil {
			t.Fatal("cleanup accepted an observer command error")
		}
		missing, _ := Plan(fixture(t), uuid.NewString())
		if err := cleanupDisposableGraphs(ctx, observer, []string{missing.key}); err == nil {
			t.Fatal("cleanup accepted a GRAPH.DELETE error")
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
	t.Run("platform_projection_round_trip_and_full_verification", func(t *testing.T) {
		snapshot, generation := platformFixture(t), uuid.NewString()
		p, err := PlanPlatform(snapshot, generation)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, p.key)
		if err := c.Build(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := c.Build(ctx, p); !errors.Is(err, ErrRejected) {
			t.Fatal(err)
		}
		got, err := c.PlatformContext(ctx, snapshot, generation)
		want, _ := snapshot.Context()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("graph dependency expansion changed frozen context", got, err)
		}
		if _, err := c.query(ctx, p.key, "CREATE (:Definition {id:'extra',kind:'name',name:'多余'}) RETURN 1", nil, false, MaxQueryTime); err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(ctx, p); !errors.Is(err, ErrProtocol) {
			t.Fatal("extra node accepted", err)
		}
		if _, err := c.PlatformContext(ctx, snapshot, generation); !errors.Is(err, ErrProtocol) {
			t.Fatal("corrupt graph fell back to definition", err)
		}
		if _, err := c.PlatformContext(ctx, snapshot, uuid.NewString()); err == nil {
			t.Fatal("absent generation fell back to definition")
		}
		maximumSnapshot, maximumGeneration := maximumPlatformFixture(t), uuid.NewString()
		maximum, err := PlanPlatform(maximumSnapshot, maximumGeneration)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, maximum.key)
		if err := c.Build(ctx, maximum); err != nil {
			t.Fatal("maximum platform graph", err)
		}
		if err := c.Verify(ctx, maximum); err != nil {
			t.Fatal("maximum platform verification", err)
		}
		got, err = c.PlatformContext(ctx, maximumSnapshot, maximumGeneration)
		want, _ = maximumSnapshot.Context()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("maximum dependency expansion", err)
		}
		evidenceSnapshot, evidenceGeneration := maximumEvidenceFixture(t), uuid.NewString()
		evidence, err := PlanPlatform(evidenceSnapshot, evidenceGeneration)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, evidence.key)
		if err := c.Build(ctx, evidence); err != nil {
			t.Fatal("maximum evidence graph", err)
		}
		got, err = c.PlatformContext(ctx, evidenceSnapshot, evidenceGeneration)
		want, _ = evidenceSnapshot.Context()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("maximum three-hop evidence expansion", err)
		}
	})
	t.Run("platform_dependency_tampering_fails_closed", func(t *testing.T) {
		for _, mutate := range []string{
			"MATCH (a:Definition {kind:'capability'})-[r:Link {kind:'requires'}]->() DELETE r RETURN count(*)",
			"MATCH (a:Definition {id:'requirement:source_discovery'})-[r:Link {kind:'uses_tool'}]->() DELETE r RETURN count(*)",
			"MATCH (n:Definition {kind:'effect'}) SET n.name='incorrect.effect' RETURN count(*)",
			"MATCH (:Definition {kind:'condition'})-[r:Link {kind:'reads_via'}]->() DELETE r RETURN count(*)",
			"MATCH (c:Definition {id:'condition:user_review'}),(r:Definition {id:'requirement:optional_draft_assistance'}) CREATE (c)-[:Link {kind:'reads_via'}]->(r) RETURN count(*)",
		} {
			snapshot, generation := platformFixture(t), uuid.NewString()
			p, _ := PlanPlatform(snapshot, generation)
			keys = append(keys, p.key)
			if err := c.Build(ctx, p); err != nil {
				t.Fatal(err)
			}
			if _, err := c.query(ctx, p.key, mutate, nil, false, MaxQueryTime); err != nil {
				t.Fatal(err)
			}
			if _, err := c.PlatformContext(ctx, snapshot, generation); !errors.Is(err, ErrProtocol) {
				t.Fatal("mutated dependency accepted", err)
			}
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

// cleanupDisposableGraphs is test-only: the endpoint is an exclusively owned
// database. Never use this whole-database verification on shared Infra.
func cleanupDisposableGraphs(ctx context.Context, observer *redis.Client, keys []string) error {
	for _, key := range keys {
		if err := observer.Do(ctx, "GRAPH.DELETE", key).Err(); err != nil {
			return fmt.Errorf("graph cleanup failed: %w", err)
		}
	}
	return waitDisposableGraphCleanup(ctx, observer, keys)
}

func waitDisposableGraphCleanup(ctx context.Context, observer *redis.Client, keys []string) error {
	// GRAPH.DELETE removes graph keys synchronously, but graph contexts and
	// their telemetry streams are released asynchronously in FalkorDB 4.20.6.
	// Never replay deletion, delete streams manually, or tolerate graph keys.
	ownedStreams := make(map[string]bool, len(keys))
	for _, key := range keys {
		ownedStreams["telemetry{"+key+"}"] = true
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("disposable graph cleanup deadline: %w", err)
		}
		remaining, err := observer.DBSize(ctx).Result()
		if err != nil {
			return fmt.Errorf("disposable graph residual check failed: %w", err)
		}
		if remaining == 0 {
			return nil
		}
		residual, err := observer.Keys(ctx, "*").Result()
		if err != nil {
			return fmt.Errorf("disposable graph residual inspection failed: %w", err)
		}
		types := make(map[string]string, len(residual))
		for _, key := range residual {
			kind, err := observer.Type(ctx, key).Result()
			if err != nil {
				return fmt.Errorf("disposable graph residual type failed: %w", err)
			}
			if kind == "none" { // Reclaimed since KEYS; still require DBSIZE=0.
				continue
			}
			types[key] = kind
			if !ownedStreams[key] || kind != "stream" {
				return fmt.Errorf("unexpected disposable graph residual: key=%s type=%s", key, kind)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("disposable graph residual: %d keys=%v: %w", remaining, types, ctx.Err())
		case <-ticker.C:
		}
	}
}
