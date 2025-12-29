# Orchestrator 参数模板化功能说明

## 概述

Orchestrator 现在支持**参数模板化**功能，允许在工作流步骤中通过 `{{stepID.field}}` 语法引用前序步骤的执行结果。这使得复杂的多步骤编排可以实现数据的流式传递。

## 模板语法

### 基本格式

```
{{stepID.field}}
{{stepID.nested.field}}
{{stepID.deeply.nested.field.value}}
```

### 组成部分

- `stepID`: 前序步骤的 ID（在编排定义中的 `steps[].id` 字段）
- `field`: 步骤结果中的字段路径（支持多级嵌套）

### 解析规则

1. **步骤结果访问**: `stepResults[stepID].Result` 是一个 `map[string]interface{}`
2. **字段路径**: 使用 `.` 分隔符逐级访问嵌套字段
3. **类型安全**: 如果路径中任何字段不存在或类型不匹配，返回 `nil`
4. **非模板字符串**: 不包含 `{{}}` 的字符串原样返回

## 使用示例

### 示例 1: SQL + GIS + Transfer 混合编排

```json
{
  "name": "数据预处理 + 空间分析 + 结果导出",
  "steps": [
    {
      "id": "sql_extract",
      "name": "SQL 数据提取",
      "engine_identifier": "sql.postgresql.default",
      "parameters": {
        "query": "SELECT geom, name FROM poi WHERE city='北京'",
        "engine_id": 5
      },
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "spatial_analysis",
      "name": "空间缓冲区分析",
      "engine_identifier": "geopandas.engine.default",
      "parameters": {
        "task_id": 1,
        "inputs": {
          "poi_location": "{{sql_extract.geojson}}",
          "buffer_distance": 0.001
        }
      },
      "depends_on": ["sql_extract"],
      "timeout": 600
    },
    {
      "id": "export_result",
      "name": "导出为 Shapefile",
      "engine_identifier": "transfer.worker.default",
      "parameters": {
        "task_type": "export",
        "source_geojson": "{{spatial_analysis.geojson}}",
        "target_path": "s3://bucket/beijing-poi-buffer.shp",
        "format": "shapefile"
      },
      "depends_on": ["spatial_analysis"],
      "timeout": 300
    }
  ]
}
```

### 示例 2: 多步 GIS 分析链

```json
{
  "name": "POI 缓冲区 + 边界叠加 + 面积计算",
  "steps": [
    {
      "id": "buffer_analysis",
      "name": "POI 缓冲区",
      "engine_identifier": "geopandas.engine.default",
      "parameters": {
        "task_id": 1,
        "inputs": {
          "poi_location": {
            "type": "FeatureCollection",
            "features": [...]
          },
          "buffer_distance": 0.001
        }
      },
      "depends_on": []
    },
    {
      "id": "boundary_clip",
      "name": "边界裁剪",
      "engine_identifier": "geopandas.engine.default",
      "parameters": {
        "task_id": 2,
        "inputs": {
          "input_gdf": "{{buffer_analysis.geojson}}",
          "clip_boundary": {
            "type": "Polygon",
            "coordinates": [...]
          }
        }
      },
      "depends_on": ["buffer_analysis"]
    },
    {
      "id": "area_calculation",
      "name": "面积计算",
      "engine_identifier": "geopandas.engine.default",
      "parameters": {
        "task_id": 3,
        "inputs": {
          "input_gdf": "{{boundary_clip.geojson}}"
        }
      },
      "depends_on": ["boundary_clip"]
    }
  ]
}
```

### 示例 3: 复杂嵌套参数

```json
{
  "id": "complex_step",
  "name": "复杂参数示例",
  "engine_identifier": "geopandas.engine.default",
  "parameters": {
    "config": {
      "source_table": "{{sql_extract.result_table}}",
      "geometry_field": "{{sql_extract.geometry_field}}",
      "properties": {
        "name": "{{sql_extract.properties.name}}",
        "count": "{{sql_extract.row_count}}"
      }
    },
    "input_data": "{{buffer_analysis.geojson}}",
    "static_value": "fixed_string"
  },
  "depends_on": ["sql_extract", "buffer_analysis"]
}
```

## 执行流程

1. **拓扑排序**: Orchestrator 根据 `depends_on` 字段对步骤进行拓扑排序
2. **顺序执行**: 按拓扑顺序逐步执行
3. **结果缓存**: 每个步骤的结果存储到 `stepResults` 中
4. **模板解析**: 执行步骤前，调用 `resolveTemplateReferences` 解析参数
5. **递归处理**: 支持嵌套 map、数组中的模板引用
6. **引擎调用**: 使用解析后的参数调用对应引擎

## 步骤结果结构

每个步骤的结果按以下结构存储：

