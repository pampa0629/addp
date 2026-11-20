<template>
  <section class="execution-detail">
    <el-descriptions :column="2" border>
      <el-descriptions-item :label="t('orchestrator.executionList.descExecutionId')">{{ currentExecution.execution_id }}</el-descriptions-item>
      <el-descriptions-item :label="t('orchestrator.executionList.descStatus')">
        <el-tag :type="executionStatusType(currentExecution.status)">
          {{ getStatusText(currentExecution.status, t) }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item :label="t('orchestrator.executionList.descStartTime')">
        {{ formatTime(currentExecution.started_at) }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('orchestrator.executionList.descEndTime')">
        {{ formatTime(currentExecution.completed_at) }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('orchestrator.executionList.descCurrentStep')" :span="2">
        {{ currentExecution.current_step || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('orchestrator.executionList.descError')" :span="2">
        {{ executionFailureMessage(currentExecution, t) || '-' }}
      </el-descriptions-item>
    </el-descriptions>

    <h4 style="margin-top: 20px">{{ t('orchestrator.executionList.stepResults') }}</h4>
    <el-collapse v-if="Object.keys(currentExecution.metadata?.step_results || {}).length">
      <el-collapse-item
        v-for="(result, stepId) in currentExecution.metadata.step_results"
        :key="stepId"
      >
        <template #title>
          <span>{{ stepId }} - {{ result.status }}</span>
          <el-button
            v-if="result.result?.execution_id && auth.hasPermission('monitor.execution.read')"
            size="small"
            text
            type="primary"
            class="step-monitor-link"
            @click.stop="openMonitorExecution(result.result.execution_id)"
          >
            {{ t('orchestrator.executionList.viewChildExecutionBtn') }}
          </el-button>
        </template>
        <pre>{{ JSON.stringify(result, null, 2) }}</pre>
      </el-collapse-item>
    </el-collapse>
    <p v-else>{{ t('orchestrator.executionList.noStepResults') }}</p>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { openMonitorExecution } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { executionFailureMessage, executionStatusType, formatTime, getStatusText } from '../utils/executionPresentation'

defineProps({ currentExecution: { type: Object, required: true } })
const { t } = useI18n()
const auth = useAuthStore()
</script>

<style scoped>
.execution-detail pre {
  background: var(--addp-bg-secondary);
  padding: 10px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
}
.step-monitor-link { margin-left: 12px; }
</style>
