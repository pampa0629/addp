# embedding_configuration 表结构说明

> 状态：当前实现说明。`manager.embedding_configuration` 保存 Manager owner 的平台向量化普通运行配置。

## 一、表定位

该表是 `platform_only` 单例配置，回答 Manager 后续向量化请求应使用哪个服务、模型和运行策略。它不保存配置管理入口声明，也不保存向量服务 API Key。

- System 的 `module_registry.configuration_management` 只登记入口、scope、路由和 Permission。
- `MANAGER_EMBEDDING_SERVICE_API_KEY` 仍是部署 Secret，只能返回是否已配置。
- 向量维度固定为 `2560`，不同维度切换必须走数据库迁移和全量重建。
- 任务定义和 execution 保存实际使用配置及 `configuration_version` 快照，不引用可变当前值作为历史事实。

## 二、字段

| 字段名 | 类型 / 约束 | 说明 |
| --- | --- | --- |
| `id` | integer，主键，固定为 `1` | 平台配置单例 |
| `version` | bigint，非空 | 每次成功更新单调递增，用于乐观并发控制 |
| `base_url` | text，非空 | 向量服务 HTTP(S) 根地址 |
| `model` | varchar(255)，非空 | 当前启用的多模态向量模型标识 |
| `timeout_seconds` | integer，非空 | 单次请求超时 |
| `max_distance` | double precision，非空 | 向量检索最大距离 |
| `max_file_size_mb` | integer，非空 | 单个文件向量化大小上限 |
| `batch_concurrency` | integer，非空 | 新批次并发数 |
| `updated_by` | integer，非空 | 最近修改的 Platform 用户 principal ID |
| `created_at` / `updated_at` | timestamp | 生命周期字段 |

## 三、更新规则

1. 首次保存只接受 `version=0`，落库版本为 `1`。
2. 后续更新必须提交当前版本；旧版本返回 `409 Conflict`，不能覆盖其他管理员刚保存的值。
3. 保存前由 Manager 校验 URL、模型、超时、距离、文件大小和并发范围；保存成功后原子替换运行时 Provider。
4. API Key 不得写入本表、API 响应、日志、审计差异或任务快照。

## 四、权限与范围

- 读取：Platform Context + `manager.configuration.read`。
- 修改：Platform Context + `manager.configuration.update`。
- Tenant Context 不可见、不可读取、不可覆盖。

## 五、相关文档

- [ADDP 配置规范](../../../docs/spec/addp配置介绍.md)
- [向量化能力说明](../向量化能力说明.md)
- [数据库架构](../数据库架构.md)
