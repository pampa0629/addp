# ADDP 工作流计算引擎接口规范

**版本**: v1.0
**最后更新**: 2025-12-29
**适用范围**: ADDP 平台所有带 workflow 能力的计算引擎

---

## 📋 概述

本规范定义了 ADDP 平台中**工作流计算引擎**的标准算子接口，确保所有计算引擎（无论使用何种语言实现）都遵循统一的算子发现和执行契约，实现算子的拖拽式工作流编排。

### 核心概念区分

ADDP 平台中存在两类不同的能力提供者：

#### 1. **任务提供者**（Task Providers）

提供**任务定义和执行**的模块，不提供算子：

| 模块 | 引擎类型 | 提供的任务 | API 端点 | 开发模式 |
|------|---------|-----------|---------|---------|
| Meta | `api.meta` | 元数据扫描任务 | `GET /api/meta/tasks` | form |
| Transfer | `api.transfer` | 数据传输任务 | `GET /api/transfer/tasks` | form |
| Manager | `api.manager` | 瓦片缓存任务 | `GET /api/manager/tasks` | form |

**特点**：
- 提供**任务**（Tasks），不是算子
- 通过表单配置参数执行
- 主要用于 Orchestrator 工作流编排
- 不支持 `workflow` 开发模式

#### 2. **计算引擎**（Compute Engines）

提供**算子**（Operators）的引擎，支持 workflow 开发模式：

| 引擎 | 引擎类型 | 提供的算子 | API 端点 | 开发模式 | 当前状态 |
|------|---------|-----------|---------|---------|---------|
| Python Workflow | `api.python-workflow` | 21 个空间算子 | `GET /api/spatial/operators` | workflow | ✅ 已实现 |
| Spark Workflow 引擎 | `api.spark_workflow` | 分布式空间算子 | `GET /api/spark-workflow/operators` | workflow | 🔄 预留 |
| Stats Engine | `api.stats` | 统计分析算子 | `GET /api/stats/operators` | workflow | 🔄 预留 |
| ML Engine | `api.ml` | 机器学习算子 | `GET /api/ml/operators` | workflow | 🔄 预留 |

**特点**：
- 提供**算子**（Operators），可在工作流画布中拖拽组合
- 支持 `workflow` 开发模式
- 算子可以串联执行，形成数据处理管道
- 用于 Develop 模块的工作流编辑器

### 设计理念

1. **语言无关**: 引擎实现可以使用任意技术栈（Python、Go、Rust、Java 等）
2. **统一接口**: 所有计算引擎提供一致的算子 API（列表查询 + 执行接口）
3. **能力声明**: 引擎通过 `dev_modes: ["workflow"]` 声明支持工作流编排
4. **动态发现**: Develop 模块通过 API 动态获取引擎的算子列表
5. **模块解耦**: 新增引擎无需修改 Develop/Orchestrator 代码

---

## 🏗️ 架构模式

### 引擎注册与算子发现流程

