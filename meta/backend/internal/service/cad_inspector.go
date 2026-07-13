package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metaenrich"
)

type SuperMapCADInspector struct {
	engineService *EngineService
}

func NewSuperMapCADInspector(engineService *EngineService) *SuperMapCADInspector {
	return &SuperMapCADInspector{engineService: engineService}
}

func (i *SuperMapCADInspector) InspectCAD(ctx context.Context, source *commonModels.Engine, tenantID uint, physicalPath string, sizeBytes int64) (*metaenrich.CADInspection, error) {
	if i == nil || i.engineService == nil || source == nil {
		return nil, fmt.Errorf("CAD inspector is not configured")
	}
	i.engineService.ensureInternalClient()
	if i.engineService.internalClient == nil {
		return nil, fmt.Errorf("CAD deep scan requires the System internal client")
	}
	engines, err := i.engineService.internalClient.ListEngines("supermap_workflow", tenantID)
	if err != nil {
		return nil, fmt.Errorf("list SuperMap workflow engines: %w", err)
	}
	var runtime *commonModels.Engine
	for idx := range engines {
		if engines[idx].IsActive && strings.EqualFold(engines[idx].EngineType, "supermap_workflow") {
			candidate := engines[idx]
			runtime = &candidate
			break
		}
	}
	if runtime == nil {
		return nil, fmt.Errorf("no active supermap_workflow engine is available for CAD deep scan")
	}

	resourceType := string(resourcetree.TypeFile)
	if strings.EqualFold(source.EngineType, "minio") || strings.EqualFold(source.EngineType, "s3") {
		resourceType = string(resourcetree.TypeObject)
	}
	locator := resourcetree.LocatorFromFullName(source.ID, source.EngineType, resourceType, physicalPath, nil)
	if locator == nil {
		return nil, fmt.Errorf("cannot build CAD source locator for %q", physicalPath)
	}
	workflowSource, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: source, Locator: locator, Kind: workflowaccess.KindFile, Format: "dwg",
	})
	if err != nil {
		return nil, fmt.Errorf("resolve CAD workflow access: %w", err)
	}
	plan, err := workflowaccess.NewSourcePlan(workflowSource)
	if err != nil {
		return nil, err
	}
	result, err := dbbridge.InvokeOperator(ctx, runtime, "cad.inspect", plugin.OperatorInvokeRequest{
		Params:  map[string]interface{}{"access_plan": plan.JSONMap()},
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("invoke cad.inspect: %w", err)
	}
	return normalizeCADInspection(result.Result, sizeBytes)
}

type cadInspectDTO struct {
	SchemaVersion string `json:"schema_version"`
	Format        string `json:"format"`
	FormatVersion string `json:"format_version"`
	Drawing       struct {
		DrawingKind          string             `json:"drawing_kind"`
		Unit                 string             `json:"unit"`
		EntityCount          *int64             `json:"entity_count"`
		LayerCount           *int64             `json:"layer_count"`
		LayoutCount          *int64             `json:"layout_count"`
		BlockDefinitionCount *int64             `json:"block_definition_count"`
		XRefCount            *int64             `json:"xref_count"`
		HasModelSpace        *bool              `json:"has_model_space"`
		HasPaperSpace        *bool              `json:"has_paper_space"`
		Bounds2D             *datatype.Bounds2D `json:"bounds_2d"`
		Bounds3D             *datatype.Bounds3D `json:"bounds_3d"`
	} `json:"drawing"`
	Interpretation struct {
		DatasetCount           *int64   `json:"dataset_count"`
		InterpretedRecordCount *int64   `json:"interpreted_record_count"`
		Provider               string   `json:"provider"`
		ProviderVersion        string   `json:"provider_version"`
		NormalizedGeometry     *bool    `json:"normalized_geometry"`
		GeometryTraversed      *bool    `json:"geometry_traversed"`
		ScanComplete           *bool    `json:"scan_complete"`
		Warnings               []string `json:"warnings"`
	} `json:"interpretation"`
}

func normalizeCADInspection(raw map[string]interface{}, sizeBytes int64) (*metaenrich.CADInspection, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var dto cadInspectDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		return nil, err
	}
	if dto.SchemaVersion != "addp.cad.inspect/v1" || dto.Format != "dwg" {
		return nil, fmt.Errorf("unsupported CAD inspection response %q/%q", dto.SchemaVersion, dto.Format)
	}
	entityCount := dto.Drawing.EntityCount
	if entityCount == nil {
		entityCount = dto.Interpretation.InterpretedRecordCount
	}
	cad := datatype.NormalizeCADInfo(&datatype.CADInfo{
		DrawingKind: dto.Drawing.DrawingKind, Unit: dto.Drawing.Unit, EntityCount: entityCount,
		LayerCount: dto.Drawing.LayerCount, LayoutCount: dto.Drawing.LayoutCount,
		BlockDefinitionCount: dto.Drawing.BlockDefinitionCount, XRefCount: dto.Drawing.XRefCount,
		HasModelSpace: dto.Drawing.HasModelSpace, HasPaperSpace: dto.Drawing.HasPaperSpace,
		Bounds2D: dto.Drawing.Bounds2D, Bounds3D: dto.Drawing.Bounds3D, SizeBytes: &sizeBytes,
	})
	formatInfo := map[string]interface{}{
		"format_version":           dto.FormatVersion,
		"provider":                 dto.Interpretation.Provider,
		"provider_version":         dto.Interpretation.ProviderVersion,
		"dataset_count":            dto.Interpretation.DatasetCount,
		"interpreted_record_count": dto.Interpretation.InterpretedRecordCount,
		"normalized_geometry":      dto.Interpretation.NormalizedGeometry,
		"geometry_traversed":       dto.Interpretation.GeometryTraversed,
		"scan_complete":            dto.Interpretation.ScanComplete,
		"warnings":                 dto.Interpretation.Warnings,
	}
	return &metaenrich.CADInspection{CAD: cad, FormatInfo: formatInfo}, nil
}
