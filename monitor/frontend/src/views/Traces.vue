<template>
  <section class="traces" data-testid="traces">
    <el-alert v-if="!canRead" :title="t('monitor.traces.denied')" type="warning" :closable="false" />
    <template v-else>
      <header>
        <h2>{{ t('monitor.traces.title') }}</h2>
        <el-button v-if="state?.query.trace_id && state.instanceID" :icon="ArrowLeft" @click="returnToTraces">{{ t('monitor.traces.browseTraces') }}</el-button>
        <el-button v-else-if="state?.instanceID || directLookup" :icon="ArrowLeft" @click="browseInstances">{{ t('monitor.traces.browseInstances') }}</el-button>
        <el-button @click="refresh">{{ t('common.refresh') }}</el-button>
      </header>
      <el-alert :title="t('monitor.traces.coverage')" type="info" :closable="false" />
      <div class="filters" data-testid="trace-search">
        <el-input v-model="traceDraft" :placeholder="t('monitor.traces.traceID')" :aria-label="t('monitor.traces.traceID')" clearable @input="traceInput.queue" @keydown.enter="traceInput.enter" />
        <el-date-picker v-model="pickerStart" type="datetime" :placeholder="t('monitor.traces.recentWindow')" :aria-label="t('monitor.traces.start')" :disabled-date="futureDate" :show-now="false" @change="changeStart" />
        <span v-if="bounds.end" class="window-hint">{{ t('monitor.traces.windowEnd', { time: date(bounds.end) }) }}</span>
      </div>
      <el-alert v-if="inputError" :title="t(inputError)" type="warning" :closable="false" />
      <div v-if="!directLookup" class="filters">
        <el-select :model-value="state?.module || ''" :placeholder="t('monitor.process.module')" :aria-label="t('monitor.process.module')" :loading="modulesLoading" clearable filterable @change="module => filter(module, state?.role || 'backend')">
          <el-option v-for="module in moduleNames" :key="module" :value="module" :label="module" />
        </el-select>
        <el-select :model-value="state?.role || 'backend'" :aria-label="t('monitor.http.layer')" @change="role => filter(state?.module || '', role)"><el-option v-for="role in ['backend', 'ingress']" :key="role" :value="role" :label="t(`monitor.http.layers.${role}`)" /></el-select>
      </div>
      <el-alert v-if="modulesFailed" :title="t('monitor.traces.modulesFailed')" type="warning" :closable="false" />
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="trace-error" />
      <el-table v-if="!directLookup && !detail" :data="instances" v-loading="loading" :empty-text="t(loading ? 'common.loading' : instancesLoaded ? 'monitor.traces.noInstances' : 'monitor.traces.notLoaded')" data-testid="trace-instances">
        <el-table-column prop="module_name" :label="t('monitor.process.module')" min-width="130" />
        <el-table-column prop="instance_id" :label="t('monitor.http.instance')" min-width="220" show-overflow-tooltip />
        <el-table-column :label="t('monitor.traces.instanceStarted')" min-width="155">
          <template #default="{ row }">{{ row.process_started_at ? date(row.process_started_at) : '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.traces.title')" width="130"><template #default="{ row }"><el-button link @click="select(row)">{{ t('monitor.traces.open') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="!directLookup && !detail" layout="total, prev, pager, next" :page-size="10" :total="total" :current-page="state?.page || 1" @current-change="page => navigate({ ...state.query, page: String(page), instance_id: undefined, trace_id: undefined })" />
      <template v-if="selected && !detail">
        <h3>{{ selected.module_name }} · {{ selected.instance_id }}</h3>
        <el-table :data="data" :empty-text="t(loading ? 'common.loading' : error && error !== 'monitor.traces.empty' ? 'monitor.traces.notLoaded' : 'monitor.traces.empty')" data-testid="trace-results">
          <el-table-column prop="trace_id" :label="t('monitor.traces.traceID')" min-width="260" />
          <el-table-column :label="t('monitor.traces.start')" min-width="170"><template #default="{ row }">{{ date(row.start) }}</template></el-table-column>
          <el-table-column prop="duration_ms" :label="t('monitor.traces.duration')" min-width="140"><template #default="{ row }">{{ number(row.duration_ms) }}</template></el-table-column>
          <el-table-column :label="t('monitor.traces.spans')" width="100"><template #default="{ row }">{{ row.spans.length }}</template></el-table-column>
          <el-table-column :label="t('common.actions')" width="130"><template #default="{ row }"><el-button link @click="navigate({ ...state.query, trace_id: row.trace_id }, 'push')">{{ t('monitor.traces.viewTrace') }}</el-button></template></el-table-column>
        </el-table>
      </template>
      <trace-details v-if="detail" :trace="detail" :can-read-logs="canReadLogs" @logs="logs" @follow-link="followLink" />
    </template>
  </section>
