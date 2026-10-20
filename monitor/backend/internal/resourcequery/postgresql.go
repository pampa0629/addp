package resourcequery

import "time"

// PostgreSQL observation is an instance-wide fixed catalog. These counters
// include observation traffic and never represent tenant/business throughput.
var postgresqlDefinitions = []Definition{
	{"postgresql.connections.total", "connections", 0},
	{"postgresql.connections.active", "connections", 0},
	{"postgresql.connections.idle", "connections", 0},
	{"postgresql.connections.idle_in_transaction", "connections", 0},
	{"postgresql.connections.waiting", "connections", 0},
	{"postgresql.connections.lock_waiting", "connections", 0},
	{"postgresql.connections.io_waiting", "connections", 0},
	{"postgresql.connections.maximum", "connections", 0},
	{"postgresql.transactions.committed_per_second", "transactions_per_second", 60},
	{"postgresql.transactions.rolled_back_per_second", "transactions_per_second", 60},
	{"postgresql.deadlocks.per_second", "events_per_second", 60},
	{"postgresql.database.total_bytes", "bytes", 0},
}

func PostgreSQLCatalog() []Definition { return append([]Definition(nil), postgresqlDefinitions...) }

// NewPostgreSQLPlan reuses the resource query policy without accepting device
// dimensions, arbitrary SQL/PromQL or another domain's metric names.
func NewPostgreSQLPlan(keys []string, start, end, now time.Time, trend bool, b Budget) (Plan, error) {
	return newPlan(keys, start, end, now, trend, nil, b, postgresqlDefinitions)
}

func NewPostgreSQLSummaryPlan(count int, at time.Time, b Budget) (Plan, error) {
	if count < 1 || count > 100 {
		return Plan{}, ErrInvalid
	}
	p, err := NewPostgreSQLPlan([]string{"postgresql.connections.total", "postgresql.connections.active",
		"postgresql.transactions.committed_per_second", "postgresql.database.total_bytes"}, at, at, at, false, b)
	if err == nil && (count*len(p.Metrics) > b.MaxSeries || count*len(p.Metrics) > b.MaxTotalPoints) {
		return Plan{}, ErrBudget
	}
	return p, err
}
