package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

// DataProfileSamplePlan is a preparation result, never an authorization. Only
// the same plan may execute after per-read gates; no credentials are stored.
type DataProfileSamplePlan struct {
	pages profilePreparedPages
	model plugin.EngineCatalogModelSpec
}

type profilePreparedPages interface {
	ReadSet() *plugin.QueryReadSet
	Positions() []preview.TablePage
	Query(int) (plugin.PreparedQuery, error)
}

func (p *DataProfileSamplePlan) Positions() []preview.TablePage {
	if p == nil || p.pages == nil {
		return nil
	}
	return p.pages.Positions()
}

func (p *DataProfileSamplePlan) ReadSet() *plugin.QueryReadSet {
	if p == nil || p.pages == nil {
		return nil
	}
	return p.pages.ReadSet()
}

func (p *DataProfileSamplePlan) SourceReadScope(rawConfig json.RawMessage) (*commonExecution.ManagerProfileReadScope, error) {
	return commonExecution.NewManagerProfileReadScope(rawConfig, p.ReadSet())
}

// Prepare freezes all bounded pages and proves their complete dependencies
// without reading any business records. It never calls Preview or ReadBatch.
func (p *PreviewDataProfileSampleProvider) Prepare(ctx context.Context, target *DataProfileTarget, scope dataprofile.DataScope, budget DataProfileBudget) (*DataProfileSamplePlan, error) {
	if p == nil || p.resolver == nil || target == nil || target.resolved == nil || target.EngineID == 0 ||
		target.resolved.Engine == nil || target.resolved.Engine.ID != target.EngineID ||
		target.resolved.Locator == nil || target.resolved.Locator.ToURI() != target.Locator ||
		target.Selection != normalizeDataProfileSelection(target.Selection) ||
		target.Selection != (DataProfileSelection{}) {
		return nil, ErrDataProfileInvalidRequest
	}
	positions, err := dataProfilePagePositions(target.RowCount, scope, budget)
	if err != nil {
		return nil, err
	}
	pages, err := p.resolver.PrepareProfilePages(ctx, target.resolved, target.Fields, scope, positions)
	if err != nil {
		return nil, err
	}
	plan := &DataProfileSamplePlan{pages: pages, model: pages.CatalogModel()}
	if err := validateSingleTableProfilePlan(plan, target.EngineID); err != nil {
		return nil, err
	}
	return plan, nil
}

// The first release profiles a single ordinary table. Do not trim a view's
// complete dependency set to fit this boundary. Concurrent source DDL binding
// is a deferred Provider concern, not a guarantee of this check.
func validateSingleTableProfilePlan(plan *DataProfileSamplePlan, engineID uint) error {
	if plan == nil || plan.pages == nil {
		return ErrDataProfileSourceAuthorizationRequired
	}
	set, err := canonicalProfileReadSet(plan.ReadSet(), engineID)
	if err != nil {
		return ErrDataProfileSourceAuthorizationRequired
	}
	if len(set.Paths) != 1 {
		return ErrDataProfileUnsupported
	}
	segments := set.Paths[0].Segments
	if len(segments) == 0 || segments[len(segments)-1].Term != plugin.EngineCatalogTermTable ||
		segments[len(segments)-1].Kind != plugin.EngineCatalogKindTable {
		return ErrDataProfileUnsupported
	}
	return nil
}

func dataProfilePagePositions(rowCount *int64, scope dataprofile.DataScope, budget DataProfileBudget) ([]preview.TablePage, error) {
	frozen := frozenDataProfileBudget{budget.SampleSize, budget.MaxRowsScanned, budget.PageSize, budget.Timeout.Milliseconds()}
	if _, err := frozen.executionBudget(); err != nil || budget.Timeout%time.Millisecond != 0 ||
		budget.MaxRowsScanned > preview.MaxProfilePreparedRows || budget.PageSize > preview.MaxPreparedTablePageRows ||
		(rowCount != nil && *rowCount < 0) ||
		(scope.Kind != dataprofile.DataScopeKindAll && scope.Kind != dataprofile.DataScopeKindCondition) {
		return nil, ErrDataProfileInvalidRequest
	}
	pageCount := (budget.MaxRowsScanned-1)/budget.PageSize + 1
	// Unknown or filtered cardinality deliberately uses a bounded sequence,
	// not a fake full-range estimate or an extra COUNT query.
	totalPages := int64(pageCount)
	if scope.Kind == dataprofile.DataScopeKindAll && rowCount != nil && *rowCount > 0 {
		totalPages = (*rowCount-1)/int64(budget.PageSize) + 1
		if totalPages < int64(pageCount) {
			pageCount = int(totalPages)
		}
	}
	result := make([]preview.TablePage, 0, pageCount)
	remaining := budget.MaxRowsScanned
	for index := 0; index < pageCount; index++ {
		page := int64(index)
		if pageCount > 1 {
			// Divide before multiplying: cardinality can approach MaxInt64.
			span, divisor := totalPages-1, int64(pageCount-1)
			page = span/divisor*int64(index) + span%divisor*int64(index)/divisor
		}
		if page > int64(int(^uint(0)>>1))/int64(budget.PageSize) {
			return nil, ErrDataProfileInvalidRequest
		}
		limit := min(budget.PageSize, remaining)
		if page*int64(budget.PageSize) > int64(int(^uint(0)>>1)-limit) {
			return nil, ErrDataProfileInvalidRequest
		}
		result = append(result, preview.TablePage{Offset: int(page) * budget.PageSize, Limit: limit})
		remaining -= limit
	}
	return result, nil
}
