<template>
  <el-form
    ref="formRef"
    :model="formState"
    :rules="computedRules"
    :label-width="labelWidth"
    :validate-on-rule-change="false"
  >
    <el-form-item
      v-if="showTypeSelector && effectiveTypeOptions.length"
      :label="t('storageEngine.type')"
      prop="engine_type"
    >
      <el-select
        v-model="formState.engine_type"
        :placeholder="t('storageEngine.typePlaceholder')"
        :disabled="isEdit && disableTypeChange"
        :validate-event="false"
        @change="handleTypeChange"
      >
        <el-option
          v-for="option in effectiveTypeOptions"
          :key="option.value"
          :label="option.label"
          :value="option.value"
        />
      </el-select>
    </el-form-item>

    <el-form-item :label="t('storageEngine.name')" prop="name">
      <el-input v-model="formState.name" :placeholder="t('storageEngine.namePlaceholder')" />
    </el-form-item>

    <template v-for="field in visibleConnectionFieldSpecs" :key="field.key">
      <el-divider v-if="field.showGroupDivider" content-position="left">{{ t(field.group_key) }}</el-divider>
      <el-form-item :label="fieldLabel(field)" :prop="`connection_info.${field.key}`">
        <el-input-number
          v-if="field.input === 'number'"
          v-model="formState.connection_info[field.key]"
          :min="field.min"
          :max="field.max"
        />
        <el-select v-else-if="field.input === 'select'" v-model="formState.connection_info[field.key]">
          <el-option
            v-for="option in field.options || []"
            :key="option.value"
            :label="option.label_key ? t(option.label_key) : option.label"
            :value="option.value"
          />
        </el-select>
        <el-switch v-else-if="field.input === 'boolean'" v-model="formState.connection_info[field.key]" />
        <el-input
          v-else
          v-model="formState.connection_info[field.key]"
          :type="field.input === 'textarea' ? 'textarea' : field.input === 'password' ? 'password' : 'text'"
          :rows="field.rows || undefined"
          :placeholder="field.placeholder_key ? t(field.placeholder_key) : field.placeholder"
          :show-password="field.input === 'password'"
        />
      </el-form-item>
      <div v-if="storedSensitiveFields.has(field.key)" class="field-hint">
        {{ t('storageEngine.sensitiveValueHint') }}
      </div>
      <div v-else-if="field.hint_key" class="field-hint">{{ t(field.hint_key) }}</div>
    </template>

    <!-- 描述 -->
    <el-form-item :label="t('storageEngine.description')" prop="description">
      <el-input
        v-model="formState.description"
        type="textarea"
        :rows="2"
        :placeholder="t('storageEngine.descPlaceholder')"
      />
    </el-form-item>

    <!-- 激活状态 -->
    <el-form-item v-if="showActiveSwitch" :label="t('storageEngine.activeStatus')">
      <el-switch
        v-model="formState.lifecycle_state"
        active-value="active"
        inactive-value="disabled"
      />
      <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
        {{ t('storageEngine.activeHint') }}
      </span>
    </el-form-item>

    <!-- 元数据扫描配置 -->
    <el-divider content-position="left">
      <span style="cursor: pointer; user-select: none;" @click="scanConfigExpanded = !scanConfigExpanded">
        {{ t('storageEngine.scanConfig') }}
        <el-icon style="margin-left: 4px;">
          <component :is="scanConfigExpanded ? 'ArrowDown' : 'ArrowRight'" />
        </el-icon>
      </span>
    </el-divider>

    <!-- 扫描配置内容（可折叠） -->
    <template v-if="scanConfigExpanded">
      <!-- 1. 立即扫描开关 -->
      <el-form-item :label="t('storageEngine.immediateScan')">
        <el-switch v-model="immediateScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          {{ t('storageEngine.immediateScanHint') }}
        </span>
      </el-form-item>

      <!-- 1.1 立即扫描深度配置（仅在立即扫描启用时显示） -->
      <template v-if="immediateScanEnabled">
        <el-form-item :label="t('storageEngine.immediateDepth')" style="margin-left: 30px;">
          <el-radio-group v-model="formState.scan_config.immediate_depth">
            <el-radio value="basic">{{ t('storageEngine.basicScan') }}</el-radio>
            <el-radio value="deep">{{ t('storageEngine.deepScan') }}</el-radio>
          </el-radio-group>
          <div class="field-hint" style="white-space: pre-line;">{{ t('storageEngine.scanDepthHint') }}</div>
        </el-form-item>
      </template>

      <!-- 2. 定时扫描开关 -->
      <el-form-item :label="t('storageEngine.scheduledScan')">
        <el-switch v-model="scheduledScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          {{ t('storageEngine.scheduledScanHint') }}
        </span>
      </el-form-item>

      <!-- 2.1 定时扫描详细配置（仅在开关打开时显示） -->
      <template v-if="scheduledScanEnabled">
        <el-form-item :label="t('storageEngine.scanFrequency')" style="margin-left: 30px;">
          <ScheduleConfig
            v-model="scheduledScanCron"
            :allow-custom-cron="false"
            compact-mode
          />
        </el-form-item>

        <!-- 2.2 定时扫描深度提示（固定深度扫描） -->
        <el-form-item :label="t('storageEngine.scanDepth')" style="margin-left: 30px;">
          <span style="color: var(--el-text-color-regular)">{{ t('storageEngine.deepScanFixed') }}</span>
          <div class="field-hint">{{ t('storageEngine.deepScanFixedHint') }}</div>
        </el-form-item>
      </template>
    </template>
  </el-form>