</template>

<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft } from '@element-plus/icons-vue'
import { openConsoleRoute } from '@common-ui'
import { createAutoFilter } from '../../../../common-frontend/basic/src/utils/autoFilter.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { useAuthStore } from '../store/auth'
import { traceAPI } from '../api/traces'
import TraceDetails from '../components/TraceDetails.vue'
import { isTraceID, traceRoute, validateTraces, traceLinkQuery } from '../utils/traces'
import { validateProcessInstances } from '../utils/processResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.trace.read') && auth.hasPermission('platform.module.read'))
const canReadLogs = computed(() => canRead.value && auth.hasPermission('platform.module_log.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => { try { return traceRoute(route.query) } catch { return null } })
const directLookup = computed(() => Boolean(state.value?.query.trace_id && !state.value.instanceID))
const instances = ref([]), total = ref(0), data = ref([]), loading = ref(false), instancesLoaded = ref(false), error = ref('')
const traceDraft = ref(''), pickerStart = ref(null), inputError = ref('')
const bounds = ref({})
const moduleNames = ref([]), modulesLoading = ref(false), modulesFailed = ref(false)
const requests = createLatestRequestScope()
const moduleRequests = createLatestRequestScope()
const selected = computed(() => instances.value.find(v => v.id === state.value?.instanceID))
const detail = computed(() => state.value?.query.trace_id ? data.value[0] : null)
const date = value => new Date(value).toLocaleString(locale.value)
const number = value => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 3 }).format(value)
const traceInput = createAutoFilter({
  apply: applyLookup,
  enabled: () => canRead.value && route.name === 'Traces',
  visible: () => true,
  pause: () => { inputError.value = '' }
})
const futureDate = day => day.getTime() > Date.now()
function navigate(query, history = 'replace') {
  traceInput.cancel()
  return navigateMonitorRoute(router, { path: '/traces', query: Object.fromEntries(Object.entries(query).filter(([, value]) => value)) }, { history })
}
function filter(module, role) {
  if (!directLookup.value && module === state.value?.module && role === state.value?.role) return
  return navigate({ module_name: module, role })
}
function browseInstances() { return navigate({ ...state.value.query, instance_id: undefined, trace_id: undefined }) }
function returnToTraces() { return navigate({ ...state.value.query, trace_id: undefined }) }
function select(row) { return navigate({ ...state.value.query, ...bounds.value, page: undefined, instance_id: String(row.id), trace_id: undefined }, 'push') }
function changeStart(start) {
  traceInput.cancel()
  const now = Date.now()
  if (start && start.getTime() >= now) {
    pickerStart.value = bounds.value.start ? new Date(bounds.value.start) : null
    inputError.value = 'monitor.traces.futureStart'
    return
  }
  bounds.value = {
    start: start ? start.toISOString() : undefined,
    end: start ? new Date(Math.min(start.getTime() + 15 * 60_000, now)).toISOString() : undefined
  }
  return traceInput.submit()
}
function applyLookup() {
  const traceID = traceDraft.value.trim()
  if (traceID && !isTraceID(traceID)) {
    inputError.value = 'monitor.traces.invalidTraceID'
    return
  }
  inputError.value = ''
  const current = state.value?.query || {}
  const query = { ...current, ...bounds.value, trace_id: traceID || undefined }
  const openingDetail = traceID && !current.trace_id
  if (traceID && traceID !== current.trace_id) query.instance_id = undefined
  if (router.resolve({ path: '/traces', query }).fullPath === route.fullPath) return
  return navigate(query, openingDetail ? 'push' : 'replace')
}
function followLink(link) {
  if (!canRead.value) return
  try { return navigate(traceLinkQuery(link), 'push') } catch { error.value = 'monitor.traces.invalid' }
}
function logs(span) {
  if (!canReadLogs.value || !detail.value) return
  const to = Math.min(Date.now(), Date.parse(span.end) + 1000)
  const from = Math.min(to - 1, Math.max(to - 3600000, Date.parse(span.start) - 1000))
  return openConsoleRoute('/system/modules?' + new URLSearchParams({ tab: 'instances', module_name: span.module_name, status: 'all', log_instance: span.instance_id, trace_id: detail.value.trace_id, log_from: new Date(from).toISOString(), log_to: new Date(to).toISOString() }), { history: 'push' })
}
async function reload() {
  const ticket = requests.invalidate(); instances.value = []; data.value = []; total.value = 0; loading.value = false; error.value = ''
  instancesLoaded.value = false
  if (!canRead.value || route.name !== 'Traces') return
  if (!state.value) { error.value = 'monitor.traces.invalid'; return }
  const current = state.value
  const params = { page: current.instanceID ? 1 : current.page, page_size: 10, role: current.role, ...(current.module ? { module_name: current.module } : {}), ...(current.instanceID ? { ids: String(current.instanceID) } : {}) }
  let listed = false
  loading.value = true
  try {
    let subject
    if (!directLookup.value) {
      const list = await requests.request(config => traceAPI.instances(params, config))
      validateProcessInstances(list, current.instanceID ? 1 : current.page, params, 10)
      if (list.data.some(row => row.role !== current.role || (current.module && row.module_name !== current.module))) throw new Error('invalid_trace_response')
      subject = list.data.find(row => row.id === current.instanceID)
      if (current.instanceID && !subject) throw new Error('invalid_trace_response')
      if (!requests.current(ticket)) return
      instances.value = list.data; total.value = list.total; listed = true; instancesLoaded.value = true
    }
    if (!subject && !current.query.trace_id) return
    const { instance_id, trace_id, start, end } = current.query
    const query = Object.fromEntries(Object.entries({ instance_id, trace_id, start, end }).filter(([, value]) => value))
    const result = validateTraces(await requests.request(config => traceAPI.read(query, config)), subject, trace_id)
    if (!requests.current(ticket)) return
    data.value = result.data
  } catch (e) {
    if (!requests.current(ticket)) return
    data.value = []
    const unavailable = listed && e.response?.status === 503 && e.response?.data?.error_code === 'trace_backend_unavailable'
    const missing = e.response?.status === 404 && e.response?.data?.error_code === 'trace_not_found'
    if (!unavailable && !missing) { instances.value = []; total.value = 0; instancesLoaded.value = false }
    error.value = unavailable || e.response?.data?.error_code === 'trace_backend_unavailable' ? 'monitor.traces.unavailable' : missing ? 'monitor.traces.empty' : e.response?.status === 403 || e.response?.status === 401 ? 'monitor.traces.denied' : e.message === 'invalid_trace_response' ? 'monitor.traces.invalidResponse' : 'monitor.traces.failed'
  } finally { if (requests.current(ticket)) loading.value = false }
}
async function loadModules() {
  const ticket = moduleRequests.invalidate()
  moduleNames.value = []
  modulesFailed.value = false
  modulesLoading.value = false
  if (!canRead.value || route.name !== 'Traces') return
  modulesLoading.value = true
  try {
    const directory = await moduleRequests.request(config => traceAPI.modules(config))
    if (!moduleRequests.current(ticket)) return
    if (!Array.isArray(directory?.modules) || directory.modules.some(row => typeof row?.module_name !== 'string')) {
      throw new Error('invalid_trace_module_directory')
    }
    moduleNames.value = directory.modules.map(row => row.module_name).sort()
  } catch {
    if (moduleRequests.current(ticket)) modulesFailed.value = true
  } finally {
    if (moduleRequests.current(ticket)) modulesLoading.value = false
  }
}
function refresh() {
  loadModules()
  reload()
}
watch([() => route.fullPath, identity], () => {
  traceInput.cancel()
  inputError.value = ''
  traceDraft.value = state.value?.query.trace_id || ''
  bounds.value = { start: state.value?.query.start, end: state.value?.query.end }
  pickerStart.value = bounds.value.start ? new Date(bounds.value.start) : null
  reload()
}, { immediate: true })
watch(identity, loadModules, { immediate: true })
onUnmounted(() => { traceInput.dispose(); requests.invalidate(); moduleRequests.invalidate() })
</script>

<style scoped>
header, .filters { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin: 16px 0; }
.filters > .el-input, .filters > .el-select { width: 230px; max-width: 100%; }
.filters > .el-date-editor { width: 255px; max-width: 100%; }
.window-hint { color: var(--addp-text-secondary); font-size: 13px; }
</style>
