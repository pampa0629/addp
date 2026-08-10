package api

import (
	"testing"

	commonExecution "github.com/addp/common/execution"
)

func TestQualityExecutionFilterIsScopedToCheckExecutions(t *testing.T) {
	filter := qualityExecutionFilter(7, 2, 50)
	if filter.TenantID != 7 || filter.Module != commonExecution.ModuleQuality || filter.TaskType != commonExecution.TaskTypeQualityCheck || filter.Page != 2 || filter.PageSize != 50 {
		t.Fatalf("quality execution filter = %#v", filter)
	}
}

func TestIsQualityCheckExecution(t *testing.T) {
	valid := &commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityCheck}
	if !isQualityCheckExecution(valid) {
		t.Fatal("check execution was rejected")
	}
	for _, item := range []*commonExecution.TaskExecution{
		nil,
		{Module: commonExecution.ModuleQuality, TaskType: "cleanup_executor"},
		{Module: commonExecution.ModuleSystem, TaskType: commonExecution.TaskTypeQualityCheck},
	} {
		if isQualityCheckExecution(item) {
			t.Fatalf("non-quality check execution accepted: %#v", item)
		}
	}
}
