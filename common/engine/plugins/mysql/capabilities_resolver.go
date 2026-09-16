package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

var _ plugin.InstanceCapabilitiesResolver = (*MySQLPlugin)(nil)

func (p *MySQLPlugin) ResolveCapabilities(ctx context.Context, info plugin.ConnectionInfo, base plugin.EngineCapabilities) (plugin.EngineCapabilities, error) {
	base, err := plugin.WithAnalyticalCapability(base, plugin.AnalyticalCapability{})
	if err != nil {
		return base, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return base, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return base, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return base, err
	}
	defer conn.Close()
	report, err := certifyAnalyticalInstance(ctx, conn)
	if err != nil || !report.Supported {
		return base, err
	}
	return plugin.WithAnalyticalCapability(base, plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}})
}
