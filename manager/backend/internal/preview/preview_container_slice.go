package preview

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func (r *PreviewResolver) previewContainerSlice(ctx context.Context, req *PreviewResolverRequest, source *PreviewRequest) (*PreviewResult, error) {
	selection := req.GridSlice
	if req.Locator.EngineID != req.Engine.ID || source.ProviderPath.EngineID != req.Engine.ID || source.ProviderPath.StringPath() != req.ItemFullName || req.ItemFingerprint != commonModels.GenerateItemFingerprint(req.Engine.ID, req.ItemFullName) || selection == nil || selection.Validate() != nil || req.TenantID == nil || *req.TenantID == 0 || req.MetaItemID == nil || req.ItemFingerprint == "" || req.CheckContainerSliceRead == nil || source.ChildName != "" || source.RefPath != "" || source.NestedChildPath != "" || itemLayoutFromMetaAttributes(source.Attributes) != format.LayoutSingle || itemDataTypeFromMetaAttributes(source.Attributes) != "container" || !isContentFileItemType(source.ItemType) {
		return nil, format.ErrGridSliceSelection
	}
	factory, err := format.GetRuntimeContainerInfoProviderFactory(formatTypeFromMetaAttributes(source.Attributes))
	if err != nil {
		return nil, format.ErrGridSliceSelection
	}
	capability, ok := factory.(format.RuntimeContainerSliceProviderFactory)
	if !ok {
		return nil, format.ErrGridSliceSelection
	}
	required := capability.RequiredContainerSliceOperators()
	if len(required) != 1 {
		return nil, fmt.Errorf("grid slice requires one declared direct reader")
	}
	version := commonJSON.String(source.Attributes, "format_info."+string(factory.Format()), "source_version")
	if version == "" {
		return nil, ErrPreviewSourceRefreshRequired
	}
	if err = req.CheckContainerSliceRead(ctx, source.ProviderPath); err != nil {
		return nil, err
	}
	client := r.runtimeDescriptorListClientForTenant(req.TenantID)
	if client == nil {
		return nil, fmt.Errorf("grid slice runtime discovery unavailable")
	}
	descriptors, err := client.ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, err
	}
	engines := make([]commonModels.Engine, 0, len(descriptors))
	for _, d := range descriptors {
		if e := d.AsEngine(); e != nil && e.LifecycleState == commonModels.EngineLifecycleActive {
			engines = append(engines, *e)
		}
	}
	runtime, _, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{OperatorName: required[0]})
	if err != nil {
		return nil, err
	}
	native, err := dbbridge.WorkflowRuntimeProviderForEngine(&runtime)
	if err != nil {
		return nil, err
	}
	input, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{Engine: req.Engine, Locator: req.Locator, Kind: workflowaccess.KindFile, Format: string(factory.Format())})
	if err != nil {
		return nil, err
	}
	plan, err := workflowaccess.NewSourcePlan(input)
	if err != nil {
		return nil, err
	}
	bound, err := factory.BindContainerInfoProvider(native, plugin.ConnectionInfo(runtime.ConnectionInfo), plan)
	if err != nil {
		return nil, err
	}
	reader, ok := bound.(format.ContainerSliceReader)
	if !ok {
		return nil, fmt.Errorf("declared grid reader not bound")
	}
	result, err := reader.ReadGridSlice(ctx, version, *selection)
	if errors.Is(err, format.ErrContainerSourceChanged) {
		return nil, ErrPreviewSourceRefreshRequired
	}
	if err != nil {
		return nil, err
	}
	if err = req.CheckContainerSliceRead(ctx, source.ProviderPath); err != nil {
		return nil, err
	}
	table := &models.TablePreview{Mode: "object", Object: &models.ObjectPreview{Path: req.ItemFullName, EngineID: req.Engine.ID, Attributes: source.Attributes, Content: &models.ObjectPreviewContent{Kind: "grid_slice", JSON: result}}}
	r.attachItemMeta(table, req)
	return r.buildPreviewResult(table, req), nil
}
