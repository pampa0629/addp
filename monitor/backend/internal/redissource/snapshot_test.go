package redissource

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func fixture() string {
	return "# Server\r\nredis_version:7.2.13\r\nredis_mode:standalone\r\nrun_id:" + strings.Repeat("a", 40) + `
uptime_in_seconds:3600
config_file:/private/not-for-output
# Clients
connected_clients:4
maxclients:10000
blocked_clients:1
# Memory
used_memory:1000
used_memory_rss:1500
maxmemory:2000
mem_not_counted_for_evict:100
# Persistence
loading:0
aof_enabled:1
rdb_last_bgsave_status:ok
aof_last_write_status:err
aof_last_bgrewrite_status:ok
# Stats
total_commands_processed:9007199254740993
keyspace_hits:30
keyspace_misses:10
evicted_keys:0
rejected_connections:2
# Errorstats
errorstat_OOM:count=3
errorstat_private-error:count=55
`
}

func TestSnapshotFixedProjectionPreservesIntegerAndSeparatesStates(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	s, err := Parse(fixture(), at)
	if err != nil {
		t.Fatal(err)
	}
	if s.Values["total_commands_processed"] != 9007199254740993 || s.Values["memory_counted_for_limit_bytes"] != 900 ||
		s.Values["oom_errors_total"] != 3 || s.Values["aof_last_write_status"] != 0 || s.Values["aof_last_bgrewrite_status"] != 1 ||
		len(s.Values) != 20 || s.SampledAt.Location() != time.UTC || !s.SampledAt.Equal(at) {
		t.Fatal("fixed numeric projection changed")
	}
	for key := range s.Values {
		if strings.Contains(key, "private") || key == "config_file" || key == "run_id" {
			t.Fatal("raw INFO metadata escaped fixed projection")
		}
	}
	noAOF := strings.Replace(fixture(), "aof_enabled:1", "aof_enabled:0", 1)
	noAOF = strings.ReplaceAll(noAOF, "aof_last_write_status:err\naof_last_bgrewrite_status:ok\n", "")
	noAOF = strings.ReplaceAll(noAOF, "errorstat_OOM:count=3\n", "")
	noAOF = strings.Replace(noAOF, "maxmemory:2000", "maxmemory:0", 1)
	s, err = Parse(noAOF, at)
	if err != nil || s.Values["maxmemory"] != 0 || s.Values["oom_errors_total"] != 0 {
		t.Fatal("unlimited memory or valid zero OOM rejected")
	}
	if _, ok := s.Values["aof_last_write_status"]; ok {
		t.Fatal("disabled AOF manufactured a write success/failure")
	}
}

func TestSnapshotRejectsUnknownMissingMalformedAndOversizedFacts(t *testing.T) {
	cases := []string{
		strings.Replace(fixture(), "# Errorstats", "# Other", 1),
		strings.Replace(fixture(), "errorstat_OOM:count=3", "errorstat_ERRORSTATS_DISABLED:count=1", 1),
		strings.Replace(fixture(), "used_memory:1000\n", "", 1),
		strings.Replace(fixture(), "blocked_clients:1", "blocked_clients:1\nblocked_clients:2", 1),
		strings.Replace(fixture(), "blocked_clients:1", "blocked_clients:NaN", 1),
		strings.Replace(fixture(), "blocked_clients:1", "blocked_clients:-1", 1),
		strings.Replace(fixture(), "blocked_clients:1", "blocked_clients:1e3", 1),
		strings.Replace(fixture(), "blocked_clients:1", "blocked_clients:18446744073709551616", 1),
		strings.Replace(fixture(), "loading:0", "loading:2", 1),
		strings.Replace(fixture(), "rdb_last_bgsave_status:ok", "rdb_last_bgsave_status:unknown", 1),
		strings.Replace(fixture(), "maxclients:10000", "maxclients:0", 1),
		strings.Replace(fixture(), "mem_not_counted_for_evict:100", "mem_not_counted_for_evict:1001", 1),
		strings.Replace(fixture(), "redis_mode:standalone", "redis_mode:cluster", 1),
		strings.Replace(fixture(), "redis_mode:standalone", "redis_mode:sentinel", 1),
		strings.Replace(fixture(), "redis_version:7.2.13", "redis_version:8.0.0", 1),
		strings.Replace(fixture(), "errorstat_OOM:count=3", "errorstat_OOM:count=3,untrusted=secret", 1),
		fixture() + "# Server\n",
		fixture() + "untrusted:\x00secret",
		strings.Repeat("x", MaxInfoBytes+1),
		fixture() + "untrusted:" + strings.Repeat("x", 4096),
	}
	for i, input := range cases {
		if s, err := Parse(input, time.Now()); !errors.Is(err, ErrInvalidSnapshot) || s.Values != nil {
			t.Errorf("case %d accepted invalid snapshot", i)
		}
	}
	if _, err := Parse(fixture(), time.Time{}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatal("accepted missing observation time")
	}
}