```
┌─────────────────────────────────────────────────────────────────┐
│  System Backend (引擎注册中心)                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ PostgreSQL (system.engines 表)                            │ │
│  │                                                            │ │
│  │ 计算引擎注册示例:                                           │ │
│  │ {                                                          │ │
│  │   id: 5,                                                   │ │
│  │   name: "python_workflow_engine",                    │ │
│  │   display_name: "Python Workflow 空间计算引擎",        │ │
│  │   engine_type: "api.python-workflow",                 │ │
│  │   capabilities: {                                          │ │
│  │     compute: [{                                            │ │
│  │       type: "spatial",                                     │ │
│  │       dev_modes: ["workflow"],  // 关键：声明支持工作流   │ │
│  │       api_endpoints: {                                     │ │
│  │         operators: "/api/spatial/operators",    // 算子列表│ │
│  │         execute: "/api/spatial/operators/:name/execute"   │ │
│  │       }                                                    │ │
│  │     }]                                                     │ │
│  │   },                                                       │ │
│  │   connection_config: {                                     │ │
│  │     base_url: "http://python-workflow-engine:8090"        │ │
│  │   }                                                        │ │
│  │ }                                                          │ │
│  └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 1. Develop 模块查询支持 workflow 的引擎
                              │    GET /api/system/engines?dev_mode=workflow
                              │    → 返回 [python_workflow_engine, spark_workflow_engine, ...]
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Develop Backend (工作流编辑器)                                 │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ 1. 获取支持 workflow 的引擎列表                            │ │
│  │    GET /api/system/engines?capabilities.compute.dev_modes=workflow │ │
│  │    → [api.python-workflow, api.spark_workflow, ...]              │ │
│  │                                                            │ │
│  │ 2. 遍历引擎，获取算子列表                                  │ │
│  │    GET http://python-workflow-engine:8090/api/spatial/operators │ │
│  │    → 返回 21 个 Python Workflow 算子                      │ │
│  │                                                            │ │
│  │ 3. 聚合所有算子，渲染工作流画布                            │ │
│  │    算子面板: [buffer] [intersection] [union] ...         │ │
│  └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 用户拖拽算子，配置参数，连接数据流
                              │ POST http://python-workflow-engine:8090/api/spatial/operators/buffer/execute
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Python Workflow Engine (Python FastAPI)                       │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ 1. 接收算子执行请求                                        │ │
│  │ 2. 执行空间计算（内存 GeoDataFrame）                      │ │
│  │ 3. 返回结果或任务 ID                                       │ │
│  └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### 关键流程说明

1. **引擎注册阶段**
   - 计算引擎在 System 模块注册，声明 `dev_modes: ["workflow"]`
   - 指定算子 API 端点（`operators` 和 `execute`）

2. **算子发现阶段**
   - Develop 模块查询所有支持 `workflow` 的引擎
   - 调用每个引擎的 `operators` 端点，获取算子列表
   - 聚合所有算子，渲染到工作流画布的算子面板

3. **算子执行阶段**
   - 用户拖拽算子到画布，配置参数
   - Develop 调用引擎的 `execute` 端点执行算子
   - 引擎返回执行结果（同步或异步）

---

## 🔌 核心接口规范

### 1. 算子列表接口

**端点**: `GET /api/{module}/operators`

**功能**: 返回引擎提供的所有算子元数据

**请求示例**:
```bash
GET http://python-workflow-engine:8090/api/spatial/operators
Authorization: Bearer <token>
```

**响应格式**:
```json
{
  "status": "success",
  "operators": [
    {
      "id": "buffer",
      "name": "buffer",
      "display_name": "缓冲区分析",
      "type": "spatial",
      "category": "几何操作",
      "description": "对几何对象创建缓冲区",
      "module": "python_workflow",
      "parameters": [
        {
          "name": "distance",
          "type": "float",
          "required": true,
          "description": "缓冲区距离（单位：米）",
          "min": 0.0,
          "default": 100.0
        },
        {
          "name": "resolution",
          "type": "integer",
          "required": false,
          "description": "圆角分辨率",
          "min": 1,
          "max": 64,
          "default": 16
        }
      ],
      "inputs": ["geodataframe"],
      "output_ports": [
        {
          "name": "default",
          "type": "geodataframe",
          "description": "缓冲区结果",
          "is_default": true
        }
      ]
    },
    {
      "id": "intersection",
      "name": "intersection",
      "display_name": "相交分析",
      "type": "spatial",
      "category": "几何操作",
      "description": "计算两个几何对象的相交部分",
      "module": "python_workflow",
      "parameters": [],
      "inputs": ["geodataframe", "geodataframe"],
      "output_ports": [
        {
          "name": "default",
          "type": "geodataframe",
          "description": "相交结果",
          "is_default": true
        }
      ]
    }
  ],
  "count": 21
}
```

**字段说明**:

| 字段 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| `id` | string | ✅ | 算子唯一标识（用于 execute 端点） |
| `name` | string | ✅ | 算子名称（通常与 id 相同） |
| `display_name` | string | ✅ | 中文显示名（用于工作流画布 UI） |
| `type` | string | ✅ | 算子类型（spatial/stats/ml 等） |
| `category` | string | ✅ | 分类名称（用于算子面板分组） |
| `description` | string | ✅ | 功能描述 |
| `module` | string | ✅ | 所属引擎模块（python-workflow/spark_workflow 等） |
| `parameters` | array | ✅ | 参数定义列表 |
| `inputs` | array | ✅ | 输入端口类型列表 |
| `output_ports` | array | ✅ | 输出端口定义（支持多输出） |

**参数定义** (`ParameterMetadata`):

```json
{
  "name": "distance",               // 参数名
  "type": "float",                  // 类型: string/integer/float/boolean/array/object
  "required": true,                 // 是否必填
  "default": 100.0,                 // 默认值（可选）
  "description": "缓冲区距离",       // 参数说明
  "min": 0.0,                       // 最小值（数值类型可选）
  "max": 1000.0,                    // 最大值（数值类型可选）
  "enum": ["meters", "degrees"],    // 枚举值（可选，用于下拉选择）
  "pattern": "^[0-9]+$",            // 正则校验（字符串类型可选）
  "item_type": "string",            // 数组元素类型（array 类型时必填）
  "properties": {...},              // 对象属性定义（object 类型时必填）
  "depends_on": "use_custom"        // 依赖的参数名（动态显示）
}
```

**输出端口定义** (`OutputPortMetadata`):

```json
{
  "name": "default",                // 端口名称（单输出时为 "default"）
  "type": "geodataframe",           // 数据类型
  "description": "缓冲区结果",       // 端口语义说明
  "is_default": true                // 是否为默认端口（单输出时为 true）
}
```

**多输出端口示例**（高级用法）:

某些算子可能产生多个输出，例如"按面积过滤"算子：

```json
{
  "id": "filter_by_area",
  "name": "filter_by_area",
  "display_name": "按面积过滤",
  "output_ports": [
    {
      "name": "large",
      "type": "geodataframe",
      "description": "大面积要素（>1000平方米）",
      "is_default": false
    },
    {
      "name": "small",
      "type": "geodataframe",
      "description": "小面积要素（<=1000平方米）",
      "is_default": false
    }
  ]
}
```

在工作流画布中，用户可以分别连接两个输出端口到不同的下游算子。

---

### 2. 算子执行接口

**端点**: `POST /api/{module}/operators/:name/execute`

**功能**: 执行指定算子

**请求示例**:
```bash
POST http://python-workflow-engine:8090/api/spatial/operators/buffer/execute
Authorization: Bearer <token>
Content-Type: application/json

