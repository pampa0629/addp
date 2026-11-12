package service

import (
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// Authority must retain the original numeric bytes. The general execution
// display map decodes JSON numbers as float64, which can round Engine IDs.
func productionScanRecord(query *gorm.DB) (*execution.TaskExecution, error) {
	var row struct {
		execution.TaskExecution
		RawExecutionConfig string `gorm:"column:raw_execution_config"`
	}
	if err := query.Model(&execution.TaskExecution{}).Select("*, execution_config AS raw_execution_config").Take(&row).Error; err != nil {
		return nil, err
	}
	config, err := jsonmap.DecodeObjectStrict([]byte(row.RawExecutionConfig), execution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return nil, err
	}
	row.ExecutionConfig = commonmodels.JSONMap(config)
	return &row.TaskExecution, nil
}
