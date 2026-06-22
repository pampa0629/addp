# Transfer 任务语义与同步模式设计草案

更新时间：2026-06-09

状态说明：本文中的 `task_type=import` 语义已被正式任务体系规范取代。当前实现和正式任务体系按 clean break 收敛为 Transfer 唯一任务类型 `task_type=sync`；稳定规则见 `docs/spec/addp任务体系规范.md` 和 `transfer/docs/transfer-基本概念及配置说明.md`，本文仅作为增量、CDC、水位、实时同步等后续专题的早期讨论记录保留。

本文用于讨论 Transfer 后续任务语义，不以当前实现为约束。ADDP 当前处于积极开发阶段，本文默认 clean break：概念确认后可以推翻旧字段、旧任务类型和旧 UI 入口。

说明：本文只讨论 Transfer 的任务语义、同步模式、写入策略和执行边界；资源选择统一走 locator / ResourceTreePicker，不在本专题中重新定义选择入口。

当前任务体系阶段 1 只在 TaskProvider 和 `common.task_executions` 层声明 `task_type=sync`。本文下方讨论的 `intent=import/export/sync` 仅是早期业务标签草案，不进入当前 TaskProvider `task_type`，也不要求当前接口并行支持导入、导出等任务类型。

## 一、价值定位

Transfer 的核心价值不是提供“导入 / 导出 / 同步”三个并列按钮，而是提供稳定的数据搬运与同步执行能力：

```text
source
  -> load strategy
  -> transform
  -> write strategy
  -> target
```

导入、导出更像 Manager 的用户操作入口：

- 用户在 Manager 中选择数据项。
- 用户选择导入或导出目标。
- Manager 负责交互、预览、字段映射、格式选择和任务创建。
- Transfer 在后台执行对应的数据搬运任务。

因此，导入 / 导出不应成为 Transfer 执行主路径的核心分支。它们可以作为 Manager 的业务意图、任务来源或 UI 标签保留，但 Transfer planner 不应按 `import` / `export` 写分支。

“同步”也不应继续作为和导入 / 导出并列的单一概念。同步应该拆到更明确的执行语义中：

- 全量。
- 增量。
- 实时增量。

## 二、核心维度

建议把 Transfer 任务语义拆成两个主维度：

| 维度 | 取值 | 说明 |
|---|---|---|
| 装载策略 | `full` / `incremental` | 读全量数据，还是只读变化数据。 |
| 触发形态 | `manual` / `scheduled` / `realtime` | 立即执行一次、按计划反复执行、持续监听变化。 |

写入策略仍是第三个必要维度，但它不参与任务类型命名：

| 写入策略 | 说明 |
|---|---|
| `overwrite` | 清理 / 重建目标后写入。常用于全量。 |
| `append` | 追加写入。只在源增量天然不会重复且目标允许重复或追加日志时使用。 |
| `merge` / `upsert` | 按主键或唯一键合并。常用于增量同步。 |
| `delete_aware_merge` | 能处理源端删除事件的合并。通常需要 CDC 或显式 tombstone。 |

触发形态和装载策略组合后，产品上可以收敛为四类合法任务：

| 任务形态 | 装载策略 | 触发形态 | 说明 |
|---|---|---|---|
| 手动全量 | `full` | `manual` | 立即执行一次全量搬运。常见于导入 / 导出。 |
| 定时全量 | `full` | `scheduled` | 按计划反复跑全量。适合小表、快照文件、周期性覆盖。 |
| 手动增量 | `incremental` | `manual` | 立即补跑一次增量。适合失败补偿、手动追数、从指定水位推进。 |
| 定时增量 | `incremental` | `scheduled` | 按计划读取变化数据并推进水位。 |
| 实时增量 | `incremental` | `realtime` | 持续消费 CDC / Kafka / stream change event。 |

`realtime + full` 不作为合法任务形态。持续运行却反复读取全量，通常没有稳定业务价值，也会制造不可控成本。

## 三、关于手动增量

手动增量是有价值的，但它不是“手动等于定时”的简单替代。

手动增量适合这些场景：

- 定时增量失败后，人工触发补跑。
- 上游修复了一段历史数据，需要从某个水位重新追一次。
- 首次全量完成后，不想等下一次调度，立即跑一次增量。
- 测试增量规则是否正确。

手动增量必须具备明确水位语义，否则它会退化成“看起来增量、实际上不知道读什么”。

手动增量至少需要回答：

| 问题 | 说明 |
|---|---|
| 起点水位 | 从任务保存的上次水位开始，还是用户指定水位。 |
| 终点水位 | 跑到当前可见上界，还是用户指定上界。 |
| 水位提交 | 成功后是否推进任务水位。 |
| 重跑语义 | 如果指定历史水位重跑，是否允许覆盖已提交水位。 |

