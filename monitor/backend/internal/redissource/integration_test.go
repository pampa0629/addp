package redissource

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

func testClient(address, user, password string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: address, Username: user, Password: password, Protocol: 3,
		DisableIdentity: true, MaxRetries: -1, PoolSize: 1, MaxActiveConns: 1,
		ContextTimeoutEnabled: true, DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	})
}

func TestIntegrationRedisObservationACLAndFixedSnapshot(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("requires the standard owned Redis T2 gate")
	}
	address, err := netip.ParseAddrPort(os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	if err != nil || !address.Addr().IsLoopback() || !strings.HasPrefix(os.Getenv("ADDP_REDIS_T2_PROJECT"), "addp-redis-t2-") {
		t.Fatal("Redis source test requires the disposable loopback fixture")
	}
	adminPassword := os.Getenv("BUSINESS_REDIS_ADMIN_PASSWORD")
	if len(adminPassword) < 32 {
		t.Fatal("missing disposable admin credential")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := testClient(address.String(), "addp_business_admin", adminPassword)
	defer admin.Close()
	before, err := admin.Do(ctx, "ACL", "GETUSER", "addp_business_reader").Result()
	if err != nil {
		t.Fatal("could not observe existing reader ACL")
	}
	keysBefore, err := admin.DBSize(ctx).Result()
	if err != nil {
		t.Fatal("could not observe existing fixture key count")
	}
	for _, scope := range []string{"infra", "business"} {
		t.Run(scope, func(t *testing.T) {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				t.Fatal("random credential unavailable")
			}
			password := hex.EncodeToString(secret)
			hash := sha256.Sum256([]byte(password))
			args, err := ObserverACL(scope, hex.EncodeToString(hash[:]), true)
			if err != nil {
				t.Fatal(err)
			}
			user := "addp_" + scope + "_observer"
			if prior, err := admin.Do(ctx, "ACL", "GETUSER", user).Result(); (err != nil && !errors.Is(err, redis.Nil)) || prior != nil {
				t.Fatal("refusing to adopt an existing observer account")
			}
			command := make([]interface{}, len(args))
			for i, arg := range args {
				command[i] = arg
			}
			if err := admin.Do(ctx, command...).Err(); err != nil {
				t.Fatal("observer installation rejected")
			}
			defer func() {
				if err := admin.ACLDelUser(context.Background(), user).Err(); err != nil {
					t.Error("owned observer account cleanup failed")
				}
			}()
			observer := testClient(address.String(), user, password)
			defer observer.Close()
			if err := observer.Ping(ctx).Err(); err != nil {
				t.Fatal("observer authentication failed")
			}
			if err := admin.ACLLogReset(ctx).Err(); err != nil {
				t.Fatal("could not reset disposable ACL diagnostics")
			}
			target := Target{Address: address, Scope: scope, Password: password}
			snapshot, err := Read(ctx, target)
			if err != nil {
				t.Fatalf("fixed source read failed: %v", err)
			}
			if snapshot.Values["used_memory"] == 0 || snapshot.Values["maxmemory"] != 256*1024*1024 ||
				snapshot.Values["aof_enabled"] != 1 || snapshot.Values["maxclients"] == 0 || !runIDPattern.MatchString(snapshot.RunID) {
				t.Fatal("real INFO did not produce expected fixed statistics")
			}
			if entries, err := admin.ACLLog(ctx, 128).Result(); err != nil || len(entries) != 0 {
				t.Fatal("source issued an undeclared/denied command")
			}
			// No pressure loop or memory allocation: the disposable admin sets a
			// one-byte logical limit so one small SET is rejected before writing.
			if err := admin.ConfigSet(ctx, "maxmemory", "1").Err(); err != nil {
				t.Fatal("could not set disposable limit")
			}
			defer func() {
				if err := admin.ConfigSet(context.Background(), "maxmemory", "256mb").Err(); err != nil {
					t.Error("could not restore disposable limit")
				}
			}()
			if err := admin.Set(ctx, "addp:observation:must-not-write", "x", 0).Err(); err == nil || !strings.HasPrefix(err.Error(), "OOM") {
				t.Fatal("disposable noeviction limit did not reject the probe write")
			}
			limited, err := Read(ctx, target)
			if err != nil || limited.Values["oom_errors_total"] <= snapshot.Values["oom_errors_total"] || limited.Values["maxmemory"] != 1 || limited.Values["memory_counted_for_limit_bytes"] <= 1 {
				t.Fatal("real OOM count/over-limit value was lost or clipped")
			}
			if err := admin.ConfigSet(ctx, "maxmemory", "0").Err(); err != nil {
				t.Fatal("could not set disposable unlimited limit")
			}
			unlimited, err := Read(ctx, target)
			if err != nil || unlimited.Values["maxmemory"] != 0 {
				t.Fatal("zero logical limit was replaced by host memory")
			}
			if err := admin.ConfigSet(ctx, "maxmemory", "256mb").Err(); err != nil {
				t.Fatal("could not restore disposable limit")
			}
			for _, arguments := range [][]interface{}{
				{"GET", "addp:sample:string"}, {"SCAN", "0"}, {"KEYS", "*"}, {"DBSIZE"},
				{"SET", "addp:sample:write-probe", "denied"}, {"FLUSHALL"},
				{"CONFIG", "GET", "requirepass"}, {"ACL", "LIST"}, {"CLIENT", "LIST"},
				{"SLOWLOG", "GET", "1"}, {"MONITOR"}, {"EVAL", "return 1", "0"},
				{"PUBLISH", "channel", "data"}, {"SUBSCRIBE", "channel"}, {"SELECT", "0"},
			} {
				if err := observer.Do(ctx, arguments...).Err(); err == nil || !strings.HasPrefix(err.Error(), "NOPERM") {
					t.Fatalf("observer must reject %s", arguments[0])
				}
			}
			// ACL deliberately permits INFO metadata broadly. The source hook,
			// not a fictitious server field ACL, narrows actual source requests.
			if err := observer.Info(ctx, "all").Err(); err != nil {
				t.Fatal("INFO account boundary did not match the confirmed scope")
			}
			wrong := target
			wrong.Password = strings.Repeat("x", 64)
			if s, err := Read(ctx, wrong); !errors.Is(err, ErrUnavailable) || s.Values != nil {
				t.Fatal("wrong password did not fail closed")
			}
			other := target
			if scope == "infra" {
				other.Scope = "business"
			} else {
				other.Scope = "infra"
			}
			// The other scope must actually exist and authenticate with its own
			// credential; failure against a missing user proves no isolation.
			otherUser := "addp_" + other.Scope + "_observer"
			if prior, err := admin.Do(ctx, "ACL", "GETUSER", otherUser).Result(); (err != nil && !errors.Is(err, redis.Nil)) || prior != nil {
				t.Fatal("refusing to adopt another scope's observer")
			}
			if _, err := rand.Read(secret); err != nil {
				t.Fatal("random second credential unavailable")
			}
			otherPassword := hex.EncodeToString(secret)
			otherHash := sha256.Sum256([]byte(otherPassword))
			otherACL, err := ObserverACL(other.Scope, hex.EncodeToString(otherHash[:]), true)
			if err != nil {
				t.Fatal(err)
			}
			otherCommand := make([]interface{}, len(otherACL))
			for i, arg := range otherACL {
				otherCommand[i] = arg
			}
			if err := admin.Do(ctx, otherCommand...).Err(); err != nil {
				t.Fatal("could not create other scoped account")
			}
			defer func() {
				if err := admin.ACLDelUser(context.Background(), otherUser).Err(); err != nil {
					t.Error("other scoped account cleanup failed")
				}
			}()
			otherOwn := other
			otherOwn.Password = otherPassword
			if _, err := Read(ctx, otherOwn); err != nil {
				t.Fatal("other scoped account did not authenticate independently")
			}
			if s, err := Read(ctx, other); !errors.Is(err, ErrUnavailable) || s.Values != nil {
				t.Fatal("observer credential crossed scope")
			}
			// Reuse the already authenticated socket after disabling the account.
			// off alone would leave it operational; reset removes its commands.
			disabled, err := ObserverACL(scope, "", false)
			if err != nil {
				t.Fatal(err)
			}
			command = make([]interface{}, len(disabled))
			for i, arg := range disabled {
				command[i] = arg
			}
			if err := admin.Do(ctx, command...).Err(); err != nil {
				t.Fatal("could not disable owned observer")
			}
			if err := observer.Ping(ctx).Err(); err == nil || !strings.HasPrefix(err.Error(), "NOPERM") {
				t.Fatal("existing observer session retained permissions after disable")
			}
			if s, err := Read(ctx, target); !errors.Is(err, ErrUnavailable) || s.Values != nil {
				t.Fatal("disabled account allowed a new source read")
			}
		})
	}
	after, err := admin.Do(ctx, "ACL", "GETUSER", "addp_business_reader").Result()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("business reader ACL changed")
	}
	if keysAfter, err := admin.DBSize(ctx).Result(); err != nil || keysAfter != keysBefore {
		t.Fatal("observation modified business keys")
	}
	reader := testClient(address.String(), "addp_business_reader", os.Getenv("BUSINESS_REDIS_READER_PASSWORD"))
	defer reader.Close()
	if value, err := reader.Get(ctx, "addp:sample:counter").Result(); err != nil || value != "9007199254740993" {
		t.Fatal("original business data/reader no longer works")
	}
}
