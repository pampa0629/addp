<template>
  <div class="operator-params-panel">
    <div v-if="!operator" class="empty-state">
      <el-empty :description="t('develop.operatorParams.selectNode')" :image-size="100" />
    </div>

    <div v-else class="params-form">
      <el-form :model="formData" label-width="120px" label-position="top">
        <!-- 节点基本信息 -->
        <section class="section">
          <h4 class="section-title">{{ t('develop.operatorParams.nodeInfo') }}</h4>
          <el-form-item :label="t('develop.operatorParams.operator')">
            <el-input :value="operator" disabled />
          </el-form-item>
          <el-form-item :label="t('develop.operatorParams.nodeId')">
            <el-input :value="nodeId" disabled />
          </el-form-item>
        </section>

        <!-- 参数配置 -->
        <section class="section">
          <h4 class="section-title">{{ t('develop.operatorParams.paramsConfig') }}</h4>

          <el-alert
            v-if="effectiveParameters.length === 0"
            type="info"
            :closable="false"
            show-icon
          >
            {{ t('develop.operatorParams.noParams') }}
          </el-alert>

          <div v-else>
            <template v-for="param in effectiveParameters" :key="param.name">
              <!-- 特殊处理：资源树选择器 -->
              <template v-if="param.ui_type === 'resource_tree_picker'">
                <div class="data-source-section">
                  <h4 class="subsection-title">{{ param.name || t('develop.operatorParams.dataSourceSelect') }}</h4>
                  <p v-if="param.description" class="subsection-description">
                    {{ param.description }}
                  </p>

                  <ResourceTreePicker
                    :api-base-url="param.ui_config?.api_base_url || '/api/v1/meta'"
                    :engine-types="param.ui_config?.engine_types || ['postgresql', 'mysql', 'doris', 'clickhouse']"
                    :engine-id="resourcePickerEngineId(param)"
                    :mode="resourcePickerMode(param)"
                    :node-filter="node => isResourcePickerVisibleNode(node, param)"
                    :selectable-filter="node => isResourcePickerNodeSelectable(node, param)"
                    :show-selection-summary="true"
                    :engine-multiple="!isTargetResourcePicker(param)"
                    :select-all-engines-by-default="!isTargetResourcePicker(param)"
                    :search-selectable-only="true"
                    :show-disabled-label="false"
                    :show-count="false"
                    :initial-locator="resourcePickerInitialLocator(param)"
                    @update:model-value="selection => handleResourceSelection(selection, param)"
                  />

                  <el-form-item
                    v-if="isTargetResourcePicker(param)"
                    :label="t('develop.operatorParams.targetTableName')"
                    class="target-name-field"
                  >
                    <el-input
                      v-model="formData.target_name"
                      :placeholder="t('develop.operatorParams.targetTableNamePlaceholder')"
                    />
                  </el-form-item>

                  <div v-if="param.notes" class="help-text" style="margin-top: 12px">
                    <el-icon style="margin-right: 4px"><InfoFilled /></el-icon>
                    {{ param.notes }}
                  </div>
                </div>
              </template>

              <!-- 特殊处理：NFS 文件选择器 -->
              <template v-else-if="param.ui_type === 'nfs_file_picker'">
                <div class="data-source-section">
                  <h4 class="subsection-title">{{ param.name || 'NFS 文件' }}</h4>
                  <p v-if="param.description" class="subsection-description">{{ param.description }}</p>
                  <NfsFilePicker
                    :engine-id="formData.engine_id"
                    :path="formData.path"
                    @change="handleNfsFileSelection"
                  />
                  <div v-if="param.notes" class="help-text" style="margin-top: 8px">
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
          <el-button type="primary" @click="saveParams">{{ t('develop.operatorParams.saveParams') }}</el-button>
          <el-button @click="resetParams">{{ t('develop.operatorParams.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { QuestionFilled, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import NfsFilePicker from './NfsFilePicker.vue'
import { parseLocatorSafe, ResourceTreePicker } from '@addp/common-frontend'

const { t } = useI18n()

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

  // 检查过滤后的参数列表中是否有可见的 resource_tree_picker
  const hasVisibleResourceTreePicker = params.some(p => p.ui_type === 'resource_tree_picker')
  if (hasVisibleResourceTreePicker) {
    // 隐藏被资源树选择器自动填充的参数
    const autoFilledParams = ['locator', 'target_parent_locator', 'target_name', 'engine_id', 'schema', 'table']
    params = params.filter(p => !autoFilledParams.includes(p.name))
  }

  // 检查是否有可见的 nfs_file_picker
  const hasVisibleNfsFilePicker = params.some(p => p.ui_type === 'nfs_file_picker')
  if (hasVisibleNfsFilePicker) {
    params = params.filter(p => !['engine_id', 'path'].includes(p.name))
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
  // 1. 检查 enum (下拉选择)
  if (param.enum && param.enum.length > 0) return 'el-select'

  // 2. 根据 type 选择基础组件
  if (param.type === 'integer' || param.type === 'float' || param.type === 'number') {
    return 'el-input-number'
  }
  if (param.type === 'boolean') return 'el-switch'

  // 3. 默认文本输入
  return 'el-input'
}

// 获取组件的 props
const getComponentProps = (param) => {
  const props = {}

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
    return t('develop.operatorParams.defaultValue', { value: param.default })
  }
  return t('develop.operatorParams.inputPlaceholder', { name: param.name })
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

const resourcePickerMode = (param) => {
  return isTargetResourcePicker(param) ? 'node' : 'item'
}

const isTargetResourcePicker = (param) => {
  return props.operator === 'save' || param.ui_config?.allow_create_table === true
}

const resourcePickerInitialLocator = (param) => {
  return isTargetResourcePicker(param)
    ? (formData.value.target_parent_locator || '')
    : (formData.value.locator || '')
}

const resourcePickerEngineId = (param) => {
  return isTargetResourcePicker(param) ? formData.value.engine_id : null
}

const isResourcePickerNodeSelectable = (node, param) => {
  if (isTargetResourcePicker(param)) {
    const selectableTypes = param.ui_config?.selectable_parent_node_types || ['schema', 'database']
    return selectableTypes.includes(node?.type)
  }
  const selectableTypes = param.ui_config?.selectable_node_types || ['table']
  return selectableTypes.includes(node?.type)
}

const isResourcePickerVisibleNode = (node, param) => {
  if (isTargetResourcePicker(param)) {
    return ['engine', 'schema', 'database', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service', 'server', 'table', 'object', 'file'].includes(String(node?.type || '').toLowerCase())
  }
  return ['engine', 'schema', 'database', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service', 'server', 'table'].includes(String(node?.type || '').toLowerCase())
}

// 处理资源树选择器的选择结果
const handleResourceSelection = (selection, param) => {
  if (!selection) {
    // 清空数据源相关参数
    formData.value.source_type = null
    formData.value.target_type = null
    formData.value.locator = null
    formData.value.target_parent_locator = null
    formData.value.target_name = null
    return
  }

  const locator = selection.identity?.locator

  // 根据算子类型设置不同的字段
  // load 算子使用 source_type，save 算子使用 target_type
  if (isTargetResourcePicker(param)) {
    formData.value.target_type = 'table'
    formData.value.target_parent_locator = locator
    formData.value.locator = null
    if (!formData.value.target_name) {
      formData.value.target_name = ''
    }
    // 设置默认的 mode
    if (!formData.value.mode) {
      formData.value.mode = 'replace'
    }
  } else {
    // 默认情况（load 算子）
    formData.value.source_type = 'table'
    formData.value.locator = locator
    formData.value.target_parent_locator = null
    formData.value.target_name = null
  }

  console.log('[OperatorParamsPanel] 数据源选择:', selection)
  ElMessage.success(t('develop.operatorParams.dataSourceSelected', { name: selection.display?.label || resourceLabelFromLocator(locator) }))
}

const resourceLabelFromLocator = (locator) => {
  const parsed = parseLocatorSafe(locator)
  return parsed.path?.[parsed.path.length - 1] || locator || ''
}

// 处理 NFS 文件选择器的选择结果
const handleNfsFileSelection = ({ engineId, path, format }) => {
  formData.value.engine_id = engineId
  formData.value.path = path
  if (format) {
    formData.value.format = format
  }
}

// 保存参数
const saveParams = () => {
  // 验证必填参数
  for (const param of effectiveParameters.value) {
    if (param.required && !formData.value[param.name]) {
      ElMessage.warning(t('develop.operatorParams.requiredParam', { name: param.name }))
      return
    }
  }

  const allParams = props.parameters && props.parameters.length > 0 ? props.parameters : []
  if (allParams.some(p => p.ui_type === 'resource_tree_picker')) {
    if (formData.value.target_type === 'table') {
      if (!formData.value.target_parent_locator) {
        ElMessage.warning(t('develop.operatorParams.requiredParam', { name: 'target_parent_locator' }))
        return
      }
      if (!formData.value.target_name) {
        ElMessage.warning(t('develop.operatorParams.requiredParam', { name: 'target_name' }))
        return
      }
    } else if (formData.value.source_type === 'table' && !formData.value.locator) {
      ElMessage.warning(t('develop.operatorParams.requiredParam', { name: 'locator' }))
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

  // 补充被 effectiveParameters 过滤掉的特殊 UI 组件自动填充字段
  if (allParams.some(p => p.ui_type === 'nfs_file_picker')) {
    if (formData.value.engine_id != null) cleanedParams.engine_id = formData.value.engine_id
    if (formData.value.path) cleanedParams.path = formData.value.path
  }
  if (allParams.some(p => p.ui_type === 'resource_tree_picker')) {
    if (formData.value.source_type === 'table' && formData.value.locator) {
      cleanedParams.locator = formData.value.locator
    }
    if (formData.value.target_type === 'table') {
      if (formData.value.target_parent_locator) cleanedParams.target_parent_locator = formData.value.target_parent_locator
      if (formData.value.target_name) cleanedParams.target_name = formData.value.target_name
    }
  }

  emit('save', {
    nodeId: props.nodeId,
    params: cleanedParams
  })

  ElMessage.success(t('develop.operatorParams.saveSuccess'))
}

// 重置参数
const resetParams = () => {
  formData.value = { ...props.initialParams }
  ElMessage.info(t('develop.operatorParams.resetSuccess'))
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
