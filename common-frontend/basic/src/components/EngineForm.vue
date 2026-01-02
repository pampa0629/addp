<template>
  <el-form ref="formRef" :model="formData" :rules="rules" label-width="120px">
    <el-form-item label="名称" prop="name">
      <el-input v-model="formData.name" placeholder="引擎显示名称（中文或英文）" />
    </el-form-item>

    <el-form-item label="描述" prop="description">
      <el-input
        v-model="formData.description"
        type="textarea"
        :rows="2"
        placeholder="资源用途说明"
      />
    </el-form-item>

    <el-form-item label="资源类型" prop="resource_type">
      <el-select v-model="formData.resource_type" placeholder="请选择资源类型" :disabled="isEdit">
        <el-option label="计算引擎" value="compute_engine" />
        <el-option label="数据库" value="database" />
        <el-option label="对象存储" value="object_storage" />
      </el-select>
    </el-form-item>

    <el-form-item label="能力声明" prop="capabilities">
      <el-tabs v-model="capabilitiesTab">
        <el-tab-pane label="JSON 编辑" name="json">
          <el-input
            v-model="formData.capabilities"
            type="textarea"
            :rows="10"
            placeholder='{"compute": [{"type": "spatial", "category": "gis", "description": "空间计算"}]}'
          />
          <div class="form-hint">
            必须是有效的 JSON，且包含 storage 或 compute 字段
          </div>
        </el-tab-pane>

        <el-tab-pane label="可视化配置" name="visual">
          <el-alert
            title="可视化配置功能即将推出"
            type="info"
            :closable="false"
            show-icon
            style="margin-bottom: 16px"
          />
          <div class="visual-config-placeholder">
            <el-icon :size="48" color="#909399"><InfoFilled /></el-icon>
            <p>暂时请使用 JSON 编辑模式配置能力声明</p>
          </div>
        </el-tab-pane>
      </el-tabs>

      <div class="json-actions">
        <el-button size="small" @click="formatJSON">格式化</el-button>
        <el-button size="small" @click="validateJSONManually">校验</el-button>
      </div>
    </el-form-item>

    <el-form-item label="启用状态">
      <el-switch v-model="formData.is_active" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'

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

// 表单校验规则
const rules = {
  name: [
    { required: true, message: '请输入名称', trigger: 'blur' }
  ],
  resource_type: [
    { required: true, message: '请选择资源类型', trigger: 'change' }
  ],
  capabilities: [
    { required: true, message: '请输入能力声明', trigger: 'blur' },
    { validator: validateCapabilities, trigger: 'blur' }
  ]
}

// 校验能力声明 JSON
function validateCapabilities(rule, value, callback) {
  if (!value) {
    callback(new Error('请输入能力声明'))
    return
  }

  try {
    const parsed = JSON.parse(value)
    if (!parsed.storage && !parsed.compute) {
      callback(new Error('必须声明至少一种能力（storage 或 compute）'))
    } else {
      callback()
    }
  } catch (e) {
    callback(new Error('JSON 格式错误'))
  }
}

// 校验通用 JSON
function validateJSON(rule, value, callback) {
  if (!value) {
    callback()
    return
  }

  try {
    JSON.parse(value)
    callback()
  } catch (e) {
    callback(new Error('JSON 格式错误'))
  }
}

// 格式化 JSON
const formatJSON = () => {
  try {
    const parsed = JSON.parse(formData.capabilities)
    formData.capabilities = JSON.stringify(parsed, null, 2)
    ElMessage.success('格式化成功')
  } catch (e) {
    ElMessage.error('JSON 格式错误，无法格式化')
  }
}

// 校验 JSON（按钮触发）
const validateJSONManually = () => {
  try {
    const parsed = JSON.parse(formData.capabilities)
    if (!parsed.storage && !parsed.compute) {
      ElMessage.warning('能力声明必须包含 storage 或 compute 字段')
      return
    }
    ElMessage.success('JSON 格式正确')
  } catch (e) {
    ElMessage.error('JSON 格式错误: ' + e.message)
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
  color: #909399;
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
  color: #909399;
}

.visual-config-placeholder p {
  margin-top: 16px;
  font-size: 14px;
}

:deep(.el-tabs__content) {
  padding-top: 16px;
}
</style>
