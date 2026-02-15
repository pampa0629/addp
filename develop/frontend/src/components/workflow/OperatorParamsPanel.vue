<template>
  <div class="operator-params-panel">
    <div v-if="!operator" class="empty-state">
      <el-empty description="请从画布中选择一个节点" :image-size="100" />
    </div>

    <div v-else class="params-form">
      <el-form :model="formData" label-width="120px" label-position="top">
        <!-- 节点基本信息 -->
        <section class="section">
          <h4 class="section-title">节点信息</h4>
          <el-form-item label="算子">
            <el-input :value="operator" disabled />
          </el-form-item>
          <el-form-item label="节点ID">
            <el-input :value="nodeId" disabled />
          </el-form-item>
        </section>

        <!-- 参数配置 -->
        <section class="section">
          <h4 class="section-title">参数配置</h4>

          <el-alert
            v-if="effectiveParameters.length === 0"
            type="info"
            :closable="false"
            show-icon
          >
            该算子无需配置参数
          </el-alert>

          <div v-else>
            <template v-for="param in effectiveParameters" :key="param.name">
              <!-- 特殊处理：数据源级联选择器 -->
              <template v-if="param.ui_type === 'data_source_cascader'">
                <div class="data-source-section">
                  <h4 class="subsection-title">{{ param.name || '数据源选择' }}</h4>
                  <p v-if="param.description" class="subsection-description">
                    {{ param.description }}
                  </p>

                  <DataSourceCascader
                    :api-base-url="param.ui_config?.api_base_url || '/api/meta'"
                    :engine-types="param.ui_config?.engine_types || ['postgresql', 'mysql', 'doris', 'clickhouse']"
                    :selectable-node-types="param.ui_config?.selectable_node_types || ['table']"
                    :enable-geometry-detection="param.ui_config?.enable_geometry_detection !== false"
                    :require-geometry="param.ui_config?.require_geometry || false"
                    :allow-create-table="param.ui_config?.allow_create_table || false"
                    :show-selection-info="true"
                    :initial-selection="{ engine_id: formData.engine_id, schema: formData.schema, table: formData.table }"
                    @update:selection="handleDataSourceSelection"
                  />

                  <div v-if="param.notes" class="help-text" style="margin-top: 12px">
                    <el-icon style="margin-right: 4px"><InfoFilled /></el-icon>
                    {{ param.notes }}
                  </div>
                </div>
              </template>

              <!-- 常规参数渲染 -->
              <el-form-item
                v-else
                :label="param.name"
                :required="param.required"
              >
                <template #label>
                  <div class="param-label">
                    <span>{{ param.name }}</span>
                    <el-tooltip v-if="param.description" placement="top">
                      <template #content>{{ param.description }}</template>
                      <el-icon class="help-icon"><QuestionFilled /></el-icon>
                    </el-tooltip>
                  </div>
                </template>

                <!-- 根据参数类型渲染不同的输入组件 -->
                <component
                  :is="getComponentByType(param)"
                  v-model="formData[param.name]"
                  v-bind="getComponentProps(param)"
                  :placeholder="getPlaceholder(param)"
                  @change="onParamChange(param.name)"
                >
                  <!-- 渲染 enum 下拉选项 -->
                  <template v-if="param.enum && param.enum.length > 0">
                    <el-option
                      v-for="option in param.enum"
                      :key="option"
                      :label="option"
                      :value="option"
                    />
                  </template>
                </component>
              </el-form-item>
            </template>
          </div>
        </section>

        <!-- 保存按钮 -->
        <el-form-item>
          <el-button type="primary" @click="saveParams">保存参数</el-button>
          <el-button @click="resetParams">重置</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { QuestionFilled, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import EngineSelect from './EngineSelect.vue'
import SchemaSelect from './SchemaSelect.vue'
import TableSelect from './TableSelect.vue'
import { DataSourceCascader } from '@addp/common-frontend'

const props = defineProps({
  nodeId: {
    type: String,
    default: ''
  },
  operator: {
    type: String,
    default: ''
  },
  paramDefinitions: {
    type: Object,
    default: () => ({})
  },
  parameters: {
    type: Array,
    default: () => []
  },
  initialParams: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['save'])

const formData = ref({})

// 使用结构化参数定义(优先)或回退到旧格式
const effectiveParameters = computed(() => {
  let params = []

  if (props.parameters && props.parameters.length > 0) {
    params = props.parameters
  } else {
    // 向后兼容:将旧的 paramDefinitions 转换为参数数组
    params = Object.entries(props.paramDefinitions).map(([name, desc]) => ({
      name,
      type: 'string',
      description: desc,
      required: false
    }))
  }

  // 过滤掉 input 类型的参数（这些参数通过连接线自动传递）
  // 识别方法：1）参数名为data/input_df/input_gdf  2）数据类型为object/GeoDataFrame/DataFrame
  params = params.filter(p => {
    const inputParamNames = ['data', 'input_df', 'input_gdf', 'input']
    const inputDataTypes = ['object', 'GeoDataFrame', 'DataFrame']

    // 如果参数名是典型的input参数名，且数据类型也是表格类型，则过滤掉
    if (inputParamNames.includes(p.name) && inputDataTypes.includes(p.type)) {
      return false
    }

    // 如果有param_type字段，也使用它判断
    if (p.param_type === 'input') {
      return false
    }

    return true
  })

  // 根据 show_when 条件过滤参数
  params = params.filter(p => {
    // 没有 show_when 条件的参数继续检查
    if (p.show_when) {
      // 检查 show_when 条件
      for (const [dependParam, expectedValue] of Object.entries(p.show_when)) {
        const currentValue = formData.value[dependParam]

        // 如果期望值是数组，检查当前值是否在数组中
        if (Array.isArray(expectedValue)) {
          if (!expectedValue.includes(currentValue)) {
            return false
          }
        } else {
          // 单个值比较
          if (currentValue !== expectedValue) {
            return false
          }
        }
      }
    }

    return true
  })

  // 检查过滤后的参数列表中是否有可见的 data_source_cascader
  const hasVisibleDataSourceCascader = params.some(p => p.ui_type === 'data_source_cascader')
  if (hasVisibleDataSourceCascader) {
    // 隐藏被 DataSourceCascader 自动填充的参数
    const autoFilledParams = ['engine_id', 'schema', 'table']
    params = params.filter(p => !autoFilledParams.includes(p.name))
  }

  return params
})

// 构建参数依赖关系映射
const dependencyMap = computed(() => {
  const deps = {}
  effectiveParameters.value.forEach(param => {
    if (param.depends_on) {
      if (!deps[param.depends_on]) {
        deps[param.depends_on] = []
      }
      deps[param.depends_on].push(param.name)
    }
  })
  return deps
})

// 监听 initialParams 变化,初始化表单
watch(() => props.initialParams, (newParams) => {
  formData.value = { ...newParams }
}, { immediate: true, deep: true })

// 监听 operator 变化,重置表单
watch(() => props.operator, () => {
  formData.value = { ...props.initialParams }
}, { deep: true })

// 根据参数元数据选择组件类型
const getComponentByType = (param) => {
  // 1. 优先检查 ui_type (自定义 UI 组件)
  if (param.ui_type === 'engine_select') return EngineSelect
  if (param.ui_type === 'schema_select') return SchemaSelect
  if (param.ui_type === 'table_select') return TableSelect

  // 2. 检查 enum (下拉选择)
  if (param.enum && param.enum.length > 0) return 'el-select'

  // 3. 根据 type 选择基础组件
  if (param.type === 'integer' || param.type === 'float' || param.type === 'number') {
    return 'el-input-number'
  }
  if (param.type === 'boolean') return 'el-switch'

  // 4. 默认文本输入
  return 'el-input'
}

// 获取组件的 props
const getComponentProps = (param) => {
  const props = {}

  // 自定义组件的专用 props
  if (param.ui_type === 'engine_select') {
    props.engineTypes = param.engine_types || []
  } else if (param.ui_type === 'schema_select') {
    props.engineId = formData.value[param.depends_on] || null
  } else if (param.ui_type === 'table_select') {
    // table_select 依赖两个参数: engine_id 和 schema
    // 找到 schema 参数的 depends_on (通常是 engine_id)
    const schemaParam = effectiveParameters.value.find(p => p.name === param.depends_on)
    if (schemaParam && schemaParam.depends_on) {
      props.engineId = formData.value[schemaParam.depends_on] || null
    }
    props.schema = formData.value[param.depends_on] || null
  }

  // enum 类型的下拉选择
  if (param.enum && param.enum.length > 0) {
    // el-select 会在模板中自动渲染 options
  }

  // el-input-number 的 props
  if (param.type === 'integer') {
    props.step = 1
    props.precision = 0
  } else if (param.type === 'float' || param.type === 'number') {
    props.step = 0.01
    props.precision = 2
  }

  // 数值范围
  if (param.min !== undefined) props.min = param.min
  if (param.max !== undefined) props.max = param.max

  return props
}

// 获取 placeholder
const getPlaceholder = (param) => {
  if (param.default !== undefined && param.default !== null) {
    return `默认: ${param.default}`
  }
  return `请输入 ${param.name}`
}

// 参数变更处理(级联清空逻辑)
const onParamChange = (paramName) => {
  // 清空所有依赖此参数的子参数
  if (dependencyMap.value[paramName]) {
    dependencyMap.value[paramName].forEach(depParam => {
      formData.value[depParam] = null
    })
  }
}

// 处理数据源选择器的选择结果
const handleDataSourceSelection = (selection) => {
  if (!selection) {
    // 清空数据源相关参数
    formData.value.source_type = null
    formData.value.target_type = null
    formData.value.engine_id = null
    formData.value.schema = null
    formData.value.table = null
    return
  }

  // 根据算子类型设置不同的字段
  // load 算子使用 source_type，save 算子使用 target_type
  if (props.operator === 'save') {
    formData.value.target_type = 'table'
    formData.value.engine_id = selection.engineId
    formData.value.schema = selection.schema
    formData.value.table = selection.tableName
    // 设置默认的 mode
    if (!formData.value.mode) {
      formData.value.mode = 'replace'
    }
  } else {
    // 默认情况（load 算子）
    formData.value.source_type = 'table'
    formData.value.engine_id = selection.engineId
    formData.value.schema = selection.schema
    formData.value.table = selection.tableName
  }

  console.log('[OperatorParamsPanel] 数据源选择:', selection)
  ElMessage.success(`已选择: ${selection.fullName}`)
}

// 保存参数
const saveParams = () => {
  // 验证必填参数
  for (const param of effectiveParameters.value) {
    if (param.required && !formData.value[param.name]) {
      ElMessage.warning(`请填写必填参数: ${param.name}`)
      return
    }
  }

  // 过滤参数：只保存当前条件下应该显示的参数（已经过 show_when 过滤的）
  const cleanedParams = {}

  // 使用 effectiveParameters 而不是 props.parameters
  // effectiveParameters 已经根据 show_when 条件过滤了不应该显示的参数
  effectiveParameters.value.forEach(param => {
    // 跳过 UI 类型参数（这些参数已经在 effectiveParameters 中被隐藏了，但以防万一）
    if (param.type === 'ui' || param.type === 'input' || param.param_type === 'input') {
      return
    }

    const paramName = param.name
    const paramValue = formData.value[paramName]

    // 如果有值，使用实际值；否则使用默认值
    if (paramValue !== undefined && paramValue !== null && paramValue !== '') {
      cleanedParams[paramName] = paramValue
    } else if (param.default !== undefined && param.default !== null) {
      cleanedParams[paramName] = param.default
    }
  })

  console.log('[OperatorParamsPanel] 保存参数:', {
    原始参数: formData.value,
    清理后参数: cleanedParams,
    有效参数列表: effectiveParameters.value.map(p => p.name)
  })

  emit('save', {
    nodeId: props.nodeId,
    params: cleanedParams
  })

  ElMessage.success('参数已保存')
}

// 重置参数
const resetParams = () => {
  formData.value = { ...props.initialParams }
  ElMessage.info('已重置参数')
}
</script>

<style scoped>
.operator-params-panel {
  height: 100%;
  overflow-y: auto;
  padding: 16px;
}

.empty-state {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.params-form {
  height: 100%;
}

.section {
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--addp-border-color);
}

.section:last-child {
  border-bottom: none;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0 0 16px 0;
  display: flex;
  align-items: center;
}

.section-title::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 14px;
  background: var(--el-color-primary);
  margin-right: 8px;
  border-radius: 2px;
}

.param-label {
  display: flex;
  align-items: center;
  gap: 6px;
}

.help-icon {
  color: var(--addp-text-tertiary);
  cursor: help;
  font-size: 14px;
}

.help-icon:hover {
  color: var(--el-color-primary);
}

.data-source-section {
  margin-bottom: 24px;
  padding: 16px;
  background: var(--addp-bg-secondary);
  border-radius: 8px;
}

.subsection-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0 0 8px 0;
}

.subsection-description {
  font-size: 13px;
  color: var(--addp-text-secondary);
  margin: 0 0 16px 0;
  line-height: 1.5;
}

/* 自定义滚动条 */
.operator-params-panel::-webkit-scrollbar {
  width: 6px;
}

.operator-params-panel::-webkit-scrollbar-track {
  background: var(--addp-bg-secondary);
  border-radius: 3px;
}

.operator-params-panel::-webkit-scrollbar-thumb {
  background: var(--addp-border-secondary);
  border-radius: 3px;
}

.operator-params-panel::-webkit-scrollbar-thumb:hover {
  background: var(--addp-text-tertiary);
}
</style>
