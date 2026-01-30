# Transfer 模块用户体验改进方案

**目标**: 让用户能快速排错和定位主键相关问题

**实施日期**: 2026-01-29

---

## 📋 改进清单

### 1️⃣ 前端字段映射 UI - 显示主键标记和映射关系

#### 1.1 后端 API 改进

**文件**: `transfer/backend/internal/api/task_api.go`

**新增 API 端点**: `GET /api/tasks/{id}/source-schema`

```go
// GetSourceSchema 获取源表 Schema 信息（包含主键）
func (api *TaskAPI) GetSourceSchema(c *gin.Context) {
    taskID := c.Param("id")

    // 1. 加载任务
    task, err := api.taskService.GetTask(taskID)

    // 2. 创建临时 Reader 连接源表
    reader := createReader(task.Config.Source)
    schema := reader.DetectSchema()

    // 3. 返回 Schema（包含字段列表 + 主键信息）
    c.JSON(200, gin.H{
        "fields": schema.Fields,
        "primary_key": schema.PrimaryKey,  // { columns: ["SmID"], name: "" }
    })
}
```

#### 1.2 前端组件改进

**文件**: `transfer/frontend/src/components/FieldMappingEditor.vue`

**改动 1**: 添加主键标记列（在源字段列旁边显示 🔑 图标）

```vue
<el-table-column label="源字段" min-width="180">
  <template #default="{ row, $index }">
    <div style="display: flex; align-items: center; gap: 8px;">
      <!-- 主键标记 -->
      <el-tooltip
        v-if="isSourcePrimaryKey(row.source_field)"
        content="主键字段"
        placement="top"
      >
        <el-tag type="warning" size="small" effect="dark">
          <el-icon><Key /></el-icon> PK
        </el-tag>
      </el-tooltip>

      <!-- 字段选择器 -->
      <el-select
        v-model="row.source_field"
        placeholder="选择源字段"
        filterable
        allow-create
        @change="handleSourceFieldChange(row, $index)"
        style="flex: 1"
      >
        <el-option
          v-for="field in sourceFields"
          :key="field"
          :label="field"
          :value="field"
        >
          <!-- 选项中也显示主键标记 -->
          <div style="display: flex; justify-content: space-between; align-items: center;">
            <span>{{ field }}</span>
            <el-tag
              v-if="sourcePrimaryKeys.includes(field)"
              type="warning"
              size="small"
              effect="plain"
            >
              PK
            </el-tag>
          </div>
        </el-option>
      </el-select>
    </div>
  </template>
</el-table-column>
```

**改动 2**: 添加主键映射提示信息（在表格上方）

```vue
<el-alert
  v-if="hasPrimaryKeyMapping"
  type="success"
  :closable="false"
  style="margin-bottom: 10px"
>
  <template #title>
    <el-icon><Key /></el-icon>
    主键映射关系
  </template>
  <div>
    <p>检测到源表主键: <el-tag type="warning" size="small">{{ sourcePrimaryKeys.join(', ') }}</el-tag></p>
    <p>映射到目标字段: <el-tag type="success" size="small">{{ mappedPrimaryKeys.join(', ') }}</el-tag></p>
    <p style="color: #67C23A; margin-top: 5px;">
      ✅ 导入完成后，系统将自动在目标表创建主键约束
    </p>
  </div>
</el-alert>

<el-alert
  v-if="hasUnmappedPrimaryKey"
  type="warning"
  :closable="false"
  style="margin-bottom: 10px"
>
  <template #title>
    ⚠️ 主键字段未映射
  </template>
  <div>
    <p>源表主键字段 <el-tag type="warning" size="small">{{ unmappedPrimaryKeys.join(', ') }}</el-tag> 未添加到字段映射中</p>
    <p>建议：将主键字段添加到映射列表，以便目标表也能创建主键约束</p>
  </div>
</el-alert>
```

**改动 3**: 添加计算属性和数据

