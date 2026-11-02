package resourcequery

import (
	"time"
)

func EngineCatalog(kind string) []Definition {
	switch kind {
	case "postgresql":
		return PostgreSQLCatalog()
	case "redis":
		return RedisCatalog()
	case "minio":
		return MinIOCatalog()
	default:
		return nil
	}
}
func NewEnginePlan(kind string, keys []string, start, end, now time.Time, trend bool, b Budget) (Plan, error) {
	if len(EngineCatalog(kind)) == 0 {
		return Plan{}, ErrInvalid
	}
	return newPlan(keys, start, end, now, trend, nil, b, EngineCatalog(kind))
}
func NewEngineSummaryPlan(kind string, count int, at time.Time, b Budget) (Plan, error) {
	if kind == "postgresql" {
		return NewPostgreSQLSummaryPlan(count, at, b)
	}
	if (kind != "redis" && kind != "minio") || count < 1 || count > 100 {
		return Plan{}, ErrInvalid
	}
	keys := []string{"redis.memory.allocated_bytes", "redis.memory.used_percent", "redis.connections.total", "redis.commands.per_second"}
	if kind == "minio" {
		keys = []string{"minio.capacity.total_bytes", "minio.capacity.free_bytes", "minio.capacity.used_percent", "minio.requests.per_second"}
	}
	p, err := NewEnginePlan(kind, keys, at, at, at, false, b)
	if err == nil && (count*len(p.Metrics) > b.MaxSeries || count*len(p.Metrics) > b.MaxTotalPoints) {
		return Plan{}, ErrBudget
	}
	return p, err
}
func counterWindow(raw string) string {
	previous := raw + ` offset 1m`
	rate := `rate(` + raw + `[1m])`
	return `(` + rate + ` and (count_over_time(` + raw + `[1m])>=4) and (resets(` + raw + `[1m])==0) and (` + raw + `>=` + previous + `) and (timestamp(` + previous + `)>=time()-76) and (timestamp(` + raw + `)>=time()-16) and (` + rate + `>=0))`
}
