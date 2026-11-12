<template>
  <section class="traces" data-testid="traces">
    <el-alert v-if="!canRead" :title="t('monitor.traces.denied')" type="warning" :closable="false" />
    <template v-else>
      <header>
        <h2>{{ t('monitor.traces.title') }}</h2>
        <el-button v-if="selected || state?.instanceID || directLookup" :icon="ArrowLeft" @click="browseInstances">{{ t('monitor.traces.browseInstances') }}</el-button>
        <el-button @click="refresh">{{ t('common.refresh') }}</el-button>
      </header>
      <el-alert :title="t('monitor.traces.coverage')" type="info" :closable="false" />
      <div class="filters" data-testid="trace-search">
        <el-select v-if="!directLookup" :model-value="state?.module || ''" :placeholder="t('monitor.process.module')" :aria-label="t('monitor.process.module')" :loading="modulesLoading" clearable filterable @change="selectModule">
          <el-option v-for="module in moduleNames" :key="module" :value="module" :label="module" />
        </el-select>
        <div class="trace-window">
          <el-select :model-value="state?.query.start ? '' : state?.query.range || '15m'" :placeholder="t('monitor.traces.customWindow')" :aria-label="t('monitor.traces.recentWindow')" @change="changeRecent">
            <el-option v-for="range in recentRanges" :key="range" :value="range" :label="t(`monitor.traces.windows.${range}`)" />
          </el-select>
          <el-date-picker v-model="pickerStart" type="datetime" :placeholder="t('monitor.traces.customStart')" :aria-label="t('monitor.traces.start')" :disabled-date="futureDate" :show-now="false" @change="changeStart" />
          <span v-if="bounds.end" class="window-hint">{{ t('monitor.traces.windowEnd', { time: date(bounds.end) }) }}</span>
        </div>
        <el-input v-model="traceDraft" :placeholder="t('monitor.traces.traceID')" :aria-label="t('monitor.traces.traceID')" clearable @input="traceInput.queue" @keydown.enter="traceInput.enter" />
      </div>
      <div v-if="state?.query.http_method">
        <el-tag closable type="warning" @close="clearHTTPFilter">{{ t('monitor.traces.httpFailureFilter', { method: state.query.http_method, route: state.query.http_route }) }}</el-tag>
      </div>
      <el-alert v-if="inputError" :title="t(inputError)" type="warning" :closable="false" />
      <el-alert v-if="modulesFailed" :title="t('monitor.traces.modulesFailed')" type="warning" :closable="false" />
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="trace-error" />
      <div v-if="missingTraceLogs && canReadLogs && !loading" class="trace-log-alternative" data-testid="trace-missing-logs">
        <p>{{ t('monitor.traces.missingLogsHint') }}</p>
        <p>{{ t('monitor.traces.logWindow', { from: date(missingTraceLogs.start), to: date(missingTraceLogs.end) }) }}</p>
        <el-button @click="openLogs(missingTraceLogs)">{{ t('monitor.traces.instanceLogs') }}</el-button>
      </div>
      <el-table v-if="!directLookup" :data="instances" v-loading="loading && !instancesLoaded" :empty-text="t(loading ? 'common.loading' : instancesLoaded ? 'monitor.traces.noInstances' : 'monitor.traces.notLoaded')" data-testid="trace-instances">
        <el-table-column prop="module_name" :label="t('monitor.process.module')" min-width="130" />
        <el-table-column :label="t('monitor.http.instance')" min-width="270" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="selected?.id === row.id">{{ row.instance_id }}</span>
            <el-button v-else link type="primary" @click="select(row)">{{ row.instance_id }}</el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.traces.instanceStarted')" min-width="155">
          <template #default="{ row }">{{ row.process_started_at ? date(row.process_started_at) : '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.traces.samplesInWindow')" min-width="150">
          <template #default="{ row }">
            <el-tag :type="sampleStates[row.id] === 'has_samples' ? 'success' : sampleStates[row.id] === 'not_found' ? 'info' : 'warning'" data-testid="trace-sample-state">
              {{ t('monitor.traces.sampleStates.' + (samplesLoading || (selected?.id === row.id && loading && !sampleStates[row.id]) ? 'checking' : sampleStates[row.id] || 'unavailable')) }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-if="!directLookup && instancesLoaded" layout="total, prev, pager, next" :page-size="10" :total="total" :current-page="state?.page || 1" @current-change="page => browseInstances(page)" />
      <div v-if="state?.query.trace_id && state.instanceID" data-testid="trace-detail-navigation">
        <el-button :icon="ArrowLeft" @click="returnToTraces">{{ t('monitor.traces.browseTraces') }}</el-button>
      </div>
      <template v-if="selected && !detail">
        <h3>{{ selected.module_name }} · {{ selected.instance_id }}</h3>
        <el-table :data="data" :empty-text="t(loading ? 'common.loading' : error && error !== 'monitor.traces.empty' ? 'monitor.traces.notLoaded' : 'monitor.traces.empty')" data-testid="trace-results">
          <el-table-column prop="trace_id" :label="t('monitor.traces.traceID')" min-width="260" />
          <el-table-column :label="t('monitor.traces.start')" min-width="170"><template #default="{ row }">{{ date(row.start) }}</template></el-table-column>
          <el-table-column prop="duration_ms" :label="t('monitor.traces.duration')" min-width="140"><template #default="{ row }">{{ number(row.duration_ms) }}</template></el-table-column>
          <el-table-column :label="t('monitor.traces.spans')" width="100"><template #default="{ row }">{{ row.spans.length }}</template></el-table-column>
          <el-table-column :label="t('common.actions')" width="130"><template #default="{ row }"><el-button link @click="openTrace(row)">{{ t('monitor.traces.viewTrace') }}</el-button></template></el-table-column>
        </el-table>
      </template>
      <trace-details v-if="detail" :trace="detail" :matched-failure="matchedFailure" :can-read-logs="canReadLogs" @logs="logs" @follow-link="followLink" />
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
import { findHTTPFailureSpan, isTraceID, traceRoute, validateTraces, validateTraceSamples, traceLinkQuery, traceRecentMinutes } from '../utils/traces'
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
const matchedFailure = ref(null)
const missingTraceLogs = ref(null)
const bounds = ref({})
const moduleNames = ref([]), modulesLoading = ref(false), modulesFailed = ref(false)
const requests = createLatestRequestScope()
const moduleRequests = createLatestRequestScope()
const sampleRequests = createLatestRequestScope()
const sampleStates = ref({}), samplesLoading = ref(false), sampleWindow = ref(null)
const selected = computed(() => {
  if (state.value?.instanceID) return instances.value.find(row => row.id === state.value.instanceID)
  if (state.value?.module && total.value === 1) return instances.value[0]
})
const detail = computed(() => state.value?.query.trace_id ? data.value[0] : null)
const date = value => new Date(value).toLocaleString(locale.value)
const recentRanges = ['15m', ...Object.keys(traceRecentMinutes)]
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
function selectModule(module) {
  if (module === state.value?.module) return
  return navigate({ module_name: module })
}
function browseInstances(page) {
  const { instance_id, trace_id, http_method, http_route, ...query } = state.value.query
  if (typeof page !== 'number' && !state.value.instanceID && selected.value) delete query.module_name
  return navigate({ ...query, ...(typeof page === 'number' ? { page: String(page) } : {}) })
}
function clearHTTPFilter() { return navigate({ ...state.value.query, http_method: undefined, http_route: undefined }) }
function returnToTraces() { return navigate({ ...state.value.query, trace_id: undefined }) }
function select(row) { return navigate({ ...state.value.query, ...bounds.value, ...sampleWindow.value, range: undefined, page: undefined, instance_id: String(row.id), trace_id: undefined }, 'push') }
function openTrace(row) { return navigate({ ...state.value.query, page: undefined, instance_id: String(selected.value.id), trace_id: row.trace_id }, 'push') }
function changeRecent(range) {
  bounds.value = { start: undefined, end: undefined, range: range === '15m' ? undefined : range }
  return traceInput.submit()
}
function changeStart(start) {
  traceInput.cancel()
  const now = Date.now()
  if (start && start.getTime() >= now) {
    pickerStart.value = bounds.value.start ? new Date(bounds.value.start) : null
    inputError.value = 'monitor.traces.futureStart'
    return
  }
  bounds.value = {
    range: undefined,
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
  if (traceID && traceID !== current.trace_id) {
    query.instance_id = undefined
    query.http_method = undefined
    query.http_route = undefined
  }
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
  return openLogs({ module_name: span.module_name, instance_id: span.instance_id, trace_id: detail.value.trace_id, start: new Date(from).toISOString(), end: new Date(to).toISOString() })
}
function openLogs(source) {
  if (!canReadLogs.value) return
  return openConsoleRoute('/system/modules?' + new URLSearchParams({ tab: 'instances', module_name: source.module_name, status: 'all', log_instance: source.instance_id, trace_id: source.trace_id, log_from: source.start, log_to: source.end }), { history: 'push' })
}
async function reload(preserveInstance = false) {
  const ticket = requests.invalidate(); data.value = []; loading.value = false; error.value = ''
  missingTraceLogs.value = null
  if (!preserveInstance) {
    instances.value = []; total.value = 0; instancesLoaded.value = false
    sampleRequests.invalidate(); sampleStates.value = {}; samplesLoading.value = false; sampleWindow.value = null
  }
  matchedFailure.value = null
  if (!canRead.value || route.name !== 'Traces') return
  if (!state.value) { error.value = 'monitor.traces.invalid'; return }
  const current = state.value
  const params = { page: current.instanceID ? 1 : current.page, page_size: 10, role: current.role, ...(current.module ? { module_name: current.module } : {}), ...(current.instanceID ? { ids: String(current.instanceID) } : {}) }
  let listed = false
  let logQuery = null
  loading.value = true
  try {
    if (!directLookup.value && !preserveInstance) {
      const list = await requests.request(config => traceAPI.instances(params, config))
      validateProcessInstances(list, current.instanceID ? 1 : current.page, params, 10)
      if (list.data.some(row => row.role !== current.role || (current.module && row.module_name !== current.module))) throw new Error('invalid_trace_response')
      if (current.instanceID && !list.data.some(row => row.id === current.instanceID)) throw new Error('invalid_trace_response')
      if (!requests.current(ticket)) return
      instances.value = list.data; total.value = list.total; listed = true; instancesLoaded.value = true
    }
    const subject = selected.value
    if (!subject && !current.query.trace_id) {
      if (instances.value.length) {
        const now = Math.floor(Date.now() / 1000) * 1000
        const window = current.query.start ? { start: current.query.start, end: current.query.end } : {
          start: new Date(now - (traceRecentMinutes[current.query.range] || 15) * 60_000).toISOString(), end: new Date(now).toISOString()
        }
        sampleWindow.value = window
        checkSamples(instances.value.map(row => row.id), window)
      }
      return
    }
    const { trace_id, range, http_method, http_route } = current.query
    let { start, end } = current.query
    if (range) {
      const now = Date.now()
      start = new Date(now - traceRecentMinutes[range] * 60_000).toISOString()
      end = new Date(now).toISOString()
    }
    if (trace_id && subject) {
      if (!start) {
        const now = Math.floor(Date.now() / 1000) * 1000
        start = new Date(now - 15 * 60_000).toISOString()
        end = new Date(now).toISOString()
      }
      logQuery = { module_name: subject.module_name, instance_id: subject.instance_id, trace_id, start, end }
    }
    const instance_id = subject ? String(subject.id) : undefined
    const query = Object.fromEntries(Object.entries({ instance_id, trace_id, start, end, http_method, http_route }).filter(([, value]) => value))
    const result = validateTraces(await requests.request(config => traceAPI.read(query, config)), subject, trace_id)
    if (!requests.current(ticket)) return
    if (http_method) {
      const matches = result.data.map(trace => findHTTPFailureSpan(trace, subject, query, result))
      if (matches.some(span => !span)) throw new Error('invalid_trace_response')
      if (trace_id) matchedFailure.value = matches[0]
    }
    data.value = result.data
    if (subject) sampleStates.value = { [subject.id]: result.data.length ? 'has_samples' : 'not_found' }
  } catch (e) {
    if (!requests.current(ticket)) return
    data.value = []
    const unavailable = listed && e.response?.status === 503 && e.response?.data?.error_code === 'trace_backend_unavailable'
    const missing = e.response?.status === 404 && e.response?.data?.error_code === 'trace_not_found'
    if (missing && logQuery) { missingTraceLogs.value = logQuery; traceDraft.value = logQuery.trace_id }
    if (!unavailable && !missing) { instances.value = []; total.value = 0; instancesLoaded.value = false }
    error.value = unavailable || e.response?.data?.error_code === 'trace_backend_unavailable' ? 'monitor.traces.unavailable' : missing ? 'monitor.traces.empty' : e.response?.status === 403 || e.response?.status === 401 ? 'monitor.traces.denied' : e.message === 'invalid_trace_response' ? 'monitor.traces.invalidResponse' : 'monitor.traces.failed'
  } finally { if (requests.current(ticket)) loading.value = false }
}
async function checkSamples(ids, window) {
  const ticket = sampleRequests.invalidate()
  samplesLoading.value = true
  try {
    const value = await sampleRequests.request(config => traceAPI.samples({ instance_ids: ids.join(','), ...window }, config))
    if (!sampleRequests.current(ticket)) return
    sampleStates.value = validateTraceSamples(value, ids, window)
  } catch (e) {
    if (!sampleRequests.current(ticket)) return
    sampleStates.value = {}
    if ([401, 403, 404].includes(e.response?.status)) {
      instances.value = []; total.value = 0; instancesLoaded.value = false
      error.value = 'monitor.traces.denied'
    }
  } finally { if (sampleRequests.current(ticket)) samplesLoading.value = false }
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
watch([() => route.fullPath, identity], ([, currentIdentity], [previousPath, previousIdentity] = []) => {
  const previousQuery = previousPath ? router.resolve(previousPath).query : {}
  const preserveInstance = currentIdentity === previousIdentity && !directLookup.value && Boolean(selected.value) && instancesLoaded.value && total.value === 1 &&
    previousQuery.trace_id !== route.query.trace_id &&
    Object.keys({ ...previousQuery, ...route.query }).filter(key => !['trace_id', 'instance_id'].includes(key)).every(key => previousQuery[key] === route.query[key])
  traceInput.cancel()
  inputError.value = ''
  traceDraft.value = directLookup.value ? state.value.query.trace_id : ''
  bounds.value = { start: state.value?.query.start, end: state.value?.query.end, range: state.value?.query.range }
  pickerStart.value = bounds.value.start ? new Date(bounds.value.start) : null
  reload(preserveInstance)
}, { immediate: true })
watch(identity, loadModules, { immediate: true })
onUnmounted(() => { traceInput.dispose(); requests.invalidate(); moduleRequests.invalidate(); sampleRequests.invalidate() })
</script>

<style scoped>
.traces {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
  padding: 24px;
}

header, .filters, .trace-window { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.filters > .el-input, .filters > .el-select { width: 195px; max-width: 100%; }
.trace-window { min-width: 0; max-width: 100%; }
.trace-window > .el-select { width: 138px; max-width: 100%; }
.trace-window > .el-date-editor { width: 212px; max-width: 100%; }
.window-hint { color: var(--addp-text-secondary); font-size: 13px; }
.trace-log-alternative p { margin: 0 0 8px; overflow-wrap: anywhere; }

@media (max-width: 700px) {
  .traces { padding: 16px; }
}
</style>