```javascript
const props = defineProps({
  // ... 现有属性
  sourcePrimaryKeys: {  // ✅ 新增：源表主键列表
    type: Array,
    default: () => []
  }
})

// ✅ 新增：计算属性
const isSourcePrimaryKey = (fieldName) => {
  return props.sourcePrimaryKeys.includes(fieldName)
}

const hasPrimaryKeyMapping = computed(() => {
  return props.sourcePrimaryKeys.length > 0 &&
         props.sourcePrimaryKeys.every(pk =>
           mappings.value.some(m => m.source_field === pk)
         )
})

const mappedPrimaryKeys = computed(() => {
  return props.sourcePrimaryKeys
    .map(pk => {
      const mapping = mappings.value.find(m => m.source_field === pk)
      return mapping?.target_field
    })
    .filter(Boolean)
})

const hasUnmappedPrimaryKey = computed(() => {
  return props.sourcePrimaryKeys.length > 0 &&
         props.sourcePrimaryKeys.some(pk =>
           !mappings.value.some(m => m.source_field === pk)
         )
})

const unmappedPrimaryKeys = computed(() => {
  return props.sourcePrimaryKeys.filter(pk =>
    !mappings.value.some(m => m.source_field === pk)
  )
})
```

#### 1.3 父组件集成

**文件**: `transfer/frontend/src/views/TaskWizard/Step3FieldMapping.vue`

```javascript
const sourcePrimaryKeys = ref([])

// 在获取源字段时，同时获取主键信息
const fetchSourceFields = async () => {
  try {
    const response = await axios.get(`/api/tasks/${taskId}/source-schema`)
    sourceFields.value = response.data.fields.map(f => f.name)
    sourceFieldDetails.value = response.data.fields

    // ✅ 提取主键信息
    if (response.data.primary_key && response.data.primary_key.columns) {
      sourcePrimaryKeys.value = response.data.primary_key.columns
    }
  } catch (error) {
    ElMessage.error('获取源字段失败')
  }
}
```

---

### 2️⃣ 任务 Config - 明确写出主键相关配置

#### 2.1 后端服务层改进

**文件**: `transfer/backend/internal/service/task_service.go`

**改动**: 创建任务时，在 config.target 中显式设置主键相关字段

```go
func (s *TaskService) CreateTask(req models.CreateTaskRequest) (*models.Task, error) {
    // ... 现有逻辑

    // ✅ 确保 config.target 中包含主键配置
    if targetConfig, ok := req.Config["target"].(map[string]interface{}); ok {
        // 如果用户没有显式设置，使用默认值
        if _, exists := targetConfig["create_primary_key"]; !exists {
            targetConfig["create_primary_key"] = true  // 默认启用
        }
        if _, exists := targetConfig["force_primary_key"]; !exists {
            targetConfig["force_primary_key"] = []string{}  // 空数组表示自动检测
        }
        if _, exists := targetConfig["primary_key_name"]; !exists {
            targetConfig["primary_key_name"] = ""  // 空字符串表示自动生成
        }

        // ✅ 空间索引配置
        if _, exists := targetConfig["create_spatial_index"]; !exists {
            targetConfig["create_spatial_index"] = true
        }

        // ✅ 统计信息更新
        if _, exists := targetConfig["update_statistics"]; !exists {
            targetConfig["update_statistics"] = true
        }

        req.Config["target"] = targetConfig
    }

    // ... 保存任务
}
```

#### 2.2 前端表单改进

**文件**: `transfer/frontend/src/views/TaskWizard/Step4Configure.vue`

**新增配置项**: 后处理选项（主键、空间索引、统计信息）

