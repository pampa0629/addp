# data_profile_fields 表结构说明

> 状态：当前实现说明。`manager.data_profile_fields` 保存当前剖析结果中每个字段的版本化指标、分布和描述性观察。

## 一、表定位

字段结果从 `manager.data_profiles` 拆分，便于按顺序加载和后续建立字段级投影。完整指标仍以 `data.profile/v2` 的 `FieldProfile` JSON 为事实结构，关系字段只保存查询和校验所需的稳定投影。

## 二、核心字段

| 字段 | 类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 字段结果 ID |
| `profile_id` | bigint | 所属 `manager.data_profiles.id`，删除父结果时级联删除 |
| `position` | integer | 字段在剖析结果中的稳定顺序 |
| `name` | varchar(512) | 字段名 |
| `type` | varchar(64) | ADDP 统一字段类型 |
| `status` | varchar(32) | `computed` 或 `unsupported`，真实零值不使用状态表达 |
| `profile` | jsonb | `FieldProfile` 完整内容，包括完整性、基数、类型指标、分布、Top N 和观察 |
| `created_at` / `updated_at` | timestamptz | 生命周期字段 |

## 三、写入语义

1. 字段结果只能随父级成功结果在同一事务内整体替换。
2. execution 失败或超时时不得提前删除旧字段结果。
3. `profile` 的解释由父级 `schema_version` 决定，不建立无版本私有 JSON 契约。
4. Top N 和分布可能包含业务值，不得写入 Meta attributes、全文索引、日志或错误详情。

## 四、索引

| 索引 | 字段 | 用途 |
| --- | --- | --- |
| `idx_data_profile_fields_profile_name` | `profile_id, name` | 同一结果内字段唯一 |
| `idx_data_profile_fields_profile_position` | `profile_id, position` | 按原字段顺序加载 |

## 五、相关文档

- [数据剖析规范](../数据剖析规范.md)
- [data_profiles表](data_profiles表.md)
- [数据库架构](../数据库架构.md)
