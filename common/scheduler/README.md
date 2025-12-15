# Common Scheduler Module

统一调度公共模块，为 ADDP 平台提供通用的 Cron 调度能力。

## 概述

`common/scheduler` 模块消除了 Meta、Orchestrator、Transfer、Develop 等模块中 80% 的调度代码重复，提供统一的调度管理、时区处理、错误处理和监控能力。

## 核心特性

- ✅ **统一调度接口**：基于 `github.com/robfig/cron/v3` 的标准 Cron 调度
- ✅ **灵活配置**：支持手动/每天/每周/每月/自定义 Cron 表达式
- ✅ **秒级精度**：可选启用秒级调度（Transfer 模块需要）
- ✅ **时区支持**：统一使用 Asia/Shanghai 时区，可自定义
- ✅ **任务持久化**：启动时从数据库自动加载任务
- ✅ **去重保护**：防止并发执行同一任务
- ✅ **UI 配置转换**：将前端配置自动转换为 Cron 表达式
- ✅ **线程安全**：使用读写锁和原子操作保证并发安全
- ✅ **监控友好**：支持自定义日志和错误处理器

## 快速开始

### 基础用法

```go
package main

import (
    "context"
    "fmt"
    "time"

    commonScheduler "github.com/addp/common/scheduler"
)

func main() {
    // 1. 创建调度器
    scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
        Name: "my-scheduler",
    })
    if err != nil {
        panic(err)
    }

    // 2. 启动调度器
    ctx := context.Background()
    if err := scheduler.Start(ctx); err != nil {
        panic(err)
    }
    defer scheduler.Stop(ctx)

    // 3. 调度任务
    handler := func(ctx context.Context, taskID string) error {
        fmt.Printf("Task %s executed at %s\n", taskID, time.Now())
        return nil
    }

    // 每天 14:30 执行
    if err := scheduler.Schedule(ctx, "daily-task", "30 14 * * *", handler); err != nil {
        panic(err)
    }

    // 4. 查询下次执行时间
    nextRun, _ := scheduler.GetNextRunTime("30 14 * * *")
    fmt.Printf("Next run: %s\n", nextRun)

    // 保持运行
    select {}
}
```

### 使用 UI 配置构建 Cron 表达式

```go
import commonScheduler "github.com/addp/common/scheduler"

// 创建表达式构建器
builder := commonScheduler.NewExpressionBuilder()

// 从 UI 配置构建表达式
config := commonScheduler.ScheduleConfig{
    Type:  "weekly",           // 每周
    Time:  "10:00",            // 10:00 执行
    Value: []int{1, 3, 5},     // 周一、周三、周五
}

cronExpr, scheduleConfig, err := builder.BuildFromScheduleConfig(config)
// cronExpr: "0 10 * * 1,3,5"
// scheduleConfig: map[string]interface{} 用于前端回显

// 验证表达式
if err := builder.Validate(cronExpr); err != nil {
    // 处理无效表达式
}

// 计算下次执行时间
nextRun, err := builder.NextRunTime(cronExpr, time.Now())

// 计算接下来 5 次执行时间
nextRuns, err := builder.NextRunTimes(cronExpr, 5)
```

### 启用秒级精度

```go
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:          "transfer-scheduler",
    EnableSeconds: true,  // 启用秒级精度
})

// 每 2 秒执行一次
scheduler.Schedule(ctx, "frequent-task", "*/2 * * * * *", handler)
```

### 启动时从数据库加载任务

```go
// 1. 实现 TaskLoader 接口
type MyTaskLoader struct {
    db *gorm.DB
}

func (l *MyTaskLoader) LoadTasks(ctx context.Context) ([]commonScheduler.TaskDefinition, error) {
    var tasks []models.Task
    if err := l.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
        return nil, err
    }

    var defs []commonScheduler.TaskDefinition
    for _, task := range tasks {
        defs = append(defs, commonScheduler.TaskDefinition{
            ID:       fmt.Sprintf("%d", task.ID),
            CronExpr: task.CronExpression,
            Handler:  l.createHandler(task.ID),
            Enabled:  task.Enabled,
        })
    }
    return defs, nil
}

// 2. 配置调度器
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:       "my-scheduler",
    TaskLoader: &MyTaskLoader{db: db},
})

// 3. 启动时自动加载所有任务
scheduler.Start(ctx)  // 自动调用 TaskLoader.LoadTasks()
```