```vue
<el-form-item label="后处理选项">
  <el-space direction="vertical" :size="10" style="width: 100%;">
    <!-- 主键创建 -->
    <el-card shadow="never">
      <template #header>
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <span>
            <el-icon><Key /></el-icon>
            创建主键约束
          </span>
          <el-switch v-model="targetConfig.create_primary_key" />
        </div>
      </template>
      <div v-if="targetConfig.create_primary_key">
        <el-form-item label="主键字段" label-width="100px">
          <el-select
            v-model="targetConfig.force_primary_key"
            multiple
            filterable
            placeholder="留空表示自动检测源表主键"
            style="width: 100%;"
          >
            <el-option
              v-for="field in targetFields"
              :key="field"
              :label="field"
              :value="field"
            />
          </el-select>
          <el-text type="info" size="small" style="margin-top: 5px;">
            💡 留空将自动使用源表的主键字段
          </el-text>
        </el-form-item>

        <el-form-item label="约束名称" label-width="100px">
          <el-input
            v-model="targetConfig.primary_key_name"
            placeholder="留空则自动生成（如: tablename_pkey）"
          />
        </el-form-item>
      </div>
    </el-card>

    <!-- 空间索引创建 -->
    <el-card shadow="never">
      <template #header>
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <span>
            <el-icon><MapLocation /></el-icon>
            创建空间索引
          </span>
          <el-switch v-model="targetConfig.create_spatial_index" />
        </div>
      </template>
      <el-text type="info" size="small">
        为几何字段自动创建空间索引（GIST），提高空间查询性能
      </el-text>
    </el-card>

    <!-- 统计信息更新 -->
    <el-card shadow="never">
      <template #header>
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <span>
            <el-icon><DataAnalysis /></el-icon>
            更新统计信息
          </span>
          <el-switch v-model="targetConfig.update_statistics" />
        </div>
      </template>
      <el-text type="info" size="small">
        执行 ANALYZE 更新表统计信息，优化查询计划
      </el-text>
    </el-card>
  </el-space>
</el-form-item>
```

#### 2.3 任务详情页显示

**文件**: `transfer/frontend/src/views/TaskDetail.vue`

```vue
<!-- ✅ 新增：后处理配置展示 -->
<el-descriptions-item label="后处理配置" :span="3">
  <el-space wrap>
    <el-tag
      :type="task.config.target.create_primary_key ? 'success' : 'info'"
    >
      主键: {{ task.config.target.create_primary_key ? '✓ 启用' : '✗ 禁用' }}
    </el-tag>

    <el-tag
      v-if="task.config.target.create_primary_key && task.config.target.force_primary_key?.length > 0"
      type="warning"
    >
      强制主键: {{ task.config.target.force_primary_key.join(', ') }}
    </el-tag>

    <el-tag
      :type="task.config.target.create_spatial_index ? 'success' : 'info'"
    >
      空间索引: {{ task.config.target.create_spatial_index ? '✓ 启用' : '✗ 禁用' }}
    </el-tag>

    <el-tag
      :type="task.config.target.update_statistics ? 'success' : 'info'"
    >
      统计信息: {{ task.config.target.update_statistics ? '✓ 启用' : '✗ 禁用' }}
    </el-tag>
  </el-space>
</el-descriptions-item>
```

---

### 3️⃣ 前端执行日志 - 显示后处理详情

#### 3.1 后端日志增强

**文件**: `transfer/backend/pkg/postprocessor/postgres_processor.go`

**改动**: 在日志中添加更详细的信息

```go
// 主键创建前
pp.logger.Info("🔑 [后处理] 准备创建主键",
    "table", pp.config.TableName,
    "columns", pkMeta.Columns,
    "constraint_name", pkMeta.Name,
)

// 主键创建成功后
pp.logger.Info("✅ [后处理] 主键创建成功",
    "table", pp.config.TableName,
    "constraint", constraintName,
    "columns", pkMeta.Columns,
    "sql", sql,  // 显示实际执行的 SQL
)

// 空间索引创建
pp.logger.Info("🗺️ [后处理] 创建空间索引",
    "table", pp.config.TableName,
    "column", column,
    "index_name", indexName,
    "srid", srid,
)

// 统计信息更新
pp.logger.Info("📊 [后处理] 更新统计信息",
    "table", pp.config.TableName,
)
```

**改动**: LogBuffer 同步这些信息

```go
// 在 ExecutionEngine 中
e.logBuffer.Append("INFO", fmt.Sprintf("🔑 [后处理] 准备创建主键: %s (%s)",
    tableName, strings.Join(columns, ", ")))

e.logBuffer.Append("INFO", fmt.Sprintf("✅ [后处理] 主键创建成功: %s.%s",
    tableName, constraintName))

e.logBuffer.Append("INFO", fmt.Sprintf("🗺️ [后处理] 创建空间索引: %s.%s",
    tableName, indexName))

e.logBuffer.Append("INFO", "📊 [后处理] 统计信息更新完成")
```

