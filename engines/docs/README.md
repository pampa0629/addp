# ADDP 工作流计算引擎 API 规范文档

## 📋 概述

本目录包含ADDP平台工作流计算引擎的标准API规范文档。所有工作流引擎（Python Workflow、Spark Workflow、Math Workflow等）都必须实现这些标准接口。

## 📂 文件结构

```
engines/docs/
├── workflow-engine-api-v1.yaml          # OpenAPI 3.0 规范主文件
├── examples/                            # API 响应示例
│   ├── operator-list-response.json      # 算子列表响应示例
│   ├── workflow-execute-request.json    # 工作流执行请求示例
│   └── execution-status-response.json   # 执行状态响应示例
└── schemas/                             # Schema 定义（预留，可选）
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

### 3. **工作流执行** - `POST /api/spatial/workflow`
执行包含多个算子的DAG工作流
- **关键**：所有工作流引擎必须实现
- 请求：工作流定义（tasks数组）+ 输入数据
- 返回：执行ID、最终结果、所有中间结果

### 4. **单算子执行** - `POST /api/spatial/operators/{name}/execute`
快速执行单个算子（用于测试）
- 请求：算子参数
- 返回：执行结果

### 5. **执行状态查询** - `GET /api/spatial/executions/{execution_id}`
查询异步执行的状态
- 返回：执行状态、进度、结果（如已完成）

## 📐 标准错误码

所有引擎必须使用统一的错误码：

| 错误码 | HTTP状态码 | 说明 |
|--------|-----------|------|
| `OPERATOR_NOT_FOUND` | 404 | 算子不存在 |
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
- **Python Workflow**: `/Users/pampa/code/addp/engines/python-workflow/` - 完整空间计算引擎
- **Spark Workflow**: `/Users/pampa/code/addp/engines/spark-workflow/` - 分布式计算引擎

### 验证符合性

确保你的引擎实现：
- [ ] 实现所有5个必需端点
- [ ] 算子元数据包含所有必填字段（name、display_name、category、parameters、output_ports）
- [ ] 错误响应包含标准错误码
- [ ] 健康检查返回完整信息（status、service、version、operators_count）
- [ ] 支持工作流DAG执行（拓扑排序、参数引用）

## 🔗 相关文档

- **详细规范**: `addp工作流计算引擎接口规范.md` - Markdown格式的完整开发指南
- **示例引擎**: `engines/math-workflow/` - 最简参考实现
- **系统架构**: `/docs/ARCHITECTURE.md` - ADDP平台整体架构说明

## 📝 版本历史

- **v1.0.0** (2025-12-31): 初始版本
  - 定义5个核心端点
  - 定义算子元数据标准格式
  - 定义工作流执行接口
  - 定义标准错误码

## 📧 联系方式

如有问题或建议，请联系ADDP开发团队。
