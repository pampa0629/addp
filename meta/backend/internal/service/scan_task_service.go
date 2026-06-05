package service

import (
	"log/slog"

	"github.com/addp/common/logger"
	commonScheduler "github.com/addp/common/scheduler"
	"gorm.io/gorm"
)

// ScanTaskService 管理扫描任务定义和 Console 提交的 engine 扫描策略绑定。
type ScanTaskService struct {
	db          *gorm.DB
	log         *slog.Logger
	exprBuilder *commonScheduler.ExpressionBuilder
}

// NewScanTaskService 创建任务服务
func NewScanTaskService(db *gorm.DB) *ScanTaskService {
	return &ScanTaskService{
		db:          db,
		log:         logger.With("component", "scan_task_service"),
		exprBuilder: commonScheduler.NewExpressionBuilder(),
	}
}