#### 3.2 前端日志展示优化

**文件**: `transfer/frontend/src/views/ExecutionDetail.vue`

**改动 1**: 日志高亮显示后处理相关内容

```javascript
// 日志处理函数
const formatLog = (logLine) => {
  // ✅ 后处理相关日志高亮
  if (logLine.includes('[后处理]')) {
    if (logLine.includes('✅') || logLine.includes('成功')) {
      return { text: logLine, type: 'success', icon: '✅' }
    }
    if (logLine.includes('🔑') || logLine.includes('主键')) {
      return { text: logLine, type: 'primary-key', icon: '🔑' }
    }
    if (logLine.includes('🗺️') || logLine.includes('空间索引')) {
      return { text: logLine, type: 'spatial-index', icon: '🗺️' }
    }
    if (logLine.includes('📊') || logLine.includes('统计')) {
      return { text: logLine, type: 'statistics', icon: '📊' }
    }
    return { text: logLine, type: 'post-process', icon: '⚙️' }
  }

  // 普通日志
  if (logLine.includes('[ERROR]')) {
    return { text: logLine, type: 'error', icon: '❌' }
  }
  if (logLine.includes('[WARN]')) {
    return { text: logLine, type: 'warning', icon: '⚠️' }
  }

  return { text: logLine, type: 'info', icon: 'ℹ️' }
}
```

**改动 2**: 日志渲染增强

```vue
<el-timeline>
  <el-timeline-item
    v-for="(log, index) in formattedLogs"
    :key="index"
    :type="getTimelineType(log.type)"
    :icon="log.icon"
  >
    <div class="log-line" :class="`log-${log.type}`">
      <span class="log-icon">{{ log.icon }}</span>
      <span class="log-text">{{ log.text }}</span>
    </div>
  </el-timeline-item>
</el-timeline>
```

**改动 3**: 样式定义

```vue
<style scoped>
.log-primary-key {
  background: #fef0f0;
  border-left: 3px solid #e6a23c;
  padding: 8px 12px;
  border-radius: 4px;
  font-weight: 500;
}

.log-spatial-index {
  background: #f0f9ff;
  border-left: 3px solid #409eff;
  padding: 8px 12px;
  border-radius: 4px;
}

.log-statistics {
  background: #f4f4f5;
  border-left: 3px solid #909399;
  padding: 8px 12px;
  border-radius: 4px;
}

.log-success {
  background: #f0f9ff;
  border-left: 3px solid #67c23a;
  padding: 8px 12px;
  border-radius: 4px;
  font-weight: 500;
}

.log-error {
  background: #fef0f0;
  border-left: 3px solid #f56c6c;
  padding: 8px 12px;
  border-radius: 4px;
}
</style>
```

#### 3.3 后处理摘要卡片

**文件**: `transfer/frontend/src/views/ExecutionDetail.vue`

**新增**: 执行结果顶部显示后处理摘要

```vue
<!-- ✅ 新增：后处理摘要 -->
<el-card v-if="execution.status === 'success'" class="post-process-summary" shadow="never">
  <template #header>
    <div class="summary-header">
      <el-icon><Operation /></el-icon>
      <span>后处理执行摘要</span>
    </div>
  </template>

  <el-space wrap :size="15">
    <el-statistic
      v-if="postProcessSummary.primary_key_created"
      title="主键创建"
      :value="'✓'"
    >
      <template #prefix>
        <el-icon color="#67C23A"><Key /></el-icon>
      </template>
      <template #suffix>
        <el-text size="small" type="success">
          {{ postProcessSummary.primary_key_columns.join(', ') }}
        </el-text>
      </template>
    </el-statistic>

    <el-statistic
      v-if="postProcessSummary.spatial_indexes_created > 0"
      title="空间索引"
      :value="postProcessSummary.spatial_indexes_created"
    >
      <template #prefix>
        <el-icon color="#409EFF"><MapLocation /></el-icon>
      </template>
      <template #suffix>
        <el-text size="small" type="primary">个</el-text>
      </template>
    </el-statistic>

    <el-statistic
      v-if="postProcessSummary.statistics_updated"
      title="统计更新"
      :value="'✓'"
    >
      <template #prefix>
        <el-icon color="#909399"><DataAnalysis /></el-icon>
      </template>
    </el-statistic>
  </el-space>
</el-card>
```

