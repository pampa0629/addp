package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
)

// CheckExecutor 执行质量检查任务
type CheckExecutor struct {
	systemClient  *commonClient.SystemClient
	ruleAppRepo   *repository.RuleApplicationRepository
	checkTaskRepo *repository.CheckTaskRepository
	issueRepo     *repository.IssueRepository
	sqlGen        *SQLGenerator
}

func NewCheckExecutor(
	systemClient *commonClient.SystemClient,
	ruleAppRepo *repository.RuleApplicationRepository,
	checkTaskRepo *repository.CheckTaskRepository,
	issueRepo *repository.IssueRepository,
) *CheckExecutor {
	return &CheckExecutor{
		systemClient:  systemClient,
		ruleAppRepo:   ruleAppRepo,
		checkTaskRepo: checkTaskRepo,
		issueRepo:     issueRepo,
		sqlGen:        NewSQLGenerator(),
	}
}

type RuleResult struct {
	RuleApplicationID int64   `json:"rule_application_id"`
	RuleType          string  `json:"rule_type"`
	Column            string  `json:"column"`
	Table             string  `json:"table"`
	Schema            string  `json:"schema"`
	PassRate          float64 `json:"pass_rate"`
	FailedCount       int64   `json:"failed_count"`
	TotalCount        int64   `json:"total_count"`
	Passed            bool    `json:"passed"`
	Error             string  `json:"error,omitempty"`
}

type FieldScore struct {
	Column string  `json:"column"`
	Score  float64 `json:"score"`
	Passed int     `json:"passed"`
	Failed int     `json:"failed"`
}

type ExecutionResult struct {
	QualityScore float64      `json:"quality_score"`
	TotalRules   int          `json:"total_rules"`
	PassedRules  int          `json:"passed_rules"`
	FailedRules  int          `json:"failed_rules"`
	FieldScores  []FieldScore `json:"field_scores"`
	RuleDetails  []RuleResult `json:"rule_details"`
}

// RunCheck 执行一个检查任务，结果写入 common.task_executions，问题写入 quality.issues
func (e *CheckExecutor) RunCheck(ctx context.Context, taskID, tenantID, userID int64) (string, error) {
	return e.RunCheckWithContext(ctx, taskID, tenantID, userID, commonExecution.TriggerTypeManual, commonExecution.ModuleQuality, nil)
}

// RunCheckWithContext 按统一 TaskProvider 执行上下文执行检查任务。
func (e *CheckExecutor) RunCheckWithContext(
	ctx context.Context,
	taskID, tenantID, userID int64,
	triggerType string,
	source string,
	parentExecutionID *string,
) (string, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleQuality
	}

	executionID := uuid.New().String()
	now := time.Now()

	// 任务 claim 与 pending execution 创建必须位于同一事务。
	taskExec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleQuality,
		TaskType:          commonExecution.TaskTypeQualityCheck,
		Source:            normalizedSource,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		TriggerType:       normalizedTriggerType,
		TriggeredBy:       intPtr(int(userID)),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	task, err := e.checkTaskRepo.ClaimExecution(ctx, taskID, tenantID, taskExec)
	if err != nil {
		return "", fmt.Errorf("claim quality check execution: %w", err)
	}

	// 异步执行检查
	go func() {
		startedAt := time.Now()
		if err := e.checkTaskRepo.StartExecution(context.Background(), taskID, tenantID, executionID, startedAt); err != nil {
			log.Printf("failed to start quality execution %s: %v", executionID, err)
			return
		}

		result, issues, execErr := e.doCheck(context.Background(), task)

		completedAt := time.Now()
		durationMs := completedAt.Sub(startedAt).Milliseconds()

		updates := map[string]interface{}{
			"completed_at":      completedAt,
			"execution_time_ms": durationMs,
		}

		if execErr != nil {
			log.Printf("quality check execution %s failed: %v", executionID, execErr)
			updates["status"] = commonExecution.ExecutionStatusFailed
			updates["error_details"] = commonModels.JSONMap{"message": execErr.Error()}
		} else {
			// 写入问题工单
			if len(issues) > 0 {
				for i := range issues {
					issues[i].ExecutionID = executionID
				}
				if bErr := e.issueRepo.BatchCreate(issues); bErr != nil {
					execErr = fmt.Errorf("failed to create quality issues: %w", bErr)
				}
			}
			if execErr != nil {
				updates["status"] = commonExecution.ExecutionStatusFailed
				updates["error_details"] = commonModels.JSONMap{"message": execErr.Error()}
			} else {
				updates["status"] = commonExecution.ExecutionStatusSuccess
				updates["metadata"] = executionResultMetadata(result)
				updates["progress"] = 100
			}
		}

		if err := e.checkTaskRepo.CompleteExecution(context.Background(), taskID, tenantID, executionID, updates, completedAt); err != nil {
			log.Printf("failed to update execution %s: %v", executionID, err)
		}
	}()

	return executionID, nil
}

func executionResultMetadata(result *ExecutionResult) commonModels.JSONMap {
	if result == nil {
		return commonModels.JSONMap{}
	}
	return commonModels.JSONMap{
		"quality_score": result.QualityScore,
		"total_rules":   result.TotalRules,
		"passed_rules":  result.PassedRules,
		"failed_rules":  result.FailedRules,
		"field_scores":  result.FieldScores,
		"rule_details":  result.RuleDetails,
	}
}