{
  "params": {
    "distance": 200.0,
    "resolution": 16
  },
  "execute_now": true,
  "task_name": "北京五环缓冲区"
}
```

**请求格式**:

```json
{
  "params": {
    "distance": 200.0,
    "resolution": 16
  },
  "execute_now": true,              // 是否立即执行（默认 true）
  "task_name": "北京五环缓冲区"      // 任务名称（可选）
}
```

**字段说明**:

| 字段 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| `params` | object | ✅ | 算子参数（key-value 映射） |
| `execute_now` | boolean | ❌ | 是否立即执行（默认 true，false 表示仅保存任务） |
| `task_name` | string | ❌ | 任务名称（用于保存和调度） |

**响应格式**:

```json
{
  "status": "success",
  "task_id": "uuid-1234-5678",
  "task_status": "completed",       // running/pending/completed/failed
  "result": {                       // 同步执行时返回
    "output_count": 100,
    "geometry_type": "Polygon",
    "bounds": [116.3, 39.9, 116.5, 40.0],
    "preview_geojson": {
      "type": "FeatureCollection",
      "features": [...]
    }
  },
  "message": "算子执行成功",
  "created_at": "2025-12-29T10:30:00Z"
}
```

**字段说明**:

| 字段 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| `status` | string | ✅ | 执行状态（success/error） |
| `task_id` | string | ✅ | 任务 ID（用于查询任务状态） |
| `task_status` | string | ✅ | 任务状态（running/pending/completed/failed） |
| `result` | object | ❌ | 执行结果（同步执行时返回） |
| `message` | string | ❌ | 提示信息或错误信息 |
| `created_at` | string | ✅ | 任务创建时间（ISO 8601 格式） |

---

## 📦 数据模型

### OperatorMetadata（算子元数据）

定义位置: [`common/models/operator.go`](../common/models/operator.go)

```go
type OperatorMetadata struct {
    ID          string               `json:"id"`           // 算子唯一标识
    Name        string               `json:"name"`         // 算子名称
    DisplayName string               `json:"display_name"` // 中文显示名
    Type        string               `json:"type"`         // 算子类型
    Category    string               `json:"category"`     // 分类
    Description string               `json:"description"`  // 功能描述
    Parameters  []ParameterMetadata  `json:"parameters"`   // 参数定义
    Inputs      []string             `json:"inputs"`       // 输入类型列表
    OutputPorts []OutputPortMetadata `json:"output_ports"` // 输出端口定义
    Module      string               `json:"module"`       // 所属模块
}
```

### ParameterMetadata（参数元数据）

```go
type ParameterMetadata struct {
    Name        string                       `json:"name"`                    // 参数名
    Type        string                       `json:"type"`                    // 类型
    Required    bool                         `json:"required"`                // 是否必填
    Default     interface{}                  `json:"default,omitempty"`       // 默认值
    Description string                       `json:"description"`             // 参数说明
    Enum        []string                     `json:"enum,omitempty"`          // 枚举值
    Min         *float64                     `json:"min,omitempty"`           // 最小值
    Max         *float64                     `json:"max,omitempty"`           // 最大值
    Pattern     string                       `json:"pattern,omitempty"`       // 正则校验
    ItemType    string                       `json:"item_type,omitempty"`     // 数组元素类型
    Properties  map[string]ParameterMetadata `json:"properties,omitempty"`    // 对象属性定义
    DependsOn   string                       `json:"depends_on,omitempty"`    // 依赖的参数名
}
```

### OutputPortMetadata（输出端口元数据）

```go
type OutputPortMetadata struct {
    Name        string `json:"name"`                  // 端口名称
    Type        string `json:"type"`                  // 数据类型
    Description string `json:"description"`           // 端口语义说明
    IsDefault   bool   `json:"is_default"`            // 是否为默认端口
}
```

### OperatorExecuteRequest（执行请求）

```go
type OperatorExecuteRequest struct {
    Params     map[string]interface{} `json:"params" binding:"required"` // 算子参数
    ExecuteNow bool                   `json:"execute_now"`               // 是否立即执行
    TaskName   string                 `json:"task_name,omitempty"`       // 任务名称
}
```

### OperatorExecuteResponse（执行响应）

```go
type OperatorExecuteResponse struct {
    Status     string                 `json:"status"`            // 执行状态
    TaskID     string                 `json:"task_id"`           // 任务ID
    TaskStatus string                 `json:"task_status"`       // 任务状态
    Result     map[string]interface{} `json:"result,omitempty"`  // 执行结果
    Message    string                 `json:"message,omitempty"` // 提示信息
    CreatedAt  string                 `json:"created_at"`        // 创建时间
}
```

---

## 🚀 引擎注册规范

### 在 System 模块中注册计算引擎

新计算引擎需要在 `system.engines` 表中注册，声明以下关键信息：

```json
{
  "id": 5,
  "name": "python_workflow_engine",
  "display_name": "Python Workflow 空间计算引擎",
  "engine_type": "api.python-workflow",
  "capabilities": {
    "compute": [
      {
        "type": "spatial",
        "dev_modes": ["workflow"],  // 关键字段：声明支持工作流
        "api_endpoints": {          // 关键字段：算子 API 端点
          "operators": "/api/spatial/operators",
          "execute": "/api/spatial/operators/:name/execute"
        },
        "supported_formats": ["geojson", "wkt", "shapely"]
      }
    ]
  },
  "connection_config": {
    "base_url": "http://python-workflow-engine:8090"
  }
}
```

### 关键字段说明

| 字段 | 说明 | 示例值 |
|-----|------|--------|
| `engine_type` | 引擎类型（必须以 `api.` 开头） | `api.python-workflow` |
| `capabilities.compute[].dev_modes` | 支持的开发模式（必须包含 `workflow`） | `["workflow"]` |
| `capabilities.compute[].api_endpoints` | 算子 API 端点配置 | 见上例 |
| `connection_config.base_url` | 引擎服务地址 | `http://python-workflow-engine:8090` |

