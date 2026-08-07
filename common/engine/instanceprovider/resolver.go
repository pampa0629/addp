package instanceprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	"github.com/addp/common/models"
)

// RuntimeDescriptorClient is the tenant-scoped System runtime descriptor API
// needed to resolve an extension provider bound to an engine instance.
type RuntimeDescriptorClient interface {
	GetEngineRuntimeDescriptor(context.Context, uint) (*models.EngineRuntimeDescriptor, error)
}

// Resolve returns the composite provider for one concrete Engine Instance. A
// PostgreSQL instance with SuperMap SDX+ for PostgreSQL keeps PostgreSQL as its
// catalog owner and routes only sdx business tables through the bound runtime.
// All other table paths continue to use the registered PostgreSQL provider.
func Resolve(ctx context.Context, runtimeClient RuntimeDescriptorClient, engine *models.Engine, requiredOperators ...string) (plugin.EnginePlugin, error) {
	if engine == nil {
		return nil, errors.New("engine resource is required")
	}
	registered, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", engine.EngineType)
	}
	workspace, ok, err := superMapSDXPostgreSQLWorkspace(engine)
	if err != nil {
		return nil, fmt.Errorf("parse engine %d SuperMap workspace: %w", engine.ID, err)
	}
	if !ok {
		return registered, nil
	}
	if workspace.BoundRuntimeEngineID == nil || *workspace.BoundRuntimeEngineID == 0 {
		return nil, fmt.Errorf("SuperMap SDX+ for PostgreSQL workspace on engine %d has no bound workflow runtime", engine.ID)
	}
	if runtimeClient == nil {
		return nil, errors.New("System service client is required to resolve the bound SuperMap workflow runtime")
	}
	boundID := *workspace.BoundRuntimeEngineID
	descriptor, err := runtimeClient.GetEngineRuntimeDescriptor(ctx, boundID)
	if err != nil {
		return nil, fmt.Errorf("get bound SuperMap workflow runtime %d: %w", boundID, err)
	}
	if descriptor == nil || descriptor.ID != boundID || descriptor.LifecycleState != models.EngineLifecycleActive {
		return nil, fmt.Errorf("bound SuperMap workflow runtime %d is not active or visible", boundID)
	}
	runtimeEngine := descriptor.AsEngine()
	workflowRuntime, err := dbbridge.WorkflowRuntimeProviderForEngine(runtimeEngine)
	if err != nil {
		return nil, err
	}
	if len(requiredOperators) > 0 {
		if err := dbbridge.RequireDirectWorkflowOperators(ctx, runtimeEngine, requiredOperators...); err != nil {
			return nil, fmt.Errorf("bound runtime %d does not provide SuperMap table operators: %w", boundID, err)
		}
	}
	return supermapworkflow.NewSDXPostgreSQLTableProvider(workflowRuntime, plugin.ConnectionInfo(runtimeEngine.ConnectionInfo))
}

// IsSuperMapSDXPostgreSQL reports whether an Engine Instance declares the
// SuperMap SDX+ for PostgreSQL workspace. It is used to keep PostgreSQL/PostGIS
// SQL paths from accidentally opening SuperMap private geometry blobs.
func IsSuperMapSDXPostgreSQL(engine *models.Engine) bool {
	_, ok, err := superMapSDXPostgreSQLWorkspace(engine)
	return err == nil && ok
}

// IsSuperMapSDXPostgreSQLTable reports whether one concrete table belongs to
// the SDX+ for PostgreSQL workspace. Engine capability alone is insufficient
// because the same PostgreSQL instance may also contain ordinary and PostGIS
// tables.
func IsSuperMapSDXPostgreSQLTable(engine *models.Engine, schema, table string) bool {
	if strings.TrimSpace(table) == "" {
		return false
	}
	if !IsSuperMapSDXPostgreSQL(engine) {
		return false
	}
	path := plugin.TabularItemPath(engine.ID, plugin.CatalogTermSchema, schema, table)
	return supermapworkflow.IsSDXPostgreSQLTablePath(path)
}

func superMapSDXPostgreSQLWorkspace(engine *models.Engine) (plugin.SpatialWorkspaceFact, bool, error) {
	if engine == nil || !strings.EqualFold(strings.TrimSpace(engine.EngineType), "postgresql") || engine.Capabilities == nil || *engine.Capabilities == "" {
		return plugin.SpatialWorkspaceFact{}, false, nil
	}
	capabilities, err := plugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil {
		return plugin.SpatialWorkspaceFact{}, false, err
	}
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(capabilities.Extensions)
	if err != nil {
		return plugin.SpatialWorkspaceFact{}, false, err
	}
	for _, workspace := range workspaces {
		if !strings.EqualFold(strings.TrimSpace(workspace.Ecosystem), "supermap") ||
			!strings.EqualFold(strings.TrimSpace(workspace.Kind), plugin.SpatialWorkspaceSuperMapSDXPostgreSQL) {
			continue
		}
		state := strings.ToLower(strings.TrimSpace(workspace.State))
		if state == plugin.SpatialWorkspaceStateDetected || state == plugin.SpatialWorkspaceStateEnabled {
			return workspace, true, nil
		}
	}
	return plugin.SpatialWorkspaceFact{}, false, nil
}
