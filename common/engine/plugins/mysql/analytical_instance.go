package mysql

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

type analyticalInstanceFacts struct {
	Version, Comment                                 string
	ClientCharset, ConnectionCharset, ResultsCharset string
	Isolation                                        string
}

var analyticalServerVersion = regexp.MustCompile(`^8\.0\.[0-9]+(-commercial)?$`)

func analyticalVersionSupported(version, comment string) bool {
	return analyticalServerVersion.MatchString(version) && (comment == "MySQL Community Server - GPL" || comment == "MySQL Enterprise Server - Commercial")
}

func checkAnalyticalInstanceFacts(f analyticalInstanceFacts) plugin.SupportReport {
	code := ""
	switch {
	case !analyticalVersionSupported(f.Version, f.Comment):
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

// This native prerequisite check does not infer transaction semantics for
// individual source tables. That belongs to transactional source preflight.
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
	// Inspect product/version first: older or compatible servers may not have
	// the 8.0 transaction_isolation variable. They must be rejected explicitly
	// instead of failing while querying a condition they cannot support.
	if err := session.QueryRowContext(ctx, `SELECT @@version, @@version_comment`).Scan(&facts.Version, &facts.Comment); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return plugin.SupportReport{}, fmt.Errorf("mysql analytical instance version: %w", err)
	}
	if !analyticalVersionSupported(facts.Version, facts.Comment) {
		return checkAnalyticalInstanceFacts(facts), nil
	}
	if err := session.QueryRowContext(ctx, `SELECT @@character_set_client, @@character_set_connection, COALESCE(@@character_set_results, ''), @@transaction_isolation`).Scan(&facts.ClientCharset, &facts.ConnectionCharset, &facts.ResultsCharset, &facts.Isolation); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return plugin.SupportReport{}, fmt.Errorf("mysql analytical instance facts: %w", err)
	}
	if report := checkAnalyticalInstanceFacts(facts); !report.Supported {
		return report, nil
	}
	return sqlcompile.ProbeInstanceSemantics(ctx, session, analyticalExpressionDialect{})
}
