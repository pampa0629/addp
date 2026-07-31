<template>
  <div ref="panelRef" class="operator-params-panel">
    <div v-if="!operator" class="empty-state">
      <el-empty :description="t('develop.operatorParams.selectNode')" :image-size="100" />
    </div>

    <div v-else class="params-form">
      <el-form :model="formData" label-width="120px" label-position="top">
        <!-- 节点基本信息 -->
        <section class="section">
          <div class="section-heading">
            <h4 class="section-title">{{ t('develop.operatorParams.nodeInfo') }}</h4>
            <el-tag size="small" type="info">{{ t('develop.operatorParams.readOnly') }}</el-tag>
          </div>
          <dl class="node-summary">
            <div class="node-summary-row">
              <dt>{{ t('develop.operatorParams.operator') }}</dt>
              <dd>{{ operator }}</dd>
            </div>
            <div class="node-summary-row">
              <dt>{{ t('develop.operatorParams.nodeId') }}</dt>
              <dd>{{ nodeId }}</dd>
            </div>
          </dl>
        </section>

        <section v-if="inputParameters.length" class="section">
          <div class="section-heading">
            <h4 class="section-title">{{ t('develop.operatorParams.inputConnections') }}</h4>
          </div>
          <el-form-item
            v-for="param in inputParameters"
            :key="param.name"
            :label="param.name"
            class="input-connection-field"
          >
            <el-select
              :model-value="inputConnectionKey(param.name)"
              clearable
              filterable
              :placeholder="inputConnectionPlaceholder(param.name)"
              @change="value => changeInputConnection(param.name, value)"
            >
              <el-option
                v-for="option in inputConnectionOptionsFor(param.name)"
                :key="option.key"
                :label="inputConnectionOptionLabel(option)"
                :value="option.key"
                :disabled="option.disabled"
              />
            </el-select>
            <div v-if="param.description" class="field-hint">{{ param.description }}</div>
          </el-form-item>
        </section>

        <!-- 参数配置 -->
        <section class="section">
          <div class="section-heading">
            <h4 class="section-title">{{ t('develop.operatorParams.paramsConfig') }}</h4>
            <el-tag size="small" type="primary">{{ t('develop.operatorParams.editable') }}</el-tag>
          </div>

          <el-alert
            v-if="effectiveParameters.length === 0"
            type="info"
            :closable="false"
            show-icon
          >
            {{ inputParameters.length
              ? t('develop.operatorParams.noOtherParams')
              : t('develop.operatorParams.noParams') }}
          </el-alert>

          <div v-else>
            <template v-for="param in effectiveParameters" :key="param.name">
              <!-- 特殊处理：资源树选择器 -->
              <template v-if="param.ui_type === 'resource_tree_picker'">
                <div
                  class="data-source-section param-field"
                  :class="{ 'is-validation-target': highlightedParamName === param.name }"
                  :data-param-name="param.name"
                  :aria-invalid="resourceValidationMessages(param).length || highlightedParamName === param.name ? 'true' : undefined"
                >
                  <h4 class="subsection-title">{{ param.name || t('develop.operatorParams.dataSourceSelect') }}</h4>
                  <p v-if="param.description" class="subsection-description">
                    {{ param.description }}
                  </p>

                  <div class="resource-selection-card">
                    <div class="resource-selection-content">
                      <span class="resource-selection-label">{{ t('develop.operatorParams.selectedResource') }}</span>
                      <span class="resource-selection-value" :title="resourcePickerCurrentLocator(param)">
                        {{ resourcePickerCurrentLabel(param) || t('develop.operatorParams.noResourceSelected') }}
                      </span>
                    </div>
                    <el-button type="primary" plain @click="openResourcePicker(param)">
                      {{ resourcePickerCurrentLocator(param) ? t('develop.operatorParams.changeResource') : t('develop.operatorParams.selectResource') }}
                    </el-button>
                  </div>
                  <ul
                    v-if="resourceValidationMessages(param).length"
                    class="resource-validation-errors"
                    role="alert"
                  >
                    <li v-for="message in resourceValidationMessages(param)" :key="message">
                      {{ message }}
                    </li>
                  </ul>

                  <el-form-item
                    v-if="resourceGeometryColumns(param).length > 0"
                    :label="t('develop.operatorParams.geometryColumn')"
                    class="geometry-column-field param-field"
                    :class="{ 'is-validation-target': highlightedParamName === resourceBindingGeometryColumnParam(param) }"
                    :data-param-name="resourceBindingGeometryColumnParam(param)"
                    :aria-invalid="validationMessageFor(resourceBindingGeometryColumnParam(param)) || highlightedParamName === resourceBindingGeometryColumnParam(param) ? 'true' : undefined"
                    :error="validationMessageFor(resourceBindingGeometryColumnParam(param))"
                  >
                    <el-select
                      v-model="formData[resourceBindingGeometryColumnParam(param)]"
                      :disabled="resourceGeometryColumns(param).length === 1"
                    >
                      <el-option
                        v-for="column in resourceGeometryColumns(param)"
                        :key="column"
                        :label="column"
                        :value="column"
                      />
                    </el-select>
                    <div class="field-hint">
                      {{ resourceGeometryColumns(param).length === 1
                        ? t('develop.operatorParams.geometryColumnDetected')
                        : t('develop.operatorParams.geometryColumnMultiple') }}
                    </div>
                  </el-form-item>

                  <el-form-item
                    v-if="isTargetResourcePicker(param)"
                    :label="targetNameLabel(param)"
                    class="target-name-field param-field"
                    :class="{ 'is-validation-target': highlightedParamName === resourceBindingNameParam(param) }"
                    :data-param-name="resourceBindingNameParam(param)"
                    :aria-invalid="validationMessageFor(resourceBindingNameParam(param)) || highlightedParamName === resourceBindingNameParam(param) ? 'true' : undefined"
                    :error="validationMessageFor(resourceBindingNameParam(param))"
                  >
                    <el-input
                      v-model="formData[resourceBindingNameParam(param)]"
                      :placeholder="targetNamePlaceholder(param)"
                    />
                  </el-form-item>

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
                class="param-field"
                :class="{ 'is-validation-target': highlightedParamName === param.name }"
                :data-param-name="param.name"
                :aria-invalid="validationMessageFor(param.name) || highlightedParamName === param.name ? 'true' : undefined"
                :error="validationMessageFor(param.name)"
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

        <!-- 草稿操作 -->
        <el-form-item>
          <el-button @click="resetParams">{{ t('develop.operatorParams.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </div>

    <el-dialog
      v-model="resourcePickerDialogVisible"
      :title="resourcePickerDialogTitle"
      width="min(760px, calc(100vw - 24px))"
      append-to-body
      destroy-on-close
      class="addp-dialog workflow-resource-picker-dialog"
      @opened="focusResourcePicker"
    >
      <ResourceTreePicker
        v-if="resourcePickerDialogParam"
        ref="resourcePickerRef"
        :api-base-url="resourcePickerDialogParam.ui_config?.api_base_url || '/api/v1/meta'"
        :engine-families="resourcePickerEngineFamilies(resourcePickerDialogParam)"
        :engine-id="resourcePickerEngineId(resourcePickerDialogParam)"
        :mode="resourcePickerMode(resourcePickerDialogParam)"
        :node-filter="node => isResourcePickerVisibleNode(node, resourcePickerDialogParam)"
        :selectable-filter="node => isResourcePickerNodeSelectable(node, resourcePickerDialogParam)"
        :show-selection-summary="true"
        :engine-multiple="!isTargetResourcePicker(resourcePickerDialogParam)"
        :select-all-engines-by-default="!isTargetResourcePicker(resourcePickerDialogParam)"
        :search-selectable-only="true"
        :show-disabled-label="false"
        :show-count="false"
        tree-height="clamp(240px, 44vh, 420px)"
        :initial-locator="resourcePickerInitialLocator(resourcePickerDialogParam)"
        @update:model-value="selection => resourcePickerDraftSelection = selection"
      />
      <template #footer>
        <el-button @click="resourcePickerDialogVisible = false">{{ t('develop.operatorParams.cancel') }}</el-button>
        <el-button type="primary" :disabled="!resourcePickerDraftSelection" @click="confirmResourceSelection">
          {{ t('develop.operatorParams.confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, watch, computed, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { QuestionFilled, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { parseLocatorSafe, ResourceTreePicker } from '@addp/common-frontend'
import { isWorkflowInputParameter } from '@/utils/workflowInputBindings'
import { validationMessagesForParams } from '@/utils/workflowValidationIssues'
import {
  applyResourceBindingSelection,
  clearResourceBindingSelection,
  collectResourceBindingParams,
  geometryColumnFactsFromSelection,
  getResourceBinding,
  isResourceDataTypeSupported,
  isResourceFormatSupported,
  isTargetResourceBinding,
  resourceBindingInitialLocator,
  resourceBindingGeometryColumnParam,
  resourceBindingNameParam,
  resourceBindingTargetExtension,
  resourceBindingTargetNameKind
} from '@/utils/workflowResourceBindings'

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
  publicParameters: {
    type: Array,
    default: () => []
  },
  initialParams: {
    type: Object,
    default: () => ({})
  },
  validationIssues: {
    type: Array,
    default: () => []
  },
  inputConnections: {
    type: Array,
    default: () => []
  },
  inputConnectionOptions: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update', 'update-connection'])

const formData = ref({})
const panelRef = ref(null)
const resourcePickerRef = ref(null)
const highlightedParamName = ref('')
const resourcePickerDialogVisible = ref(false)
const resourcePickerDialogParam = ref(null)
const resourcePickerDraftSelection = ref(null)
const resourcePickerDialogTitle = computed(() => {
  const name = resourcePickerDialogParam.value?.name
  return name
    ? t('develop.operatorParams.resourceDialogTitle', { name })
    : t('develop.operatorParams.dataSourceSelect')
})
const resourceGeometryColumnsByParam = ref({})
let syncingFromProps = false
let highlightTimer = null

const inputParameters = computed(() => props.publicParameters.filter(isWorkflowInputParameter))

// 使用工作流引擎返回的结构化参数定义。
const effectiveParameters = computed(() => {
  let params = props.publicParameters

  // 过滤掉由工作流连线自动传递的输入参数。
  params = params.filter(p => !isWorkflowInputParameter(p))
  // ResourceLocator 身份字段由资源选择器维护，不渲染为普通文本框。
  params = params.filter(p => p.param_type !== 'resource')
  const managedParams = new Set(props.publicParameters
    .filter(p => p.ui_type === 'resource_tree_picker')
    .map(resourceBindingGeometryColumnParam)
    .filter(Boolean))
  params = params.filter(p => !managedParams.has(p.name))

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

function inputConnectionOptionsFor(targetParam) {
  return props.inputConnectionOptions.filter(option => option.targetParam === targetParam)
}

function inputConnectionKey(targetParam) {
  const connection = props.inputConnections.find(item => item.targetParam === targetParam)
  return connection ? JSON.stringify([connection.sourceId, connection.sourcePort || 'default']) : ''
}

function inputConnectionOptionLabel(option) {
  const nodeLabel = option.sourceLabel === option.sourceId
    ? option.sourceLabel
    : `${option.sourceLabel} (${option.sourceId})`
  return option.sourcePort && option.sourcePort !== 'default'
    ? `${nodeLabel} / ${option.sourcePortLabel || option.sourcePort}`
    : nodeLabel
}

function inputConnectionPlaceholder(targetParam) {
  return inputConnectionOptionsFor(targetParam).length
    ? t('develop.operatorParams.selectInputConnection')
    : t('develop.operatorParams.noCompatibleOutputs')
}

function changeInputConnection(targetParam, optionKey) {
  const option = props.inputConnectionOptions.find(item => item.key === optionKey && item.targetParam === targetParam)
  emit('update-connection', {
    nodeId: props.nodeId,
    targetParam,
    sourceId: option?.sourceId || '',
    sourcePort: option?.sourcePort || 'default'
  })
}

// 监听 initialParams 变化,初始化表单
watch(() => props.initialParams, (newParams) => {
  syncingFromProps = true
  formData.value = { ...newParams }
  nextTick(() => {
    syncingFromProps = false
  })
}, { immediate: true, deep: true })

// 监听 operator 变化,重置表单
watch(() => props.operator, () => {
  clearParamHighlight()
  syncingFromProps = true
  formData.value = { ...props.initialParams }
  nextTick(() => {
    syncingFromProps = false
  })
}, { deep: true })

watch(() => props.nodeId, clearParamHighlight)

watch(formData, () => {
  if (!syncingFromProps) {
    emit('update', {
      nodeId: props.nodeId,
      params: buildDraftParams()
    })
  }
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

const resourcePickerEngineFamilies = (param) => {
  if (Array.isArray(param.ui_config?.engine_families)) {
    return param.ui_config.engine_families
  }
  const selectableTypes = [
    ...(param.ui_config?.selectable_node_types || []),
    ...(param.ui_config?.selectable_parent_node_types || [])
  ].map(type => String(type).toLowerCase())
  if (selectableTypes.some(type => ['file', 'object', 'root', 'directory', 'dir', 'bucket', 'prefix'].includes(type))) {
    return ['file', 'object']
  }
  return ['tabular', 'dynamic_schema']
}

const isTargetResourcePicker = (param) => {
  return isTargetResourceBinding(param)
}

const resourcePickerInitialLocator = (param) => {
  return resourceBindingInitialLocator(param, formData.value)
}

const resourcePickerEngineId = (param) => {
  return null
}

const isResourcePickerNodeSelectable = (node, param) => {
  if (isTargetResourcePicker(param)) {
    const selectableTypes = param.ui_config?.selectable_parent_node_types || ['schema', 'database']
    return selectableTypes.includes(node?.type)
  }
  const selectableTypes = param.ui_config?.selectable_node_types || ['table']
  return selectableTypes.includes(node?.type) && isResourceFormatSupported(param, node) && isResourceDataTypeSupported(param, node)
}

const isResourcePickerVisibleNode = (node, param) => {
  if (isTargetResourcePicker(param)) {
    return ['engine', 'schema', 'database', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service', 'server', 'table', 'object', 'file'].includes(String(node?.type || '').toLowerCase())
  }
  const nodeType = String(node?.type || '').toLowerCase()
  const visible = ['engine', 'schema', 'database', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service', 'server', 'table', 'collection', 'object', 'file'].includes(nodeType)
  return visible && isResourceFormatSupported(param, node) && isResourceDataTypeSupported(param, node)
}

const targetNameLabel = (param) => resourceBindingTargetNameKind(param) === 'dataset'
  ? t('develop.operatorParams.targetDatasetName')
  : t('develop.operatorParams.targetFileName')

const targetNamePlaceholder = (param) => {
  const extension = resourceBindingTargetExtension(param)
  return extension
    ? t('develop.operatorParams.targetFileNameWithExtensionPlaceholder', { extension })
    : t('develop.operatorParams.targetDatasetNamePlaceholder')
}

const resourcePickerCurrentLocator = (param) => resourcePickerInitialLocator(param)

const resourcePickerCurrentLabel = (param) => resourceLabelFromLocator(resourcePickerCurrentLocator(param))

const resourceGeometryColumns = (param) => {
  const detected = resourceGeometryColumnsByParam.value[param.name] || []
  if (detected.length > 0) return detected
  const geometryParam = resourceBindingGeometryColumnParam(param)
  const savedValue = geometryParam ? formData.value[geometryParam] : ''
  return savedValue ? [savedValue] : []
}

const openResourcePicker = (param) => {
  resourcePickerDialogParam.value = param
  resourcePickerDraftSelection.value = null
  resourcePickerDialogVisible.value = true
}

const focusResourcePicker = () => {
  resourcePickerRef.value?.focus?.()
}

const confirmResourceSelection = () => {
  if (!resourcePickerDialogParam.value || !resourcePickerDraftSelection.value) return
  const geometryParam = resourceBindingGeometryColumnParam(resourcePickerDialogParam.value)
  if (geometryParam) {
    const facts = geometryColumnFactsFromSelection(resourcePickerDraftSelection.value)
    resourceGeometryColumnsByParam.value = {
      ...resourceGeometryColumnsByParam.value,
      [resourcePickerDialogParam.value.name]: facts.columns
    }
    formData.value[geometryParam] = facts.selected || null
  }
  handleResourceSelection(resourcePickerDraftSelection.value, resourcePickerDialogParam.value)
  resourcePickerDialogVisible.value = false
}

// 处理资源树选择器的选择结果
const handleResourceSelection = (selection, param) => {
  if (!selection) {
    formData.value = clearResourceBindingSelection(param, formData.value)
    return
  }

  const locator = selection.identity?.locator
  const resourceType = parseLocatorSafe(locator).type || selection.type
  formData.value = applyResourceBindingSelection(param, formData.value, locator, resourceType)

  console.log('[OperatorParamsPanel] 数据源选择:', selection)
  ElMessage.success(t('develop.operatorParams.dataSourceSelected', { name: selection.display?.label || resourceLabelFromLocator(locator) }))
}

const resourceLabelFromLocator = (locator) => {
  const parsed = parseLocatorSafe(locator)
  return parsed.path?.[parsed.path.length - 1] || locator || ''
}

const buildDraftParams = () => {
  const visibleResourceParameters = effectiveParameters.value.filter(param => param.ui_type === 'resource_tree_picker')
  const cleanedParams = {}

  effectiveParameters.value.forEach(param => {
    if (param.type === 'ui' || param.type === 'input' || param.param_type === 'input' || param.param_type === 'resource') {
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

  Object.assign(cleanedParams, collectResourceBindingParams(visibleResourceParameters, formData.value))
  return cleanedParams
}

// 重置参数
const resetParams = () => {
  formData.value = { ...props.initialParams }
  ElMessage.info(t('develop.operatorParams.resetSuccess'))
}

async function focusParam(paramName) {
  if (!paramName) return false
  await nextTick()

  let targetName = paramName
  let target = findParamElement(targetName)
  if (!target) {
    const resourceParameter = effectiveParameters.value.find(param => (
      param.ui_type === 'resource_tree_picker' && resourceBindingFieldNames(param).includes(paramName)
    ))
    targetName = resourceParameter?.name || ''
    target = findParamElement(targetName)
  }
  if (!target) return false

  highlightedParamName.value = targetName
  await nextTick()
  target.scrollIntoView({ behavior: 'smooth', block: 'center' })
  const focusable = target.querySelector(
    'input:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])'
  )
  focusable?.focus({ preventScroll: true })

  clearTimeout(highlightTimer)
  highlightTimer = setTimeout(() => {
    if (highlightedParamName.value === targetName) highlightedParamName.value = ''
  }, 2400)
  return true
}

function findParamElement(paramName) {
  if (!paramName || !panelRef.value) return null
  return [...panelRef.value.querySelectorAll('[data-param-name]')]
    .find(element => element.dataset.paramName === paramName) || null
}

function resourceBindingFieldNames(param) {
  const binding = getResourceBinding(param) || {}
  return [
    binding.locator_param,
    binding.parent_locator_param,
    binding.name_param,
    binding.type_param,
    binding.geometry_column_param
  ].filter(Boolean)
}

function validationMessageFor(paramName) {
  return validationMessagesForParams(props.validationIssues, [paramName])[0] || ''
}

function resourceValidationMessages(param) {
  const binding = getResourceBinding(param) || {}
  const fieldNames = [
    param.name,
    binding.locator_param,
    binding.parent_locator_param,
    binding.type_param
  ]
  if (resourceGeometryColumns(param).length === 0) fieldNames.push(binding.geometry_column_param)
  return validationMessagesForParams(props.validationIssues, fieldNames)
}

function clearParamHighlight() {
  clearTimeout(highlightTimer)
  highlightedParamName.value = ''
}

onBeforeUnmount(clearParamHighlight)

defineExpose({ focusParam })
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

.param-field.is-validation-target {
  outline: 2px solid var(--el-color-danger);
  outline-offset: 3px;
  border-radius: 4px;
}

.resource-validation-errors {
  margin: 8px 0 0;
  padding: 0;
  color: var(--el-color-danger);
  font-size: 12px;
  line-height: 1.5;
  list-style: none;
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
  margin: 0;
  display: flex;
  align-items: center;
}

.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.node-summary {
  margin: 0;
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.node-summary-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 12px;
  padding: 6px 0;
}

.node-summary-row dt {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.node-summary-row dd {
  margin: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.geometry-column-field {
  margin-top: 16px;
}

.geometry-column-field :deep(.el-select) {
  width: 100%;
}

.input-connection-field :deep(.el-select) {
  width: 100%;
}

.field-hint {
  margin-top: 6px;
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 1.5;
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

.resource-selection-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-primary);
}

.resource-selection-content {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.resource-selection-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.resource-selection-value {
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
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
