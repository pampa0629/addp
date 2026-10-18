package scanprocessor

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

// Snapshot the exact source used by this new index, including known-item
// refreshes whose catalog size/time may have changed since the previous scan.
func readDelimitedIndexSource(ctx context.Context, input *input, reader plugin.ContentReadableProvider) (*plugin.EngineCatalogFacts, error) {
	if !input.IncludeAccessIndex || input.Detected.Layout != format.LayoutSingle ||
		(input.Detected.Format != "csv" && input.Detected.Format != "tsv") {
		return nil, nil
	}
	provider, ok := reader.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return nil, nil
	}
	facts, err := provider.DescribeEngineCatalogFacts(ctx, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath), plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, fmt.Errorf("read delimited index source facts: %w", err)
	}
	if facts == nil || facts.Storage == nil || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes < 0 ||
		(facts.Storage.ETag == "" && (facts.UpdatedAt == nil || facts.UpdatedAt.IsZero())) {
		return nil, fmt.Errorf("delimited index source version is unavailable")
	}
	return facts, nil
}

func delimitedIndexSource(facts *plugin.EngineCatalogFacts) map[string]interface{} {
	source := map[string]interface{}{"size_bytes": *facts.Storage.SizeBytes}
	if facts.Storage.ETag != "" {
		source["etag"] = facts.Storage.ETag
	}
	if facts.UpdatedAt != nil && !facts.UpdatedAt.IsZero() {
		source["last_modified_at"] = facts.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return source
}

func bindDelimitedIndexSource(ctx context.Context, input *input, attrs models.JSONMap, reader plugin.ContentReadableProvider, before *plugin.EngineCatalogFacts) error {
	if before == nil || len(commonJSON.Section(attrs, "access_index.table")) == 0 {
		return nil
	}
	after, err := readDelimitedIndexSource(ctx, input, reader)
	if err != nil {
		return err
	}
	index := &datatype.AccessIndex{Source: delimitedIndexSource(before)}
	if after == nil || !index.MatchesSource(*after.Storage.SizeBytes, after.Storage.ETag, after.UpdatedAt) {
		return fmt.Errorf("delimited source changed while building access index")
	}
	metaattr.UpsertNested(attrs, "access_index", "table", map[string]interface{}{"source": index.Source})
	return nil
}