</template>

<script setup>
import { computed, reactive, ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown, ArrowRight } from '@element-plus/icons-vue'
import ScheduleConfig from './ScheduleConfig.vue'
import { buildScheduleFromForm, decodeScheduleToForm } from '../utils/schedule'
import {
  SENSITIVE_PLACEHOLDER,
  applyConnectionSpecDefaults,
  buildConnectionRules,
  isMaskedSensitiveValue,
  visibleConnectionFields
} from '../utils/engineConnectionSpec'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: Object,
    default: () => ({})
  },
  isEdit: {
    type: Boolean,
    default: false
  },
  disableTypeChange: {
    type: Boolean,
    default: true
  },
  engineTypeDescriptors: {
    type: Array,
    default: () => []
  },
  showActiveSwitch: {
    type: Boolean,
    default: true
  },
  showTypeSelector: {
    type: Boolean,
    default: true
  },
  labelWidth: {
    type: String,
    default: '120px'
  }
})

const emit = defineEmits(['update:modelValue', 'type-change'])

const effectiveTypeOptions = computed(() => props.engineTypeDescriptors.map(descriptor => ({
  label: descriptor.display_name,
  value: descriptor.type
})))

const selectedEngineTypeDescriptor = computed(() =>
  props.engineTypeDescriptors.find(descriptor => descriptor.type === formState.engine_type) || null
)
const selectedConnectionSpec = computed(() => selectedEngineTypeDescriptor.value?.connection_spec || null)
const visibleConnectionFieldSpecs = computed(() =>
  visibleConnectionFields(selectedConnectionSpec.value, formState.connection_info)
)
const fieldLabel = field => t(field.label_key)

const formRef = ref(null)
const storedSensitiveFields = computed(() => new Set(
  (selectedConnectionSpec.value?.fields || [])
    .filter(field => field.sensitive && formState.connection_info?.[`_has_${field.key}`] === true)
    .map(field => field.key)
))
const immediateScanEnabled = ref(true)  // 默认启用立即扫描
const scheduledScanEnabled = ref(false) // 默认不启用定时扫描
const scanConfigExpanded = ref(false)   // 扫描配置折叠状态（默认折叠）

const defaultScheduledScanCron = '0 0 * * *'

