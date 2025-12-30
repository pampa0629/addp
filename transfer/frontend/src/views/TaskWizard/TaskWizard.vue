<template>
  <div class="task-wizard">
    <!-- 步骤指示器 -->
    <el-steps :active="wizardState.currentStep" finish-status="success" align-center>
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
      />
    </div>

    <!-- 底部导航 -->
    <div class="wizard-footer">
      <el-button
        v-if="wizardState.currentStep > 0"
        @click="wizardState.prevStep"
      >
        上一步
      </el-button>

      <el-button
        v-if="wizardState.currentStep < 4"
        type="primary"
        :disabled="!wizardState.canGoNext"
        @click="wizardState.nextStep"
      >
        下一步
      </el-button>

      <el-button
        v-if="wizardState.currentStep === 4"
        type="success"
        @click="handleSubmit"
        :loading="submitting"
      >
        创建任务
      </el-button>

      <el-button @click="handleCancel">
        取消
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useTaskWizardState } from './useTaskWizardState'

// 导入步骤组件
import Step1SelectSource from './Step1SelectSource.vue'
import Step2SelectTarget from './Step2SelectTarget.vue'
import Step3FieldMapping from './Step3FieldMapping.vue'
import Step4Configure from './Step4Configure.vue'
import Step5Review from './Step5Review.vue'

const router = useRouter()
const wizardState = useTaskWizardState()
const submitting = ref(false)

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

// 提交任务
async function handleSubmit() {
  try {
    await ElMessageBox.confirm(
      '确认创建此数据传输任务吗？',
      '确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    submitting.value = true
    const success = await wizardState.submitTask()

    if (success) {
      router.push('/transfer/tasks')
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
    await ElMessageBox.confirm(
      '确定要取消创建任务吗？未保存的数据将丢失',
      '警告',
      {
        confirmButtonText: '确定',
        cancelButtonText: '继续编辑',
        type: 'warning'
      }
    )

    wizardState.reset()
    router.push('/transfer/tasks')
  } catch (error) {
    // 用户点击了"继续编辑"
  }
}
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
  background: #fff;
  border-radius: 4px;
}

.wizard-footer {
  display: flex;
  justify-content: center;
  gap: 12px;
  padding: 20px 0;
  border-top: 1px solid #e4e7ed;
}
</style>