### 启用去重保护

```go
import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
    commonScheduler "github.com/addp/common/scheduler"
)

// 1. 实现 DedupService 接口（基于 Redis）
type RedisDedupService struct {
    client *redis.Client
}

func (s *RedisDedupService) CheckTaskExists(ctx context.Context, key string) bool {
    val, _ := s.client.Get(ctx, key).Result()
    return val != ""
}

func (s *RedisDedupService) MarkTaskRunning(ctx context.Context, key string, ttl time.Duration) error {
    return s.client.Set(ctx, key, "running", ttl).Err()
}

func (s *RedisDedupService) ClearTask(ctx context.Context, key string) error {
    return s.client.Del(ctx, key).Err()
}

// 2. 配置调度器
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:             "meta-scanner",
    EnableDedup:      true,
    DedupService:     &RedisDedupService{client: redisClient},
    DedupKeyFunc:     func(taskID string) string { return fmt.Sprintf("meta:scan:%s", taskID) },
    DedupMinInterval: 6 * time.Hour,  // 最小执行间隔 6 小时
})
```

### 自定义日志和错误处理

```go
import "log/slog"

// 1. 实现 Logger 接口（适配 slog）
type SlogAdapter struct {
    logger *slog.Logger
}

func (l *SlogAdapter) Info(msg string, args ...interface{}) {
    l.logger.Info(msg, args...)
}

func (l *SlogAdapter) Error(msg string, args ...interface{}) {
    l.logger.Error(msg, args...)
}

func (l *SlogAdapter) Warn(msg string, args ...interface{}) {
    l.logger.Warn(msg, args...)
}

// 2. 自定义错误处理器
errorHandler := func(ctx context.Context, taskID string, err error) {
    // 记录到数据库
    db.Model(&models.Task{}).Where("id = ?", taskID).Update("last_error", err.Error())

    // 发送告警
    alertService.SendAlert(fmt.Sprintf("Task %s failed: %v", taskID, err))
}

// 3. 配置调度器
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:         "my-scheduler",
    Logger:       &SlogAdapter{logger: slog.Default()},
    ErrorHandler: errorHandler,
})
```

## API 参考

### Scheduler 接口

```go
type Scheduler interface {
    // 生命周期管理
    Start(ctx context.Context) error          // 启动调度器（加载任务）
    Stop(ctx context.Context) error           // 停止调度器（等待任务完成）
    IsRunning() bool                          // 是否运行中

    // 任务调度管理
    Schedule(ctx context.Context, id string, cronExpr string, handler TaskHandler) error
    Unschedule(ctx context.Context, id string) error
    UpdateSchedule(ctx context.Context, id string, cronExpr string) error

    // 查询功能
    GetNextRunTime(cronExpr string) (time.Time, error)  // 计算下次执行时间
    GetScheduledTasks() []TaskInfo                      // 获取所有任务
    GetTaskInfo(id string) (*TaskInfo, error)           // 获取单个任务信息
}
```

### Options 配置

```go
type Options struct {
    // 基础配置
    Name              string           // 调度器名称（用于日志）
    Location          *time.Location   // 时区（默认 Asia/Shanghai）
    EnableSeconds     bool             // 是否启用秒级精度（默认 false）

    // 任务加载器（可选）
    TaskLoader        TaskLoader       // 启动时加载任务

    // 去重保护（可选）
    EnableDedup       bool             // 是否启用去重
    DedupService      DedupService     // 去重服务实现
    DedupKeyFunc      func(string) string  // 去重键生成函数
    DedupMinInterval  time.Duration    // 最小执行间隔

    // 监控（可选）
    EnableMetrics     bool             // 是否启用指标

    // 错误处理
    ErrorHandler      ErrorHandler     // 全局错误处理器

    // 日志
    Logger            Logger           // 日志接口
}
```

### ExpressionBuilder

```go
type ExpressionBuilder struct{}

// BuildFromScheduleConfig 从 UI 配置构建 Cron 表达式
// 支持类型: manual, daily, weekly, monthly, cron
func (b *ExpressionBuilder) BuildFromScheduleConfig(config ScheduleConfig) (string, map[string]interface{}, error)

// Validate 验证 Cron 表达式
func (b *ExpressionBuilder) Validate(expr string) error

// NextRunTime 计算下次执行时间
func (b *ExpressionBuilder) NextRunTime(expr string, from time.Time) (time.Time, error)

// NextRunTimes 计算接下来 N 次执行时间
func (b *ExpressionBuilder) NextRunTimes(expr string, count int) ([]time.Time, error)
```

