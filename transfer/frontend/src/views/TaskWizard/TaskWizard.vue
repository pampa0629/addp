<template>
  <div class="task-wizard" v-loading="loading" :element-loading-text="t('transfer.taskWizard.loadingTaskDetail')">
    <!-- 步骤指示器 -->
    <el-steps :active="wizardState.currentStep.value" finish-status="success" align-center>
      <el-step :title="t('transfer.taskWizard.stepSelectSource')" :description="t('transfer.taskWizard.configureSourceDesc')" />
      <el-step :title="t('transfer.taskWizard.stepSelectTarget')" :description="t('transfer.taskWizard.configureTargetDesc')" />
      <el-step :title="t('transfer.taskWizard.stepFieldMapping')" :description="t('transfer.taskWizard.fieldMappingDesc')" />
      <el-step :title="t('transfer.taskWizard.stepConfigure')" :description="t('transfer.taskWizard.configureTaskDesc')" />
      <el-step :title="t('transfer.taskWizard.stepReview')" :description="t('transfer.taskWizard.confirmCreateDesc')" />
    </el-steps>

    <!-- 步骤内容 -->
    <div class="step-content">
      <component
        :is="currentStepComponent"
        :wizard-state="wizardState"
        @next="wizardState.nextStep"
        @prev="wizardState.prevStep"
        @submit="handleSubmit"
        @cancel="handleCancel"
        :is-edit-mode="isEditMode"
        :submitting="submitting"
      />
    </div>

    <!-- 底部导航 -->
    <div class="wizard-footer">
      <el-button
        v-if="wizardState.currentStep.value > 0"
        @click="wizardState.prevStep"
      >
        {{ t('transfer.taskWizard.previousStep') }}
      </el-button>

      <el-button
        v-if="wizardState.currentStep.value < 4"
        type="primary"
        :disabled="!wizardState.canGoNext.value"
        @click="wizardState.nextStep"
      >
        {{ t('transfer.taskWizard.nextStep') }}
      </el-button>

      <el-button
        v-if="wizardState.currentStep.value === 4"
        type="success"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEditMode ? t('transfer.taskWizard.updateTask') : t('transfer.taskWizard.createTask2') }}
      </el-button>

      <el-button @click="handleCancel">
        {{ t('transfer.taskWizard.cancel') }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useTaskWizardState } from './useTaskWizardState'
import { taskAPI } from '@/api/tasks'
import { getItemFieldsByCatalogPath, getTableFields } from '@/api/meta'

// 导入步骤组件
import Step1SelectSource from './Step1SelectSource.vue'
import Step2SelectTarget from './Step2SelectTarget.vue'
import Step3FieldMapping from './Step3FieldMapping.vue'
import Step4Configure from './Step4Configure.vue'
import Step5Review from './Step5Review.vue'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const wizardState = useTaskWizardState()
const submitting = ref(false)
const loading = ref(false)

// 判断是否为编辑模式
const isEditMode = computed(() => !!route.params.id)
const taskId = computed(() => route.params.id)

// 步骤组件映射
const stepComponents = [
  Step1SelectSource,
  Step2SelectTarget,
  Step3FieldMapping,
  Step4Configure,
  Step5Review
]

const currentStepComponent = computed(() => {
  return stepComponents[wizardState.currentStep.value]
})

