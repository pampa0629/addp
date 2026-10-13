<template>
  <section v-if="failed" class="execution-failure" role="alert" :aria-label="t('common.executionFailure.title')">
    <h4>{{ fallbackMessage || presentation.reason }}</h4>
    <dl>
      <template v-if="presentation.stage">
        <dt>{{ t('common.executionFailure.stage') }}</dt><dd>{{ presentation.stage }}</dd>
      </template>
      <dt>{{ t('common.executionFailure.action') }}</dt><dd>{{ presentation.action }}</dd>
      <template v-if="presentation.code">
        <dt>{{ t('common.executionFailure.code') }}</dt><dd>{{ presentation.code }}</dd>
      </template>
      <template v-if="executionId">
        <dt>{{ t('common.executionFailure.executionId') }}</dt><dd>{{ executionId }}</dd>
      </template>
    </dl>
    <div class="execution-failure-actions">
      <el-button v-if="executionId" size="small" @click="copyDetails">{{ t('common.executionFailure.copy') }}</el-button>
      <el-button v-if="ownerRoute" type="primary" size="small" @click="openConsoleRoute(ownerRoute)">
        {{ t('common.executionFailure.openQueryExecution') }}
      </el-button>
    </div>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { executionFailurePresentation } from '../utils/executionFailure.mjs'
import { openConsoleRoute } from '../utils/taskOwnerUrl'

const props = defineProps({
  status: { type: String, default: '' },
  errorDetails: { type: Object, default: () => ({}) },
  executionId: { type: String, default: '' },
  ownerRoute: { type: String, default: '' },
  // Request admission errors are already localized by the HTTP Owner. Execution
  // diagnostics never use a raw runtime message as a fallback.
  fallbackMessage: { type: String, default: '' }
})
const { t } = useI18n()
const failed = computed(() => ['failed', 'timeout', 'cancelled'].includes(props.status))
const presentation = computed(() => executionFailurePresentation(props.errorDetails, t))
async function copyDetails() {
  const { stage, reason, action, code } = presentation.value
  try {
    await navigator.clipboard.writeText([props.fallbackMessage || reason, stage, action, code, props.executionId].filter(Boolean).join('\n'))
    ElMessage.success(t('common.executionFailure.copied'))
  } catch {
    ElMessage.error(t('common.executionFailure.copyFailed'))
  }
}
</script>

<style scoped>
.execution-failure {
  margin: 12px 0;
  padding: 14px 16px;
  border: 1px solid var(--el-color-danger-light-5);
  border-radius: var(--el-border-radius-base);
  background: var(--el-color-danger-light-9);
  overflow-wrap: anywhere;
  min-width: 0;
}
h4 { margin: 0 0 12px; color: var(--el-color-danger); }
dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; margin: 0; font-size: var(--el-font-size-base); }
dt { color: var(--el-text-color-secondary); }
dd { margin: 0; white-space: pre-wrap; user-select: text; }
.execution-failure-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.execution-failure-actions :deep(.el-button + .el-button) { margin-left: 0; }
@media (max-width: 600px) { dl { grid-template-columns: minmax(0, 1fr); gap: 4px; } dd { margin-bottom: 8px; } }
</style>
