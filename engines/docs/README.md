# ADDP 工作流运行时文档入口

本目录维护 `addp.workflow/v1` 的 [OpenAPI](workflow-engine-api-v1.yaml)、请求响应示例和元数据校验器。协议实现、Provider 边界与注册要求统一见[工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)。

## 规范导航

| 阅读目的 | 文档 |
| --- | --- |
| Provider 与 HTTP Runtime 架构、当前引擎及目录结构 | [总体架构](../../docs/spec/workflow/总体架构.md) |
| 五个标准接口、请求响应、执行状态与错误码 | [HTTP 协议](../../docs/spec/workflow/HTTP协议.md) |
| 算子必填字段、效果、参数、输出端口与分类 | [算子元数据与实现](../../docs/spec/workflow/算子元数据与实现.md) |
| 公开资源参数、执行期适配与授权 | [Develop 资源与执行](../../docs/spec/workflow/Develop资源与执行.md) |
| Runtime 注册、能力探测、扩展步骤与接入验证 | [Runtime 注册与扩展](../../docs/spec/workflow/Runtime注册与扩展.md) |
| 坐标转换、构面、几何检查修复、吸附与面叠检查 | [矢量算子契约](../../docs/spec/workflow/矢量算子契约.md) |
| 栅格资源、成果、网格、统计、分类与资源准入 | [栅格算子契约](../../docs/spec/workflow/栅格算子契约.md) |
| 验证层级、环境边界与标准门禁 | [测试与验收规范](../../docs/spec/addp测试与验收规范.md) |

## 示例索引

| 文件 | 内容 |
| --- | --- |
| [operator-list-response.json](examples/operator-list-response.json) | Math `add` 的元数据结构示例，仅保留一个算子 |
| [workflow-execute-request.json](examples/workflow-execute-request.json) | Math 两节点 DAG 请求，包含后端注入的租户与执行授权上下文 |
| [execution-status-response.json](examples/execution-status-response.json) | 执行成功态响应结构，结果为 GeoJSON |

示例用于说明各接口字段；实际算子目录和执行结果以对应 Runtime 返回为准。

## 元数据校验

从仓库根目录校验算子列表示例：

```bash
python engines/docs/workflow_operator_contract.py \
  engines/docs/examples/operator-list-response.json \
  --engine-type math_workflow
```

校验实际实例导出的 `GET /api/operators` 响应时，替换输入文件，并将 `--engine-type` 设为该实例注册的真实类型；响应中所有算子均须与其一致。校验器也支持直接输入算子数组，供测试复用的函数见 [workflow_operator_contract.py](workflow_operator_contract.py)。

## 参考实现

- [Math Workflow](../math-workflow/README.md)：协议与公共 Python 执行核心的参考实现；开发启动后不自动注册，需要在 System 中手动注册。
- [GeoPython Workflow](../geopython-workflow/)：空间与数据处理运行时。
- [Spark Workflow](../spark-workflow/README.md)：分布式计算运行时。

启动、依赖准备与 owner 验证入口见[引擎目录说明](../README.md)；新增 Runtime 按[扩展步骤](../../docs/spec/workflow/Runtime注册与扩展.md#扩展新引擎指南)接入。
