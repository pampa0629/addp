/**
 * TaskWizard 状态管理
 * 职责：集中管理向导的所有状态和业务逻辑
 */

import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { createTask, validateTaskConfig } from '@/api/transfer'

export function useTaskWizardState() {
  // ===== 状态定义 =====
  const currentStep = ref(0)
  const taskName = ref('')
  const taskDescription = ref('')
  const schedule = ref('')

  // Source 配置
  const sourceType = ref('postgresql')
  const sourceConfig = ref({})
  const sourceEngineID = ref(null)
  const sourceSchema = ref('')
  const sourceTable = ref('')

  // Target 配置
  const targetType = ref('postgresql')
  const targetConfig = ref({})
  const targetEngineID = ref(null)
  const targetSchema = ref('')
  const targetTable = ref('')

  // 字段映射
  const fieldMappings = ref([])
  const sourceFields = ref([])
  const targetFields = ref([])

  // 转换配置
  const transforms = ref([])

  // ===== 计算属性 =====
  const canGoNext = computed(() => {
    switch (currentStep.value) {
      case 0: // 选择Source
        return sourceEngineID.value && sourceTable.value
      case 1: // 选择Target
        return targetEngineID.value && targetTable.value
      case 2: // 字段映射
        return fieldMappings.value.length > 0
      case 3: // 配置
        return taskName.value.trim() !== ''
      default:
        return true
    }
  })

  const taskConfig = computed(() => {
    return {
      name: taskName.value,
      description: taskDescription.value,
      schedule: schedule.value,
      source: {
        type: sourceType.value,
        engine_id: sourceEngineID.value,
        schema: sourceSchema.value,
        table: sourceTable.value,
        ...sourceConfig.value
      },
      target: {
        type: targetType.value,
        engine_id: targetEngineID.value,
        schema: targetSchema.value,
        table: targetTable.value,
        ...targetConfig.value
      },
      mappings: fieldMappings.value,
      transforms: transforms.value
    }
  })

  // ===== 方法 =====

  // 步骤导航
  function nextStep() {
    if (canGoNext.value && currentStep.value < 4) {
      currentStep.value++
    }
  }

  function prevStep() {
    if (currentStep.value > 0) {
      currentStep.value--
    }
  }

  function goToStep(step) {
    if (step >= 0 && step <= 4) {
      currentStep.value = step
    }
  }

  // Source 配置
  function updateSource(config) {
    sourceEngineID.value = config.engineID
    sourceSchema.value = config.schema
    sourceTable.value = config.table
    sourceConfig.value = config.extra || {}
  }

  function loadSourceFields(fields) {
    sourceFields.value = fields
    // 自动初始化字段映射
    if (fieldMappings.value.length === 0) {
      autoGenerateFieldMappings()
    }
  }

  // Target 配置
  function updateTarget(config) {
    targetEngineID.value = config.engineID
    targetSchema.value = config.schema
    targetTable.value = config.table
    targetConfig.value = config.extra || {}
  }

  function loadTargetFields(fields) {
    targetFields.value = fields
  }

  // 字段映射
  function autoGenerateFieldMappings() {
    if (sourceFields.value.length === 0) return

    fieldMappings.value = sourceFields.value.map(field => ({
      sourceField: field.name,
      targetField: field.name, // 默认同名映射
      fieldType: field.type,
      format: null,
      defaultValue: null
    }))
  }

  function updateFieldMapping(index, mapping) {
    fieldMappings.value[index] = mapping
  }

  function addFieldMapping() {
    fieldMappings.value.push({
      sourceField: '',
      targetField: '',
      fieldType: 'string',
      format: null,
      defaultValue: null
    })
  }

  function removeFieldMapping(index) {
    fieldMappings.value.splice(index, 1)
  }

  // 转换配置
  function addTransform(transform) {
    transforms.value.push(transform)
  }

  function removeTransform(index) {
    transforms.value.splice(index, 1)
  }

  // 提交任务
  async function submitTask() {
    try {
      // 验证配置
      const validation = await validateTaskConfig(taskConfig.value)
      if (!validation.valid) {
        ElMessage.error(validation.message || '任务配置验证失败')
        return false
      }

      // 创建任务
      await createTask(taskConfig.value)
      ElMessage.success('任务创建成功')
      return true
    } catch (error) {
      ElMessage.error(error.message || '任务创建失败')
      return false
    }
  }

  // 重置状态
  function reset() {
    currentStep.value = 0
    taskName.value = ''
    taskDescription.value = ''
    schedule.value = ''
    sourceEngineID.value = null
    sourceSchema.value = ''
    sourceTable.value = ''
    sourceConfig.value = {}
    targetEngineID.value = null
    targetSchema.value = ''
    targetTable.value = ''
    targetConfig.value = {}
    fieldMappings.value = []
    sourceFields.value = []
    targetFields.value = []
    transforms.value = []
  }

  return {
    // 状态
    currentStep,
    taskName,
    taskDescription,
    schedule,
    sourceEngineID,
    sourceSchema,
    sourceTable,
    targetEngineID,
    targetSchema,
    targetTable,
    fieldMappings,
    sourceFields,
    targetFields,
    transforms,

    // 计算属性
    canGoNext,
    taskConfig,

    // 方法
    nextStep,
    prevStep,
    goToStep,
    updateSource,
    loadSourceFields,
    updateTarget,
    loadTargetFields,
    autoGenerateFieldMappings,
    updateFieldMapping,
    addFieldMapping,
    removeFieldMapping,
    addTransform,
    removeTransform,
    submitTask,
    reset
  }
}
