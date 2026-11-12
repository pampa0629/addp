<template>
  <section class="cdc-task-chain" :aria-busy="loading" data-testid="cdc-task-chain">
    <header class="toolbar">
      <div class="page-title"><h3>{{ t('monitor.cdc.tasksTitle') }}</h3><p class="hint">{{ t('monitor.cdc.tasksSubtitle') }}</p></div>
      <AutoRefreshControl v-if="canRead" :model-value="state.refresh" :status="refreshStatus" test-id="cdc-task-refresh" @update:model-value="refresh => navigate({ ...state.query, refresh })" />
      <el-button :disabled="!canRead" :loading="loading" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="!canRead" :title="t('monitor.cdc.taskDenied')" type="warning" :closable="false" />
    <template v-else>
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="cdc-task-error" />
      <div class="task-picker">
        <span>{{ t('monitor.cdc.taskLabel') }}</span>
        <el-select :model-value="state.taskID || undefined" :aria-label="t('monitor.cdc.taskLabel')" :placeholder="t('monitor.cdc.chooseTask')" clearable filterable @update:model-value="task_id => navigate({ ...state.query, task_id: task_id || undefined })">
          <el-option v-for="item in choices" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
        <el-button link type="primary" @click="openTask()">{{ t('monitor.cdc.openTasks') }}</el-button>
      </div>
      <el-pagination v-if="total > 20" small layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="state.page" @current-change="page => navigate({ ...state.query, page: String(page) })" />
      <el-empty v-if="!choices.length && !loading && !error" :description="t('monitor.cdc.emptyTasks')" data-testid="cdc-tasks-empty" />
      <el-empty v-else-if="!task && !error && !loading" :description="t('monitor.cdc.chooseTask')" />
      <template v-if="task">
        <div class="task-heading"><h3>{{ task.name }}</h3><el-tag>{{ taskState(task.desired_state) }}</el-tag><el-button link type="primary" @click="openTask(task.id)">{{ t('monitor.cdc.openTask') }}</el-button></div>
        <div class="chain-grid">
          <el-card shadow="never" data-testid="cdc-task-source"><template #header>{{ t('monitor.cdc.chainSource') }}</template>
            <h3 class="resource-path">{{ endpoints.source.path }}</h3><p>{{ t('monitor.cdc.engineReference', { id: endpoints.source.engineID }) }}</p>
            <p class="hint">{{ t('monitor.cdc.configuredSource') }}</p>
          </el-card>
          <el-card shadow="never" data-testid="cdc-task-capture"><template #header>{{ t('monitor.cdc.chainCapture') }}</template>
            <template v-if="task.capture"><dl>
              <div><dt>{{ t('monitor.execution.detail.continuous.capture.generation') }}</dt><dd>{{ task.capture.generation }}</dd></div>
              <div><dt>{{ t('monitor.cdc.captureStatus') }}</dt><dd>{{ taskState(task.capture.status) }}</dd></div>
              <div><dt>{{ t('monitor.cdc.connectorStatus') }}</dt><dd>{{ task.capture.connector_status || '—' }}</dd></div>
              <div><dt>{{ t('monitor.cdc.sourceStatus') }}</dt><dd>{{ task.capture.source_status || '—' }}</dd></div>
            </dl><p class="hint">{{ date(task.capture.last_observed_at) }}</p></template>
            <el-empty v-else :description="t('monitor.cdc.noCapture')" :image-size="48" />
          </el-card>
          <el-card shadow="never" data-testid="cdc-task-target"><template #header>{{ t('monitor.cdc.chainTarget') }}</template>
            <h3 class="resource-path">{{ endpoints.target.path }}</h3><p>{{ t('monitor.cdc.engineReference', { id: endpoints.target.engineID }) }}</p>
            <p>{{ t('monitor.cdc.latestExecution') }}: {{ execution ? t(`monitor.execution.status.${execution.status}`) : t('monitor.cdc.notStarted') }}</p>
          </el-card>
        </div>
        <el-card shadow="never" class="progress-card" data-testid="cdc-task-buffer">
          <template #header>{{ t('monitor.cdc.bufferTitle') }}</template>
          <p>{{ t(`monitor.cdc.bufferStates.${bufferState}`) }}</p>
          <p class="hint">{{ t('monitor.cdc.bufferHint') }}</p>
          <p v-if="buffer" class="hint">{{ t('monitor.cdc.lastSample') }}: {{ date(buffer.sampled_at) }}</p>
          <dl v-if="bufferState === 'available' || bufferState === 'lost' || bufferState === 'not_started'">
            <div v-for="field in ['lag_offsets', 'committed_offset', 'earliest_offset', 'latest_offset']" :key="field"><dt>{{ t(`monitor.cdc.bufferFields.${field}`) }}</dt><dd :data-testid="`cdc-buffer-${field}`">{{ metric(buffer[field]) }}</dd></div>
          </dl>
        </el-card>
        <el-alert v-if="executionError" :title="t(executionError)" type="warning" :closable="false" data-testid="cdc-task-execution-error" />
        <el-card v-if="execution" shadow="never" class="progress-card" data-testid="cdc-task-progress">
          <template #header><div class="toolbar"><h3>{{ t('monitor.cdc.latestProgress') }}</h3><el-button link type="primary" @click="openExecution">{{ t('monitor.cdc.openExecution') }}</el-button></div></template>
          <p class="hint">{{ t('monitor.cdc.lastSample') }}: {{ date(diagnostics.sampled_at) }}</p>
          <p class="hint">{{ t('monitor.cdc.sampleHint') }}</p>
          <div class="progress-grid">
            <div v-for="partition in partitions" :key="partition.partition" class="partition" data-testid="cdc-task-partition">
              <h4>{{ t('monitor.execution.detail.continuous.partition') }} {{ partition.partition }}</h4>
              <dl>
                <div><dt>{{ t('monitor.execution.detail.continuous.lag') }}</dt><dd data-testid="cdc-task-lag">{{ metric(partition.lagRecords) }}</dd></div>
                <div><dt>{{ t('monitor.execution.detail.continuous.committed') }}</dt><dd data-testid="cdc-task-committed">{{ metric(partition.nextOffset) }}</dd></div>
                <div><dt>{{ t('monitor.execution.detail.continuous.latest') }}</dt><dd>{{ metric(partition.latestOffset) }}</dd></div>
              </dl>
            </div>
          </div>
          <p v-if="!partitions.length" class="hint">{{ t('monitor.cdc.noTaskSample') }}</p>
        </el-card>
      </template>
    </template>
  </section>