func (e *CheckExecutor) doCheck(ctx context.Context, task *models.CheckTask) (*ExecutionResult, []models.Issue, error) {
	// 获取引擎配置
	engine, err := e.systemClient.GetEngine(uint(task.EngineID))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 通过 dbbridge 获取数据库连接
	poolCfg := dbbridge.DefaultPoolConfig()
	targetDB, err := dbbridge.GetOrCreatePool(engine, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to engine: %w", err)
	}

	// 获取需要检查的规则应用
	ruleApps, err := e.ruleAppRepo.ListByTask(task.TenantID, task.EngineID, task.SchemaName, task.Table)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list rule applications: %w", err)
	}

	if len(ruleApps) == 0 {
		return &ExecutionResult{QualityScore: 100, TotalRules: 0, FieldScores: []FieldScore{}, RuleDetails: []RuleResult{}}, nil, nil
	}

	ruleDetails := make([]RuleResult, 0, len(ruleApps))
	fieldScoreMap := map[string]*FieldScore{}
	passedRules := 0

	for _, ra := range ruleApps {
		var ruleConfig map[string]interface{}
		if err := json.Unmarshal(ra.RuleConfig, &ruleConfig); err != nil {
			log.Printf("invalid rule config for rule_application %d: %v", ra.ID, err)
			continue
		}

		ruleType, _ := ruleConfig["rule_type"].(string)
		if ruleType == "" {
			continue
		}

		failSQL, countSQL, genErr := e.sqlGen.GenerateCheckSQL(ra.SchemaName, ra.Table, ra.ColumnName, ruleType, ruleConfig)
		if genErr != nil {
			ruleDetails = append(ruleDetails, RuleResult{
				RuleApplicationID: ra.ID,
				RuleType:          ruleType,
				Column:            ra.ColumnName,
				Table:             ra.Table,
				Schema:            ra.SchemaName,
				Error:             genErr.Error(),
			})
			continue
		}

		var totalCount, failedCount int64
		if err := targetDB.WithContext(ctx).Raw(countSQL).Scan(&totalCount).Error; err != nil {
			ruleDetails = append(ruleDetails, RuleResult{
				RuleApplicationID: ra.ID,
				RuleType:          ruleType,
				Column:            ra.ColumnName,
				Table:             ra.Table,
				Schema:            ra.SchemaName,
				Error:             err.Error(),
			})
			continue
		}
		if err := targetDB.WithContext(ctx).Raw(failSQL).Scan(&failedCount).Error; err != nil {
			ruleDetails = append(ruleDetails, RuleResult{
				RuleApplicationID: ra.ID,
				RuleType:          ruleType,
				Column:            ra.ColumnName,
				Table:             ra.Table,
				Schema:            ra.SchemaName,
				Error:             err.Error(),
			})
			continue
		}

		passRate := 100.0
		if totalCount > 0 {
			passRate = float64(totalCount-failedCount) / float64(totalCount) * 100
		}
		passed := failedCount == 0

		ruleResult := RuleResult{
			RuleApplicationID: ra.ID,
			RuleType:          ruleType,
			Column:            ra.ColumnName,
			Table:             ra.Table,
			Schema:            ra.SchemaName,
			PassRate:          passRate,
			FailedCount:       failedCount,
			TotalCount:        totalCount,
			Passed:            passed,
		}
		ruleDetails = append(ruleDetails, ruleResult)

		if passed {
			passedRules++
		}

		// 汇总字段得分
		if _, ok := fieldScoreMap[ra.ColumnName]; !ok {
			fieldScoreMap[ra.ColumnName] = &FieldScore{Column: ra.ColumnName}
		}
		fs := fieldScoreMap[ra.ColumnName]
		if passed {
			fs.Passed++
		} else {
			fs.Failed++
		}
	}

	totalRules := len(ruleDetails)
	failedRules := totalRules - passedRules
	qualityScore := 0.0
	if totalRules > 0 {
		qualityScore = float64(passedRules) / float64(totalRules) * 100
	}

	// 计算字段分数
	fieldScores := make([]FieldScore, 0, len(fieldScoreMap))
	for _, fs := range fieldScoreMap {
		total := fs.Passed + fs.Failed
		if total > 0 {
			fs.Score = float64(fs.Passed) / float64(total) * 100
		}
		fieldScores = append(fieldScores, *fs)
	}

	result := &ExecutionResult{
		QualityScore: qualityScore,
		TotalRules:   totalRules,
		PassedRules:  passedRules,
		FailedRules:  failedRules,
		FieldScores:  fieldScores,
		RuleDetails:  ruleDetails,
	}

	// 为失败的规则创建工单
	issues := make([]models.Issue, 0)
	for _, rd := range ruleDetails {
		if rd.Passed || rd.Error != "" {
			continue
		}
		issues = append(issues, models.Issue{
			TenantID:          task.TenantID,
			ExecutionID:       "", // 调用方填入
			RuleApplicationID: rd.RuleApplicationID,
			RuleType:          rd.RuleType,
			ColumnName:        rd.Column,
			Table:             rd.Table,
			SchemaName:        rd.Schema,
			EngineID:          task.EngineID,
			FailedCount:       rd.FailedCount,
			TotalCount:        rd.TotalCount,
			PassRate:          rd.PassRate,
			Status:            "open",
		})
	}

	return result, issues, nil
}

func intPtr(v int) *int {
	return &v
}