### 开发模式类型

| 开发模式 | 说明 | 适用对象 |
|---------|------|---------|
| `sql` | SQL 编辑器 | 数据库引擎（PostgreSQL、MySQL 等） |
| `workflow` | 工作流画布 | **计算引擎**（Python Workflow、Spark 工作流引擎 等） |
| `form` | 表单配置 | **任务提供者**（Meta、Transfer、Manager） |
| `script` | 脚本编辑器 | 脚本执行引擎（预留） |

**重要**：只有声明了 `workflow` 的引擎才会被 Develop 模块的工作流编辑器识别！

---

## 🛠️ 实现指南

### Python 引擎实现（推荐）

以 Python Workflow Engine 为例，展示如何用 Python + FastAPI 实现计算引擎。

#### 1. 定义算子注册表

文件: `engines/python_workflow/operators.py`

```python
from typing import List, Dict, Any

OPERATORS = {
    "buffer": {
        "id": "buffer",
        "name": "buffer",
        "display_name": "缓冲区分析",
        "type": "spatial",
        "category": "几何操作",
        "description": "对几何对象创建缓冲区",
        "module": "python_workflow",
        "parameters": [
            {
                "name": "distance",
                "type": "float",
                "required": True,
                "description": "缓冲区距离（单位：米）",
                "min": 0.0,
                "default": 100.0
            },
            {
                "name": "resolution",
                "type": "integer",
                "required": False,
                "description": "圆角分辨率",
                "min": 1,
                "max": 64,
                "default": 16
            }
        ],
        "inputs": ["geodataframe"],
        "output_ports": [
            {
                "name": "default",
                "type": "geodataframe",
                "description": "缓冲区结果",
                "is_default": True
            }
        ]
    },
    "intersection": {
        "id": "intersection",
        "name": "intersection",
        "display_name": "相交分析",
        "type": "spatial",
        "category": "几何操作",
        "description": "计算两个几何对象的相交部分",
        "module": "python_workflow",
        "parameters": [],
        "inputs": ["geodataframe", "geodataframe"],
        "output_ports": [
            {
                "name": "default",
                "type": "geodataframe",
                "description": "相交结果",
                "is_default": True
            }
        ]
    }
    # ... 其他 19 个算子
}

def list_operators() -> Dict[str, Any]:
    """返回标准化的算子列表"""
    return {
        "status": "success",
        "operators": list(OPERATORS.values()),
        "count": len(OPERATORS)
    }
```