// 加载任务详情（编辑模式）
async function loadTaskDetail() {
  if (!taskId.value) return

  loading.value = true
  try {
    const response = await taskAPI.get(taskId.value)
    const task = response.data || response

    // 回填任务数据到向导状态
    wizardState.loadFromTask(task)

    // 加载源字段和目标字段
    await loadSourceFieldsForEdit(task)
    await loadTargetFieldsForEdit(task)

    ElMessage.success(t('transfer.taskWizard.taskDetailLoadSuccess'))
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.taskDetailLoadFailed', { error: error.response?.data?.message || error.message }))
    console.error('加载任务详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载源字段（编辑模式）
async function loadSourceFieldsForEdit(task) {
  if (!task.config?.source) return

  const source = task.config.source
  const engineId = source.engine?.id
  const path = source.resource?.path || {}

  try {
    let fieldList = []

    const catalogPath = catalogPathFromEndpoint(source)
    if (catalogPath) {
      const response = source.resource?.kind === 'native_table'
        ? await getTableFields(engineId, path.schema || '', path.table || '')
        : await getItemFieldsByCatalogPath(engineId, catalogPath)
      fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    }

    wizardState.loadSourceFields(fieldList)
  } catch (error) {
    console.error('加载源字段失败:', error)
    // 不阻断整体加载流程，仅记录错误
  }
}

function catalogPathFromEndpoint(endpoint) {
  const resource = endpoint?.resource || {}
  const path = resource.path || {}
  if (resource.kind === 'native_table') {
    return [path.schema, path.table || path.name].filter(Boolean).join('.')
  }
  if (resource.kind === 'object') {
    return [path.bucket, path.path].filter(Boolean).join('/')
  }
  if (resource.kind === 'file') {
    return path.path || path.name || ''
  }
  return ''
}

// 加载目标字段（编辑模式）
async function loadTargetFieldsForEdit(task) {
  if (!task.config?.target) return

  const target = task.config.target
  if (target.representation !== 'native') return

  const engineId = target.engine?.id
  const path = target.resource?.path || {}

  try {
    let fieldList = []

    const schema = path.schema || ''
    const table = path.table || ''
    if (table) {
      const response = await getTableFields(engineId, schema, table)
      fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    }

    wizardState.loadTargetFields(fieldList)
  } catch (error) {
    console.error('加载目标字段失败:', error)
    // 不阻断整体加载流程，仅记录错误
  }
}

// 提交任务
async function handleSubmit() {
  try {
    const confirmMessage = isEditMode.value
      ? t('transfer.taskWizard.confirmUpdate')
      : t('transfer.taskWizard.confirmCreate')

    await ElMessageBox.confirm(
      confirmMessage,
      t('transfer.taskWizard.confirmTitle'),
      {
        confirmButtonText: t('transfer.taskWizard.confirmOk'),
        cancelButtonText: t('transfer.taskWizard.confirmCancel'),
        type: 'warning'
      }
    )

    submitting.value = true
    const success = isEditMode.value
      ? await wizardState.updateTask(taskId.value)
      : await wizardState.submitTask()

    if (success) {
      // 跳转到任务列表页面
      router.push('/tasks')
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('提交任务失败:', error)
    }
  } finally {
    submitting.value = false
  }
}

// 取消
async function handleCancel() {
  try {
    const confirmMessage = isEditMode.value
      ? t('transfer.taskWizard.cancelEditConfirm')
      : t('transfer.taskWizard.cancelCreateConfirm')

    await ElMessageBox.confirm(
      confirmMessage,
      t('transfer.taskWizard.warningTitle'),
      {
        confirmButtonText: t('transfer.taskWizard.confirmOk'),
        cancelButtonText: t('transfer.taskWizard.continueEditing'),
        type: 'warning'
      }
    )

    wizardState.reset()
    // 跳转到任务列表页面
    router.push('/tasks')
  } catch (error) {
    // 用户点击了"继续编辑"
  }
}

// 组件挂载时加载任务详情（编辑模式）
onMounted(() => {
  if (isEditMode.value) {
    loadTaskDetail()
  }
})

</script>

<style scoped>
.task-wizard {
  padding: 20px;
  max-width: 1200px;
  margin: 0 auto;
}

.step-content {
  margin: 40px 0;
  min-height: 400px;
  padding: 20px;
  background: var(--addp-bg-primary);
  border-radius: 4px;
}

.wizard-footer {
  display: flex;
  justify-content: center;
  gap: 12px;
  padding: 20px 0;
  border-top: 1px solid var(--addp-border-color);
}
</style>
