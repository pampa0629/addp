package redissource

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

var infoArguments = []interface{}{"info", "server", "clients", "memory", "persistence", "stats", "errorstats"}

// Target is supplied by a deployment-owned source, never by browser input.
// Address is the exact loopback/private IP endpoint, not a URL or discovery rule.
type Target struct {
	Address  netip.AddrPort
	Scope    string
	Password string
}

func (t Target) valid() bool {
	addr := t.Address.Addr()
	return t.Address.IsValid() && t.Address.Port() != 0 && !addr.Is4In6() && addr.Zone() == "" && !addr.IsUnspecified() &&
		(addr.IsPrivate() || addr.IsLoopback()) && (t.Scope == "infra" || t.Scope == "business") &&
		len(t.Password) >= 32 && len(t.Password) <= 128 && !containsControl(t.Password)
}

func containsControl(s string) bool {
	for _, c := range s {
		if c < 33 || c > 126 {
			return true
		}
	}
	return false
}

// fixedCommands enforces our exact protocol even if the shared client adds a
// new handshake, identity or fallback command in a later dependency version.
type fixedCommands struct{ user, password string }

func (h fixedCommands) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h fixedCommands) allows(cmd redis.Cmder) bool {
	args := cmd.Args()
	return reflect.DeepEqual(args, infoArguments) ||
		reflect.DeepEqual(args, []interface{}{"info", "server"}) ||
		reflect.DeepEqual(args, []interface{}{"ping"}) ||
		reflect.DeepEqual(args, []interface{}{"hello", 3, "auth", h.user, h.password})
}
func (h fixedCommands) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if !h.allows(cmd) {
			return ErrUnavailable
		}
		return next(ctx, cmd)
	}
}
func (h fixedCommands) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if !h.allows(cmd) {
				return ErrUnavailable
			}
		}
		return next(ctx, cmds)
	}
}

// Read performs one three-second observation, without retries or stale cache.
// No upstream error, INFO text, credentials or endpoint is returned on failure.
func Read(ctx context.Context, target Target) (Snapshot, error) {
	if !target.valid() {
		return Snapshot{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	user := "addp_" + target.Scope + "_observer"
	client := redis.NewClient(&redis.Options{
		Addr: target.Address.String(), Username: user, Password: target.Password,
		Protocol: 3, DisableIdentity: true, MaxRetries: -1,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
		PoolSize:                 1, MaxActiveConns: 1, MinIdleConns: 0,
		ContextTimeoutEnabled: true, DialTimeout: 3 * time.Second,
		ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
		},
	})
	defer client.Close()
	client.AddHook(fixedCommands{user: user, password: target.Password})
	if value, err := client.Ping(ctx).Result(); err != nil || value != "PONG" {
		return Snapshot{}, ErrUnavailable
	}
	info, err := client.Do(ctx, infoArguments...).Text()
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	snapshot, err := Parse(info, time.Now())
	if err != nil {
		return Snapshot{}, err
	}
	boundary, err := client.Info(ctx, "server").Result()
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	if err := snapshot.VerifyBoundary(boundary); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}
