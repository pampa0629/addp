# ADDP 工作流计算引擎 API 规范文档

## 📋 概述

本目录包含 ADDP 平台工作流计算引擎的 `addp.workflow/v1` HTTP 运行时规范。所有工作流引擎（GeoPython Workflow、Spark Workflow、Math Workflow 等）都必须先通过 `EnginePlugin` 和 `engine.capabilities/v1` 纳入 System 统一引擎体系，再由 Common Engine 的 `WorkflowRuntimeProvider` 消费这些运行时接口。

## 📂 文件结构

```
engines/docs/
├── workflow-engine-api-v1.yaml          # addp.workflow/v1 OpenAPI 3.0 规范
├── examples/                            # API 响应示例
│   ├── operator-list-response.json      # 算子列表响应示例
│   ├── workflow-execute-request.json    # 工作流执行请求示例
│   └── execution-status-response.json   # 执行状态响应示例
```

## 🔑 核心接口

所有工作流引擎必须实现以下5个标准HTTP端点：

### 1. **健康检查** - `GET /health`
检查引擎服务是否正常运行
- 返回：服务状态、版本、运行时长、算子数量

### 2. **算子列表** - `GET /api/operators`
获取引擎提供的所有算子元数据
- **关键**：Develop模块通过此接口动态发现算子
- 返回：算子列表（包含名称、分类、参数、输出端口等）

### 3. **工作流执行** - `POST /api/workflow`
执行包含多个算子的DAG工作流
- **关键**：所有工作流引擎必须实现
- 请求：工作流定义（非空 `tasks` 数组）+ 输入数据；需要外部运行时资源的引擎使用顶层 `engine_id`。Spark Workflow 执行时该字段必填，必须指向实际 Spark 通用引擎资源。
- 返回：执行ID、最终结果、所有中间结果

### 4. **单算子 direct 调用** - `POST /api/operators/{name}/invoke`
受控调用单个声明了 `execution_modes: ["direct"]` 的算子
- 请求：算子参数；需要外部运行时资源的引擎使用顶层 `engine_id`
- 返回：调用结果；不创建 ADDP 任务，不进入 Orchestrator 或 Monitor

### 5. **执行状态查询** - `GET /api/executions/{execution_id}`
查询异步执行的状态
- 返回：执行状态、进度、结果（如已完成）

## 📐 标准错误码

所有引擎必须使用统一的错误码：

| 错误码 | HTTP状态码 | 说明 |
|--------|-----------|------|
| `OPERATOR_NOT_FOUND` | 404 | 算子不存在 |
| `DIRECT_NOT_SUPPORTED` | 403 | 算子未声明 `execution_modes: ["direct"]`，不允许单算子调用 |
| `INVALID_PARAMS` | 400 | 参数错误 |
| `EXECUTION_FAILED` | 500 | 执行失败 |
| `WORKFLOW_INVALID` | 400 | 工作流定义无效 |
| `INTERNAL_ERROR` | 500 | 内部错误 |

**错误响应格式**:
```json
{
  "status": "failed",
  "error": "描述性错误信息",
  "error_code": "OPERATOR_NOT_FOUND",
  "details": "详细错误信息（可选）"
}
```

## 🚀 使用指南

### 查看规范

1. **在线查看**: 将 `workflow-engine-api-v1.yaml` 上传到 [Swagger Editor](https://editor.swagger.io/)
2. **生成文档**: 使用工具生成交互式API文档
3. **生成客户端**: 使用 OpenAPI Generator 生成任意语言的客户端SDK

### 实现引擎

参考示例引擎实现：
- **Math Workflow**: `/Users/pampa/code/addp/engines/math-workflow/` - 最简参考实现（约350行核心代码）
- **GeoPython Workflow**: `/Users/pampa/code/addp/engines/python-workflow/` - 完整空间计算引擎
- **Spark Workflow**: `/Users/pampa/code/addp/engines/spark-workflow/` - 分布式计算引擎

Math Workflow 是参考实现，ADDP 开发环境可随 `-all` / `-develop` 自动启动服务，但启动时不自动注册到 System；需要使用时在 System 引擎管理中按扩展引擎手动注册。

### 验证符合性

确保你的引擎实现：
- [ ] 实现所有5个必需端点
- [ ] 算子元数据包含所有必填字段（id、name、display_name、engine_type、category、category_path、description、execution_modes、parameters、output_ports）
- [ ] 算子元数据可通过 `workflow_operator_contract.py` 的 `assert_operator_metadata_contract()` 校验
- [ ] 工作流定义只接受 `workflow_def.tasks` 非空数组
- [ ] 错误响应包含标准错误码
- [ ] 健康检查返回完整信息（status、service、version、operators_count）
- [ ] 支持工作流DAG执行（拓扑排序、参数引用）

也可以直接校验保存下来的 `/api/operators` 响应：

```bash
python engines/docs/workflow_operator_contract.py engines/docs/examples/operator-list-response.json
```

校验某个具体引擎实例导出的算子列表时，可额外传入 `--engine-type geopython_workflow` 等参数，要求所有算子都属于同一扩展引擎类型。

## 🔗 相关文档

- **详细规范**: `/Users/pampa/code/addp/docs/spec/addp工作流计算引擎接口规范.md` - Markdown格式的完整开发指南
- **示例引擎**: `engines/math-workflow/` - 最简参考实现

## 📝 版本历史

- **v1.0.0** (2026-01-09): 与 Common Engine `WorkflowRuntimeProvider` 收敛
  - 统一 5 个 `addp.workflow/v1` 运行时端点
  - 工作流定义统一为 `workflow_def.tasks` 非空数组
  - 算子元数据通过 `GET /api/operators` 动态发现
