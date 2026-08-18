package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type WorkflowContainerInspector struct {
	engineService *EngineService
}

const workflowContainerChildLimit = 100

func NewWorkflowContainerInspector(engineService *EngineService) *WorkflowContainerInspector {
	return &WorkflowContainerInspector{engineService: engineService}
}

func (i *WorkflowContainerInspector) DetectFormat(
	ctx context.Context,
	source *commonModels.Engine,
	tenantID uint,
	physicalPath, candidateFormat, sourceLayout string,
) (format.FormatType, error) {
	if i == nil || i.engineService == nil || source == nil {
		return format.FormatUnknown, fmt.Errorf("workflow format detector is not configured")
	}
	formatType := format.NormalizeFormat(candidateFormat)
	factory, err := format.GetRuntimeFormatDetectorFactory(formatType)
	if err != nil {
		return format.FormatUnknown, err
	}
	runtime, runtimeProvider, runtimeConn, err := i.resolveRuntime(ctx, tenantID, factory.RequiredFormatDetectionOperators())
	if err != nil {
		return format.FormatUnknown, fmt.Errorf("resolve %s format detection runtime: %w", formatType, err)
	}

	kind := workflowaccess.KindFile
	resourceType := resourcetree.TypeFile
	if sourceLayout == format.LayoutWhole {
		kind = workflowaccess.KindDirectory
		resourceType = resourcetree.TypeDirectory
	} else if strings.EqualFold(source.EngineType, "minio") || strings.EqualFold(source.EngineType, "s3") {
		resourceType = resourcetree.TypeObject
	}
	locator := resourcetree.LocatorFromFullName(source.ID, source.EngineType, string(resourceType), physicalPath, nil)
	if locator == nil {
		return format.FormatUnknown, fmt.Errorf("cannot build %s detection source locator for %q", formatType, physicalPath)
	}
	workflowSource, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: source, Locator: locator, Kind: kind, Format: string(formatType),
	})
	if err != nil {
		return format.FormatUnknown, fmt.Errorf("resolve %s detection workflow access: %w", formatType, err)
	}
	plan, err := workflowaccess.NewSourcePlan(workflowSource)
	if err != nil {
		return format.FormatUnknown, err
	}
	detector, err := factory.BindFormatDetector(runtimeProvider, runtimeConn, plan)
	if err != nil {
		return format.FormatUnknown, err
	}
	detectCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	detected, err := detector.DetectFormat(detectCtx)
	if err != nil {
		return format.FormatUnknown, fmt.Errorf("detect %s format with runtime %d: %w", formatType, runtime.ID, err)
	}
	return detected, nil
}

func (i *WorkflowContainerInspector) InspectContainer(
	ctx context.Context,
	source *commonModels.Engine,
	tenantID uint,
	physicalPath, sourceFormat, sourceLayout string,
) (*format.ContainerDescribeResult, error) {
	if i == nil || i.engineService == nil || source == nil {
		return nil, fmt.Errorf("workflow container inspector is not configured")
	}
	formatType := format.NormalizeFormat(sourceFormat)
	factory, err := format.GetRuntimeContainerInfoProviderFactory(formatType)
	if err != nil {
		return nil, err
	}
	runtime, runtimeProvider, runtimeConn, err := i.resolveRuntime(ctx, tenantID, factory.RequiredContainerInfoOperators())
	if err != nil {
		return nil, fmt.Errorf("resolve %s container inspection runtime: %w", formatType, err)
	}

	kind := workflowaccess.KindFile
	resourceType := resourcetree.TypeFile
	if sourceLayout == format.LayoutWhole {
		kind = workflowaccess.KindDirectory
		resourceType = resourcetree.TypeDirectory
	} else if strings.EqualFold(source.EngineType, "minio") || strings.EqualFold(source.EngineType, "s3") {
		resourceType = resourcetree.TypeObject
	}
	locator := resourcetree.LocatorFromFullName(source.ID, source.EngineType, string(resourceType), physicalPath, nil)
	if locator == nil {
		return nil, fmt.Errorf("cannot build %s container source locator for %q", formatType, physicalPath)
	}
	workflowSource, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: source, Locator: locator, Kind: kind, Format: string(formatType),
	})
	if err != nil {
		return nil, fmt.Errorf("resolve %s container workflow access: %w", formatType, err)
	}
	plan, err := workflowaccess.NewSourcePlan(workflowSource)
	if err != nil {
		return nil, err
	}
	provider, err := factory.BindContainerInfoProvider(runtimeProvider, runtimeConn, plan)
	if err != nil {
		return nil, err
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := provider.DescribeContainer(inspectCtx, format.ContainerParseOptions(workflowContainerChildLimit, 0))
	if err != nil {
		return nil, fmt.Errorf("inspect %s container with runtime %d: %w", formatType, runtime.ID, err)
	}
	return result, nil
}

func (i *WorkflowContainerInspector) resolveRuntime(
	ctx context.Context,
	tenantID uint,
	operators []string,
) (*commonModels.Engine, engineplugin.WorkflowRuntimeProvider, engineplugin.ConnectionInfo, error) {
	engines, err := i.engineService.GetWorkflowRuntimesByTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, nil, err
	}
	sort.SliceStable(engines, func(left, right int) bool {
		if engines[left].IsBuiltin != engines[right].IsBuiltin {
			return !engines[left].IsBuiltin
		}
		return engines[left].ID < engines[right].ID
	})
	failures := make([]string, 0)
	for _, candidate := range engines {
		if !candidate.IsUsable() {
			continue
		}
		if _, providerErr := dbbridge.WorkflowRuntimeProviderForEngine(candidate); providerErr != nil {
			continue
		}
		runtime, providerErr := dbbridge.WorkflowRuntimeProviderForEngine(candidate)
		if providerErr != nil {
			continue
		}
		if requireErr := dbbridge.RequireDirectWorkflowOperators(ctx, candidate, operators...); requireErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.Name, requireErr))
			continue
		}
		return candidate, runtime, engineplugin.ConnectionInfo(candidate.ConnectionInfo), nil
	}
	message := fmt.Sprintf("no active workflow runtime provides direct operators %s", strings.Join(operators, ", "))
	if len(failures) > 0 {
		message += "; discovery failures: " + strings.Join(failures, "; ")
	}
	return nil, nil, nil, fmt.Errorf("%s", message)
}