建议第一版支持两种手动增量：

| 类型 | 语义 |
|---|---|
| `manual_incremental_resume` | 从任务当前保存的水位跑到当前上界，成功后推进水位。 |
| `manual_incremental_replay` | 用户指定起止水位补跑，默认不推进主水位，除非显式确认。 |

如果第一阶段想更简单，可以先只做 `manual_incremental_resume`，把 replay 留到后续。

## 四、全量与增量的边界

全量任务不需要记住上一次读到哪里。它每次都重新定义完整目标结果。

全量通常搭配：

- `write_mode=overwrite`。
- 写后 Meta scan。
- restartable retry。

增量任务必须有增量状态。增量状态不是 checkpoint，也不是 append。

增量状态至少包括：

| 状态 | 说明 |
|---|---|
| `watermark` | 基于字段的水位，例如 `updated_at`、自增 ID。 |
| `offset` | 基于日志或 stream 的位点，例如 Kafka offset、CDC LSN。 |
| `snapshot_version` | 基于版本快照的增量边界。 |
| `processed_manifest` | 基于文件清单、etag、mtime 的已处理集合。 |

增量读取还需要定义变化识别方式：

| 类型 | 示例 | 说明 |
|---|---|---|
| watermark | `updated_at > last_watermark` | 第一阶段最适合 batch incremental。 |
| monotonic id | `id > last_id` | 只适合纯新增，不适合更新。 |
| CDC offset | binlog / WAL / LSN | 实时增量和可靠删除同步的基础。 |
| file fingerprint | etag / mtime / hash | 文件型源的增量扫描基础。 |

## 五、实时增量

实时增量是 stream / CDC 能力，不应该和当前 batch Transfer 混在一个实现里硬凑。

实时增量至少需要：

- change event 抽象。
- partition / offset / LSN。
- checkpoint 提交语义。
- 目标写入幂等策略。
- insert / update / delete 事件语义。
- backfill 或 snapshot + incremental 衔接方式。

因此实时增量可以晚于 batch incremental。当前概念上只保留位置，不急于实现。

## 六、导入 / 导出与任务形态的关系

导入 / 导出是业务入口，不是 Transfer 执行类型。

| Manager 入口 | Transfer 任务形态 |
|---|---|
| 导入文件到表 | 手动全量，通常 `overwrite`。 |
| 导出表为文件 | 手动全量，通常 `overwrite`。 |
| 周期性导出快照 | 定时全量。 |
| 周期性同步外部表变化 | 定时增量。 |
| CDC 同步数据库变化 | 实时增量。 |

这样设计后，Manager 可以继续提供用户熟悉的“导入 / 导出”入口；Transfer 则保持少量、稳定、可组合的执行语义。

## 七、只给 Transfer 使用的 System engine

引擎统一由 System 管理，不恢复 Transfer 私有引擎路线。

但不应为了尚未出现的需求添加复杂 visibility / role / preview / query / ttl 组合。当前真实需求只有一个：

```text
这个 System engine 是否只允许 Transfer 使用。
```

因此第一版只需要一个最小策略：

```json
{
  "usage_scope": "transfer_only"
}
```

或等价表达：

```json
{
  "allowed_modules": ["transfer"]
}
```

二者取其一即可，不要同时存在。

建议优先选择 `allowed_modules`，因为它表达更直接，也方便未来自然扩展到 `manager`、`meta`、`develop`，但第一版 UI 和校验只暴露“仅 Transfer 可用”一个开关。

规则：

- 引擎仍由 System 统一登记、加密、审计和测试连接。
- `allowed_modules=["transfer"]` 的引擎不进入 Manager 普通资源树、不被 Meta 自动扫描、不出现在 Develop 查询入口。
- Transfer 可以在任务中引用该 engine。
- 如果未来需要临时连接、一次性凭据或过期时间，再基于真实需求扩展，不提前设计。

## 八、建议的任务语义草案

后续可以把 Transfer 任务配置逐步收敛成：

```json
{
  "intent": "sync",
  "load": {
    "mode": "incremental",
    "incremental": {
      "type": "watermark",
      "field": "updated_at",
      "start": "last_committed",
      "end": "now",
      "commit_watermark": true
    }
  },
  "trigger": {
    "type": "scheduled",
    "cron": "0 */10 * * * *"
  },
  "source": {},
  "target": {
    "policy": {
      "write_mode": "merge",
      "keys": ["id"]
    }
  }
}
```

说明：

- `intent` 可以保留为 `import` / `export` / `sync`，只做业务标签。
- `load.mode` 决定全量 / 增量。
- `trigger.type` 决定手动 / 定时 / 实时。
- `target.policy.write_mode` 决定覆盖 / 追加 / 合并。
- 具体 endpoint 结构仍沿用 source / target。

