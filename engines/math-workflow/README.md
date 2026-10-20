# Math Workflow Engine

Math Workflow 是 `addp.workflow/v1` 的参考实现，提供基础数学算子，用于学习公共 DAG 执行核心、异步状态和标准 HTTP 协议。协议与扩展要求见[工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)。

## 开发启动

从仓库根目录使用标准生命周期入口：

```bash
bash scripts/dev/start.sh -math-workflow
```

需要重启时，由开发者在自己的终端执行 `./scripts/dev/restart.sh -math-workflow`。默认端口为 `8089`，实际地址以标准启动输出和 `.dev-state/ports.env` 为准。依赖准备、停止与日志入口见[引擎开发指南](../README.md)。

Math 启动后不自动注册到 System。需要在平台中使用时，在 System 引擎管理中选择“注册扩展引擎”，填写 `engine_type=math_workflow` 和实际连接地址，测试连接并保存。完整能力声明与注册规则见[注册契约](../../docs/spec/workflow/Runtime注册与扩展.md#引擎注册)。

## 算子

| 算子 | 参数 | 行为 |
| --- | --- | --- |
| `add` | `a, b` | 两数相加 |
| `subtract` | `a, b` | 两数相减 |
| `multiply` | `a, b` | 两数相乘 |
| `divide` | `a, b` | 两数相除；除数为零时失败 |
| `average` | 非空 `values` 数组 | 计算平均值 |

内置算子均只声明 `workflow` 模式；通过 `POST /api/operators/{name}/invoke` 调用会返回 HTTP `403` 和 `DIRECT_NOT_SUPPORTED`。实际参数、分类、执行效果和输出端口由 `GET /api/operators` 返回。

## 工作流与执行

[共享请求示例](../docs/examples/workflow-execute-request.json)计算 `(10 + 20) × 2 = 60`。请求中的 `runtime.tenant_id` 和 `runtime.execution_authorization` 由拥有执行入口的后端从可信上下文注入；字段来源与校验要求见 [HTTP 协议](../../docs/spec/workflow/HTTP协议.md#执行工作流)。

Math 的 `POST /api/workflow` 使用公共 `ExecutionRegistry` 异步受理，返回 HTTP `202`、`pending` 和运行时本地 `execution_id`。随后通过 `GET /api/executions/{execution_id}` 查询至终态；上述示例成功时，节点结果依次为 `30`、`60`。未知 ID 返回 HTTP `404` 和 `EXECUTION_NOT_FOUND`。响应字段与状态收敛规则见[执行状态协议](../../docs/spec/workflow/HTTP协议.md#查询执行状态)。

在 Develop 中使用时，先完成 Runtime 注册，再进入工作流编辑器，从“数学运算”或“统计分析”分类选择算子，配置参数与连线并执行。

## 实现导航

| 入口 | 职责 |
| --- | --- |
| [math_operators.py](operators/math_operators.py) | 五个算子、元数据及注册表 |
| [base.py](operators/base.py) | 参数、端口与算子元数据模型 |
| [api_server.py](api_server.py) | 五个标准 HTTP 接口及公共执行核心适配 |
| [公共 Workflow Runtime](../../common-python/addp_common/workflow_runtime/) | DAG 校验、拓扑排序、引用与结果端口解析、异步执行快照 |
| [tests/](tests/) | 算子、工作流与进程内 API 测试 |

新增算子、输出端口或引用处理按[算子实现规范](../../docs/spec/workflow/算子元数据与实现.md)和[Runtime 扩展步骤](../../docs/spec/workflow/Runtime注册与扩展.md#扩展新引擎指南)接入；多输出端口与引用解析继续复用公共核心。

## 镜像构建

[Dockerfile](Dockerfile)使用仓库根目录作为构建上下文，打包 Math 与 `common-python`。从仓库根目录使用统一镜像构建入口：

```bash
make build-images IMAGE_BUILD_ARGS="--services math-workflow-engine --verify --jobs 1"
```

独立部署的网络、端口与环境配置见本目录 [Compose 定义](docker-compose.yml)，其中 `addp-network` 为外部网络。构建参数见[镜像构建说明](../../scripts/build/README.md#build-imagessh)。

## 测试与验收边界

准备 Python 3.12 测试环境并执行 Math T1：

```bash
python3.12 -m venv engines/math-workflow/venv
engines/math-workflow/venv/bin/python -m pip install -r engines/math-workflow/requirements-dev.txt -e ./common-python
make test-math-workflow
```

该入口自动发现 `tests/` 下的 pytest 用例，覆盖算子、公共 DAG 执行和 Flask `test_client()`，不要求启动个人开发服务。默认解释器为 `engines/math-workflow/venv/bin/python`，可通过 `MATH_WORKFLOW_PYTHON` 指定已有独立测试环境。CI 使用 Python 3.12、[requirements-dev.txt](requirements-dev.txt) 和 editable Common Python，根 `make test`、Engines owner 门禁和 Platform CI 的 Math Job 复用该入口。

完整 owner 门禁使用 `make test-module MODULE=engines`。真实 System 登记、Develop 工作流及外部数据源的跨模块验收单独认定；测试分层见[测试与验收规范](../../docs/spec/addp测试与验收规范.md)。

本引擎遵循 ADDP 平台的许可证。
