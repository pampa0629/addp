# ADDP 配置：Manager配置

[返回配置规范](../addp配置介绍.md)

## Manager 导入中转对象存储

Manager 的 Shapefile 导入不是由 Manager 自己解析写库。Manager 只负责接收 ZIP 包、上传到中转对象存储，然后创建并触发 Transfer `sync`：

```json
{
  "source": {
    "locator": "addp-infra://minio/manager/tenant_7/import/20260622/upload-uuid/roads.shp?type=object",
    "data_type": "table",
    "representation": "encoded",
    "format": "shapefile"
  }
}
```

Manager 上传导入文件时写入 ADDP infra MinIO 的 `manager` bucket，并通过 `addp-infra://minio/manager/...` locator 调用 Transfer。infra MinIO 不进入 System engines，上传暂存对象也不进入 Meta。
对于 Shapefile 多文件上传，source locator 指向 primary `.shp`；同 basename 的 `.dbf`、`.shx`、`.prj`、`.cpg` 等组件随同写入同一暂存目录，由 Transfer 按格式能力读取相关 refs。

相关环境变量：

```bash
MINIO_API_PORT=19000
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
```

宿主机开发时由 `MINIO_API_PORT` 组成 `localhost:<port>`；容器部署由 Compose 显式注入
`MINIO_ENDPOINT=minio:9000`。模块不得读取 `MINIO_SYSTEM_*`、`MINIO_ACCESS_KEY` 或
`MINIO_SECRET_KEY` 等平行别名。

规则：

1. Manager 负责上传暂存和后续 cleanup，Transfer 只按 locator 读取。
2. 暂存路径使用 `tenant_{tenant_id}/import/{yyyymmdd}/{upload_uuid}/...`。
3. 当前导入入口支持一个 Shapefile ZIP 包，或浏览器同时选择同一套 Shapefile 的多个组件文件；`.shp/.dbf/.shx` 必须同 basename，不能混入多套 Shapefile。

## Manager 栅格 mosaic 生成配置

栅格 mosaic 生成是离线任务，Manager 通过 GeoPython Workflow 的 `build_raster_mosaic` 算子执行 GDAL 处理。该调用不同于在线瓦片渲染，允许更长的执行预算：

```bash
# 容器版 GeoPython Workflow 的 gunicorn worker 超时。默认 7200 秒。
GEOPYTHON_WORKFLOW_GUNICORN_TIMEOUT=7200
```

栅格 mosaic 生成超时已迁移为 Manager 配置页面中“快显策略”Tab 的平台普通运行配置，作为后续 execution 的默认预算；GeoPython 容器的 `GEOPYTHON_WORKFLOW_GUNICORN_TIMEOUT` 仍由部署环境维护。

leaf COG 生成并发不通过全局环境变量固定，而是在任务 `config.cog` 中归一化为明确值。默认策略按运行机器 CPU 预算计算：逻辑 CPU 小于 8 时 `leaf_concurrency=1`，8 到 15 时为 `2`，16 到 31 时为 `4`，32 及以上时为 `6`，上限 `8`；单个 leaf COG 的 GDAL `num_threads` 默认按 `逻辑 CPU / (leaf_concurrency * 2)` 计算并限制在 `1` 到 `4`。当前 18 逻辑 CPU 开发机默认得到 `leaf_concurrency=4`、`num_threads=2`。`cog.leaf_retry_attempts` 默认 `2`，上限 `5`，用于单个 leaf COG 生成或校验的瞬时失败重试。`detached` 模式重跑时会复用目标数据集中已经存在且内容级 COG 校验通过的 leaf，因此超时或中断后的恢复通过再次执行同一任务继续完成未生成部分，而不是从头覆盖全部 leaf。

## Manager 有界执行监督器配置

Manager 的快显派生、业务派生、PPTX 静态预览、向量化和数据剖析 execution 统一由 Manager Backend 内嵌有界执行监督器领取，执行 API 只创建 `pending` execution。以下配置属于 Backend 运行容量与租约治理，不进入任务定义，也不在配置页面中维护：

