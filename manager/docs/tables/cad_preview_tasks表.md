# cad_preview_tasks 表结构说明

> 状态：当前实现说明。`manager.cad_preview_tasks` 表达二维 DWG / DXF 栅格瓦片预览生成任务定义，TaskProvider `task_type=cad_preview_generation`。

## 一、表定位

该表保存“以后按什么配置为哪个 CAD item 生成或刷新 Manager 受管预览 artifact”。它不替代 `manager.cad_previews` 的结果状态、`manager.preview_state` 的用户视角状态、`common.task_executions` 的执行历史或源 CAD data item。

## 二、核心字段

| 字段名 | 类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | Manager 内部任务定义 ID |
| `tenant_id` | bigint | 租户 ID |
| `name` / `description` | varchar / text | 任务名称与说明 |
| `enabled` | boolean | 是否启用 |
| `schedule` / `next_run_at` | varchar / timestamp | 当前不支持自身调度，必须为空 |
| `last_run_at` | timestamp | 最近运行时间 |
| `last_execution_id` / `last_execution_status` | varchar | 最近统一 execution 摘要 |
| `config` | jsonb | CAD 预览任务配置 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 生命周期字段 | 创建与软删除信息 |

同租户同 `config.source.item_fingerprint` 只保留一个当前任务定义。

启动请求在任务行锁事务内检查同任务是否已有 `pending` 或 `running` execution；存在时返回 HTTP 409。启动成功时原子创建 `pending` execution 并推进任务摘要，后台执行器领取后进入 `running`。CAD 结果、execution 终态和任务摘要使用 `last_execution_id` fencing，并在同一 Infra PostgreSQL 事务提交。

## 三、config 语义

```json
{
  "source": {
    "item_locator": "addp://engine/26/path/cad/site.dwg?type=file&item_id=91",
    "source_engine_id": 26,
    "item_fingerprint": "sha256",
    "item_id": 91,
    "format": "dwg",
    "source_size_bytes": 1048576
  },
  "result": {
    "storage_ref": "Manager 内部对象存储引用"
  },
  "options": {
    "tile_size": 512,
    "max_zoom": 4
  }
}
```

源必须是 `data_type=cad + layout=single + format=dwg|dxf`。`storage_ref` 缺省时由服务端按 `tenant_<tenant>/cad-previews/<fingerprint>` 生成。`tile_size` 范围为 128–1024，`max_zoom` 数值范围为 0–8，但总输出不得超过 25,000 张，因此当前可执行上限为 7。

## 四、执行语义

执行器通过 `addp.workflow.access-plan/v1` direct 调用 `supermap_workflow cad.render_preview`。SuperMap 直接打开 DWG / DXF Datasource 并渲染 Dataset，不遍历 Geometry，不生成 WKB / GeoJSON，不让前端重画 entity。

标准入口：

```text
GET  /api/v1/manager/tasks?task_type=cad_preview_generation
GET  /api/v1/manager/tasks/cad_preview_generation/{id}
POST /api/v1/manager/tasks/cad_preview_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

当前 `supports_schedule=false`、`supports_cancel=false`。

`POST .../execute` 接受后返回 HTTP 202、`status=pending` 和新 `execution_id`；同任务已有活跃 execution 时返回 HTTP 409。

## 五、相关文档

- [CAD 后续路线](../../../docs/next/addp-CAD数据支持设计.md)
- [数据预览语义协议](../数据预览语义协议.md)
- [cad_previews 表结构说明](./cad_previews表.md)
- [数据库架构](../数据库架构.md)
