// deployment-metrics-source reads fixed product health and aggregate metrics on the deployment network.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/addp/monitor/internal/sourceserver"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

var unavailable = errors.New("deployment observation unavailable")

type config struct {
	Kind     string     `json:"kind"`
	Address  netip.Addr `json:"address"`
	Password string     `json:"password,omitempty"`
}

func readJSON(ctx context.Context, address, path string, dest any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+path, nil)
	if err != nil {
		return unavailable
	}
	request.Header.Set("Accept", "application/json")
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return unavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return unavailable
	}
	const maxBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(raw) > maxBytes || json.Unmarshal(raw, dest) != nil {
		return unavailable
	}
	return nil
}

func (cfg config) snapshot(ctx context.Context) (map[string]uint64, error) {
	host := cfg.Address.String()
	switch cfg.Kind {
	case "spark":
		var master struct {
			Status       string
			AliveWorkers *uint64
			Cores        *uint64
			CoresUsed    *uint64
			ActiveApps   *[]json.RawMessage
		}
		if err := readJSON(ctx, net.JoinHostPort(host, "8080"), "/json/", &master); err != nil {
			return nil, err
		}
		if master.Status != "ALIVE" || master.AliveWorkers == nil || master.Cores == nil || master.CoresUsed == nil || *master.CoresUsed > *master.Cores || master.ActiveApps == nil {
			return nil, unavailable
		}
		return map[string]uint64{"workers_alive": *master.AliveWorkers, "cores_free": *master.Cores - *master.CoresUsed, "applications_running": uint64(len(*master.ActiveApps))}, nil
	case "neo4j":
		var discovery struct {
			Version string `json:"neo4j_version"`
		}
		if err := readJSON(ctx, net.JoinHostPort(host, "7474"), "/", &discovery); err != nil || discovery.Version == "" {
			return nil, unavailable
		}
		return map[string]uint64{"http_available": 1}, nil
	case "ftp":
		socket, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, "2121"))
		if err != nil {
			return nil, unavailable
		}
		defer socket.Close()
		deadline, _ := ctx.Deadline()
		if socket.SetDeadline(deadline) != nil {
			return nil, unavailable
		}
		line, err := bufio.NewReader(io.LimitReader(socket, 512)).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "220 ") {
			return nil, unavailable
		}
		return map[string]uint64{"control_available": 1}, nil
	case "mysql":
		dsn := mysql.NewConfig()
		dsn.User = "addp_metrics_reader"
		dsn.Passwd = cfg.Password
		dsn.Net = "tcp"
		dsn.Addr = net.JoinHostPort(host, "3306")
		connector, err := mysql.NewConnector(dsn)
		if err != nil {
			return nil, unavailable
		}
		db := sql.OpenDB(connector)
		defer db.Close()
		rows, err := db.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Uptime')")
		if err != nil {
			return nil, unavailable
		}
		defer rows.Close()
		values := map[string]uint64{}
		for rows.Next() {
			var key string
			var value uint64
			if rows.Scan(&key, &value) != nil {
				return nil, unavailable
			}
			values[strings.ToLower(key)] = value
		}
		if rows.Err() != nil || len(values) != 3 {
			return nil, unavailable
		}
		return values, nil
	case "falkordb":
		client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(host, "6379"), Username: "addp_metrics_reader", Password: cfg.Password, Protocol: 2, MaxRetries: -1, DisableIdentity: true, ContextTimeoutEnabled: true})
		defer client.Close()
		values := map[string]uint64{}
		for _, section := range []string{"clients", "memory"} {
			report, err := client.Info(ctx, section).Result()
			if err != nil {
				return nil, unavailable
			}
			for _, line := range strings.Split(report, "\r\n") {
				key, value, _ := strings.Cut(line, ":")
				if key == "connected_clients" || key == "used_memory" {
					number, err := strconv.ParseUint(value, 10, 64)
					if err != nil {
						return nil, unavailable
					}
					values[key] = number
				}
			}
		}
		if len(values) != 2 {
			return nil, unavailable
		}
		return values, nil
	}
	return nil, unavailable
}

func main() {
	raw, err := os.ReadFile("/source/config.json")
	var cfg config
	if err != nil || json.Unmarshal(raw, &cfg) != nil || !cfg.Address.IsPrivate() || ((cfg.Kind == "mysql" || cfg.Kind == "falkordb") && cfg.Password == "") {
		os.Exit(1)
	}
	handler := sourceserver.Handler(func(parent context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		values, err := cfg.snapshot(ctx)
		if err != nil {
			return nil, err
		}
		var output strings.Builder
		for key, value := range values {
			fmt.Fprintf(&output, "addp_%s_%s %d\n", cfg.Kind, key, value)
		}
		fmt.Fprintf(&output, "addp_%s_observed_at_seconds %d\n", cfg.Kind, time.Now().Unix())
		return []byte(output.String()), nil
	})
	if sourceserver.Serve(handler, "/source") != nil {
		os.Exit(1)
	}
}
