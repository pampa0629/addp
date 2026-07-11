# GeoPython Workflow 运行说明

本文档说明当前 `geopython_workflow` 扩展引擎的运行入口。历史空间专用实现已废弃，不再作为兼容路径保留。

## 引擎定位

GeoPython Workflow 是 ADDP 的工作流运行时扩展引擎：

- 插件类型：`geopython_workflow`
- 引擎来源：`extension`
- Provider：`WorkflowRuntimeProvider`
- 运行时协议：`addp.workflow/v1`
- 默认端口：`8099`
- 代码目录：`engines/python-workflow`

业务模块不直接拼接 GeoPython Workflow 的 HTTP URL。Develop 等调用方通过 Common Engine 获取 `WorkflowRuntimeProvider`，由 provider 按统一协议调用运行时接口。

## 标准接口

GeoPython Workflow 必须实现以下入口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 健康检查和依赖状态 |
| `GET` | `/api/operators` | 动态发现算子元数据 |
| `POST` | `/api/workflow` | 执行非空 `tasks` 数组工作流 |
| `POST` | `/api/operators/{name}/invoke` | 单算子 direct 调用 |
| `GET` | `/api/executions/{execution_id}` | 查询执行状态 |

工作流定义只支持标准 `workflow_def.tasks` 数组格式，且 `tasks` 必须非空。

## 本地启动

推荐使用项目统一开发脚本：

```bash
bash scripts/dev/start.sh -python-workflow
```

服务启动后可验证：

```bash
curl http://localhost:8099/health
curl http://localhost:8099/api/operators
```

日志位置：

```bash
tail -f logs/python-workflow-engine.log
tail -f logs/python-workflow-engine-stderr.log
```

## 容器运行

根目录 `docker-compose.yml` 中的服务名是 `python-workflow-engine`，构建上下文为 `./engines/python-workflow`。

```bash
docker compose up -d python-workflow-engine
docker compose logs -f python-workflow-engine
```

生产和本地容器脚本如需构建镜像，应使用当前服务与目录：

```bash
bash scripts/build/build-images.sh --services python-workflow-engine
```

## System 自注册

引擎启动时向 System 自注册身份和连接信息。作为内置运行时，GeoPython Workflow 不在自注册 payload 中提交 `capabilities`；System 按 `geopython_workflow` 插件的 `Capabilities()` 生成 `engine.capabilities/v1` 落库能力声明。

```json
{
  "engine_type": "geopython_workflow",
  "connection_info": {
    "protocol": "http",
    "port": 8099
  },
  "is_builtin": true
}
```

算子列表、参数、分类、输入输出端口等动态能力由 `GET /api/operators` 实时提供。