```bash
MANAGER_EXECUTION_CONCURRENCY=4
MANAGER_EXECUTION_LEASE_DURATION=2m
MANAGER_EXECUTION_HEARTBEAT_INTERVAL=30s
MANAGER_EXECUTION_CLAIM_INTERVAL=1s
MANAGER_EXECUTION_IDLE_MAX_INTERVAL=5s
```

并发数和四个时间项必须为正，心跳周期必须短于租约，空闲最大间隔不得小于基础领取间隔。协调器在全部 Manager bounded task type 中统一领取，空队列时指数退避；本进程新建 pending execution 会立即唤醒领取，唤醒丢失或其他 Manager 实例入队时最迟在空闲最大间隔后发现。Manager Backend 进程失联且租约过期时，该 execution、任务摘要和构建中的结果会原子收敛为失败；当前所有 task type 都不会自动重放，用户显式重试时创建新的 execution。

## Manager 向量化配置

Manager 向量化的模型提供能力统一来自 Inference Runtime。Manager 只保存 `manager.embedding` Scenario Binding、向量检索策略、成本限制和 execution 快照，不保存模型服务 Base URL、上游模型标识或 API Key。

| 字段 | 归属与规则 |
| --- | --- |
| Model Profile / Deployment 绑定 | Manager Scenario Binding；平台默认可被 Tenant 显式绑定覆盖。 |
| 请求 timeout | Manager 平台普通运行配置。 |
| 向量检索最大距离 | Manager 平台检索默认策略；当前不开放 Tenant 覆盖。 |
| 最大文件大小 | Manager 平台成本与资源限制；当前不开放 Tenant 覆盖。 |
| 批处理并发数 | Manager 平台运行资源策略。 |
| 向量维度 | 模型输出与 pgvector 列结构的只读约束，不是普通可编辑配置。不同维度切换必须走数据库迁移和全量重建。 |
| Provider credential | Inference 专用加密凭据；Manager API、表和 execution 均不得持有。 |

Manager 配置定义提供业务策略默认值；显式平台值和 Tenant 覆盖保存在 Manager 自己的持久化配置中，不从环境变量回退。Manager 必须先解析当前场景绑定，再调用 `addp.inference/v1`；未配置、无授权或能力不匹配时返回明确错误，不能直连其他模型服务。

任务定义和 execution 中的 `model_profile_id/profile_version/deployment_id/dimension/binding_version` 是实际使用配置的快照。场景绑定变化只影响后续创建或显式更新的任务及后续 execution，不能改写历史执行事实。

`MANAGER_EMBEDDING_SERVICE_API_KEY` 不再存在。Provider credential 只在 Inference owner 中保存和解密。

## 在线底图供应商

Manager 的在线底图供应商作为 Manager-owned 强类型资源管理。平台可提供默认资源，Tenant 可以在允许的范围内覆盖；高德、天地图浏览器 Key 属于客户端可见凭据，不将其当作服务器 Secret，但管理 API 仍只返回配置状态，运行时端点按授权返回浏览器确需的公开 Key。

## Manager 快显与动态 MVT 配置

Manager 快显中的动态 MVT 是交互式预览能力，单瓦片查询必须受响应时间预算保护。以下配置同时影响能力接口返回的 `realtime_tile.timeout_budget_ms`、动态 MVT 查询的实际超时控制，以及超时响应头中的诊断信息。

Manager 配置页面的“快显策略”Tab 维护 FlatGeobuf 行数上限、动态 MVT 超时预算、重试等待时间和栅格 mosaic 生成超时；这些字段不再通过环境变量注入。

MVT 进程内 LRU 的容量和条目 TTL 是按部署机器内存预算确定的进程级实现参数，使用代码默认值（8192 条目、5 分钟），不作为平台或租户业务配置，也不从未登记的环境变量读取。
