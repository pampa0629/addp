<template>
  <el-form ref="formRef" :model="formData" :rules="computedRules" label-width="120px">
    <el-form-item :label="t('engineForm.name')" prop="name">
      <el-input v-model="formData.name" :placeholder="t('engineForm.namePlaceholder')" />
    </el-form-item>

    <el-form-item :label="t('engineForm.description')" prop="description">
      <el-input
        v-model="formData.description"
        type="textarea"
        :rows="2"
        :placeholder="t('engineForm.descPlaceholder')"
      />
    </el-form-item>

    <el-form-item :label="t('engineForm.resourceType')" prop="resource_type">
      <el-select v-model="formData.resource_type" :placeholder="t('engineForm.resourceTypePlaceholder')" :disabled="isEdit">
        <el-option :label="t('engineForm.computeEngine')" value="compute_engine" />
        <el-option :label="t('engineForm.database')" value="database" />
        <el-option :label="t('engineForm.objectStorage')" value="object_storage" />
      </el-select>
    </el-form-item>

    <el-form-item :label="t('engineForm.capabilities')" prop="capabilities">
      <el-tabs v-model="capabilitiesTab">
        <el-tab-pane :label="t('engineForm.capabilityTabJson')" name="json">
          <el-input
            v-model="formData.capabilities"
            type="textarea"
            :rows="10"
            placeholder='{"compute": [{"type": "spatial", "category": "gis", "description": "空间计算"}]}'
          />
          <div class="form-hint">
            {{ t('engineForm.jsonHint') }}
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('engineForm.capabilityTabVisual')" name="visual">
          <el-alert
            :title="t('engineForm.comingSoon')"
            type="info"
            :closable="false"
            show-icon
            style="margin-bottom: 16px"
          />
          <div class="visual-config-placeholder">
            <el-icon :size="48" color="#909399"><InfoFilled /></el-icon>
            <p>{{ t('engineForm.useJsonMode') }}</p>
          </div>
        </el-tab-pane>
      </el-tabs>

      <div class="json-actions">
        <el-button size="small" @click="formatJSON">{{ t('engineForm.format') }}</el-button>
        <el-button size="small" @click="validateJSONManually">{{ t('engineForm.validate') }}</el-button>
      </div>
    </el-form-item>

    <el-form-item :label="t('engineForm.enabledStatus')">
      <el-switch v-model="formData.is_active" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref, reactive, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: Object,
    default: () => ({})
  },
  isEdit: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue'])

const formRef = ref(null)
const capabilitiesTab = ref('json')

const formData = reactive({
  name: '',
  description: '',
  resource_type: 'compute_engine',
  capabilities: '',
  is_active: true
})

// 校验能力声明 JSON
function validateCapabilities(rule, value, callback) {
  if (!value) {
    callback(new Error(t('engineForm.valid.capabilities')))
    return
  }

  try {
    const parsed = JSON.parse(value)
    if (!parsed.storage && !parsed.compute) {
      callback(new Error(t('engineForm.valid.capabilitiesField')))
    } else {
      callback()
    }
  } catch (e) {
    callback(new Error(t('engineForm.valid.jsonFormat')))
  }
}

// 表单验证规则（响应式，支持语言切换）
const computedRules = computed(() => ({
  name: [{ required: true, message: t('engineForm.valid.name'), trigger: 'blur' }],
  resource_type: [{ required: true, message: t('engineForm.valid.resourceType'), trigger: 'change' }],
  capabilities: [
    { required: true, message: t('engineForm.valid.capabilities'), trigger: 'blur' },
    { validator: validateCapabilities, trigger: 'blur' }
  ]
}))

// 格式化 JSON
const formatJSON = () => {
  try {
    const parsed = JSON.parse(formData.capabilities)
    formData.capabilities = JSON.stringify(parsed, null, 2)
    ElMessage.success(t('engineForm.valid.formatSuccess'))
  } catch (e) {
    ElMessage.error(t('engineForm.valid.jsonFormat'))
  }
}

// 校验 JSON（按钮触发）
const validateJSONManually = () => {
  try {
    const parsed = JSON.parse(formData.capabilities)
    if (!parsed.storage && !parsed.compute) {
      ElMessage.warning(t('engineForm.valid.capabilitiesFieldWarning'))
      return
    }
    ElMessage.success(t('engineForm.valid.jsonFormatSuccess'))
  } catch (e) {
    ElMessage.error(t('engineForm.valid.jsonFormat') + ': ' + e.message)
  }
}

// 监听 props 变化
watch(
  () => props.modelValue,
  (newVal) => {
    if (newVal) {
      Object.assign(formData, {
        ...newVal,
        capabilities: typeof newVal.capabilities === 'string'
          ? newVal.capabilities
          : JSON.stringify(newVal.capabilities || {}, null, 2),
        extension_api_config: typeof newVal.extension_api_config === 'string'
          ? newVal.extension_api_config
          : JSON.stringify(newVal.extension_api_config || {}, null, 2),
        health_check_config: typeof newVal.health_check_config === 'string'
          ? newVal.health_check_config
          : JSON.stringify(newVal.health_check_config || {}, null, 2)
      })
    }
  },
  { immediate: true, deep: true }
)

// 监听 formData 变化，同步到父组件
watch(
  formData,
  () => {
    emit('update:modelValue', { ...formData })
  },
  { deep: true }
)

// 暴露表单校验方法
const validate = async () => {
  if (!formRef.value) return false
  try {
    await formRef.value.validate()
    return true
  } catch {
    return false
  }
}

const reset = () => {
  formRef.value?.resetFields()
}

defineExpose({
  validate,
  reset
})
</script>

<style scoped>
.form-hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
  line-height: 1.5;
}

.json-actions {
  margin-top: 8px;
  display: flex;
  gap: 8px;
}

.visual-config-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  color: var(--addp-text-tertiary);
}

.visual-config-placeholder p {
  margin-top: 16px;
  font-size: 14px;
}

:deep(.el-tabs__content) {
  padding-top: 16px;
}
</style>
