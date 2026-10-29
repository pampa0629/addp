<template>
  <section class="cleanup-module-results">
    <p v-if="executions === undefined">{{ t('common.cleanupObservation.notApplicable') }}</p>
    <p v-else-if="!executions.length">{{ t('common.cleanupObservation.noReadableChildren') }}</p>
    <article v-for="row in executions || []" :key="row.execution_id">
      <h4>{{ row.module }} · {{ t(`common.cleanupObservation.status.${row.status}`) }}</h4>
      <CleanupAdmission :execution="row" />
      <dl>
        <dt>{{ t('common.cleanupObservation.execution') }}</dt><dd>{{ row.execution_id }}</dd>
        <dt>{{ t('common.cleanupObservation.startedAt') }}</dt><dd>{{ row.started_at ? new Date(row.started_at).toLocaleString() : '—' }}</dd>
        <template v-for="key in summaryFields" :key="key">
          <dt>{{ t(`common.cleanupObservation.summary.${key}`) }}</dt><dd>{{ row.receipt ? (row.receipt.summary[key] ?? 0) : '—' }}</dd>
        </template>
      </dl>
      <el-button v-if="canReadResult(row.module)" :loading="loading && selected?.execution_id === row.execution_id" @click="load(row)">{{ t('common.cleanupObservation.professional') }}</el-button>
      <div v-if="selected?.execution_id === row.execution_id" class="cleanup-professional-result">
        <p v-if="error" role="alert">{{ t(`common.cleanupObservation.errors.${error}`) }}</p>
        <template v-else-if="result">
          <p v-if="result.reason">{{ t(`common.cleanupObservation.reasons.${result.reason}`) }}</p>
          <dl><template v-for="(value, key) in result.statistics" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></template></dl>
          <p v-if="!Object.keys(result.statistics).length">{{ t('common.cleanupObservation.noCounters') }}</p>
        </template>
      </div>
    </article>
  </section>
</template>
<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CleanupAdmission from './CleanupAdmission.vue'
import { parseCleanupProfessionalResult } from '../utils/cleanupObservation.mjs'
import { createLatestRequestScope } from '../utils/latestRequestScope.mjs'
const props = defineProps({
  executions: { type: Array, default: undefined },
  requestId: { type: String, required: true },
  identityGeneration: { type: Number, required: true },
  canReadResult: { type: Function, required: true },
  readResult: { type: Function, required: true }
})
const { t } = useI18n()
const selected = ref(null), result = ref(null), error = ref(''), loading = ref(false)
const requests = createLatestRequestScope()
const summaryFields = ['scanned_items', 'affected_records', 'deleted_physical_artifacts', 'freed_bytes', 'marked_missing_source', 'marked_outdated', 'disabled_task_definitions', 'deleted_task_definitions', 'skipped_items', 'error_count']
function clear() { requests.invalidate(); selected.value = null; result.value = null; error.value = ''; loading.value = false }
async function load(row) {
  clear()
  const ticket = requests.invalidate()
  selected.value = row; loading.value = true
  try {
    const data = await requests.request(options => props.readResult(row.module, row.execution_id, options))
    if (!requests.current(ticket)) return
    result.value = parseCleanupProfessionalResult(data, { ...row, request_id: props.requestId })
  } catch (failure) {
    if (!requests.current(ticket)) return
    result.value = null
    error.value = failure.response?.status === 403 ? 'forbidden' : failure.response?.status === 409 ? 'notReady' : 'unavailable'
  } finally { if (requests.current(ticket)) loading.value = false }
}
watch(() => [props.requestId, props.identityGeneration, props.executions], clear, { flush: 'sync' })
onBeforeUnmount(() => requests.dispose())
</script>
<style scoped>
article { border-bottom: 1px solid var(--el-border-color); padding: 12px 0; }
h4 { margin: 0 0 8px; }
dl { display: grid; grid-template-columns: minmax(120px, auto) minmax(0, 1fr); gap: 6px 16px; font-size: var(--el-font-size-base); }
dt { color: var(--el-text-color-secondary); } dd { margin: 0; overflow-wrap: anywhere; }
.cleanup-professional-result { margin: 12px 0; padding: 12px; background: var(--el-fill-color-light); }
</style>
