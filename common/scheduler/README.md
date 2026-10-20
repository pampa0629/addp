# Common Scheduler

`common/scheduler` 提供 Cron 表达式构建、校验、时间计算和轻量进程内触发。业务代码通过该包使用 `robfig/cron`，不各自注册 `cron.New` / `AddFunc`。

## 平台职责与适用范围

用户可配置的持久任务采用 owner 的 DB-driven due task claim 主路径：任务定义保存 `schedule`、`enabled`、`next_run_at`，owner scheduler 领取到期任务并创建 execution，执行方按统一协议领取工作。具体闭环、多实例行锁及固定系统维护任务例外见[编排与调度规范](../../docs/spec/tasks/编排与调度.md#调度规范)。

本包的内存注册表、`TaskLoader` 或回调去重不能替代上述持久任务模型；执行状态、lease 和恢复归[统一执行契约](../../docs/spec/tasks/执行记录与租约.md)。编排调度和子任务自身调度的关系也由正式规范维护，不在此保存另一份模块迁移方案。

## 源码导航

| 入口 | 内容 |
| --- | --- |
| [expression.go](expression.go) | `ScheduleConfig`、表达式构建器、语法校验和时间计算。 |
| [handler.go](handler.go) | `Scheduler`、`TaskHandler`、`TaskLoader`、`DedupService`、`Logger` 等接口和值对象。 |
| [options.go](options.go) | 调度器选项、默认值和配置校验。 |
| [scheduler.go](scheduler.go) | 进程内生命周期、任务注册、回调和内存查询实现。 |

## 表达式构建与校验

| 能力 | 行为 |
| --- | --- |
| `NewExpressionBuilder()` | 使用 5 字段解析器，分钟级。 |
| `NewExpressionBuilderWithSeconds()` | 使用 6 字段解析器，增加秒字段；不自动给构建结果补秒。 |
| `BuildFromScheduleConfig(config)` | 返回 `(cronExpr, UI配置map, error)`，构建与校验分开。 |
| `Validate(expr)` | 用所选解析器校验完整表达式语法，失败返回 error。 |
| `NextRunTime(expr, from)` | 从显式传入的时间计算下一次触发，返回 `(time.Time, error)`。 |
| `NextRunTimes(expr, count)` | 从调用时刻计算接下来 count 次触发；调用方传正数预算。 |

`ScheduleConfig` 的字段为 `Type`、`Time`、`Value`、`Expr`：

- `manual` 返回空表达式；`cron` 返回 Expr，包括用于清空计划的空值。这两种返回结果仍需按 owner 的启用/停用语义处理。
- `daily`、`weekly`、`monthly` 使用 `HH:MM`；空 Time 按当前实现取零点，非法格式或范围返回错误。
- weekly 的 Value 使用星期整数列表（0–6），monthly 使用日期整数列表（1–31）。未提供或空列表时分别使用周日和每月 1 日。
- 三种表单模式当前都生成 5 字段表达式，即使用秒级构建器也不自动转换。必须按选定精度校验实际返回值，不能宣称已支持表单到秒级计划的自动转换。
- 返回的 map 只用于 UI 回显，不是另一份任务调度事实；持久计划字段仍为 `schedule`。

### 校验并计算下次时间

```go
package example

import (
	"time"

	commonScheduler "github.com/addp/common/scheduler"
)

func NextPlannedRun(from time.Time) (time.Time, error) {
	builder := commonScheduler.NewExpressionBuilder()
	expr, _, err := builder.BuildFromScheduleConfig(commonScheduler.ScheduleConfig{
		Type:  "weekly",
		Time:  "10:00",
		Value: []int{1, 3, 5},
	})
	if err != nil {
		return time.Time{}, err
	}
	if err := builder.Validate(expr); err != nil {
		return time.Time{}, err
	}
	return builder.NextRunTime(expr, from)
}
```

示例表达式为 `0 10 * * 1,3,5`。空计划是否允许、计划精度、时区及何时回写 `next_run_at` 由 owner 按正式契约决定；前端控件的粗校验不替代这里的语法校验。

## 进程内 Scheduler

以下接口用于符合正式规范范围的进程内触发；完整签名见 `handler.go`。

| 接口 | 当前行为 |
| --- | --- |
| `NewScheduler(options)` | 应用默认值、校验选项并创建内存调度器；尚未启动。 |
| `Start(ctx)` | 可选调用 TaskLoader，注册其中启用的任务后启动 Cron。Loader 返回错误会中止；单项注册失败记录日志后继续，不等于所有任务注册成功。 |
| `Stop(ctx)` | 停止后续触发并等待已有回调，等待受 ctx 控制；超时不等于回调已退出或业务执行已取消。 |
| `IsRunning()` | 返回本实例维护的运行状态，不是跨实例健康或持久任务状态。 |
| `Schedule(ctx, id, expr, handler)` | 注册或替换同 ID 的内存任务。先移除旧注册再添加新表达式，失败时不保证保留旧计划。 |
| `Unschedule(ctx, id)` | 删除内存注册；不存在时返回错误。 |
| `UpdateSchedule(ctx, id, expr)` | 当前未实现更新，返回错误；不能作为已交付的动态更新能力使用。 |
| `GetNextRunTime(expr)` | 按实例选定的字段精度解析，并从当前时间计算；不是数据库 `next_run_at` 的读写入口。 |
| `GetScheduledTasks()` / `GetTaskInfo(id)` | 查询本实例内存 TaskInfo；不等于统一 execution 历史或审计。 |

TaskHandler 的签名是 `func(context.Context, string) error`。注册时传入的 context 会被回调捕获，调用方必须管理其生命周期；Stop 的超时 context 不会自动取消该回调 context。

## Options 与边界

| 选项 | 默认值或责任 |
| --- | --- |
| `Name` | 默认为 `scheduler`，用于日志与默认去重键。 |
| `Location` | 未传时加载 Asia/Shanghai；加载失败按当前实现使用 UTC，不修改进程全局 `time.Local`。 |
| `EnableSeconds` | 默认 false；开启后进程内 Cron 和实例时间查询使用 6 字段精度。 |
| `TaskLoader` | 可选，由调用方实现 `LoadTasks(ctx)`；包自身不连接数据库、不保存任务定义。 |
| `EnableDedup` / `DedupService` | 默认不启用；启用时必须提供服务实现。它是回调去重辅助，不提供数据库 claim 或跨实例原子领取保证。 |
| `DedupKeyFunc` / `DedupMinInterval` | 默认键为 `scheduler:<Name>:<taskID>`。当前运行前检查受 MinInterval 开关影响，标记固定写入 2 小时 TTL、回调结束清除；不能解释为“完成后强制等待最小间隔”。 |
| `Logger` / `ErrorHandler` | 可注入；默认错误处理只记录日志，不自动重试或创建 execution。 |
| `EnableMetrics` | 当前只增加执行耗时等日志，Prometheus 记录尚未实现。 |

默认封装没有配置跳过重叠执行的 Cron wrapper，不能承诺任务执行超过间隔时自动跳过下一次。DedupService 的检查、标记与清理是分离调用，标记/清理错误当前未传播，不能据此宣称分布式互斥或 exactly-once。

辅助回调应返回错误并使用带模块语义的任务 ID；共享库不代替 owner 决定业务重试、授权、任务终态或告警。不要把自定义 ErrorHandler 中直接重放业务、修改执行状态的示例作为平台契约。

## 验证与文档入口

本包现有确定性测试位于[scheduler_test.go](scheduler_test.go)与[expression_test.go](expression_test.go)。实现变更按[测试与验收规范](../../docs/spec/addp测试与验收规范.md)选择根 `make test-module MODULE=common GO_ONLY=1` 或受影响门禁；共享依赖按规范扩散验证。仅 Go 验证不计为完整模块门禁，也不能替代真实 owner 调度与多实例运行验收。

前端接入使用[ScheduleConfig / ScheduleDisplay](../../common-frontend/basic/docs/ScheduleConfig.md)。本说明不保留旧模块迁移脚本、未经测试的性能数字或未确认的分布式技术路线。

## 许可证

本模块是 ADDP 平台的一部分，遵循项目整体许可证。
