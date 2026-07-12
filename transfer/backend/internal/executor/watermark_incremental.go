package executor

import (
	"context"
	"fmt"

	engineplugin "github.com/addp/common/engine/plugin"
)

type WatermarkIncrementalPlan struct {
	Source         TableSourcePlan
	Target         TableTargetPlan
	WatermarkField string
	TieBreakers    []string
	Start          *engineplugin.WatermarkCursor
	TargetKeys     []string
	Transforms     []TableTransformPlan
	BatchSize      int
	BeforeApply    func(context.Context) error
	AfterApply     func(context.Context, *engineplugin.WatermarkCursor, int64, int64) error
}

type WatermarkIncrementalMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	UpperBound     *engineplugin.WatermarkCursor
}

type WatermarkIncrementalExecutor struct {
	Source engineplugin.BoundedWatermarkReadProvider
	Target engineplugin.TableUpsertProvider
}

func NewWatermarkIncrementalExecutor(sourceEngineType, targetEngineType string) (*WatermarkIncrementalExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, err
	}
	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, err
	}
	source, ok := sourcePlugin.(engineplugin.BoundedWatermarkReadProvider)
	if !ok {
		return nil, fmt.Errorf("source engine %q does not support bounded watermark reads", sourceEngineType)
	}
	target, ok := targetPlugin.(engineplugin.TableUpsertProvider)
	if !ok {
		return nil, fmt.Errorf("target engine %q does not support idempotent table upsert", targetEngineType)
	}
	return &WatermarkIncrementalExecutor{Source: source, Target: target}, nil
}

func (e *WatermarkIncrementalExecutor) Execute(ctx context.Context, plan WatermarkIncrementalPlan) (*WatermarkIncrementalMetrics, error) {
	if plan.BatchSize <= 0 {
		plan.BatchSize = 1000
	}
	session, err := e.Source.OpenBoundedWatermarkRead(ctx, plan.Source.ConnInfo, plan.Source.Path, engineplugin.BoundedWatermarkReadOptions{
		WatermarkField: plan.WatermarkField, TieBreakers: plan.TieBreakers, Start: plan.Start,
	})
	if err != nil {
		return nil, err
	}
	defer session.Close(context.Background())
	metrics := &WatermarkIncrementalMetrics{UpperBound: session.UpperBound()}
	transforms, err := buildTableTransforms(plan.Transforms, nil)
	if err != nil {
		return metrics, err
	}
	tableInfo, spatialInfo := session.TableInfo()
	tableInfo, spatialInfo, err = applyTableInfoTransforms(tableInfo, spatialInfo, transforms)
	if err != nil {
		return metrics, err
	}
	if err := e.Target.PrepareTableUpsert(ctx, plan.Target.ConnInfo, plan.Target.Path, engineplugin.TableUpsertOptions{Fields: tableInfo.Fields, SpatialInfo: spatialInfo, Keys: plan.TargetKeys}); err != nil {
		return metrics, err
	}
	for {
		batch, err := session.ReadBatch(ctx, plan.BatchSize)
		if err != nil {
			return metrics, err
		}
		if batch == nil || len(batch.Rows) == 0 {
			return metrics, nil
		}
		position, err := session.PositionForRow(batch.Rows[len(batch.Rows)-1])
		if err != nil {
			return metrics, err
		}
		metrics.RecordsRead += int64(len(batch.Rows))
		batch, err = applyBatchTransforms(ctx, batch, transforms)
		if err != nil {
			return metrics, err
		}
		if plan.BeforeApply != nil {
			if err := plan.BeforeApply(ctx); err != nil {
				return metrics, err
			}
		}
		if err := e.Target.UpsertBatch(ctx, plan.Target.ConnInfo, plan.Target.Path, batch, engineplugin.TableUpsertOptions{Fields: batch.Fields, SpatialInfo: batch.Spatial, Keys: plan.TargetKeys}); err != nil {
			return metrics, err
		}
		written := int64(len(batch.Rows))
		metrics.RecordsWritten += written
		if plan.AfterApply != nil {
			if err := plan.AfterApply(ctx, position, metrics.RecordsRead, metrics.RecordsWritten); err != nil {
				return metrics, err
			}
		}
	}
}