const scanPolicyToCron = (scanConfig = {}) => {
  if (!scanConfig.scheduled_scan) {
    return ''
  }
  if (scanConfig.schedule_mode === 'cron') {
    return scanConfig.cron_expression || ''
  }

  const form = {
    mode: scanConfig.schedule_mode || 'daily',
    time: scanConfig.schedule_time || '00:00',
    weekDays: (scanConfig.schedule_value || []).map(value => String(value)),
    dayOfMonth: scanConfig.schedule_value?.[0] || 1
  }
  return buildScheduleFromForm(form)?.cron || defaultScheduledScanCron
}

const applyCronToScanPolicy = (cron) => {
  const normalized = (cron || '').trim().replace(/\s+/g, ' ')
  if (!normalized) {
    scheduledScanEnabled.value = false
    formState.scan_config.scheduled_scan = false
    formState.scan_config.schedule_mode = 'cron'
    formState.scan_config.cron_expression = ''
    formState.scan_config.schedule_time = '00:00'
    formState.scan_config.schedule_value = []
    return
  }

  const decoded = decodeScheduleToForm(normalized)
  scheduledScanEnabled.value = true
  formState.scan_config.scheduled_scan = true
  formState.scan_config.schedule_mode = 'cron'
  formState.scan_config.cron_expression = normalized
  formState.scan_config.schedule_time = decoded?.time || '00:00'
  if (decoded?.mode === 'weekly') {
    formState.scan_config.schedule_value = decoded.weekDays.map(value => Number(value))
  } else if (decoded?.mode === 'monthly') {
    formState.scan_config.schedule_value = [decoded.dayOfMonth]
  } else {
    formState.scan_config.schedule_value = []
  }
}

const ensureConnectionDefaults = (form) => {
  if (!selectedConnectionSpec.value) {
    return
  }
  form.connection_info = applyConnectionSpecDefaults(selectedConnectionSpec.value, form.connection_info)
}

const applySensitiveHints = () => {
  formState.connection_info = applyConnectionSpecDefaults(selectedConnectionSpec.value, formState.connection_info)
}

const formState = reactive({
	engine_type: '',
	name: '',
	description: '',
	lifecycle_state: 'active',
  connection_info: {},
  scan_config: {
    enabled: true,
    immediate_scan: true,  // 默认启用立即扫描
    immediate_depth: 'basic',  // 立即扫描默认基础
    scheduled_scan: false,  // 默认只立即扫描一次
    schedule_mode: 'cron',
    cron_expression: '',
    schedule_time: '00:00',  // 凌晨执行
    schedule_value: []
  }
})

watch(selectedConnectionSpec, spec => {
  if (!spec) return
  formState.connection_info = applyConnectionSpecDefaults(spec, formState.connection_info)
})

const syncFromProps = (value) => {
  formState.engine_type = value.engine_type || ''
  formState.name = value.name || ''
  formState.description = value.description || ''
	formState.lifecycle_state = value.lifecycle_state || 'active'
  formState.connection_info = { ...(value.connection_info || {}) }

  // 同步扫描配置
  if (value.scan_config) {
    formState.scan_config = {
      enabled: value.scan_config.enabled || false,
      immediate_scan: value.scan_config.immediate_scan !== undefined ? value.scan_config.immediate_scan : true,
      immediate_depth: value.scan_config.immediate_depth || 'basic',
      scheduled_scan: value.scan_config.scheduled_scan !== undefined ? value.scan_config.scheduled_scan : true,
      schedule_mode: 'cron',
      cron_expression: scanPolicyToCron(value.scan_config),
      schedule_time: value.scan_config.schedule_time || '00:00',
      schedule_value: value.scan_config.schedule_value || []
    }
    immediateScanEnabled.value = formState.scan_config.immediate_scan
    scheduledScanEnabled.value = formState.scan_config.scheduled_scan
  } else {
    // 没有既有 Meta 调度时，默认只在保存后触发一次基础扫描。
    immediateScanEnabled.value = true
    scheduledScanEnabled.value = false
    formState.scan_config = {
      enabled: true,
      immediate_scan: true,
      immediate_depth: 'basic',
      scheduled_scan: false,
      schedule_mode: 'cron',
      cron_expression: '',
      schedule_time: '00:00',
      schedule_value: []
    }
  }

  ensureConnectionDefaults(formState)
  applySensitiveHints()
}

