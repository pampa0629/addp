package service

import (
	"context"
	"fmt"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"strings"
)

func (i *WorkflowContainerInspector) InspectTable(ctx context.Context, source *commonModels.Engine, tenant uint, path, sourceFormat string) (*format.TableDescribeResult, error) {
	if i == nil || i.engineService == nil || source == nil {
		return nil, fmt.Errorf("runtime table inspector is not configured")
	}
	f := format.NormalizeFormat(sourceFormat)
	factory, err := format.GetRuntimeTableProviderFactory(f)
	if err != nil {
		return nil, err
	}
	_, runtime, conn, err := i.resolveRuntime(ctx, tenant, factory.RequiredTableReadOperators())
	if err != nil {
		return nil, err
	}
	kind := resourcetree.TypeFile
	if strings.EqualFold(source.EngineType, "minio") || strings.EqualFold(source.EngineType, "s3") {
		kind = resourcetree.TypeObject
	}
	locator := resourcetree.LocatorFromFullName(source.ID, source.EngineType, string(kind), path, nil)
	if locator == nil {
		return nil, fmt.Errorf("invalid runtime table source locator")
	}
	resource, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{Engine: source, Locator: locator, Kind: workflowaccess.KindFile, Format: string(f)})
	if err != nil {
		return nil, err
	}
	plan, err := workflowaccess.NewSourcePlan(resource)
	if err != nil {
		return nil, err
	}
	bound, err := factory.BindTableProvider(runtime, conn, plan)
	if err != nil {
		return nil, err
	}
	return bound.DescribeTable(ctx, nil)
}
