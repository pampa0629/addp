package tidb

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

type tidbAnalyticalInstanceFacts struct {
	Version, Comment                                 string
	ClientCharset, ConnectionCharset, ResultsCharset string
	Isolation                                        string
}

var tidbAnalyticalServerVersion = regexp.MustCompile(`^8\.0\.[0-9]+-TiDB-v8\.5\.[0-9]+$`)

func tidbAnalyticalVersionSupported(version, comment string) bool {
	return tidbAnalyticalServerVersion.MatchString(version) && strings.HasPrefix(comment, "TiDB Server (Apache License 2.0)")
}

func checkTiDBAnalyticalInstanceFacts(f tidbAnalyticalInstanceFacts) plugin.SupportReport {
	code := ""
	switch {
	case !tidbAnalyticalVersionSupported(f.Version, f.Comment):
		code = "analytical_server_version"
	case f.ClientCharset != "utf8mb4" || f.ConnectionCharset != "utf8mb4" || f.ResultsCharset != "utf8mb4":
		code = "analytical_encoding"
	case f.Isolation != "READ-COMMITTED" && f.Isolation != "REPEATABLE-READ" && f.Isolation != "SERIALIZABLE":
		code = "analytical_isolation"
	}
	if code != "" {
		return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: code}}}
	}
	return plugin.SupportReport{Supported: true}
}

func certifyTiDBAnalyticalInstance(ctx context.Context, session sqlcompile.InstanceProbeSession) (plugin.SupportReport, error) {
	if session == nil {
		return plugin.SupportReport{}, plugin.ErrAnalyticalInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var facts tidbAnalyticalInstanceFacts
	if err := session.QueryRowContext(ctx, `SELECT @@version, @@version_comment`).Scan(&facts.Version, &facts.Comment); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return plugin.SupportReport{}, fmt.Errorf("tidb analytical instance version: %w", err)
	}
	if !tidbAnalyticalVersionSupported(facts.Version, facts.Comment) {
		return checkTiDBAnalyticalInstanceFacts(facts), nil
	}
	if err := session.QueryRowContext(ctx, `SELECT @@character_set_client, @@character_set_connection, COALESCE(@@character_set_results, ''), @@transaction_isolation`).Scan(&facts.ClientCharset, &facts.ConnectionCharset, &facts.ResultsCharset, &facts.Isolation); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return plugin.SupportReport{}, fmt.Errorf("tidb analytical instance facts: %w", err)
	}
	if report := checkTiDBAnalyticalInstanceFacts(facts); !report.Supported {
		return report, nil
	}
	return sqlcompile.ProbeInstanceSemantics(ctx, session, analytical.NewMySQLCompatibleExpressionDialect())
}

var _ plugin.InstanceCapabilitiesResolver = (*Plugin)(nil)

func (p *Plugin) ResolveCapabilities(ctx context.Context, info plugin.ConnectionInfo, base plugin.EngineCapabilities) (plugin.EngineCapabilities, error) {
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
	report, err := certifyTiDBAnalyticalInstance(ctx, conn)
	if err != nil || !report.Supported {
		return base, err
	}
	return plugin.WithAnalyticalCapability(base, plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}})
}