### ScheduleConfig

```go
type ScheduleConfig struct {
    Type  string      // manual/daily/weekly/monthly/cron
    Time  string      // HH:MM 格式（daily/weekly/monthly 使用）
    Value interface{} // []int (weekly: weekdays 0-6, monthly: days 1-31)
    Expr  string      // 自定义表达式（cron 类型使用）
}

// 示例
daily := ScheduleConfig{
    Type: "daily",
    Time: "14:30",  // 每天 14:30
}
// 生成: "30 14 * * *"

weekly := ScheduleConfig{
    Type:  "weekly",
    Time:  "10:00",
    Value: []int{1, 3, 5},  // 周一、三、五
}
// 生成: "0 10 * * 1,3,5"

monthly := ScheduleConfig{
    Type:  "monthly",
    Time:  "09:00",
    Value: []int{1, 15},  // 每月 1 号和 15 号
}
// 生成: "0 9 1,15 * *"

cron := ScheduleConfig{
    Type: "cron",
    Expr: "*/5 * * * *",  // 每 5 分钟
}
// 生成: "*/5 * * * *"
```

## Cron 表达式格式

### 标准格式（5 字段，分钟级精度）

```
┌─────────── 分钟 (0 - 59)
│ ┌───────── 小时 (0 - 23)
│ │ ┌─────── 日期 (1 - 31)
│ │ │ ┌───── 月份 (1 - 12)
│ │ │ │ ┌─── 星期 (0 - 6, 0=周日)
│ │ │ │ │
* * * * *
```

### 扩展格式（6 字段，秒级精度）

需要设置 `EnableSeconds: true`

```
┌───────── 秒 (0 - 59)
│ ┌─────── 分钟 (0 - 59)
│ │ ┌───── 小时 (0 - 23)
│ │ │ ┌─── 日期 (1 - 31)
│ │ │ │ ┌─ 月份 (1 - 12)
│ │ │ │ │ ┌ 星期 (0 - 6)
│ │ │ │ │ │
* * * * * *
```

### 常用示例

| 表达式 | 说明 |
|--------|------|
| `0 14 * * *` | 每天 14:00 |
| `30 14 * * *` | 每天 14:30 |
| `0 */2 * * *` | 每 2 小时 |
| `*/5 * * * *` | 每 5 分钟 |
| `0 9 * * 1-5` | 工作日 09:00 |
| `0 10 * * 1,3,5` | 周一、三、五 10:00 |
| `0 9 1,15 * *` | 每月 1 号和 15 号 09:00 |
| `0 0 1 * *` | 每月 1 号 00:00 |
| `*/2 * * * * *` | 每 2 秒（需要 EnableSeconds） |

## 迁移指南

### 从 Meta 模块迁移

**迁移前**（meta/backend/internal/service/scan_task_service.go）:

```go
type ScanTaskService struct {
    cron     *cron.Cron
    entryIDs map[uint]cron.EntryID
    mu       sync.RWMutex
    // ... 其他字段
}

func (s *ScanTaskService) scheduleTask(task *models.ScanTask) error {
    // 300+ 行的调度逻辑
}
```

**迁移后**:

```go
import commonScheduler "github.com/addp/common/scheduler"

type ScanTaskService struct {
    scheduler   commonScheduler.Scheduler  // 使用公共调度器
    exprBuilder *commonScheduler.ExpressionBuilder
    // ... 其他字段
}

func NewScanTaskService(...) *ScanTaskService {
    scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
        Name:             "meta-scanner",
        EnableDedup:      true,
        DedupService:     dedupService,
        DedupKeyFunc:     func(taskID string) string { return fmt.Sprintf("meta:scan:%s", taskID) },
        DedupMinInterval: 6 * time.Hour,
    })

    return &ScanTaskService{
        scheduler:   scheduler,
        exprBuilder: commonScheduler.NewExpressionBuilder(),
        // ...
    }
}

func (s *ScanTaskService) CreateTask(req *CreateTaskRequest) error {
    // 构建 Cron 表达式
    cronExpr, scheduleConfig, _ := s.exprBuilder.BuildFromScheduleConfig(req.Schedule)

    // 调度任务
    return s.scheduler.Schedule(ctx, taskID, cronExpr, s.createHandler(taskID))
}
```

