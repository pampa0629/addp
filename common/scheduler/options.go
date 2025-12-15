package scheduler

import (
	"context"
	"fmt"
	"time"
)

// Options 调度器配置选项
type Options struct {
	// 基础配置
	Name          string         // 调度器名称（用于日志和指标）
	Location      *time.Location // 时区（默认 Asia/Shanghai）
	EnableSeconds bool           // 是否启用秒级精度（默认 false）

	// 任务加载器（可选）
	TaskLoader TaskLoader // 启动时加载任务的函数

	// 去重保护（可选）
	EnableDedup      bool             // 是否启用去重
	DedupService     DedupService     // 去重服务实现
	DedupKeyFunc     func(string) string // 去重键生成函数
	DedupMinInterval time.Duration    // 最小执行间隔

	// 监控（可选）
	EnableMetrics bool // 是否启用 Prometheus 指标

	// 错误处理
	ErrorHandler ErrorHandler // 全局错误处理器

	// 日志
	Logger Logger // 日志接口
}

// defaultLogger 默认日志实现（使用标准输出）
type defaultLogger struct{}

func (l *defaultLogger) Info(msg string, args ...interface{}) {
	fmt.Printf("[INFO] %s %v\n", msg, args)
}

func (l *defaultLogger) Error(msg string, args ...interface{}) {
	fmt.Printf("[ERROR] %s %v\n", msg, args)
}

func (l *defaultLogger) Warn(msg string, args ...interface{}) {
	fmt.Printf("[WARN] %s %v\n", msg, args)
}

// defaultErrorHandler 默认错误处理器
func defaultErrorHandler(logger Logger) ErrorHandler {
	return func(ctx context.Context, taskID string, err error) {
		logger.Error("task execution failed", "task_id", taskID, "error", err)
	}
}

// ApplyDefaults 应用默认配置
func (o *Options) ApplyDefaults() error {
	// 设置默认时区
	if o.Location == nil {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			// 如果加载失败，使用 UTC
			loc = time.UTC
		}
		o.Location = loc
	}

	// 设置默认日志
	if o.Logger == nil {
		o.Logger = &defaultLogger{}
	}

	// 设置默认错误处理器
	if o.ErrorHandler == nil {
		o.ErrorHandler = defaultErrorHandler(o.Logger)
	}

	// 设置默认调度器名称
	if o.Name == "" {
		o.Name = "scheduler"
	}

	// 验证去重配置
	if o.EnableDedup {
		if o.DedupService == nil {
			return fmt.Errorf("dedup service is required when EnableDedup is true")
		}
		if o.DedupKeyFunc == nil {
			// 默认去重键函数
			o.DedupKeyFunc = func(taskID string) string {
				return fmt.Sprintf("scheduler:%s:%s", o.Name, taskID)
			}
		}
	}

	return nil
}
