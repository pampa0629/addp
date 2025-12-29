package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/meta/internal/operators"
	metaModels "github.com/addp/meta/internal/models"
)

// OperatorService 算子服务
type OperatorService struct {
	scanTaskService *ScanTaskService
}

// NewOperatorService 创建算子服务
func NewOperatorService(scanTaskService *ScanTaskService) *OperatorService {
	return &OperatorService{
		scanTaskService: scanTaskService,
	}
}

// GetOperators 获取所有算子
func (s *OperatorService) GetOperators() []models.OperatorMetadata {
	return operators.MetaOperators
}

// ExecuteOperator 执行算子
func (s *OperatorService) ExecuteOperator(
	ctx context.Context,
	operatorName string,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	switch operatorName {
	case "scan_basic", "scan_deep":
		return s.executeScanOperator(ctx, operatorName, tenantID, userID, params, executeNow, taskName)
	default:
		return nil, fmt.Errorf("未知的算子: %s", operatorName)
	}
}

// executeScanOperator 执行扫描算子
func (s *OperatorService) executeScanOperator(
	ctx context.Context,
	operatorName string,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	// 解析参数
	engineID, ok := params["engine_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 engine_id 必须为数字")
	}

	var schemaNames []string
	if sn, ok := params["schema_names"].([]interface{}); ok {
		for _, v := range sn {
			if str, ok := v.(string); ok {
				schemaNames = append(schemaNames, str)
			}
		}
	}

	var objectPaths []string
	if op, ok := params["object_paths"].([]interface{}); ok {
		for _, v := range op {
			if str, ok := v.(string); ok {
				objectPaths = append(objectPaths, str)
			}
		}
	}

	// 确定扫描深度
	scanDepth := "basic"
	if operatorName == "scan_deep" {
		scanDepth = "deep"
	}

	// 创建扫描任务
	scanReq := &metaModels.ScanRequest{
		EngineID:  uint(engineID),
		SchemaNames: schemaNames,
		ObjectPaths: objectPaths,
		ScanDepth:   scanDepth,
		ScanType:    "manual",
	}

	run, err := s.scanTaskService.CreateManualRun(ctx, tenantID, userID, "", scanReq)
	if err != nil {
		return nil, err
	}

	response := &models.OperatorExecuteResponse{
		Status:     "success",
		TaskID:     fmt.Sprintf("%d", run.ID),
		TaskStatus: string(run.Status),
		Message:    "扫描任务已创建",
		CreatedAt:  run.CreatedAt.Format(time.RFC3339),
	}

	// 如果是同步执行,等待结果(简化实现,实际应该轮询任务状态)
	if executeNow {
		// 这里暂不实现等待逻辑,直接返回任务ID
		// 真正的实现需要轮询任务状态直到完成
		response.Message = "扫描任务已创建并开始执行"
	}

	return response, nil
}
