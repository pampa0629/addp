// Package falkor owns the single, bounded FalkorDB transport for Ontology.
// It is not a user Cypher API or an authorization boundary.
package falkor

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const MaxQueryTime = 2 * time.Second

var (
	ErrInvalid   = errors.New("invalid_graph_request")
	ErrBusy      = errors.New("graph_capacity_exceeded")
	ErrTimeout   = errors.New("graph_query_timeout")
	ErrTransport = errors.New("graph_transport_failure")
	ErrRejected  = errors.New("graph_query_rejected")
	ErrProtocol  = errors.New("graph_response_invalid")
)

type Config struct {
	Address  string
	Username string
	Password string
	TLS      *tls.Config
}

type Client struct {
	config Config
	slots  chan struct{}
}

func New(config Config) (*Client, error) {
	host, port, err := net.SplitHostPort(config.Address)
	if err != nil || host == "" || port == "" || config.Password == "" || (config.TLS != nil && config.TLS.InsecureSkipVerify) {
		return nil, ErrInvalid
	}
	if config.TLS != nil {
		config.TLS = config.TLS.Clone()
	}
	return &Client{config: config, slots: make(chan struct{}, 4)}, nil
}

// command owns a fresh physical connection. Context cancellation closes only
// that connection; the server may continue until its own timeout/rollback.
// There are deliberately no automatic retries or shared socket cancellation.
func (c *Client) command(ctx context.Context, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return nil, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, MaxQueryTime+2*time.Second)
	defer cancel()
	var mu sync.Mutex
	var conn net.Conn
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	})
	defer stop()
	client := redis.NewClient(&redis.Options{
		Addr: c.config.Address, Username: c.config.Username, Password: c.config.Password,
		Protocol: 2, MaxRetries: -1, PoolSize: 1, MaxActiveConns: 1,
		DisableIdentity: true, ContextTimeoutEnabled: true,
		DialTimeout: time.Second, ReadTimeout: MaxQueryTime + 2*time.Second, WriteTimeout: time.Second,
		Dialer: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: time.Second}
			var opened net.Conn
			var err error
			if c.config.TLS != nil {
				opened, err = (&tls.Dialer{NetDialer: dialer, Config: c.config.TLS}).DialContext(dialCtx, network, addr)
			} else {
				opened, err = dialer.DialContext(dialCtx, network, addr)
			}
			if err != nil {
				return nil, err
			}
			mu.Lock()
			defer mu.Unlock()
			if err := ctx.Err(); err != nil {
				_ = opened.Close()
				return nil, err
			}
			conn = opened
			return opened, nil
		},
	})
	defer client.Close()
	result, err := client.Do(ctx, args...).Result()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var serverError redis.Error
		if errors.As(err, &serverError) {
			if err.Error() == "Query timed out" {
				return nil, ErrTimeout
			}
			return nil, ErrRejected // Do not expose query text, credentials or server diagnostics.
		}
		return nil, ErrTransport // A write outcome is unknown, never safe to replay automatically.
	}
	return result, nil
}

// query accepts only owner-written templates. User values must be parameters.
func (c *Client) query(ctx context.Context, key, statement string, params map[string]any, readOnly bool, budget time.Duration) ([][]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !graphKeyPattern.MatchString(key) || statement == "" || len(statement) > 16384 || budget < time.Millisecond || budget > MaxQueryTime {
		return nil, ErrInvalid
	}
	encoded, err := parameters(params)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline))
		if budget < time.Millisecond {
			return nil, context.DeadlineExceeded
		}
	}
	command := "GRAPH.QUERY"
	if readOnly {
		command = "GRAPH.RO_QUERY"
	}
	result, err := c.command(ctx, command, key, encoded+statement, "timeout", budget.Milliseconds())
	if err != nil {
		return nil, err
	}
	rows, err := scalarRows(result)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return rows, err
}

func scalarRows(result any) ([][]any, error) {
	parts, ok := result.([]any)
	if !ok || len(parts) != 3 {
		return nil, ErrProtocol
	}
	header, ok := parts[0].([]any)
	if !ok || len(header) > 16 {
		return nil, ErrProtocol
	}
	rawRows, ok := parts[1].([]any)
	if !ok || len(rawRows) > 4097 {
		return nil, ErrProtocol
	}
	rows := make([][]any, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.([]any)
		if !ok || len(row) != len(header) {
			return nil, ErrProtocol
		}
		for _, value := range row {
			switch v := value.(type) {
			case nil, int64, bool, float64:
			case string:
				if len(v) > 8192 {
					return nil, ErrProtocol
				}
			default:
				return nil, ErrProtocol // No schema caches or entity decoding.
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