```go
type StepResult struct {
    Status    string                 `json:"status"`    // "success" / "failed"
    Result    map[string]interface{} `json:"result"`    // 步骤返回的实际数据
    Error     string                 `json:"error,omitempty"`
    StartedAt time.Time              `json:"started_at"`
    EndedAt   time.Time              `json:"ended_at"`
    Duration  int64                  `json:"duration"`  // 毫秒
}
```

**模板引用访问**: `{{stepID.field}}` 等价于 `stepResults[stepID].Result[field]`

## 支持的数据类型

### 1. 简单类型

```json
{
  "string_field": "{{step1.name}}",
  "number_field": "{{step1.count}}",
  "boolean_field": "{{step1.enabled}}"
}
```

### 2. 嵌套对象

```json
{
  "geometry": "{{step1.feature.geometry}}",
  "coordinates": "{{step1.feature.geometry.coordinates}}"
}
```

### 3. 数组

```json
{
  "tables": [
    "{{step1.table1}}",
    "{{step2.table2}}",
    "static_table"
  ]
}
```

### 4. 混合嵌套

```json
{
  "config": {
    "data": "{{step1.geojson}}",
    "metadata": {
      "source": "{{step1.source}}",
      "count": "{{step1.row_count}}"
    }
  }
}
```

## 错误处理

### 场景 1: 步骤不存在

```json
{
  "value": "{{nonexistent_step.field}}"
}
```

**结果**: `{"value": null}`

### 场景 2: 字段不存在

```json
{
  "value": "{{sql_extract.nonexistent_field}}"
}
```

**结果**: `{"value": null}`

### 场景 3: 类型不匹配

```json
{
  "value": "{{step1.string_field.nested_field}}"
}
```

**结果**: `{"value": null}` (string 类型无法继续访问嵌套字段)

### 场景 4: 依赖步骤失败

如果依赖步骤 (如 `step1`) 执行失败 (`status: "failed"`)，则当前步骤不会执行，整个编排标记为失败。

## 实现细节

### 核心函数

**1. resolveTemplateReferences**
- 入口函数，遍历参数 map 并解析所有值
- 递归处理嵌套 map 和数组

**2. resolveValue**
- 递归解析单个值
- 识别字符串、map、数组类型并分别处理

**3. resolveStringTemplate**
- 解析字符串模板 `{{stepID.field}}`
- 从 `stepResults` 中提取对应值

**4. splitPath**
- 分割路径字符串（如 `"step1.field1.field2"` → `["step1", "field1", "field2"]`）

### 代码位置

- **实现**: `/orchestrator/backend/internal/service/executor.go`
- **测试**: `/orchestrator/backend/internal/service/executor_test.go`

## 最佳实践

### 1. 明确步骤依赖

```json
{
  "id": "step2",
  "depends_on": ["step1"],
  "parameters": {
    "input": "{{step1.output}}"
  }
}
```

**原因**: 确保 `step1` 在 `step2` 之前执行，避免引用不存在的结果。

### 2. 验证引擎返回结构

不同引擎返回的 `Result` 结构不同，使用前确认字段名称：

- **GeoPandas Engine**: `{ "geojson": {...}, "execution_id": "..." }`
- **SQL Engine**: `{ "result_table": "temp_123", "row_count": 100 }`
- **Transfer Worker**: `{ "file_path": "s3://...", "status": "completed" }`

### 3. 处理 nil 值

如果字段可能不存在，提供默认值或在引擎端处理 `nil`：

```go
// 引擎端代码
inputData := params["input_geojson"]
if inputData == nil {
    return nil, fmt.Errorf("input_geojson is required")
}
```

### 4. 避免循环依赖

```json
// ❌ 错误示例
{
  "steps": [
    {"id": "step1", "depends_on": ["step2"]},
    {"id": "step2", "depends_on": ["step1"]}
  ]
}
```

**结果**: 拓扑排序失败，编排执行错误。

## 测试示例

参见 `/orchestrator/backend/internal/service/executor_test.go`:

- `TestResolveTemplateReferences`: 完整参数解析测试（9个测试用例）
- `TestSplitPath`: 路径分割测试
- `TestResolveStringTemplate`: 字符串模板解析测试

## 兼容性

- **向后兼容**: 支持旧的硬编码模块调用方式 (`module`, `endpoint`, `method`)
- **新架构优先**: 优先使用 `engine_identifier` 动态引擎调用
- **自动降级**: 如果 `engine_identifier` 为空，回退到 `module` 模式

## 未来扩展

1. **条件执行**: 支持 `{{if step1.success}}...{{end}}` 语法
2. **函数调用**: 支持 `{{step1.value | upper}}` 等转换函数
3. **表达式计算**: 支持 `{{step1.count + step2.count}}` 等算术运算
4. **循环迭代**: 支持 `{{range step1.items}}...{{end}}` 循环