#### 2. 实现 API 端点

文件: `engines/python_workflow/api_server.py`

```python
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Dict, Any, Optional
import uuid
from datetime import datetime

app = FastAPI()

class OperatorExecuteRequest(BaseModel):
    params: Dict[str, Any]
    execute_now: bool = True
    task_name: Optional[str] = None

@app.get("/api/spatial/operators")
def get_operators():
    """获取算子列表"""
    return list_operators()

@app.post("/api/spatial/operators/{name}/execute")
def execute_operator(name: str, request: OperatorExecuteRequest):
    """执行算子"""
    if name not in OPERATORS:
        raise HTTPException(status_code=404, detail=f"算子 '{name}' 不存在")

    # 执行算子逻辑
    result = execute_spatial_operator(name, request.params)

    return {
        "status": "success",
        "task_id": str(uuid.uuid4()),
        "task_status": "completed",
        "result": result,
        "message": "算子执行成功",
        "created_at": datetime.now().isoformat()
    }

def execute_spatial_operator(name: str, params: Dict[str, Any]) -> Dict[str, Any]:
    """实际的算子执行逻辑"""
    import geopandas as gpd

    if name == "buffer":
        # 从上下文或输入参数获取 GeoDataFrame
        gdf = get_input_geodataframe()
        distance = params.get("distance", 100.0)
        resolution = params.get("resolution", 16)

        # 执行缓冲区分析
        result_gdf = gdf.buffer(distance, resolution=resolution)

        return {
            "output_count": len(result_gdf),
            "geometry_type": result_gdf.geom_type[0],
            "preview_geojson": result_gdf.head(10).to_json()
        }

    # ... 其他算子的实现
```

