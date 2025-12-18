# Changelog - 统一算子API架构

## [v0.0.16] - 2025-12-17

### 🎉 重大更新: 统一算子API架构

#### 新增功能

##### 1. 统一算子API (所有模块)

所有模块现在提供标准化的算子发现和执行API:

```bash
# Meta模块 - 元数据扫描算子
GET  /api/meta/operators                 # 获取算子列表
POST /api/meta/operators/:name/execute   # 执行算子

# Transfer模块 - 数据传输算子
GET  /api/transfer/operators
POST /api/transfer/operators/:name/execute

# Manager模块 - 瓦片缓存算子
GET  /api/manager/operators
POST /api/manager/operators/:name/execute

# GeoPandas Engine - 空间计算算子(已调整格式)
GET  /api/develop/spatial/operators
POST /api/develop/spatial/operators/:name/execute
```

##### 2. 空间引擎动态发现

Develop模块新增API支持多空间引擎:

```bash
# 获取支持workflow开发模式的空间引擎列表
GET /api/develop/spatial/engines
```

响应示例:
```json
{
  "status": "success",
  "engines": [
    {
      "id": 1,
      "name": "geopandas_engine",
      "display_name": "GeoPandas空间计算引擎",
      "resource_type": "api.geopandas",
      "capabilities": {
        "compute": [{
          "type": "spatial",
          "dev_modes": ["workflow"],
          "supported_formats": ["geojson", "wkt", "shapely"]
        }]
      }
    }
  ]
}
```

##### 3. 资源能力声明增强

新增 `dev_modes` 字段到 ComputeCapability:

```json
{
  "capabilities": {
    "compute": [{
      "type": "sql_query",
      "dev_modes": ["sql"]  // 新增字段
    }]
  }
}
```

支持的开发模式:
- `sql`: SQL编辑器
- `workflow`: 工作流画布(算子拖拽)
- `form`: 表单配置
- `script`: 脚本编辑器(未来)

##### 4. API引擎资源类型

新增 `api.*` 资源类型命名规范:

- `api.meta`: Meta模块算子
- `api.transfer`: Transfer模块算子
- `api.manager`: Manager模块算子
- `api.geopandas`: GeoPandas空间计算引擎
- `api.spark_sedona`: Spark Sedona引擎(预留)

##### 5. 能力过滤工具函数

新增通用过滤函数 (`common/utils/capability_filter.go`):

```go
// 检查资源是否支持指定开发模式
SupportsDevMode(resource, "sql")

// 过滤资源列表
FilterResourcesByDevMode(resources, "workflow")

// 判断引擎类型
IsAPIEngine(resource)
IsStandardLibraryEngine(resource)
```

#### 改进优化

##### 1. Develop模块资源过滤

**SQL编辑器**:
- 改进: 使用 `SupportsDevMode(&res, "sql")` 替代硬编码类型判断
- 效果: 自动识别所有支持SQL的资源(PostgreSQL, MySQL, Doris, Spark SQL等)

**工作流画布**:
- 新增: `ListSpatialEngines()` 函数获取支持workflow的引擎
- 效果: 为未来多空间引擎支持做好准备

##### 2. GeoPandas Engine返回格式标准化

**更新**: `list_operators()` 函数返回标准化格式

**旧格式**:
```python
{
  "buffer": {
    "params": {...},
    "category": "...",
    "description": "..."
  }
}
```

**新格式**:
```python
[
  {
    "id": "buffer",
    "name": "buffer",
    "display_name": "缓冲区分析",
    "type": "spatial",
    "category": "几何操作",
    "module": "geopandas",
    "parameters": [...],  // 标准化的参数定义
    "inputs": ["geodataframe"],
    "outputs": ["geodataframe"]
  }
]
```

##### 3. 目录结构优化

```
addp/
├── engines/                    # 计算引擎统一目录(新增)
│   ├── geopandas/             # GeoPandas引擎(移动)
│   ├── spark-sedona/          # 预留Spark Sedona引擎
│   └── README.md              # 引擎开发指南
│
├── common/
│   ├── models/
│   │   └── operator.go        # 统一算子模型(新增)
│   └── utils/
│       └── capability_filter.go  # 能力过滤工具(扩展)
│
├── meta/backend/internal/
│   ├── operators/             # Meta算子定义(新增)
│   ├── api/operator_handler.go
│   └── service/operator_service.go
│
├── transfer/backend/internal/
│   ├── operators/             # Transfer算子定义(新增)
│   ├── api/operator_handler.go
│   └── service/operator_service.go
│
└── manager/backend/internal/
    ├── operators/             # Manager算子定义(新增)
    ├── api/operator_handler.go
    └── service/operator_service.go
```

#### 算子统计

| 模块 | 算子数量 | 算子列表 |
|------|---------|---------|
| Meta | 2 | scan_basic, scan_deep |
| Transfer | 2 | batch_transfer, stream_transfer |
| Manager | 1 | mvt_tile_cache |
| GeoPandas | 21 | buffer, intersection, union等 |
| **总计** | **26** | |

#### 新增文档

1. **实施总结**: `docs/UNIFIED_OPERATOR_API_IMPLEMENTATION.md`
   - 完整的架构设计和实施细节
   - 所有改动文件列表
   - 测试和验证指南

2. **前端集成指南**: `develop/frontend/SPATIAL_ENGINE_INTEGRATION.md`
   - 前端API调用示例
   - 引擎选择器实现参考
   - 未来扩展说明

3. **引擎开发指南**: `engines/README.md`
   - 新增引擎的开发规范
   - API接口标准
   - 注册机制说明

4. **验证脚本**: `scripts/test/verify-operator-api.sh`
   - 自动化API测试脚本
   - 验证所有模块的算子API

#### 破坏性变更

⚠️ **GeoPandas Engine**:
- `list_operators()` 返回格式从字典改为列表
- 现有调用代码需要更新(如果有直接调用GeoPandas API的代码)

**迁移方式**:
```javascript
// 旧代码
const operators = response.operators  // 字典格式
Object.keys(operators).forEach(name => { ... })

// 新代码
const operators = response.operators  // 数组格式
operators.forEach(op => { ... })
```

#### 依赖更新

无新增外部依赖,所有改动使用现有技术栈。

#### 配置变更

无需修改配置文件,所有改动向后兼容。

#### 部署说明

1. **标准部署**: 无特殊步骤,正常启动即可
   ```bash
   bash scripts/dev/start.sh
   ```

2. **验证部署**: 运行验证脚本
   ```bash
   export ADDP_TOKEN="your_token"
   bash scripts/test/verify-operator-api.sh
   ```

#### 已知问题

- 无

#### 下一步计划

- [ ] 前端引擎选择器UI实现(可选)
- [ ] Spark Sedona引擎集成(可选)
- [ ] 算子执行结果可视化增强

#### 贡献者

- 架构设计与实施: Claude Code
- 日期: 2025-12-17

---

## 相关链接

- [详细实施文档](docs/UNIFIED_OPERATOR_API_IMPLEMENTATION.md)
- [前端集成指南](develop/frontend/SPATIAL_ENGINE_INTEGRATION.md)
- [引擎开发规范](engines/README.md)
- [架构设计方案](~/.claude/plans/buzzing-bubbling-porcupine.md)

---

**版本**: v0.0.16
**发布日期**: 2025-12-17
**类型**: 重大功能更新
