# data_profiles 表结构说明

> 状态：当前实现说明。`manager.data_profiles` 保存一个稳定 data item、剖析模式和冻结配置对应的当前成功结果。

## 一、表定位

`manager.data_profiles` 回答：

> 某个表格型 data item 在指定剖析配置下，最近一次成功结果是什么，该结果基于哪个源版本和 execution。

它不保存任务定义、失败结果或完整执行历史。`data_profiling` 首期是 ad-hoc execution，过程统一写入 `common.task_executions`。

## 二、核心字段

| 字段 | 类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 当前结果 ID |
| `tenant_id` | bigint | 租户隔离键 |
| `item_fingerprint` | varchar(64) | 稳定 data item 身份 |
| `item_id` | bigint nullable | 当前 Meta item 行引用，不建跨模块外键 |
| `engine_id` | bigint | 当前源引擎引用 |
| `locator` | text | 当前 ResourceLocator，用于回查和跳转 |
| `source_version` | varchar(64) | 结果所依据的源内容版本 |
| `dependency_snapshot` | jsonb | 执行时冻结的源依赖快照 |
| `profile_mode` | varchar(32) | 当前为 `sample`，契约预留 `full` |
| `profile_config_hash` | varchar(64) | 采样预算、选择上下文、规范化数据范围和算法版本的 hash |
| `data_scope` | jsonb | 本结果分析的数据范围：`all` 或规范化后的单层 `condition` |
| `schema_version` | varchar(64) | 公共结果契约版本，当前为 `data.profile/v2` |
| `sample_method` | varchar(64) | 全范围为 `systematic_pages_reservoir`，条件范围为 `filtered_bounded_reservoir` |
| `sample_size` / `rows_scanned` | bigint | 最终样本行数 / 实际扫描行数 |
| `row_count` / `row_count_exact` | bigint / boolean | Meta 提供的行数及精确语义 |
| `field_count` | integer | 字段数 |
| `truncated` / `partial` | boolean | 读取是否受预算截断 / 结果是否部分完成 |
| `observations` | jsonb | 表级汇总的描述性观察，不是质量结论 |
| `last_execution_id` | varchar(36) | 最近成功提交该结果的 execution |
| `profiled_at` | timestamptz | 指标计算时间 |

## 三、身份与更新

唯一身份为：

```text
tenant_id + item_fingerprint + profile_mode + profile_config_hash
```

新 execution 成功时，在一个事务中更新本表并替换 `manager.data_profile_fields`。失败或超时不修改本表，因此上一份成功结果继续可读。

`data_scope` 必须在服务端校验、规范化后参与 `profile_config_hash` 计算。全范围与不同条件范围因此分别保存当前成功结果，互不覆盖。

## 四、索引

| 索引 | 字段 | 用途 |
| --- | --- | --- |
| `idx_data_profiles_current` | `tenant_id, item_fingerprint, profile_mode, profile_config_hash` | 当前结果唯一身份 |
| `idx_data_profiles_tenant_item` | `tenant_id, item_fingerprint, item_id` | 源 item 回查 |
| `idx_data_profiles_last_execution_id` | `last_execution_id` | execution 关联 |

## 五、相关文档

- [数据剖析规范](../数据剖析规范.md)
- [data_profile_fields表](data_profile_fields表.md)
- [数据库架构](../数据库架构.md)
