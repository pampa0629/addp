package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type ManagerModel3DTilesExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	invokeTimeout   time.Duration
}

func NewManagerModel3DTilesExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	invokeTimeout time.Duration,
) *ManagerModel3DTilesExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 6 * time.Hour
	}
	return &ManagerModel3DTilesExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerModel3DTilesExecutor) BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil {
		return nil, errors.New("model 3d tiles generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("model 3d tiles generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("model 3d tiles execution_id is required")
	}
	accessPlan, err := e.buildAccessPlan(ctx, req.Task.TenantID, req.Config)
	if err != nil {
		return nil, err
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "osgb_scene_to_3dtiles")
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan,
			"tiles": commonModels.JSONMap{
				"format": req.Config.Tiles.Format,
			},
			"options": req.Config.Options.Clone(),
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke OSGB scene to 3D Tiles operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("OSGB scene to 3D Tiles direct operator invocation failed", invokeResult, nil)
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	result := &Model3DTilesExecutionResult{
		TilesetLocator: jsonString(facts["tileset_locator"]),
		TilesetRef:     jsonString(facts["tileset_ref"]),
		TileCount:      jsonInt64(facts["tile_count"]),
		Metadata: commonModels.JSONMap{
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"access_plan": accessPlanAudit(accessPlan),
			"tiles_facts": facts,
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if result.TilesetRef == "" && result.TilesetLocator == "" {
		return nil, errors.New("model 3d tiles generation returned no tileset reference")
	}
	return result, nil
}

func (e *ManagerModel3DTilesExecutor) buildAccessPlan(ctx context.Context, tenantID uint, cfg Model3DTilesExecutionConfig) (commonModels.JSONMap, error) {
	sourceLoc, err := resourcetree.ParseURI(cfg.Source.ItemLocator)
	if err != nil {
		return nil, fmt.Errorf("parse source locator: %w", err)
	}
	targetLoc, err := resourcetree.ParseURI(cfg.Target.StorageLocator)
	if err != nil {
		return nil, fmt.Errorf("parse target locator: %w", err)
	}
	sourceEngine, err := e.getModel3DTilesEngine(ctx, tenantID, cfg.Source.SourceEngineID)
	if err != nil {
		return nil, fmt.Errorf("get source engine: %w", err)
	}
	targetEngine, err := e.getModel3DTilesEngine(ctx, tenantID, cfg.Target.TargetEngineID)
	if err != nil {
		return nil, fmt.Errorf("get target engine: %w", err)
	}
	source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: sourceEngine, Locator: sourceLoc, Kind: workflowaccess.KindDirectory, Format: string(format.FormatOSGBScene),
		Metadata: commonModels.JSONMap{"locator": cfg.Source.ItemLocator, "engine_id": cfg.Source.SourceEngineID, "engine_type": sourceEngine.EngineType},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare source: %w", err)
	}
	target, _, err := workflowaccess.ResolveTarget(workflowaccess.ResourceSpec{
		Engine: targetEngine, Locator: targetLoc, Kind: workflowaccess.KindDirectory, Format: "3dtiles",
		Name: cfg.Target.DatasetName, WriteMode: workflowaccess.WriteModeReplace,
		Metadata: commonModels.JSONMap{"locator": cfg.Target.StorageLocator, "engine_id": cfg.Target.TargetEngineID, "engine_type": targetEngine.EngineType},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare target: %w", err)
	}
	plan, err := workflowaccess.New(source, target)
	if err != nil {
		return nil, err
	}
	return plan.JSONMap(), nil
}

func (e *ManagerModel3DTilesExecutor) getModel3DTilesEngine(ctx context.Context, tenantID uint, engineID uint) (*commonModels.Engine, error) {
	if engineID == 0 {
		return nil, errors.New("engine_id is required")
	}
	engine, err := e.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, err
	}
	if engine == nil || !engine.IsActive {
		return nil, errors.New("engine is not active")
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return nil, ErrEngineAccessDenied
	}
	return engine, nil
}

func (e *ManagerModel3DTilesExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for model 3d tiles generation: %w", operatorName, err)
	}
	return engine, operator, nil
}

func accessPlanAudit(plan commonModels.JSONMap) commonModels.JSONMap {
	audit := commonModels.JSONMap{}
	for _, section := range []string{"source", "target"} {
		if part, ok := asJSONMap(plan[section]); ok {
			metadata, _ := asJSONMap(part["metadata"])
			audit[section] = metadata.Clone()
		}
	}
	return audit
}
