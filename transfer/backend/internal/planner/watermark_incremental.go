package planner

import (
	"fmt"
	"strings"

	"github.com/addp/transfer/internal/executor"
)

type WatermarkIncrementalBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Plan             executor.WatermarkIncrementalPlan
}

func BuildWatermarkIncrementalPlan(spec TableExportTaskSpec, resolver EngineResolver) (*WatermarkIncrementalBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateTableTransferSpec(spec); err != nil {
		return nil, err
	}
	if !IsWatermarkIncrementalSpec(spec) {
		return nil, fmt.Errorf("task is not a bounded watermark incremental spec")
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return nil, err
	}
	target, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return nil, err
	}
	sourceType := strings.ToLower(effectiveEngineType(source, sourceRef))
	targetType := strings.ToLower(effectiveEngineType(target, targetRef))
	if sourceType != "postgresql" || targetType != "postgresql" {
		return nil, fmt.Errorf("watermark incremental first version only supports PostgreSQL -> PostgreSQL, got %s -> %s", sourceType, targetType)
	}
	if source.Capabilities == nil || source.Capabilities.Storage == nil || source.Capabilities.Storage.Store == nil || !source.Capabilities.Storage.Store.BoundedWatermarkRead {
		return nil, fmt.Errorf("source PostgreSQL engine does not declare bounded_watermark_read")
	}
	if target.Capabilities == nil || target.Capabilities.Storage == nil || target.Capabilities.Storage.Store == nil || target.Capabilities.Storage.Store.TableUpsert == nil || !target.Capabilities.Storage.Store.TableUpsert.Supported || !target.Capabilities.Storage.Store.TableUpsert.Idempotent {
		return nil, fmt.Errorf("target PostgreSQL engine does not declare idempotent table_upsert")
	}
	sourcePlan, err := buildTableSourcePlan(spec.Source, source, spec.Transforms)
	if err != nil {
		return nil, err
	}
	targetPlan, err := buildTableTargetPlan(spec.Target, target)
	if err != nil {
		return nil, err
	}
	targetPlan.DeleteBeforeWrite = false
	if err := applySourceGeometryEncodingForTarget(&sourcePlan, targetPlan, source, target); err != nil {
		return nil, err
	}
	return &WatermarkIncrementalBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Plan: executor.WatermarkIncrementalPlan{
			Source: sourcePlan, Target: targetPlan,
			WatermarkField: strings.TrimSpace(spec.Load.ChangeDetection.Field),
			TieBreakers:    append([]string(nil), spec.Load.ChangeDetection.TieBreaker...),
			TargetKeys:     policyStrings(spec.Target.Policy, "keys"),
			Transforms:     buildTableTransforms(spec.Transforms), BatchSize: spec.BatchSize,
		},
	}, nil
}