#### 3. 启动引擎服务

文件: `engines/python_workflow/main.py`

```python
import uvicorn
from api_server import app

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8090)
```

---

### Go 模块实现（可选）

如果要用 Go 实现计算引擎，可参考以下结构：

#### 1. 定义算子注册表

文件: `engines/stats/internal/operators/registry.go`

```go
package operators

import "addp/common/models"

var Registry = []models.OperatorMetadata{
    {
        ID:          "mean",
        Name:        "mean",
        DisplayName: "平均值计算",
        Type:        "stats",
        Category:    "描述统计",
        Description: "计算数据列的平均值",
        Module:      "stats",
        Parameters: []models.ParameterMetadata{
            {
                Name:        "column",
                Type:        "string",
                Required:    true,
                Description: "目标数据列名",
            },
        },
        Inputs: []string{"dataframe"},
        OutputPorts: []models.OutputPortMetadata{
            {
                Name:        "default",
                Type:        "number",
                Description: "平均值结果",
                IsDefault:   true,
            },
        },
    },
}
```

#### 2. 实现 API Handler

文件: `engines/stats/internal/api/operator_handler.go`

```go
package api

import (
    "addp/common/models"
    "net/http"
    "github.com/gin-gonic/gin"
)

type OperatorHandler struct {
    service *service.OperatorService
}

func (h *OperatorHandler) ListOperators(c *gin.Context) {
    c.JSON(http.StatusOK, models.OperatorsResponse{
        Status:    "success",
        Operators: operators.Registry,
        Count:     len(operators.Registry),
    })
}

func (h *OperatorHandler) ExecuteOperator(c *gin.Context) {
    name := c.Param("name")
    var req models.OperatorExecuteRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    resp, err := h.service.ExecuteOperator(c.Request.Context(), name, &req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, resp)
}
```

---

## 🔍 能力过滤机制

Develop 模块使用能力过滤工具来发现支持 workflow 的计算引擎。

### Capability 过滤工具

位置: [`common/utils/capability_filter.go`](../common/utils/capability_filter.go)

```go
// 检查引擎是否支持指定开发模式
func SupportsDevMode(engine *models.Engine, mode string) bool {
    for _, cap := range engine.Capabilities.Compute {
        for _, devMode := range cap.DevModes {
            if devMode == mode {
                return true
            }
        }
    }
    return false
}

// 过滤支持指定开发模式的引擎列表
func FilterEnginesByDevMode(engines []models.Engine, mode string) []models.Engine {
    var filtered []models.Engine
    for _, engine := range engines {
        if SupportsDevMode(&engine, mode) {
            filtered = append(filtered, engine)
        }
    }
    return filtered
}
```

### 工作流引擎发现服务

文件: `develop/backend/internal/service/spatial_workflow_service.go`

