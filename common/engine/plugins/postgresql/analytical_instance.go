package postgresql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

type analyticalInstanceFacts struct {
	VersionNumber                          int
	Banner, ServerEncoding, ClientEncoding string
}

func checkAnalyticalInstanceFacts(f analyticalInstanceFacts) plugin.SupportReport {
	code := ""
	switch {
	case f.VersionNumber < 150000 || f.VersionNumber >= 160000 || !strings.HasPrefix(f.Banner, "PostgreSQL 15."):
		code = "analytical_server_version"
	case f.ServerEncoding != "UTF8" || f.ClientEncoding != "UTF8":
		code = "analytical_encoding"
	}
	if code != "" {
		return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: code}}}
	}
	return plugin.SupportReport{Supported: true}
}

// certifyAnalyticalInstance is shared by the instance capability resolver and
// transactional preflight. It never publishes capability or caches a result.
func certifyAnalyticalInstance(ctx context.Context, session sqlcompile.InstanceProbeSession) (plugin.SupportReport, error) {
	if session == nil {
		return plugin.SupportReport{}, plugin.ErrAnalyticalInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return plugin.SupportReport{}, err
	}
	var facts analyticalInstanceFacts
	if err := session.QueryRowContext(ctx, `SELECT current_setting('server_version_num')::integer, version(), current_setting('server_encoding'), current_setting('client_encoding')`).Scan(&facts.VersionNumber, &facts.Banner, &facts.ServerEncoding, &facts.ClientEncoding); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return plugin.SupportReport{}, fmt.Errorf("postgresql analytical instance facts: %w", err)
	}
	if report := checkAnalyticalInstanceFacts(facts); !report.Supported {
		return report, nil
	}
	return sqlcompile.ProbeInstanceSemantics(ctx, session, analyticalExpressionDialect{})
}