**计算属性**: 从日志中提取后处理摘要

```javascript
const postProcessSummary = computed(() => {
  const summary = {
    primary_key_created: false,
    primary_key_columns: [],
    spatial_indexes_created: 0,
    statistics_updated: false
  }

  if (!execution.value?.logs) return summary

  const logs = execution.value.logs.split('\n')

  logs.forEach(log => {
    if (log.includes('主键创建成功')) {
      summary.primary_key_created = true
      // 从日志中提取列名: "主键创建成功: columns=[SmID]"
      const match = log.match(/columns=\[(.*?)\]/)
      if (match) {
        summary.primary_key_columns = match[1].split(',').map(s => s.trim())
      }
    }

    if (log.includes('创建空间索引') && log.includes('成功')) {
      summary.spatial_indexes_created++
    }

    if (log.includes('统计信息更新完成')) {
      summary.statistics_updated = true
    }
  })

  return summary
})
```

---

## 🎯 实施优先级

### Phase 1 (高优先级) - 后端基础
1. ✅ 已完成：回调机制和主键创建功能
2. **待实现**: 日志增强（添加 emoji 和"[后处理]"标记）
3. **待实现**: TaskService 确保 config 包含显式主键配置

### Phase 2 (中优先级) - 前端展示
1. **待实现**: 字段映射 UI 显示主键标记
2. **待实现**: 任务配置表单添加后处理选项
3. **待实现**: 任务详情页显示后处理配置

### Phase 3 (中优先级) - 日志优化
1. **待实现**: 执行日志高亮后处理相关内容
2. **待实现**: 后处理摘要卡片

### Phase 4 (低优先级) - API 优化
1. **待实现**: 源表 Schema 查询 API（用于获取主键信息）

---

## ✅ 验证标准

### 用户故事 1: 字段映射时识别主键
**场景**: 用户在创建导入任务，配置字段映射
**验证**:
- [ ] 源字段列显示 🔑 PK 标记（在主键字段旁边）
- [ ] 表格上方显示主键映射关系提示
- [ ] 如果主键未映射，显示警告提示

### 用户故事 2: 查看任务配置
**场景**: 用户在任务详情页查看配置
**验证**:
- [ ] config.target 包含 `create_primary_key: true`
- [ ] config.target 包含 `force_primary_key: []`（空数组表示自动检测）
- [ ] config.target 包含 `primary_key_name: ""`（空字符串表示自动生成）
- [ ] config.target 包含 `create_spatial_index: true`
- [ ] config.target 包含 `update_statistics: true`

### 用户故事 3: 执行日志排错
**场景**: 任务执行失败,用户查看日志排查原因
**验证**:
- [ ] 日志中包含 "🔑 [后处理] 准备创建主键"
- [ ] 日志中包含 "✅ [后处理] 主键创建成功"
- [ ] 日志中包含实际执行的 SQL 语句
- [ ] 如果失败,日志中有明确的错误原因（如"重复值"、"权限不足"等）

### 用户故事 4: 后处理结果一目了然
**场景**: 任务执行成功,用户想快速确认后处理是否执行
**验证**:
- [ ] 执行详情页顶部显示后处理摘要卡片
- [ ] 摘要显示: 主键创建 ✓、空间索引 2 个、统计更新 ✓
- [ ] 日志区域的后处理日志高亮显示（不同颜色）

---

## 📝 实施注意事项

1. **向后兼容**: 旧任务的 config 如果没有这些字段,后端要自动补充默认值
2. **性能**: 获取源表 Schema 时不要执行全表扫描,只读取元数据
3. **错误处理**: 主键创建失败不影响任务成功,但要在日志中清晰说明原因
4. **国际化**: emoji 和中文标记要考虑多语言支持（预留）

---

**文档版本**: v1.0
**最后更新**: 2026-01-29
**负责人**: Claude Code