// Avoid infinite update loop between props -> local state -> emit -> props
let syncingFromProps = false
watch(
  () => props.modelValue,
  async (value) => {
    syncingFromProps = true
    try {
      syncFromProps(value || {})
    } finally {
      // ensure local watchers run while the flag is set
      await nextTick()
      syncingFromProps = false
    }
  },
  { immediate: true, deep: true }
)

// 计算属性：判断是否启用了任何扫描配置
const scanConfigEnabled = computed(() => {
  return immediateScanEnabled.value || scheduledScanEnabled.value
})

const scheduledScanCron = computed({
  get() {
    return scanPolicyToCron(formState.scan_config)
  },
  set(value) {
    applyCronToScanPolicy(value)
  }
})

// 监听立即扫描开关，同步到 formState
watch(immediateScanEnabled, (value) => {
  formState.scan_config.immediate_scan = value
})

// 监听定时扫描开关，同步到 formState
watch(scheduledScanEnabled, (value) => {
  formState.scan_config.scheduled_scan = value
  if (!value) {
    // 禁用定时扫描时，重置相关配置
    formState.scan_config.schedule_mode = 'cron'
    formState.scan_config.cron_expression = ''
    formState.scan_config.schedule_time = '00:00'
    formState.scan_config.schedule_value = []
  } else if (!formState.scan_config.cron_expression) {
    applyCronToScanPolicy(defaultScheduledScanCron)
  }
})

watch(
  formState,
  (value) => {
    // Skip emitting while we are syncing from props to prevent recursion
    if (syncingFromProps) return
    const payload = {
      engine_type: value.engine_type,
      name: value.name,
      description: value.description,
		lifecycle_state: value.lifecycle_state,
      connection_info: { ...value.connection_info }
    }
    payload.scan_config = { ...value.scan_config, enabled: scanConfigEnabled.value }
    emit('update:modelValue', payload)
  },
  { deep: true }
)

// 表单验证规则（响应式，支持语言切换）
const computedRules = computed(() => ({
  engine_type: [{ required: true, message: t('storageEngine.valid.selectType'), trigger: 'change' }],
  name: [{ required: true, message: t('storageEngine.valid.inputName'), trigger: 'blur' }],
  ...buildConnectionRules(selectedConnectionSpec.value, formState.connection_info, t)
}))

const handleTypeChange = (type) => {
  ensureConnectionDefaults(formState)
  applySensitiveHints()
  nextTick(() => formRef.value?.clearValidate())
  emit('type-change', type)
}

const validate = async () => {
  if (!formRef.value) return true
  try {
    await formRef.value.validate()
    return true
  } catch {
    return false
  }
}

const reset = () => {
  syncFromProps({})
  formRef.value?.clearValidate()
}

defineExpose({
  validate,
  reset,
  formRef,
  formState
})

watch(
  () => (selectedConnectionSpec.value?.fields || [])
    .filter(field => field.sensitive)
    .map(field => [field.key, formState.connection_info?.[field.key]]),
  entries => {
    if (syncingFromProps) return
    for (const [key, value] of entries) {
      const storedFlag = `_has_${key}`
      const hadStoredValue = formState.connection_info?.[storedFlag] === true
      if (!hadStoredValue && isMaskedSensitiveValue(value)) {
        formState.connection_info[key] = ''
        continue
      }
      if (isMaskedSensitiveValue(value)) {
        formState.connection_info[storedFlag] = true
      } else if (value) {
        formState.connection_info[storedFlag] = true
      } else {
        delete formState.connection_info[storedFlag]
      }
    }
  },
  { deep: true }
)
</script>

<style scoped>
.field-hint {
  margin: -8px 0 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

<style>
.el-input__inner::placeholder,
.el-textarea__inner::placeholder {
  color: var(--el-text-color-placeholder) !important;
  opacity: 0.6;
}
</style>
