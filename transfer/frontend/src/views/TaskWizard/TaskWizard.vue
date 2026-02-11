<template>
  <div class="task-wizard" v-loading="loading" element-loading-text="加载任务详情中...">
    <!-- 步骤指示器 -->
    <el-steps :active="wizardState.currentStep.value" finish-status="success" align-center>
      <el-step title="选择数据源" description="配置源数据库和表" />
      <el-step title="选择目标" description="配置目标数据库和表" />
      <el-step title="字段映射" description="配置字段对应关系" />
      <el-step title="任务配置" description="设置任务名称和调度" />
      <el-step title="确认创建" description="预览并提交任务" />
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
        上一步
      </el-button>

      <el-button
        v-if="wizardState.currentStep.value < 4"
        type="primary"
        :disabled="!wizardState.canGoNext.value"
        @click="wizardState.nextStep"
      >
        下一步
      </el-button>

      <el-button
        v-if="wizardState.currentStep.value === 4"
        type="success"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEditMode ? '更新任务' : '创建任务' }}
      </el-button>

      <el-button @click="handleCancel">
        取消
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useTaskWizardState } from './useTaskWizardState'
import { taskAPI } from '@/api/tasks'
import { getTableFields } from '@/api/meta'
import { localEnginesAPI } from '@/api/localEngines'

// 导入步骤组件
import Step1SelectSource from './Step1SelectSource.vue'
import Step2SelectTarget from './Step2SelectTarget.vue'
import Step3FieldMapping from './Step3FieldMapping.vue'
import Step4Configure from './Step4Configure.vue'
import Step5Review from './Step5Review.vue'

const router = useRouter()
const route = useRoute()
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

    ElMessage.success('任务详情加载成功')
  } catch (error) {
    ElMessage.error('加载任务详情失败: ' + (error.response?.data?.message || error.message))
    console.error('加载任务详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载源字段（编辑模式）
async function loadSourceFieldsForEdit(task) {
  if (!task.config?.source) return

  const source = task.config.source
  const engineId = source.engine_id
  const scope = source.scope || 'system'

  try {
    let fieldList = []

    if (scope === 'system') {
      // 系统引擎：从 Meta 模块获取字段
      const schema = source.schema || ''
      const table = source.table || ''
      if (table) {
        const response = await getTableFields(engineId, schema, table)
        fieldList = Array.isArray(response?.data) ? response.data : (response || [])
      }
    } else if (scope === 'local') {
      // 本地引擎：通过 Transfer 模块自己的 API 实时扫描字段
      const table = source.table || ''
      if (table) {
        const res = await localEnginesAPI.listFields(engineId, table)
        fieldList = Array.isArray(res) ? res : (Array.isArray(res?.data) ? res.data : [])
      }
    }

    wizardState.loadSourceFields(fieldList)
  } catch (error) {
    console.error('加载源字段失败:', error)
    // 不阻断整体加载流程，仅记录错误
  }
}

// 加载目标字段（编辑模式）
async function loadTargetFieldsForEdit(task) {
  if (!task.config?.target) return

  const target = task.config.target
  const engineId = target.engine_id
  const scope = target.scope || 'system'

  try {
    let fieldList = []

    if (scope === 'system') {
      // 系统引擎：从 Meta 模块获取字段
      const schema = target.schema || ''
      const table = target.table || ''
      if (table) {
        const response = await getTableFields(engineId, schema, table)
        fieldList = Array.isArray(response?.data) ? response.data : (response || [])
      }
    } else if (scope === 'local') {
      // 本地引擎：通过 Transfer 模块自己的 API 实时扫描字段
      const table = target.table || ''
      if (table) {
        const res = await localEnginesAPI.listFields(engineId, table)
        fieldList = Array.isArray(res) ? res : (Array.isArray(res?.data) ? res.data : [])
      }
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
      ? '确认更新此数据传输任务吗？'
      : '确认创建此数据传输任务吗？'

    await ElMessageBox.confirm(
      confirmMessage,
      '确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
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
      ? '确定要取消编辑任务吗？未保存的数据将丢失'
      : '确定要取消创建任务吗？未保存的数据将丢失'

    await ElMessageBox.confirm(
      confirmMessage,
      '警告',
      {
        confirmButtonText: '确定',
        cancelButtonText: '继续编辑',
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
