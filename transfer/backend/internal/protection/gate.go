package protection

import (
	"context"
	"fmt"
	"github.com/addp/transfer/internal/models"
	"slices"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
)

var ErrSourceRequired = fmt.Errorf("%w: source protection gate is required", executor.ErrProtectionPreparation)

const exportAction = "export"

type EngineDescriptorGetter func(context.Context, uint, uint) (*commonmodels.EngineRuntimeDescriptor, error)

func EngineDescriptorLookup(client *commonClient.SystemServiceClient) EngineDescriptorGetter {
	return func(ctx context.Context, tenant, engine uint) (*commonmodels.EngineRuntimeDescriptor, error) {
		return client.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, engine)
	}
}

type Gate struct {
	store   *projectionstore.Store
	engines EngineDescriptorGetter
}

func NewGate(store *projectionstore.Store, engines EngineDescriptorGetter) *Gate {
	return &Gate{store: store, engines: engines}
}

// RequireSourceConfig applies Transfer's resource-level fail-closed gate to
// the exact source locator shared by bounded, replay, continuous, and CDC task
// configurations. It must run in every data-plane process before source data
// is read.
func (g *Gate) RequireSourceConfig(ctx context.Context, tenantID uint, config map[string]interface{}) error {
	if choices, err := models.SourceProtectionChoices(config); err != nil {
		return err
	} else if len(choices) > 0 {
		return dataprotection.ErrRoleOptionUnavailable
	}
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
func (g *Gate) PrepareBoundedTableProtection(ctx context.Context, tenantID uint, config map[string]interface{}, roles func(context.Context) (*dataprotection.QueryRoleSelection, error)) (executor.TableSourceProtector, error) {
	locator, err := SourceLocator(config)
	if err != nil {
		return nil, err
	}
	model, path, err := g.resolveLocator(ctx, tenantID, locator)
	if err != nil {
		return nil, err
	}
	choices, err := models.SourceProtectionChoices(config)
	if err != nil {
		return nil, err
	}
	if len(choices) > 0 && roles == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return &boundedTableProtection{
		store: g.store, tenantID: int64(tenantID), model: model, sourcePath: path, choices: choices, roles: roles,
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
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return nil, err
	}
	if err := captureExportTargets(ctx, g.store, int64(tenantID), []dataprotection.ResourceReference{target}); err != nil {
		return nil, err
	}
	protect, err := g.store.PrepareTableProtection(ctx, int64(tenantID), model, path, fields, exportAction, nil, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", executor.ErrProtectionPreparation, err)
	}
	return func(document map[string]interface{}) error {
		result := &engineplugin.QueryResult{Rows: []map[string]interface{}{document}}
		if err := protect.Apply(result); err != nil {
			return fmt.Errorf("%w: %w", executor.ErrProtectionResult, err)
		}
		return nil
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
	engine, err := g.engines(ctx, tenantID, locator.EngineID)
	if err != nil || engine == nil || engine.ID != locator.EngineID {
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
	choices    []dataprotection.FieldRoleChoice
	roles      func(context.Context) (*dataprotection.QueryRoleSelection, error)
	store      *projectionstore.Store
	tenantID   int64
	model      engineplugin.EngineCatalogModelSpec
	sourcePath engineplugin.EngineCatalogPath
}

func (p *boundedTableProtection) PrepareCatalogTableProtection(ctx context.Context, path engineplugin.EngineCatalogPath, fields []datatype.FieldInfo) (*dataprotection.PreparedTableProtection, error) {
	if p == nil || !sameCatalogPath(p.sourcePath, path) {
		return nil, fmt.Errorf("%w: native source path does not match task source", ErrSourceRequired)
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(p.model, path)
	if err != nil {
		return nil, err
	}
	if err := captureExportTargets(ctx, p.store, p.tenantID, []dataprotection.ResourceReference{target}); err != nil {
		return nil, err
	}
	selection, err := p.selection(ctx)
	if err != nil {
		return nil, err
	}
	protect, err := p.store.PrepareTableProtection(ctx, p.tenantID, p.model, path, fields, exportAction, selection, time.Now().UTC())
	return p.wrap(ctx, protect, selection, err)
}

func (p *boundedTableProtection) PrepareQueryProtection(ctx context.Context, prepared engineplugin.PreparedQuery) (*dataprotection.PreparedTableProtection, error) {
	if p == nil {
		return nil, ErrSourceRequired
	}
	if _, export := ctx.Value(exportExecutionKey{}).(*commonExecution.TaskExecution); export {
		readSet, err := prepared.ReadSet(ctx)
		if err != nil {
			return nil, err
		}
		targets, err := dataprotection.DataItemTargetsFromQueryReadSet(p.model, readSet)
		if err != nil {
			return nil, err
		}
		if err := captureExportTargets(ctx, p.store, p.tenantID, targets); err != nil {
			return nil, err
		}
	}
	selection, err := p.selection(ctx)
	if err != nil {
		return nil, err
	}
	protect, err := p.store.PrepareQueryProtection(ctx, p.tenantID, p.model, prepared, exportAction, dataprotection.SubjectReference{}, selection, time.Now().UTC())
	return p.wrap(ctx, protect, selection, err)
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

func (g *Gate) RoleOptions(ctx context.Context, tenant int64, target dataprotection.ResourceReference, action string, roles []string, now time.Time) ([]dataprotection.FieldRoleOptions, error) {
	return g.store.RoleOptions(ctx, tenant, target, action, roles, now)
}
func (g *Gate) ValidateRoleChoices(ctx context.Context, tenant int64, selection *dataprotection.QueryRoleSelection, action string, now time.Time) error {
	return g.store.ValidateRoleChoices(ctx, tenant, selection, action, now)
}
func (p *boundedTableProtection) selection(ctx context.Context) (*dataprotection.QueryRoleSelection, error) {
	if len(p.choices) == 0 {
		return nil, nil
	}
	if p.roles == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	selection, err := p.roles(ctx)
	if err != nil {
		return nil, err
	}
	if selection == nil || !slices.Equal(selection.Choices, p.choices) {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if err := p.store.ValidateRoleChoices(ctx, p.tenantID, selection, exportAction, time.Now().UTC()); err != nil {
		return nil, err
	}
	return selection, nil
}
func (p *boundedTableProtection) wrap(ctx context.Context, protect *dataprotection.PreparedTableProtection, selection *dataprotection.QueryRoleSelection, err error) (*dataprotection.PreparedTableProtection, error) {
	if err != nil || selection == nil {
		return protect, err
	}
	apply := protect.Apply
	protect.Apply = func(result *engineplugin.QueryResult) error {
		current, err := p.selection(ctx)
		if err != nil {
			return err
		}
		selection.RoleKeys = append([]string(nil), current.RoleKeys...)
		selection.ExpiresAt = current.ExpiresAt
		return apply(result)
	}
	return protect, nil
}