```go
func (s *SpatialWorkflowService) ListSpatialEngines(ctx context.Context) ([]models.Engine, error) {
    // 从 System 获取所有引擎
    allEngines, err := s.systemClient.ListEngines(ctx)
    if err != nil {
        return nil, err
    }

    // 过滤支持 workflow 开发模式的引擎
    workflowEngines := utils.FilterEnginesByDevMode(allEngines, "workflow")
    return workflowEngines, nil
}
```

---

## 📊 当前实现状态

### 计算引擎统计

| 引擎 | 引擎类型 | 算子数量 | 算子列表 | 开发模式 | 状态 |
|------|---------|---------|---------|---------|------|
| Python Workflow | `api.python-workflow` | 21 | buffer, intersection, union, clip, dissolve 等 | workflow | ✅ 已实现 |
| Spark Workflow 引擎 | `api.spark_workflow` | - | - | workflow | 🔄 预留 |
| Stats Engine | `api.stats` | - | - | workflow | 🔄 预留 |
| ML Engine | `api.ml` | - | - | workflow | 🔄 预留 |

### Python Workflow 算子列表

| 分类 | 算子名称 | 功能描述 |
|------|---------|---------|
| **几何操作** | buffer | 缓冲区分析 |
| | intersection | 相交分析 |
| | union | 合并分析 |
| | difference | 差异分析 |
| | clip | 裁剪分析 |
| | dissolve | 融合分析 |
| **空间关系** | contains | 包含判断 |
| | within | 被包含判断 |
| | touches | 接触判断 |
| | crosses | 穿越判断 |
| **几何计算** | area | 面积计算 |
| | length | 长度计算 |
| | centroid | 质心计算 |
| | convex_hull | 凸包计算 |
| **数据转换** | to_crs | 坐标系转换 |
| | simplify | 几何简化 |
| **数据合并** | sjoin | 空间连接 |
| | overlay | 叠加分析 |
| **其他** | explode | 几何炸开 |
| | boundary | 边界提取 |
| | envelope | 外包矩形 |

**总计**: 21 个空间算子

---

## 🎯 API 端点汇总

### 计算引擎算子 API

| 引擎 | 算子列表端点 | 算子执行端点 |
|------|-------------|-------------|
| Python Workflow | `GET /api/spatial/operators` | `POST /api/spatial/operators/:name/execute` |
| Spark Workflow 引擎 | `GET /api/spark-workflow/operators` | `POST /api/spark-workflow/operators/:name/execute` |
| Stats Engine | `GET /api/stats/operators` | `POST /api/stats/operators/:name/execute` |

### Develop 模块发现端点

| 端点 | 方法 | 功能 |
|------|------|------|
| `/api/develop/spatial/engines` | GET | 获取支持 workflow 的计算引擎列表 |
| `/api/develop/operators` | GET | 聚合所有计算引擎的算子列表 |

---

## 🧪 测试指南

### 1. 单元测试

```bash
# 测试 Capability 过滤函数
cd common/utils
go test -run TestSupportsDevMode

# 测试算子服务
cd develop/backend/internal/service
go test -run TestSpatialWorkflowService
```

### 2. API 测试

```bash
# 设置 Token
export TOKEN="your_jwt_token"

# 测试 Python Workflow 算子列表
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8090/api/spatial/operators

# 测试算子执行
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "params": {"distance": 200.0, "resolution": 16},
    "execute_now": true,
    "task_name": "测试缓冲区"
  }' \
  http://localhost:8090/api/spatial/operators/buffer/execute

# 测试引擎发现
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/develop/spatial/engines
```

### 3. 集成测试

1. 启动所有服务: `bash scripts/dev/start.sh`
2. 登录 Portal: http://localhost:5170
3. 进入 Develop 模块 → GIS 工作流编辑器
   - 验证左侧算子面板显示 21 个 Python Workflow 算子
   - 验证算子按分类分组（几何操作、空间关系等）
   - 拖拽算子到画布，验证参数配置表单
   - 连接多个算子，验证数据流连接
   - 执行工作流，验证结果输出

