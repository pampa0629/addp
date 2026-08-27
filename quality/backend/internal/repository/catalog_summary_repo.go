package repository

import (
	"context"
	"fmt"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type CatalogSummaryRepository struct{ db *gorm.DB }

type CatalogSummaryFact struct {
	Task       models.CheckTask
	Execution  *commonExecution.TaskExecution
	OpenIssues int64
}

func NewCatalogSummaryRepository(db *gorm.DB) *CatalogSummaryRepository {
	return &CatalogSummaryRepository{db: db}
}

func (r *CatalogSummaryRepository) Resolve(ctx context.Context, tenantID int64, references []models.CatalogSummaryReference) (map[string]CatalogSummaryFact, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("quality catalog summary database is required")
	}
	keys := make([][]interface{}, 0, len(references))
	for _, reference := range references {
		keys = append(keys, []interface{}{reference.EngineID, reference.SchemaName, reference.TableName})
	}
	var tasks []models.CheckTask
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND (engine_id, schema_name, table_name) IN ?", tenantID, keys).Find(&tasks).Error; err != nil {
		return nil, err
	}
	executionIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.LastExecutionID != "" {
			executionIDs = append(executionIDs, task.LastExecutionID)
		}
	}
	executions := make(map[string]commonExecution.TaskExecution, len(executionIDs))
	if len(executionIDs) > 0 {
		var rows []commonExecution.TaskExecution
		if err := r.db.WithContext(ctx).Where("tenant_id = ? AND execution_id IN ?", tenantID, executionIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			executions[row.ExecutionID] = row
		}
	}
	type issueCount struct {
		EngineID   int64
		SchemaName string
		TableName  string
		Count      int64
	}
	var counts []issueCount
	if err := r.db.WithContext(ctx).Model(&models.Issue{}).
		Select("engine_id, schema_name, table_name, count(*) AS count").
		Where("tenant_id = ? AND status = ? AND (engine_id, schema_name, table_name) IN ?", tenantID, "open", keys).
		Group("engine_id, schema_name, table_name").Scan(&counts).Error; err != nil {
		return nil, err
	}
	issueCounts := make(map[string]int64, len(counts))
	for _, row := range counts {
		issueCounts[catalogSummaryKey(row.EngineID, row.SchemaName, row.TableName)] = row.Count
	}
	result := make(map[string]CatalogSummaryFact, len(tasks))
	for _, task := range tasks {
		key := catalogSummaryKey(task.EngineID, task.SchemaName, task.Table)
		fact := CatalogSummaryFact{Task: task, OpenIssues: issueCounts[key]}
		if execution, exists := executions[task.LastExecutionID]; exists {
			copy := execution
			fact.Execution = &copy
		}
		result[key] = fact
	}
	return result, nil
}

func catalogSummaryKey(engineID int64, schemaName, tableName string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", engineID, schemaName, tableName)
}

func QualityScoreFromExecution(execution *commonExecution.TaskExecution) *float64 {
	if execution == nil || execution.Status != commonExecution.ExecutionStatusSuccess || execution.Metadata == nil || execution.Metadata["schema_version"] != "addp.quality.execution-result/v1" {
		return nil
	}
	value, ok := execution.Metadata["quality_score"]
	if !ok {
		return nil
	}
	score, ok := numericFloat64(value)
	if !ok || score < 0 || score > 100 {
		return nil
	}
	return &score
}

func numericFloat64(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}
