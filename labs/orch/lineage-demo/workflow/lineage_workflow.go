package workflow

import (
	"context"
	"fmt"
	"lineage/models"
	"lineage/service"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

// DataTransformWorkflow 数据转换工作流（示例）
func DataTransformWorkflow(ctx workflow.Context, sourceItemID, targetItemID uint) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("DataTransformWorkflow started", "sourceItemID", sourceItemID, "targetItemID", targetItemID)

	// 获取 Workflow 元数据（Temporal 特有）
	workflowInfo := workflow.GetInfo(ctx)

	// 记录血缘开始
	startTime := workflow.Now(ctx)
	lineageInput := &models.LineageInput{
		// 注入 Temporal 元数据（仅在 Activity 中完成）
		ExternalWorkflowID:  workflowInfo.WorkflowExecution.ID,
		ExternalExecutionID: workflowInfo.WorkflowExecution.RunID,
		WorkflowEngine:      "temporal",

		// 业务数据
		SourceItemID:      sourceItemID,
		SourceFingerprint: fmt.Sprintf("source-%d-fingerprint", sourceItemID),
		SourceType:        "table",
		SourcePath:        fmt.Sprintf("db.schema.source_table_%d", sourceItemID),

		TargetItemID:      targetItemID,
		TargetFingerprint: fmt.Sprintf("target-%d-fingerprint", targetItemID),
		TargetType:        "table",
		TargetPath:        fmt.Sprintf("db.schema.target_table_%d", targetItemID),

		LineageType: "transform",
		Status:      "success",
		StartTime:   startTime,
		EndTime:     startTime, // 临时值，Activity 中更新

		RecordsProcessed: 1000,
		BytesWritten:     50000,
		TransformConfig: map[string]interface{}{
			"operation": "aggregate",
			"group_by":  []string{"category", "date"},
		},
		Metrics: map[string]interface{}{
			"cpu_usage":    45.2,
			"memory_usage": 120.5,
		},
	}

	// 执行 Activity 记录血缘（依赖注入 LineageRecorder）
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	err := workflow.ExecuteActivity(ctx, RecordLineageActivity, lineageInput).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to record lineage", "error", err)
		return err
	}

	logger.Info("DataTransformWorkflow completed successfully")
	return nil
}

// RecordLineageActivity 记录血缘的 Activity（薄封装，调用 lineage 库）
func RecordLineageActivity(ctx context.Context, input *models.LineageInput) error {
	logger := activity.GetLogger(ctx)
	logger.Info("RecordLineageActivity started")

	// 从 Activity Context 获取注入的 LineageRecorder（依赖注入）
	recorder := ctx.Value("lineage_recorder").(service.LineageRecorder)
	if recorder == nil {
		return fmt.Errorf("lineage_recorder not found in context")
	}

	// 更新结束时间
	input.EndTime = time.Now()

	// 调用 lineage 库记录血缘（纯业务逻辑，无 Temporal 依赖）
	lineage, err := recorder.Record(ctx, input)
	if err != nil {
		logger.Error("Failed to record lineage", "error", err)
		return err
	}

	logger.Info("Lineage recorded successfully", "lineage_id", lineage.ID)
	return nil
}
