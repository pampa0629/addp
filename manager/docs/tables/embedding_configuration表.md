# embedding_configuration 表结构说明

> 状态：当前实现说明。`manager.embedding_configuration` 只保存 Manager owner 的平台向量化业务策略。

## 一、表定位

该表是 `platform_only` 单例策略配置。模型选择由 `manager.inference_scenario_bindings` 表达，Provider、端点、模型部署和凭据归 Inference Runtime。

- System 的 `module_registry.configuration_management` 只登记覆盖平台策略与 Tenant 场景绑定的入口、scope、路由和 Permission。
- 向量维度固定为 `2560`，不同维度切换必须走数据库迁移和全量重建。
- 任务定义和 execution 保存实际使用的 Profile、Profile Version、Deployment 与维度快照，不引用可变当前值作为历史事实。

## 二、字段

| 字段名 | 类型 / 约束 | 说明 |
| --- | --- | --- |
| `id` | integer，主键，固定为 `1` | 平台配置单例 |
| `version` | bigint，非空 | 每次成功更新单调递增，用于乐观并发控制 |
| `max_distance` | double precision，非空 | 向量检索最大距离 |
| `max_file_size_mb` | integer，非空 | 单个文件向量化大小上限 |
| `batch_concurrency` | integer，非空 | 新批次并发数 |
| `updated_by` | integer，非空 | 最近修改的 Platform 用户 principal ID |
| `created_at` / `updated_at` | timestamp | 生命周期字段 |

## 三、更新规则

1. 首次保存只接受 `version=0`，落库版本为 `1`。
2. 后续更新必须提交当前版本；旧版本返回 `409 Conflict`，不能覆盖其他管理员刚保存的值。
3. 保存前由 Manager 校验距离、文件大小和并发范围；保存成功后原子替换运行时策略。
4. API Key、Provider endpoint、upstream model 和 Deployment 不得写入本表、API 响应、日志或审计差异。

## 四、权限与范围

- 读取：Platform Context + `manager.configuration.read`。
- 修改：Platform Context + `manager.configuration.update`。
- Tenant Context 不可读取或修改本表；Tenant 只通过 `manager.inference_scenario_bindings` 覆盖 Model Profile。

## 五、相关文档

- [ADDP 配置规范](../../../docs/spec/addp配置介绍.md)
- [向量化能力说明](../向量化能力说明.md)
- [数据库架构](../数据库架构.md)
