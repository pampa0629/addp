package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/addp/common/engine/plugin"
)

func (p *PostgreSQLPlugin) ReadInstanceOverview(ctx context.Context, info plugin.ConnectionInfo) (*plugin.InstanceOverview, error) {
	if p.identity != nil {
		return nil, fmt.Errorf("instance overview is not certified for protocol-compatible engines")
	}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(p.sqlDriverName(), dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var database string
	var trackCounts bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_catalog.current_database(), pg_catalog.current_setting('track_counts')::boolean`).Scan(&database, &trackCounts); err != nil {
		return nil, err
	}
	metrics := []plugin.InstanceOverviewMetric{
		{Key: "database_size_bytes", Scope: "database", ScopeName: database, Unit: "bytes", Semantics: "gauge", Status: "unavailable"},
		{Key: "database_connections", Scope: "database", ScopeName: database, Unit: "count", Semantics: "gauge", Status: "unavailable"},
		{Key: "transactions_committed", Scope: "database", ScopeName: database, Unit: "count", Semantics: "counter", Status: "unavailable"},
		{Key: "transactions_rolled_back", Scope: "database", ScopeName: database, Unit: "count", Semantics: "counter", Status: "unavailable"},
	}
	permissionDenied := func(err error) bool {
		var state interface{ SQLState() string }
		return errors.As(err, &state) && state.SQLState() == "42501"
	}
	var size int64
	if err := conn.QueryRowContext(ctx, `SELECT pg_catalog.pg_database_size(pg_catalog.current_database())`).Scan(&size); err != nil {
		if !permissionDenied(err) {
			return nil, err
		}
		metrics[0].Status = "permission_denied"
	} else if size >= 0 {
		metrics[0].SetValue(uint64(size))
	}
	var counts [3]sql.NullInt64
	err = conn.QueryRowContext(ctx, `SELECT numbackends, xact_commit, xact_rollback FROM pg_catalog.pg_stat_database WHERE datname = pg_catalog.current_database()`).Scan(&counts[0], &counts[1], &counts[2])
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		if !permissionDenied(err) {
			return nil, err
		}
		for i := 1; i < len(metrics); i++ {
			metrics[i].Status = "permission_denied"
		}
	} else if err == nil {
		for i, value := range counts {
			if value.Valid && value.Int64 >= 0 && (i == 0 || trackCounts) {
				metrics[i+1].SetValue(uint64(value.Int64))
			}
		}
	}
	return &plugin.InstanceOverview{Metrics: metrics}, nil
}