## 九、需要继续讨论的问题

1. `manual_incremental_replay` 是否第一版就需要，还是先只做 `manual_incremental_resume`。
2. 增量水位状态保存在哪里：任务表、统一 execution metadata，还是单独的 transfer state 表。
3. 第一版 batch incremental 只支持 watermark，还是也支持 monotonic id。
4. `allowed_modules` 是否作为 System engine 的通用字段，还是先作为 connection metadata 中的受控策略。
5. Manager 导入 / 导出创建 Transfer 任务时，是否保留 `intent=import/export` 作为审计和 UI 回显标签。

## 十、任务体系接入遗留点

任务体系阶段 1 只要求 Transfer 在接口层符合 TaskProvider 规范，并且对外只声明一个任务类型：

```text
provider=transfer
task_type=sync
```

Transfer 内部任务语义、同步模式、取消、重试、进度和日志仍作为本专题后续处理，不在当前任务体系主线中展开。

### 10.1 执行资源标识收敛记录

已确认 Develop 和 Transfer 的执行资源 HTTP 入口统一使用 `execution_id`。Transfer 不再在 `/executions` 路径空间中保留按内部自增 ID 访问的私有执行管理入口；取消、重试、进度和日志入口也统一使用 `execution_id`：

```text
GET  /executions/{execution_id}
POST /executions/{execution_id}/cancel
POST /executions/{execution_id}/retry
GET  /executions/{execution_id}/progress
GET  /executions/{execution_id}/logs
```

这里的统一只是执行资源标识收敛，不等于声明 TaskProvider 标准取消能力。`supports_cancel=false` 时，Orchestrator 和 Monitor 仍不得展示 Transfer 标准取消入口；取消接口是否纳入跨模块标准能力，仍必须先确认 worker 可中断、资源可清理、状态可一致落库。

后续专题至少需要收敛以下问题：

1. 取消、重试、进度和日志入口已统一到 `execution_id`，但其能力语义仍属于 Transfer 专题：需要继续明确哪些可作为跨模块标准能力，哪些只是 Transfer 私有执行视图。
2. 当前不声明标准取消能力，即 `supports_cancel=false`，因此 Orchestrator 和 Monitor 不应展示 Transfer 标准取消入口。只有在明确 worker 可中断、资源可清理、状态可一致落库后，才能开放 `POST /executions/{execution_id}/cancel`。
3. `retry` 当前是 restartable retry，不是 checkpoint resumable。后续如果要支持从中断点续跑，需要先定义 checkpoint commit / resume marker / 写入幂等语义，不能只复用现有 retry 按钮。
4. `progress` 和 `logs` 当前是 Transfer 私有执行视图。后续要决定哪些信息沉淀到 `common.task_executions.metadata/error_details`，哪些保留在 Transfer 私有观测表或日志中。
5. `task_type=sync` 是 Transfer 阶段 1 的唯一对外任务类型。导入 / 导出是 Manager 等调用方入口语义，不进入 Transfer task_type。
6. Manager 入口创建 Transfer 任务时，Manager 负责用户交互、字段映射和入口语义；Transfer 负责执行计划与搬运。后续需要明确 Manager 到 Transfer 的创建契约，避免把 Manager UI 概念反向写成 Transfer planner 分支。
7. Transfer 写后触发 Meta scan 属于执行后派生动作。后续需要明确它在父子 execution 中如何表达：是 Transfer execution 的 metadata，还是单独的 Meta 子 execution，并与 Orchestrator 的 `parent_execution_id` 语义保持一致。

## 十一、任务体系后续边界

以下内容作为 Transfer 专题继续推进，不进入任务体系主干文档：

1. 是否扩展 `task_type` 必须先修订正式任务体系规范。阶段 1 对外只声明 `task_type=sync`，不得并行保留 `import`、`export`、`transfer` 等旧任务类型。
2. 导入 / 导出应优先作为 Manager 或其他业务模块的入口 intent / UI 标签 / 审计标签，而不是 Transfer planner 主路径的执行类型分支。
3. 全量、增量、实时增量的任务定义结构需要在本文中统一，包括 `load.mode`、`trigger.type`、水位状态、写入策略和 retry 语义。
4. 当前 TaskProvider capabilities 对外只声明 `restartable_retry`，不得声明 checkpoint resumable。checkpoint 仍是观测和诊断信息，真正断点续跑需要 source seek、target 幂等提交和 provider marker 消费同时成立。
5. Transfer 队列、进度、日志和私有执行管理 API 后续如进入 Monitor，必须先明确哪些属于统一 execution metadata，哪些仍属于 Transfer 私有观测视图。