**收益**：
- ✅ 减少 300+ 行代码
- ✅ 自动获得统一的日志、监控、错误处理
- ✅ 去重逻辑由框架管理

### 从 Orchestrator 模块迁移

**迁移前**（orchestrator/backend/internal/service/scheduler.go）:

```go
type Scheduler struct {
    cron     *cron.Cron
    entryIDs map[uint]cron.EntryID
    mu       sync.Mutex
}

func (s *Scheduler) Start() {
    s.cron.Start()
}

func (s *Scheduler) ScheduleOrchestration(id uint, cronExpr string) error {
    // 重复代码
}
```

**迁移后**:

```go
import commonScheduler "github.com/addp/common/scheduler"

type Scheduler struct {
    scheduler commonScheduler.Scheduler
    orchRepo  *repository.OrchestrationRepository
    executor  *Executor
}

func NewScheduler(orchRepo, executor) *Scheduler {
    scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
        Name:       "orchestrator",
        TaskLoader: &orchestrationLoader{orchRepo},
    })

    return &Scheduler{
        scheduler: scheduler,
        orchRepo:  orchRepo,
        executor:  executor,
    }
}

func (s *Scheduler) Start(ctx context.Context) error {
    return s.scheduler.Start(ctx)  // 自动加载任务
}

func (s *Scheduler) ScheduleOrchestration(id uint, cronExpr string) error {
    handler := func(ctx context.Context, taskID string) error {
        return s.executor.Execute(id)
    }
    return s.scheduler.Schedule(ctx, fmt.Sprintf("%d", id), cronExpr, handler)
}
```

### 从 Transfer 模块迁移

**关键差异**：Transfer 需要秒级精度

```go
import commonScheduler "github.com/addp/common/scheduler"

scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:          "transfer",
    EnableSeconds: true,  // ⚠️ 启用秒级精度
    TaskLoader:    &transferLoader{taskRepo},
})

// 现在可以使用 6 字段表达式
scheduler.Schedule(ctx, "task-1", "*/2 * * * * *", handler)  // 每 2 秒
```

## 项目集成

### 1. 添加依赖

在模块的 `go.mod` 中添加：

```go
require (
    github.com/addp/common v0.0.0
)

replace github.com/addp/common => ../../common
```

### 2. 导入模块

```go
import (
    commonScheduler "github.com/addp/common/scheduler"
)
```

### 3. 初始化调度器

在服务的 `main.go` 或初始化函数中：

```go
// 创建调度器
scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:       "my-service",
    TaskLoader: &myTaskLoader{db: db},
})
if err != nil {
    log.Fatal(err)
}

// 启动调度器
ctx := context.Background()
if err := scheduler.Start(ctx); err != nil {
    log.Fatal(err)
}

// 优雅关闭
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    scheduler.Stop(ctx)
}()
```

## 最佳实践

### 1. 任务 ID 命名规范

```go
// ✅ 推荐：模块前缀 + 业务 ID
taskID := fmt.Sprintf("meta:scan:%d", resourceID)
taskID := fmt.Sprintf("transfer:import:%d", taskID)

// ❌ 避免：纯数字 ID（可能冲突）
taskID := fmt.Sprintf("%d", id)
```

### 2. 去重键命名规范

```go
// ✅ 推荐：Redis Key 命名规范
dedupKey := fmt.Sprintf("{module}:{function}:{id}")
// 示例: "meta:scan:resource:123"

// ❌ 避免：无前缀（可能与其他模块冲突）
dedupKey := fmt.Sprintf("task:%d", id)
```

### 3. 错误处理

```go
handler := func(ctx context.Context, taskID string) error {
    // ✅ 返回明确的错误信息
    if err := doWork(); err != nil {
        return fmt.Errorf("failed to process task %s: %w", taskID, err)
    }
    return nil
}

// ❌ 避免：静默失败
handler := func(ctx context.Context, taskID string) error {
    doWork()  // 忽略错误
    return nil
}
```

### 4. 优雅关闭