</template>
<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { buildContinuousPartitionRows, getContinuousDiagnostics, openConsoleRoute } from '@common-ui'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { useAuthStore } from '../store/auth'
import { cdcTasksAPI } from '../api/cdcTasks'
import { getExecutionByExecutionID } from '../api/monitor'
import { cdcEndpoints, resolveCDCTaskRoute, validateCDCTask, validateCDCTaskPage, validateCDCTaskExecution } from '../utils/cdcTasks'
import { resourceErrorKey } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
import { executionDetailLocation } from '../utils/executionNavigation'
const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, te, locale } = useI18n()
const state = computed(() => resolveCDCTaskRoute(route.query))
const canRead = computed(() => auth.contextType === 'tenant' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('transfer.task.read') && auth.hasPermission('monitor.execution.read'))
const task = ref(null), execution = ref(null), choices = ref([]), total = ref(0), loading = ref(false), error = ref(''), executionError = ref(''), blocked = ref(false), visible = ref(!document.hidden)
const endpoints = computed(() => task.value ? cdcEndpoints(task.value) : null)
const diagnostics = computed(() => getContinuousDiagnostics(execution.value?.metadata))
const partitions = computed(() => buildContinuousPartitionRows(execution.value?.metadata))
const buffer = computed(() => task.value?.capture?.buffer_observation)
const bufferExpired = ref(false)
let bufferExpiryTimer
watch(buffer, value => {
  clearTimeout(bufferExpiryTimer)
  const remaining = value ? Date.parse(value.expires_at) - Date.now() : 0
  bufferExpired.value = remaining <= 0
  if (remaining > 0) bufferExpiryTimer = setTimeout(() => { bufferExpired.value = true }, remaining)
}, { immediate: true })
const bufferState = computed(() => !buffer.value ? 'no_sample' : bufferExpired.value ? 'stale' : buffer.value.status === 'unavailable' ? 'unknown' : buffer.value.recovery_state)
const requests = createLatestRequestScope()
let context = ''
const cycle = createAutoRefresh({ interval: () => state.value.refresh, enabled: () => canRead.value && !blocked.value && !loading.value, refresh: () => reload(true), pause: () => { requests.invalidate(); loading.value = false }, visibilityChanged: value => { visible.value = value } })
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const date = value => value ? new Date(value).toLocaleString(locale.value) : '—'
const metric = value => value === null || value === undefined ? '—' : String(value)
function taskState(value) { const key = `monitor.cdc.taskStates.${value}`; return te(key) ? t(key) : value }
function navigate(query) { navigateMonitorRoute(router, { path: '/cdc-resources', query: resolveCDCTaskRoute(query).query }, { history: 'replace' }) }
function openTask(id) { openConsoleRoute(id ? `/transfer/tasks/${id}/detail` : '/transfer/tasks', { history: 'push' }) }
function openExecution() { const location = executionDetailLocation(execution.value); if (location) navigateMonitorRoute(router, location, { history: 'push' }) }
function clear() { task.value = null; execution.value = null; choices.value = []; total.value = 0 }
function failure(cause) { if ([401, 403, 404].includes(cause.response?.status)) { blocked.value = true; clear() }; return resourceErrorKey(cause) }
async function reload(automatic = false) {
  cycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) blocked.value = false
  const next = JSON.stringify([auth.authContext, state.value.taskID, state.value.page])
  if (next !== context) { context = next; clear() }
  error.value = ''; executionError.value = ''; loading.value = false
  if (!canRead.value) { clear(); return }
  loading.value = true
  try {
    const page = validateCDCTaskPage(await requests.request(config => cdcTasksAPI.list(state.value.page, config)), state.value.page)
    if (!requests.current(ticket)) return
    choices.value = page.items; total.value = page.total
    if (!state.value.taskID) { task.value = null; execution.value = null; return }
    const selected = validateCDCTask(await requests.request(config => cdcTasksAPI.get(state.value.taskID, config)), state.value.taskID)
    if (!requests.current(ticket)) return
    task.value = selected; execution.value = null
    if (!choices.value.some(item => item.id === selected.id)) choices.value = [selected, ...choices.value]
    if (selected.last_execution_id) {
      try {
        const observed = await requests.request(config => getExecutionByExecutionID(selected.last_execution_id, config))
        if (!requests.current(ticket)) return
        execution.value = validateCDCTaskExecution(observed, selected)
      } catch (cause) {
        if (requests.current(ticket)) {
          execution.value = null
          const message = failure(cause)
          if (blocked.value) error.value = message
          else executionError.value = message
        }
      }
    }
  } catch (cause) { if (requests.current(ticket)) { clear(); error.value = failure(cause) } }
  finally { if (requests.current(ticket)) { loading.value = false; cycle.schedule() } }
}
watch(() => JSON.stringify([route.query, auth.authContext]), () => {
  if (state.value.changed) navigate(state.value.query)
  else reload()
}, { immediate: true })
onBeforeUnmount(() => { clearTimeout(bufferExpiryTimer); cycle.dispose(); requests.dispose() })
</script>
<style scoped>
.cdc-task-chain { min-width: 0; }
.toolbar, .task-picker, .task-heading { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-bottom: 16px; }
.page-title { flex: 1; min-width: 240px; }
h3, h4 { margin: 0 0 8px; }
.hint { color: var(--addp-text-secondary); font-size: 13px; overflow-wrap: anywhere; }
.task-picker :deep(.el-select) { width: 360px; max-width: 100%; }
.chain-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin: 16px 0; }
.resource-path { overflow-wrap: anywhere; }
dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
dt { color: var(--addp-text-secondary); font-size: 13px; } dd { margin: 8px 0 0; font-size: 20px; overflow-wrap: anywhere; }
.progress-card { margin-top: 16px; }
.progress-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; }
.partition { border: 1px solid var(--addp-border-color); padding: 12px; border-radius: 6px; }
@media (max-width: 900px) { .chain-grid { grid-template-columns: 1fr; } }
</style>
