package resourcequery

import (
	"time"
)

var productCatalog = map[string][]Definition{
	"falkordb": {{"falkordb.connected_clients", "connections", 0}, {"falkordb.used_memory", "bytes", 0}},
	"mysql":    {{"mysql.threads_connected", "connections", 0}, {"mysql.threads_running", "threads", 0}, {"mysql.uptime", "seconds", 0}},
	"neo4j":    {{"neo4j.http_available", "status", 0}},
	"ftp":      {{"ftp.control_available", "status", 0}},
	"spark":    {{"spark.workers_alive", "workers", 0}, {"spark.cores_free", "cores", 0}, {"spark.applications_running", "applications", 0}},
}

func EngineCatalog(kind string) []Definition {
	switch kind {
	case "postgresql":
		return PostgreSQLCatalog()
	case "redis":
		return RedisCatalog()
	case "minio":
		return MinIOCatalog()
	default:
		return append([]Definition(nil), productCatalog[kind]...)
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
	if len(EngineCatalog(kind)) == 0 || count < 1 || count > 100 {
		return Plan{}, ErrInvalid
	}
	keys := []string{"redis.memory.allocated_bytes", "redis.memory.used_percent", "redis.connections.total", "redis.commands.per_second"}
	if kind == "minio" {
		keys = []string{"minio.capacity.total_bytes", "minio.capacity.free_bytes", "minio.capacity.used_percent", "minio.requests.per_second"}
	}
	if defs := productCatalog[kind]; len(defs) > 0 {
		keys = nil
		for _, d := range defs {
			keys = append(keys, d.Key)
		}
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