func TestSnapshotRuntimeBoundaryRejectsRestartAndBackwardUptime(t *testing.T) {
	s, err := Parse(fixture(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyBoundary(fixture()); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{
		strings.ReplaceAll(fixture(), strings.Repeat("a", 40), strings.Repeat("b", 40)),
		strings.Replace(fixture(), "uptime_in_seconds:3600", "uptime_in_seconds:3599", 1),
	} {
		if err := s.VerifyBoundary(boundary); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatal("mixed Redis runtime accepted")
		}
	}
}

func TestObserverACLIsCompleteAndScopeIsolated(t *testing.T) {
	hash := sha256.Sum256([]byte("private credential"))
	for _, scope := range []string{"infra", "business"} {
		args, err := ObserverACL(scope, hex.EncodeToString(hash[:]), true)
		if err != nil || !reflect.DeepEqual(args, []string{"ACL", "SETUSER", "addp_" + scope + "_observer", "reset", "on", "#" + hex.EncodeToString(hash[:]), "+hello", "+ping", "+info"}) {
			t.Fatal("observer ACL widened")
		}
		args, err = ObserverACL(scope, "", false)
		if err != nil || !reflect.DeepEqual(args, []string{"ACL", "SETUSER", "addp_" + scope + "_observer", "reset", "off"}) {
			t.Fatal("off account retains permissions or password")
		}
	}
	for _, input := range []struct {
		scope, hash string
		enabled     bool
	}{
		{"tenant", strings.Repeat("a", 64), true}, {"business", "", true},
		{"infra", strings.Repeat("A", 64), true}, {"business", strings.Repeat("a", 64), false},
	} {
		if args, err := ObserverACL(input.scope, input.hash, input.enabled); !errors.Is(err, ErrInvalidAccount) || args != nil {
			t.Fatal("accepted invalid account declaration")
		}
	}
}

func TestSourceProtocolHookRejectsFallbackAndUndeclaredCommands(t *testing.T) {
	h := fixedCommands{user: "addp_business_observer", password: strings.Repeat("x", 32)}
	called := 0
	process := h.ProcessHook(func(context.Context, redis.Cmder) error { called++; return nil })
	for _, args := range [][]interface{}{
		{"hello", 3, "auth", h.user, h.password}, {"ping"}, infoArguments, {"info", "server"},
	} {
		if err := process(context.Background(), redis.NewCmd(context.Background(), args...)); err != nil {
			t.Fatal("fixed source command rejected")
		}
	}
	before := called
	for _, args := range [][]interface{}{
		{"auth", h.user, h.password}, {"hello", 2, "auth", h.user, h.password},
		{"info"}, {"info", "all"}, {"info", "keyspace"}, {"config", "get", "*"},
		{"get", "business-key"}, {"client", "setinfo", "lib-name", "client"},
	} {
		cmd := redis.NewCmd(context.Background(), args...)
		if !errors.Is(process(context.Background(), cmd), ErrUnavailable) {
			t.Fatal("undeclared source command accepted")
		}
		pipeline := h.ProcessPipelineHook(func(context.Context, []redis.Cmder) error { called++; return nil })
		if !errors.Is(pipeline(context.Background(), []redis.Cmder{cmd}), ErrUnavailable) {
			t.Fatal("undeclared pipelined source command accepted")
		}
	}
	if called != before {
		t.Fatal("forbidden command reached transport")
	}
}

func TestSourceRejectsUnownedEndpointAndBadCredentialWithoutDial(t *testing.T) {
	for _, address := range []string{"0.0.0.0:6379", "8.8.8.8:6379", "[::ffff:127.0.0.1]:6379", "[fe80::1%en0]:6379", "127.0.0.1:0"} {
		if _, err := Read(context.Background(), Target{Address: netip.MustParseAddrPort(address), Scope: "business", Password: strings.Repeat("x", 32)}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("source accepted invalid endpoint")
		}
	}
	if _, err := Read(context.Background(), Target{Address: netip.MustParseAddrPort("127.0.0.1:6379"), Scope: "tenant", Password: "bad\nsecret"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("source accepted invalid scope/credentials")
	}
}

func TestSourceSanitizesUpstreamErrorAndHonorsWholeReadCancellation(t *testing.T) {
	for _, sendError := range []bool{true, false} {
		t.Run(strconv.FormatBool(sendError), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				reader := bufio.NewReader(conn)
				line, err := reader.ReadString('\n')
				if err != nil || line != "*5\r\n" {
					done <- errors.New("unexpected handshake shape")
					return
				}
				for i := 0; i < 5; i++ {
					header, err := reader.ReadString('\n')
					if err != nil || !strings.HasPrefix(header, "$") {
						done <- errors.New("invalid handshake bulk")
						return
					}
					size, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "$"), "\r\n"))
					if err != nil || size < 0 || size > 128 {
						done <- errors.New("unexpected handshake argument size")
						return
					}
					if _, err := io.CopyN(io.Discard, reader, int64(size+2)); err != nil {
						done <- err
						return
					}
				}
				if sendError {
					_, _ = io.WriteString(conn, "-ERR private-upstream-content\r\n")
				}
				// There must be no legacy AUTH fallback, retry or later command.
				_, err = reader.ReadByte()
				if err != io.EOF {
					done <- errors.New("source failed to close or sent another command")
					return
				}
				done <- nil
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			snapshot, err := Read(ctx, Target{Address: netip.MustParseAddrPort(listener.Addr().String()), Scope: "business", Password: strings.Repeat("x", 32)})
			if err != ErrUnavailable || snapshot.Values != nil || time.Since(start) > time.Second {
				t.Fatal("source leaked upstream error, retained data or exceeded cancellation budget")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
