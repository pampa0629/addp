package preview

import (
	"context"
	"errors"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
)

var ErrPreviewSourceRefreshRequired = errors.New("preview source version requires metadata refresh")
var ErrPreviewSourceUnavailable = errors.New("preview source facts unavailable")

// validateIndexedDelimitedSource never opens source bytes on a stale binding.
// Rechecking after the read discards a page changed during that observation.
func validateIndexedDelimitedSource(ctx context.Context, req *PreviewRequest, formatType format.FormatType) error {
	if req == nil || req.ChildName != "" || (formatType != format.FormatCSV && formatType != format.FormatTSV) ||
		len(commonJSON.Section(req.Attributes, "access_index.table")) == 0 {
		return nil
	}
	index := tableAccessIndexFromMetaAttributes(req.Attributes)
	if !usableTableAccessIndex(index) || index.Format != string(formatType) {
		return ErrPreviewSourceRefreshRequired
	}
	if req.Engine == nil {
		return ErrPreviewSourceUnavailable
	}
	pl := req.EnginePlugin
	if pl == nil {
		var err error
		pl, err = plugin.Get(req.Engine.EngineType)
		if err != nil {
			return ErrPreviewSourceUnavailable
		}
	}
	provider, ok := pl.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return ErrPreviewSourceUnavailable
	}
	facts, err := provider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrPreviewSourceUnavailable
	}
	if facts == nil || facts.Storage == nil || facts.Storage.SizeBytes == nil {
		return ErrPreviewSourceUnavailable
	}
	if !index.MatchesSource(*facts.Storage.SizeBytes, facts.Storage.ETag, facts.UpdatedAt) {
		return ErrPreviewSourceRefreshRequired
	}
	return nil
}
