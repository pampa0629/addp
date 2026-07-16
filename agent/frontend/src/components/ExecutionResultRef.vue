<template>
  <div class="execution-result-ref">
    <el-icon><Operation /></el-icon>
    <span class="execution-id">{{ executionId }}</span>
    <el-tag v-if="execution" size="small" :type="statusType">
      {{ execution.status || t('agent.resultRef.unknownStatus') }}
    </el-tag>
    <span v-else-if="loadError" class="load-error">{{ t('agent.resultRef.loadFailed') }}</span>
    <el-tooltip :content="t('agent.resultRef.openExecution')">
      <el-button :icon="TopRight" :aria-label="t('agent.resultRef.openExecution')" @click="openExecution" />
    </el-tooltip>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { Operation, TopRight } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import { resultRefAPI } from '../api/index'

const props = defineProps({
  resultRef: { type: Object, required: true }
})

const { t } = useI18n()
const execution = ref(null)
const loadError = ref(false)
const executionId = computed(() => String(props.resultRef.ref || '').replace(/^execution:/, ''))
const statusType = computed(() => ({
  success: 'success',
  failed: 'danger',
  timeout: 'danger',
  cancelled: 'info',
  running: 'warning',
  pending: 'info'
}[execution.value?.status] || 'info'))

async function loadExecution() {
  execution.value = null
  loadError.value = false
  if (!executionId.value) return
  try {
    execution.value = await resultRefAPI.getDevelopExecution(executionId.value)
  } catch {
    loadError.value = true
  }
}

function openExecution() {
  window.open(`/develop/executions/${encodeURIComponent(executionId.value)}`, '_blank', 'noopener')
}

onMounted(loadExecution)
watch(executionId, loadExecution)
</script>

<style scoped>
.execution-result-ref {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--addp-border-color);
  color: var(--addp-text-secondary);
}

.execution-id {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--el-font-family-mono);
}

.load-error {
  color: var(--el-color-danger);
}
</style>
