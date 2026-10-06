package preview

import (
	"context"
	"errors"
	"reflect"

	"github.com/addp/common/authorization"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/profilefilter"
)

// TablePage is a server-owned bounded position, not a public query DTO.
type TablePage struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

const (
	MaxProfilePreparedRows   = 10000
	MaxPreparedTablePageRows = 2000
)

// PreparedTablePages contains only Provider-owned one-shot queries. Preparing
// it never reads business rows and does not confer permission to execute them.
type PreparedTablePages struct {
	positions []TablePage
	queries   []plugin.PreparedQuery
	readSet   *plugin.QueryReadSet
	model     plugin.EngineCatalogModelSpec
}

func (p *PreparedTablePages) CatalogModel() plugin.EngineCatalogModelSpec { return p.model }

func (p *PreparedTablePages) ReadSet() *plugin.QueryReadSet {
	if p == nil {
		return nil
	}
	return p.readSet.Clone()
}

func (p *PreparedTablePages) Positions() []TablePage {
	if p == nil {
		return nil
	}
	return append([]TablePage(nil), p.positions...)
}

// Query returns the already-bound plan. Owner gates must use this very same
// object; no new query text, parameters or page position can be submitted.
func (p *PreparedTablePages) Query(index int) (plugin.PreparedQuery, error) {
	if p == nil || index < 0 || index >= len(p.queries) {
		return nil, ErrSourceAuthorizationRequired
	}
	return p.queries[index], nil
}

func (r *PreviewResolver) PrepareProfilePages(ctx context.Context, req *PreviewResolverRequest, fields []datatype.FieldInfo, scope dataprofile.DataScope, pages []TablePage) (*PreparedTablePages, error) {
	if r == nil || req == nil || req.Engine == nil || req.Locator == nil || req.Metadata == nil ||
		req.TenantID == nil || *req.TenantID == 0 || req.MetaItemID == nil || *req.MetaItemID == 0 ||
		req.ChildName != "" || req.RefPath != "" || req.NestedChildPath != "" {
		return nil, ErrSourceAuthorizationRequired
	}
	providerReq, err := r.buildProviderRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	providerReq.DataScope = scope
	return prepareProfilePages(ctx, providerReq, fields, pages)
}

func prepareProfilePages(ctx context.Context, req *PreviewRequest, fields []datatype.FieldInfo, pages []TablePage) (*PreparedTablePages, error) {
	if req == nil || req.Engine == nil || req.EnginePlugin == nil || req.EnginePlugin.Type() != "postgresql" ||
		req.ProviderPath.EngineID != req.Engine.ID || len(fields) == 0 || len(pages) == 0 || len(pages) > MaxProfilePreparedRows {
		return nil, ErrSourceAuthorizationRequired
	}
	// Validate the whole budget before any Provider preparation. The sample
	// budget is 10000 rows; a page never exceeds the existing preview bound.
	remaining := MaxProfilePreparedRows
	for i, page := range pages {
		if page.Offset < 0 || page.Limit <= 0 || page.Limit > MaxPreparedTablePageRows || page.Limit > remaining ||
			page.Offset > int(^uint(0)>>1)-page.Limit ||
			(i > 0 && page.Offset < pages[i-1].Offset+pages[i-1].Limit) {
			return nil, ErrSourceAuthorizationRequired
		}
		remaining -= page.Limit
	}
	normalized, err := profilefilter.Normalize(req.DataScope, fields)
	if err != nil {
		return nil, err
	}
	bound := *req
	bound.DataScope = normalized
	model, ok := req.EnginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, ErrSourceAuthorizationRequired
	}
	result := &PreparedTablePages{positions: append([]TablePage(nil), pages...), model: model.EngineCatalogModel()}
	for _, page := range result.positions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query, err := (&DatabaseTablePreviewProvider{}).preparePostgreSQLPreview(ctx, &bound, fields, page.Offset, page.Limit)
		if err != nil || query == nil {
			if err != nil {
				return nil, err
			}
			return nil, ErrSourceAuthorizationRequired
		}
		set, err := query.ReadSet(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateProfilePageReadSet(set, req.Engine.ID); err != nil {
			return nil, err
		}
		var sources []plugin.EngineCatalogPath
		if result.readSet != nil {
			sources = append(sources, result.readSet.Paths...)
		}
		result.readSet, err = plugin.NewQueryReadSet(append(sources, set.Paths...)...)
		if err != nil || len(result.readSet.Paths) > 200 {
			return nil, ErrSourceAuthorizationRequired
		}
		result.queries = append(result.queries, query)
	}
	return result, nil
}

func validateProfilePageReadSet(set *plugin.QueryReadSet, engineID uint) error {
	if set == nil || len(set.Paths) == 0 || len(set.Paths) > 200 {
		return plugin.ErrQueryReadSetUnresolved
	}
	canonical, err := plugin.NewQueryReadSet(set.Paths...)
	if err != nil || !reflect.DeepEqual(set, canonical) {
		return plugin.ErrQueryReadSetUnresolved
	}
	for _, path := range set.Paths {
		if path.EngineID != engineID {
			return plugin.ErrQueryReadSetUnresolved
		}
		if _, err := authorization.EncodeSharingTarget(path); err != nil {
			return errors.Join(plugin.ErrQueryReadSetUnresolved, err)
		}
	}
	return nil
}
