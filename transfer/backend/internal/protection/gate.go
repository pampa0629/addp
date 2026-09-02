package protection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
)

var ErrSourceRequired = errors.New("transfer source protection gate is required")

const exportAction = "export"

type EngineGetter interface {
	GetEngineForTenant(context.Context, uint, uint) (*commonmodels.Engine, error)
}

type Gate struct {
	store   *projectionstore.Store
	engines EngineGetter
}

func NewGate(store *projectionstore.Store, engines EngineGetter) *Gate {
	return &Gate{store: store, engines: engines}
}

// RequireSourceConfig applies Transfer's resource-level fail-closed gate to
// the exact source locator shared by bounded, replay, continuous, and CDC task
// configurations. It must run in every data-plane process before source data
// is read.
func (g *Gate) RequireSourceConfig(ctx context.Context, tenantID uint, config map[string]interface{}) error {
	locator, err := SourceLocator(config)
	if err != nil {
		return err
	}
	return g.RequireLocator(ctx, tenantID, locator)
}

func (g *Gate) RequireLocator(ctx context.Context, tenantID uint, locatorURI string) error {
	model, path, err := g.resolveLocator(ctx, tenantID, locatorURI)
	if err != nil {
		return err
	}
	if err := g.store.RequireCatalogPathUnmanaged(ctx, int64(tenantID), model, path, time.Now().UTC()); err != nil {
		return fmt.Errorf("%w: %w", ErrSourceRequired, err)
	}
	return nil
}

// PrepareBoundedTableProtection binds one bounded snapshot execution to the
// exact source engine model and locator. Native and query sources then prepare
// their local result protector at the last point before opening the reader.
func (g *Gate) PrepareBoundedTableProtection(ctx context.Context, tenantID uint, config map[string]interface{}) (executor.TableSourceProtector, error) {
	locator, err := SourceLocator(config)
	if err != nil {
		return nil, err
	}
	model, path, err := g.resolveLocator(ctx, tenantID, locator)
	if err != nil {
		return nil, err
	}
	return &boundedTableProtection{
		store: g.store, tenantID: int64(tenantID), model: model, sourcePath: path,
	}, nil
}

// PrepareBoundedEncodedRecordProtection compiles the same export projection
// for one exact schema-flexible collection. The engine provider invokes the
// returned transform while BSON scalar values are still native and before it
// serializes the document to the requested exchange format.
func (g *Gate) PrepareBoundedEncodedRecordProtection(
	ctx context.Context,
	tenantID uint,
	config map[string]interface{},
	fields []datatype.FieldInfo,
) (engineplugin.EncodedRecordTransform, error) {
	locator, err := SourceLocator(config)
	if err != nil {
		return nil, err
	}
	model, path, err := g.resolveLocator(ctx, tenantID, locator)
	if err != nil {
		return nil, err
	}
	protect, err := g.store.PrepareTableProtection(ctx, int64(tenantID), model, path, fields, exportAction, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return func(document map[string]interface{}) error {
		result := &engineplugin.QueryResult{Rows: []map[string]interface{}{document}}
		return protect(result)
	}, nil
}

func (g *Gate) resolveLocator(ctx context.Context, tenantID uint, locatorURI string) (engineplugin.EngineCatalogModelSpec, engineplugin.EngineCatalogPath, error) {
	if g == nil || g.store == nil || g.engines == nil || tenantID == 0 {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, ErrSourceRequired
	}
	locator, err := resourcetree.ParseURI(strings.TrimSpace(locatorURI))
	if err != nil || locator.EngineID == 0 {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, fmt.Errorf("%w: invalid source locator", ErrSourceRequired)
	}
	engine, err := g.engines.GetEngineForTenant(ctx, tenantID, locator.EngineID)
	if err != nil || engine == nil {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, fmt.Errorf("%w: resolve source engine: %v", ErrSourceRequired, err)
	}
	enginePlugin, err := engineplugin.Get(engine.EngineType)
	if err != nil {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, fmt.Errorf("%w: resolve source provider: %v", ErrSourceRequired, err)
	}
	modelProvider, ok := enginePlugin.(engineplugin.EngineCatalogModelProvider)
	if !ok {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, fmt.Errorf("%w: source provider has no catalog model", ErrSourceRequired)
	}
	model := modelProvider.EngineCatalogModel()
	path, err := resourcetree.EngineCatalogPathFromLocator(model, locator)
	if err != nil {
		return engineplugin.EngineCatalogModelSpec{}, engineplugin.EngineCatalogPath{}, fmt.Errorf("%w: resolve source DataItem: %v", ErrSourceRequired, err)
	}
	return model, path, nil
}

type boundedTableProtection struct {
	store      *projectionstore.Store
	tenantID   int64
	model      engineplugin.EngineCatalogModelSpec
	sourcePath engineplugin.EngineCatalogPath
}

func (p *boundedTableProtection) PrepareCatalogTableProtection(ctx context.Context, path engineplugin.EngineCatalogPath, fields []datatype.FieldInfo) (func(*engineplugin.QueryResult) error, error) {
	if p == nil || !sameCatalogPath(p.sourcePath, path) {
		return nil, fmt.Errorf("%w: native source path does not match task source", ErrSourceRequired)
	}
	return p.store.PrepareTableProtection(ctx, p.tenantID, p.model, path, fields, exportAction, time.Now().UTC())
}

func (p *boundedTableProtection) PrepareQueryProtection(ctx context.Context, prepared engineplugin.PreparedQuery) (func(*engineplugin.QueryResult) error, error) {
	if p == nil {
		return nil, ErrSourceRequired
	}
	return p.store.PrepareQueryProtection(ctx, p.tenantID, p.model, prepared, exportAction, time.Now().UTC())
}

func sameCatalogPath(left, right engineplugin.EngineCatalogPath) bool {
	if left.Version != right.Version || left.EngineID != right.EngineID || len(left.Segments) != len(right.Segments) {
		return false
	}
	for index := range left.Segments {
		if left.Segments[index] != right.Segments[index] {
			return false
		}
	}
	return true
}

func SourceLocator(config map[string]interface{}) (string, error) {
	if len(config) == 0 {
		return "", fmt.Errorf("%w: task config is empty", ErrSourceRequired)
	}
	source, ok := config["source"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("%w: source config is missing", ErrSourceRequired)
	}
	locator, ok := source["locator"].(string)
	locator = strings.TrimSpace(locator)
	if !ok || locator == "" {
		return "", fmt.Errorf("%w: source locator is missing", ErrSourceRequired)
	}
	return locator, nil
}
