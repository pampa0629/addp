package scheduler

import (
	"context"
	"time"
)

// TaskHandler 任务执行回调接口
// taskID: 任务唯一标识符
// 返回 error 表示任务执行失败
type TaskHandler func(ctx context.Context, taskID string) error

// TaskInfo 调度任务信息
type TaskInfo struct {
	ID          string     // 任务唯一标识
	CronExpr    string     // Cron 表达式
	NextRunTime time.Time  // 下次执行时间
	LastRunTime *time.Time // 上次执行时间
	IsEnabled   bool       // 是否启用
}

// TaskDefinition 任务定义（用于启动时加载任务）
type TaskDefinition struct {
	ID       string      // 任务唯一标识
	CronExpr string      // Cron 表达式
	Handler  TaskHandler // 执行回调
	Enabled  bool        // 是否启用
}

// TaskLoader 任务加载器接口（用于启动时从数据库加载任务）
type TaskLoader interface {
	LoadTasks(ctx context.Context) ([]TaskDefinition, error)
}

// Scheduler 通用调度器接口
type Scheduler interface {
	// 生命周期管理
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool

	// 任务调度管理
	Schedule(ctx context.Context, id string, cronExpr string, handler TaskHandler) error
	Unschedule(ctx context.Context, id string) error
	UpdateSchedule(ctx context.Context, id string, cronExpr string) error

	// 查询功能
	GetNextRunTime(cronExpr string) (time.Time, error)
	GetScheduledTasks() []TaskInfo
	GetTaskInfo(id string) (*TaskInfo, error)
}

// ErrorHandler 错误处理器
type ErrorHandler func(ctx context.Context, taskID string, err error)

// Logger 日志接口（兼容多种日志库）
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
}

// DedupService 去重服务接口
type DedupService interface {
	CheckTaskExists(ctx context.Context, key string) bool
	MarkTaskRunning(ctx context.Context, key string, ttl time.Duration) error
	ClearTask(ctx context.Context, key string) error
}