---

## ⚠️ 注意事项

### 1. 任务提供者 vs 计算引擎

**重要**：不要混淆这两个概念！

| 对比项 | 任务提供者（Meta/Transfer/Manager） | 计算引擎（Python Workflow/Sedona） |
|-------|-----------------------------------|----------------------------|
| 提供内容 | **任务**（Tasks） | **算子**（Operators） |
| API 端点 | `/api/{module}/tasks` | `/api/{module}/operators` |
| 开发模式 | `form`（表单配置） | `workflow`（工作流画布） |
| 使用场景 | Orchestrator 编排 | Develop 工作流编辑器 |
| 特点 | 单次执行的任务 | 可串联的计算单元 |

### 2. 输出端口变更

从 v0.0.16 开始，`OperatorMetadata` 的输出定义从 `Outputs` 字段改为 `OutputPorts` 字段：

```json
// ❌ 旧格式（已弃用）
{
  "outputs": ["geodataframe"]
}

// ✅ 新格式（推荐）
{
  "output_ports": [
    {
      "name": "default",
      "type": "geodataframe",
      "description": "缓冲区结果",
      "is_default": true
    }
  ]
}
```

### 3. 算子列表返回格式

所有引擎的 `list_operators()` 必须返回数组格式：

```json
// ❌ 错误（字典格式）
{
  "operators": {
    "buffer": {...},
    "intersection": {...}
  }
}

// ✅ 正确（数组格式）
{
  "status": "success",
  "operators": [
    {...},
    {...}
  ],
  "count": 2
}
```

### 4. 引擎类型命名规范

- **计算引擎**必须以 `api.` 开头（如 `api.python-workflow`）
- 标准库引擎使用简单名称（如 `postgresql`、`mysql`）

### 5. 开发模式声明

计算引擎必须在注册时明确声明 `dev_modes: ["workflow"]`，否则无法被 Develop 模块发现：

```json
{
  "capabilities": {
    "compute": [
      {
        "type": "spatial",
        "dev_modes": ["workflow"]  // ✅ 必须声明
      }
    ]
  }
}
```

---

## 📚 相关文档

- [ADDP 统一算子实施总结](统一算子.md) - 完整的架构设计和实施细节
- [Develop 模块文档](../develop/CLAUDE.md) - 工作流编辑器使用指南
- [Orchestrator 模块文档](../orchestrator/CLAUDE.md) - 工作流编排架构
- [Capability 过滤工具](../common/utils/capability_filter.go) - 能力过滤函数文档
- [三开发方式设计](../develop/docs/三开发方式设计.md) - SQL/工作流/脚本三种开发模式

---

## 🎉 总结

本规范定义了 ADDP 平台工作流计算引擎的标准算子接口，明确区分了**任务提供者**和**计算引擎**：

**任务提供者**（Meta/Transfer/Manager）:
- 提供**任务**，不是算子
- 通过表单配置执行
- 用于 Orchestrator 工作流编排

**计算引擎**（Python Workflow/Sedona 等）:
- 提供**算子**，支持 `workflow` 开发模式
- 算子可在工作流画布中拖拽组合
- 用于 Develop 工作流编辑器

**核心价值**：
- ✅ **统一性**: 所有计算引擎遵循相同的算子 API 规范
- ✅ **可扩展性**: 新增引擎无需修改 Develop 代码
- ✅ **灵活性**: 支持多输出端口、参数动态显示等高级特性
- ✅ **语言无关**: 引擎实现可使用任意技术栈（Python/Go/Rust/Java）
- ✅ **解耦性**: 计算引擎与任务提供者各司其职，职责清晰

**当前状态**: 已在 Python Workflow Engine 中实施，提供 21 个标准化空间算子。

---

**版本**: v1.0
**最后更新**: 2025-12-29
**维护者**: ADDP 开发团队
