# 空间引擎选择器集成指南

## 概述

ADDP平台现已支持多空间引擎架构。Develop模块通过新的API可以动态获取具备 `compute.workflow` 能力的空间引擎列表,为未来扩展(如Spark 工作流)做好准备。

## 后端API

### 获取空间引擎列表

**端点**: `GET /api/develop/spatial/engines`

**响应示例**:
```json
{
  "status": "success",
  "engines": [
    {
      "id": 1,
      "name": "python_workflow_engine",
      "display_name": "Python Workflow 空间计算引擎",
      "resource_type": "python_workflow",
      "capabilities": {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "python_workflow",
        "engine_family": "workflow",
        "compute": {
          "workflow": {
            "supported": true,
            "runtime_api": "addp.workflow/v1",
            "dynamic_operators": true
          }
        }
      },
      "is_builtin": true,
      "status": "active"
    }
  ],
  "count": 1
}
```

## 前端API调用

### 1. API函数 (已实现)

```javascript
// src/api/spatial.js
import client from './client'

/**
 * 获取支持空间工作流的引擎列表
 * @returns {Promise} 返回支持workflow开发模式的引擎列表
 */
export function listSpatialEngines() {
  return client.get('/develop/spatial/engines')
}
```

### 2. 在组件中使用

#### 示例1: 在工作流编辑器中添加引擎选择器

```vue
<template>
  <div class="workflow-header">
    <!-- 引擎选择器 -->
    <el-select
      v-model="selectedEngine"
      placeholder="选择空间计算引擎"
      @change="handleEngineChange"
    >
      <el-option
        v-for="engine in availableEngines"
        :key="engine.id"
        :label="engine.display_name"
        :value="engine.id"
      >
        <div class="engine-option">
          <span>{{ engine.display_name }}</span>
          <el-tag size="small" type="info">{{ engine.resource_type }}</el-tag>
        </div>
      </el-option>
    </el-select>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { listSpatialEngines } from '@/api/spatial'
import { ElMessage } from 'element-plus'

const availableEngines = ref([])
const selectedEngine = ref(null)

// 加载可用引擎
const loadEngines = async () => {
  try {
    const res = await listSpatialEngines()
    availableEngines.value = res.data.engines || []

    // 默认选择第一个引擎
    if (availableEngines.value.length > 0) {
      selectedEngine.value = availableEngines.value[0].id
    } else {
      ElMessage.warning('没有可用的空间计算引擎')
    }
  } catch (error) {
    console.error('加载引擎列表失败:', error)
    ElMessage.error('加载引擎列表失败')
  }
}

// 引擎切换处理
const handleEngineChange = (engineId) => {
  const engine = availableEngines.value.find(e => e.id === engineId)
  console.log('切换到引擎:', engine)
  // TODO: 重新加载该引擎的算子列表
  // TODO: 更新工作流配置中的引擎信息
}

onMounted(() => {
  loadEngines()
})
</script>

<style scoped>
.engine-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
```

#### 示例2: 在设置页面中配置默认引擎

```vue
<template>
  <el-card>
    <template #header>
      <span>空间计算引擎配置</span>
    </template>

    <el-form label-width="120px">
      <el-form-item label="默认引擎">
        <el-select v-model="settings.defaultEngine" placeholder="选择默认引擎">
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="engine.display_name"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="引擎列表">
        <el-table :data="engines" border>
          <el-table-column prop="display_name" label="名称" />
          <el-table-column prop="resource_type" label="类型" />
          <el-table-column label="支持格式">
            <template #default="{ row }">
              <el-tag
                v-for="format in getFormats(row)"
                :key="format"
                size="small"
                style="margin-right: 4px"
              >
                {{ format }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="特性">
            <template #default="{ row }">
              <el-tag
                v-for="feature in getFeatures(row)"
                :key="feature"
                size="small"
                type="success"
                style="margin-right: 4px"
              >
                {{ feature }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { listSpatialEngines } from '@/api/spatial'

const engines = ref([])
const settings = ref({
  defaultEngine: null
})

const getFormats = (engine) => {
  return engine.capabilities?.extensions?.workflow_runtime?.supported_formats || []
}

const getFeatures = (engine) => {
  return engine.capabilities?.extensions?.workflow_runtime?.features || []
}

onMounted(async () => {
  try {
    const res = await listSpatialEngines()
    engines.value = res.data.engines || []

    // 加载保存的设置
    const savedEngine = localStorage.getItem('preferredSpatialEngine')
    if (savedEngine) {
      settings.value.defaultEngine = parseInt(savedEngine)
    } else if (engines.value.length > 0) {
      settings.value.defaultEngine = engines.value[0].id
    }
  } catch (error) {
    console.error('加载引擎失败:', error)
  }
})
</script>
```

## 当前状态

### 已支持的引擎

1. **Python Workflow Engine** (`python_workflow`)
   - 类型: 内存计算引擎
   - 支持格式: GeoJSON, WKT, Shapely
   - 特性: DAG工作流, 内存高效, 批处理
   - 算子数量: 21个空间算子

### 未来扩展

当添加新的空间引擎(如Spark 工作流)时:

1. **后端**: 引擎自动注册到System资源中心
   ```python
   # 新引擎启动时自动注册
   registration_data = {
       "engine_type": "spark_workflow",
       "name": "Spark 工作流引擎",
       "description": "基于 Apache Spark 的分布式工作流执行引擎",
       "connection_info": {
           "protocol": "http",
           "port": 8098
       },
       "is_builtin": true
   }
   ```

2. **前端**: 无需修改代码,引擎选择器自动显示新引擎

3. **算子**: 新引擎提供自己的算子列表API
   ```
   GET /api/operators?engine=spark_workflow
   ```

## 注意事项

### 引擎能力过滤

系统会自动过滤出支持 `workflow` 开发模式的引擎:

```go
// 后端自动过滤逻辑 (develop/backend/internal/service/spatial_workflow_service.go)
workflowEngines := utils.FilterResourcesByDevMode(allResources, "workflow")
```

### 引擎状态监控

可通过引擎的健康检查端点监控状态:

```javascript
async function checkEngineHealth(engine) {
  try {
    const res = await fetch(`${engine.base_url}/health`)
    return res.ok
  } catch {
    return false
  }
}
```

## 测试指南

### 1. 测试引擎列表API

```bash
# 获取空间引擎列表
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8000/api/develop/spatial/engines
```

### 2. 测试引擎能力

```javascript
// 检查引擎是否支持工作流能力
const engine = engines[0]
const workflow = engine.capabilities.compute.workflow
console.log('支持工作流:', workflow.supported)
// 输出: true
```

### 3. 测试引擎切换

```javascript
// 切换到指定引擎并加载其算子
async function switchEngine(engineId) {
  const engine = engines.find(e => e.id === engineId)

  // 加载该引擎的算子
  const operators = await listOperators({ engine: engine.resource_type })

  console.log(`引擎 ${engine.display_name} 的算子:`, operators)
}
```

## 相关文档

- [统一算子API设计](../../docs/OPERATOR_API_DESIGN.md)
- [资源引擎管理架构](../../docs/RESOURCE_ENGINE_ARCHITECTURE.md)
- [Capability过滤机制](../../common/utils/capability_filter.go)
