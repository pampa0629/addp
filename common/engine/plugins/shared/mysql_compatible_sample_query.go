package shared

import (
	"context"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

// GenerateSampleQuery builds a bounded direct-column query from current
// catalog fields. MySQL-compatible protection intentionally rejects wildcard
// lineage, so generated templates must not use SELECT *.
func (p MySQLCompatibleQueryProvenance) GenerateSampleQuery(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	dialectName string,
	limit int,
) string {
	if p.DescribeFacts == nil {
		return ""
	}
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) != 2 || strings.TrimSpace(segments[0].Name) == "" || strings.TrimSpace(segments[1].Name) == "" {
		return ""
	}
	facts, err := p.DescribeFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) == 0 {
		return ""
	}
	dialect := commonquery.ForDialect(dialectName)
	columns := make([]string, 0, len(facts.Table.Fields))
	for _, field := range facts.Table.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			return ""
		}
		columns = append(columns, dialect.QuoteIdentifier(name))
	}
	if limit <= 0 {
		limit = 10
	}
	return dialect.SelectTableSQL(
		strings.Join(columns, ", "),
		segments[0].Name,
		segments[1].Name,
		"",
		"",
		limit,
		0,
	)
}
