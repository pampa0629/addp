# ADDP 计算引擎开发指南

本目录集中管理所有ADDP计算引擎服务。

## 目录结构

```
engines/
├── python_workflow/    # Python Workflow 空间计算引擎(Python)
│   ├── Dockerfile
│   ├── requirements.txt
│   ├── api_server.py
│   ├── operators.py
│   └── workflow_engine.py
├── spark-workflow/       # Spark 工作流空间计算引擎(未来)
└── README.md           # 本文件
```

## 引擎分类

### API引擎 (api.*)
通过HTTP API调用的内置模块,资源类型命名规范: `api.{module}`

**已有引擎**:
- `api.meta` - 元数据管理模块(元数据扫描等算子)
- `api.transfer` - 数据传输模块(数据传输等算子)
- `api.manager` - 数据管理模块(瓦片生成等算子)
- `api.python-workflow` - Python Workflow 引擎(空间计算算子)

**规划中**:
- `api.spark_workflow` - Spark Workflow 引擎(大数据空间计算)

### 标准库引擎
通过标准协议(JDBC/S3)访问的外部数据源:
- `postgresql`, `mysql`, `doris` - 数据库
- `spark` - Apache Spark
- `minio`, `s3`, `oss` - 对象存储

## 统一算子API规范

所有API引擎必须提供以下标准HTTP API:

### 1. 算子发现API
```
GET /api/{module}/operators
```

返回格式:
```json
{
  "status": "success",
  "operators": [
    {
      "id": "buffer",
      "name": "buffer",
      "display_name": "缓冲区分析",
      "type": "spatial",
      "category": "空间分析",
      "description": "对几何对象生成缓冲区",
      "module": "python_workflow",
      "parameters": [
        {
          "name": "distance",
          "type": "float",
          "required": true,
          "description": "缓冲区距离",
          "min": 0
        }
      ],
      "inputs": ["geometry"],
      "outputs": ["geometry"]
    }
  ],
  "count": 1
}
```

### 2. 算子执行API
```
POST /api/{module}/operators/:name/execute
```

请求格式:
```json
{
  "params": {
    "engine_id": 123,
    "depth": "deep"
  },
  "execute_now": true,
  "task_name": "扫描任务1"
}
```

响应格式:
```json
{
  "status": "success",
  "task_id": "task-123",
  "task_status": "running",
  "message": "任务已创建",
  "created_at": "2025-01-15T10:00:00Z",
  "result": {}
}
```

## 开发模式声明

引擎通过`dev_modes`字段声明支持的开发方式:

| DevMode | 说明 | 使用场景 |
|---------|------|---------|
| `sql` | SQL编辑器(文本编码) | PostgreSQL, Apache Spark等数据库引擎 |
| `workflow` | 工作流画布(算子拖拽成DAG) | GeoPandas, Spark 工作流等计算引擎 |
| `form` | 简单表单配置 | 单一算子快速执行(如手动触发扫描) |
| `script` | 脚本编辑器(未来) | Python/JavaScript等脚本语言 |

## 引擎自动注册

引擎启动时应自动注册到System资源中心:

**注册端点**: `POST http://system-backend:8080/internal/registry/capabilities`

**注册数据格式**:
```json
{
  "unique_identifier": "api.python-workflow",
  "name": "python_workflow_engine",
  "display_name": "Python Workflow 空间计算引擎",
  "resource_type": "api.python-workflow",
  "is_builtin": true,
  "capabilities": {
    "compute": [{
      "type": "spatial",
      "dev_modes": ["workflow"],
      "supported_formats": ["geojson", "wkt", "shapely"],
      "features": ["dag", "memory_efficient", "batch"]
    }]
  },
  "task_api_config": {
    "base_url": "http://python-workflow-engine:8099",
    "endpoints": {
      "list_operators": {
        "method": "GET",
        "path": "/api/spatial/operators"
      },
      "execute": {
        "method": "POST",
        "path": "/api/spatial/operators/{{.OperatorName}}/execute"
      }
    }
  },
  "health_check_config": {
    "endpoint": "/health",
    "timeout": 5,
    "interval": 60
  }
}
```

## 新增引擎checklist

创建新引擎时,请遵循以下步骤:

- [ ] 在`engines/`目录下创建引擎目录
- [ ] 实现统一算子API(`/api/operators`, `/api/operators/:op/execute`)
- [ ] 声明引擎能力(compute/storage)和支持的dev_modes
- [ ] 实现健康检查端点(`/health`)
- [ ] 配置自动注册逻辑
- [ ] 添加到docker-compose.yml
- [ ] 更新启动脚本(scripts/dev/start.sh)
- [ ] 编写README说明引擎功能和使用方法

## 参考实现

- **Python Workflow Engine**: [engines/python_workflow/](./python_workflow/)
  - Python FastAPI 实现
  - 21个空间算子
  - 自动注册到System

## 相关文档

- [ADDP架构设计方案](/Users/pampa/.claude/plans/buzzing-bubbling-porcupine.md)
- [Common模块文档](../common/README.md)
- [System模块文档](../system/CLAUDE.md)