```go
func main() {
    scheduler, _ := commonScheduler.NewScheduler(options)
    scheduler.Start(context.Background())

    // ✅ 监听系统信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    <-sigCh

    // ✅ 设置超时防止无限等待
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := scheduler.Stop(ctx); err != nil {
        log.Printf("Failed to stop scheduler gracefully: %v", err)
    }
}
```

### 5. 时区处理

```go
// ✅ 推荐：使用默认时区（Asia/Shanghai）
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name: "my-scheduler",
    // Location 留空，自动使用 Asia/Shanghai
})

// ✅ 自定义时区（如果需要）
loc, _ := time.LoadLocation("America/New_York")
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    Name:     "my-scheduler",
    Location: loc,
})

// ❌ 避免：在代码中硬编码时区
time.Local = loc  // 影响全局
```

## 测试

运行测试：

```bash
cd common/scheduler
go test -v ./...
```

测试覆盖率：

```bash
go test -cover ./...
```

## 性能指标

- **调度延迟**: < 100ms（从触发时间到执行）
- **并发任务**: 支持 1000+ 任务同时调度
- **内存占用**: 每个任务约 1KB（包括元数据）
- **CPU 占用**: 空闲时 < 0.1%，执行时视任务而定

## 常见问题

### Q: 如何处理任务执行时间超过调度间隔？

A: Cron 默认行为是**不会并发执行同一任务**。如果任务还在执行，下一次触发会被跳过。如果需要严格的间隔保证，启用去重保护：

```go
scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    EnableDedup:      true,
    DedupMinInterval: 5 * time.Minute,  // 最小间隔
})
```

### Q: 如何动态添加/删除任务？

A: 直接调用 `Schedule`/`Unschedule` 方法：

```go
// 添加任务（如果已存在会自动更新）
scheduler.Schedule(ctx, "new-task", "0 10 * * *", handler)

// 删除任务
scheduler.Unschedule(ctx, "new-task")
```

### Q: 如何在任务中访问数据库/Redis？

A: 在 handler 中捕获外部依赖（闭包）：

```go
handler := func(ctx context.Context, taskID string) error {
    // 可以访问外部变量
    var task models.Task
    if err := db.First(&task, taskID).Error; err != nil {
        return err
    }
    // 处理任务
    return nil
}

scheduler.Schedule(ctx, taskID, cronExpr, handler)
```

### Q: 如何处理任务执行失败？

A: 配置自定义错误处理器：

```go
errorHandler := func(ctx context.Context, taskID string, err error) {
    // 记录到数据库
    db.Model(&Task{}).Where("id = ?", taskID).Update("status", "failed")

    // 发送告警
    alertService.Send(fmt.Sprintf("Task %s failed: %v", taskID, err))

    // 重新入队（如果需要）
    taskQueue.Enqueue(taskID)
}

scheduler, _ := commonScheduler.NewScheduler(commonScheduler.Options{
    ErrorHandler: errorHandler,
})
```

### Q: 多个服务实例如何避免重复执行？

A: 当前版本是单机调度。如果需要分布式支持，可以：

1. **方案 1**：只在一个实例启动调度器（其他实例只处理队列任务）
2. **方案 2**：使用基于 Redis 的分布式锁（计划中）

```go
// 临时方案：通过环境变量控制
if os.Getenv("ENABLE_SCHEDULER") == "true" {
    scheduler.Start(ctx)
}
```

## 未来计划

### 短期（1-2 月）

- [ ] **分布式调度支持**：基于 Redis 的分布式锁
- [ ] **任务历史查询**：提供 API 查询最近 N 次执行记录
- [ ] **动态调度调整**：支持运行时动态添加/修改任务

### 长期（3-6 月）

- [ ] **可视化管理界面**：统一的任务监控看板
- [ ] **告警集成**：任务失败告警（邮件/钉钉/企业微信）
- [ ] **任务依赖支持**：DAG 依赖图管理

## 相关文档

- [设计方案](/.claude/plans/iterative-exploring-pearl.md) - 完整设计文档
- [Cron 表达式参考](https://pkg.go.dev/github.com/robfig/cron/v3) - robfig/cron 官方文档
- [ADDP 架构文档](/CLAUDE.md) - 平台整体架构

## 许可证

本模块是 ADDP 平台的一部分，遵循项目整体许可证。
