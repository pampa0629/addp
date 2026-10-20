<template>
  <section class="traces" data-testid="traces">
    <el-alert v-if="!canRead" :title="t('monitor.traces.denied')" type="warning" :closable="false" />
    <template v-else>
      <header><h2>{{ t('monitor.traces.title') }}</h2><el-button @click="reload">{{ t('common.refresh') }}</el-button></header>
      <el-alert :title="t('monitor.traces.coverage')" type="info" :closable="false" />
      <form class="filters" @submit.prevent="filter">
        <el-input v-model="moduleDraft" :placeholder="t('monitor.process.module')" :aria-label="t('monitor.process.module')" clearable />
        <el-select v-model="roleDraft" :aria-label="t('monitor.http.layer')"><el-option v-for="role in ['backend', 'ingress']" :key="role" :value="role" :label="t(`monitor.http.layers.${role}`)" /></el-select>
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
      </form>
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="trace-error" />
      <el-table :data="instances" v-loading="loading" :empty-text="t('monitor.http.empty')">
        <el-table-column prop="module_name" :label="t('monitor.process.module')" min-width="130" />
        <el-table-column prop="instance_id" :label="t('monitor.http.instance')" min-width="240" show-overflow-tooltip />
        <el-table-column :label="t('monitor.traces.title')" width="130"><template #default="{ row }"><el-button link @click="select(row)">{{ t('monitor.traces.open') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination layout="total, prev, pager, next" :page-size="10" :total="total" :current-page="state?.page || 1" @current-change="page => navigate({ ...state.query, page: String(page), instance_id: undefined, trace_id: undefined })" />
      <template v-if="selected">
        <h3>{{ selected.module_name }} · {{ selected.instance_id }}</h3>
        <form class="filters" @submit.prevent="search">
          <el-date-picker v-model="rangeDraft" type="datetimerange" :start-placeholder="t('monitor.traces.start')" :end-placeholder="t('monitor.traces.end')" />
          <el-input v-model="traceDraft" :placeholder="t('monitor.traces.traceID')" :aria-label="t('monitor.traces.traceID')" clearable />
          <el-button native-type="submit">{{ t('common.search') }}</el-button>
        </form>
        <el-table :data="data" :empty-text="t('monitor.traces.empty')" data-testid="trace-results">
          <el-table-column prop="trace_id" :label="t('monitor.traces.traceID')" min-width="260" />
          <el-table-column :label="t('monitor.traces.start')" min-width="170"><template #default="{ row }">{{ date(row.start) }}</template></el-table-column>
          <el-table-column prop="duration_ms" :label="t('monitor.traces.duration')" min-width="140"><template #default="{ row }">{{ number(row.duration_ms) }}</template></el-table-column>
          <el-table-column :label="t('monitor.traces.spans')" width="100"><template #default="{ row }">{{ row.spans.length }}</template></el-table-column>
          <el-table-column :label="t('common.actions')" width="130"><template #default="{ row }"><el-button link @click="navigate({ ...state.query, trace_id: row.trace_id }, 'push')">{{ t('monitor.http.details') }}</el-button></template></el-table-column>
        </el-table>
      </template>
      <template v-if="detail">
        <h3>{{ detail.trace_id }}</h3>
        <el-tag v-if="detail.partial" type="warning">{{ t('monitor.traces.partial') }}</el-tag>
        <el-table :data="detail.spans" data-testid="trace-spans">
          <el-table-column prop="span_id" label="Span ID" min-width="160" show-overflow-tooltip />
          <el-table-column prop="parent_span_id" :label="t('monitor.traces.parentSpan')" min-width="160" show-overflow-tooltip />
          <el-table-column prop="module_name" :label="t('monitor.process.module')" min-width="130" />
          <el-table-column prop="instance_id" :label="t('monitor.http.instance')" min-width="180" show-overflow-tooltip />
          <el-table-column prop="kind" :label="t('monitor.traces.kind')" width="100" />
          <el-table-column prop="method" :label="t('monitor.http.method')" width="100" />
          <el-table-column prop="route" :label="t('monitor.http.route')" min-width="180" />
          <el-table-column prop="status" :label="t('monitor.traces.status')" width="90" />
          <el-table-column :label="t('monitor.traces.duration')" min-width="130"><template #default="{ row }">{{ number(row.duration_ms) }}</template></el-table-column>
          <el-table-column v-if="canReadLogs" :label="t('monitor.traces.logs')" width="100"><template #default="{ row }"><el-button link @click="logs(row)">{{ t('monitor.traces.logs') }}</el-button></template></el-table-column>
        </el-table>
        <h3>{{ t('monitor.traces.relationships') }}</h3>
        <p class="hint">{{ t('monitor.traces.relationshipHint') }}</p>
        <el-table :data="detail.edges" :empty-text="t('monitor.traces.noEdges')" data-testid="trace-edges">
          <el-table-column prop="from_module" :label="t('monitor.traces.caller')" />
          <el-table-column prop="from_instance" :label="t('monitor.http.instance')" show-overflow-tooltip />
          <el-table-column prop="to_module" :label="t('monitor.traces.callee')" />
          <el-table-column prop="to_instance" :label="t('monitor.http.instance')" show-overflow-tooltip />
        </el-table>
      </template>
    </template>
  </section>
</template>

<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { useAuthStore } from '../store/auth'
import { traceAPI } from '../api/traces'
import { traceRoute, validateTraces } from '../utils/traces'
import { validateProcessInstances } from '../utils/processResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.trace.read') && auth.hasPermission('platform.module.read'))
const canReadLogs = computed(() => canRead.value && auth.hasPermission('platform.module_log.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => { try { return traceRoute(route.query) } catch { return null } })
const instances = ref([]), total = ref(0), data = ref([]), loading = ref(false), error = ref('')
const moduleDraft = ref(''), roleDraft = ref('backend'), traceDraft = ref(''), rangeDraft = ref(null)
const requests = createLatestRequestScope()
const selected = computed(() => instances.value.find(v => v.id === state.value?.instanceID))
const detail = computed(() => state.value?.query.trace_id ? data.value[0] : null)
const date = value => new Date(value).toLocaleString(locale.value)
const number = value => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 3 }).format(value)
function navigate(query, history = 'replace') { return navigateMonitorRoute(router, { path: '/traces', query: Object.fromEntries(Object.entries(query).filter(([, value]) => value)) }, { history }) }
function filter() { return navigate({ module_name: moduleDraft.value.trim(), role: roleDraft.value }) }
function select(row) { const now = new Date(); return navigate({ ...state.value.query, page: undefined, instance_id: String(row.id), trace_id: undefined, start: new Date(now.getTime() - 900000).toISOString(), end: now.toISOString() }, 'push') }
function search() { if (!rangeDraft.value?.every(value => Number.isFinite(new Date(value).getTime()))) { error.value = 'monitor.traces.invalid'; return }; return navigate({ ...state.value.query, trace_id: traceDraft.value.trim(), start: new Date(rangeDraft.value[0]).toISOString(), end: new Date(rangeDraft.value[1]).toISOString() }) }
function logs(span) {
  if (!canReadLogs.value || !detail.value) return
  const to = Math.min(Date.now(), Date.parse(span.end) + 1000)
  const from = Math.min(to - 1, Math.max(to - 3600000, Date.parse(span.start) - 1000))
  return openConsoleRoute('/system/modules?' + new URLSearchParams({ tab: 'instances', module_name: span.module_name, status: 'all', log_instance: span.instance_id, trace_id: detail.value.trace_id, log_from: new Date(from).toISOString(), log_to: new Date(to).toISOString() }), { history: 'push' })
}
async function reload() {
  const ticket = requests.invalidate(); instances.value = []; data.value = []; total.value = 0; loading.value = false; error.value = ''
  if (!canRead.value || route.name !== 'Traces' || document.hidden) return
  if (!state.value) { error.value = 'monitor.traces.invalid'; return }
  const current = state.value
  moduleDraft.value = current.module; roleDraft.value = current.role; traceDraft.value = current.query.trace_id || ''
  rangeDraft.value = current.query.start ? [new Date(current.query.start), new Date(current.query.end)] : null
  const params = { page: current.instanceID ? 1 : current.page, page_size: 10, role: current.role, ...(current.module ? { module_name: current.module } : {}), ...(current.instanceID ? { ids: String(current.instanceID) } : {}) }
  loading.value = true
  try {
    const list = await requests.request(config => traceAPI.instances(params, config))
    validateProcessInstances(list, current.instanceID ? 1 : current.page, params, 10)
    if (list.data.some(row => row.role !== current.role || (current.module && row.module_name !== current.module))) throw new Error('invalid_trace_response')
    const subject = list.data.find(row => row.id === current.instanceID)
    if (current.instanceID && !subject) throw new Error('invalid_trace_response')
    let result = { data: [] }
    if (subject) result = validateTraces(await requests.request(config => traceAPI.read({ instance_id: String(subject.id), ...(current.query.trace_id ? { trace_id: current.query.trace_id } : {}), ...(current.query.start ? { start: current.query.start, end: current.query.end } : {}) }, config)), subject, current.query.trace_id)
    if (!requests.current(ticket)) return
    instances.value = list.data; total.value = list.total; data.value = result.data
    if (subject && !current.query.start) rangeDraft.value = [new Date(result.start), new Date(result.end)]
  } catch (e) {
    if (!requests.current(ticket)) return
    data.value = []; instances.value = []; total.value = 0
    error.value = e.response?.status === 404 ? 'monitor.traces.empty' : e.response?.status === 403 || e.response?.status === 401 ? 'monitor.traces.denied' : e.message === 'invalid_trace_response' ? 'monitor.traces.invalidResponse' : 'monitor.traces.failed'
  } finally { if (requests.current(ticket)) loading.value = false }
}
function visibility() { if (document.hidden) { requests.invalidate(); data.value = []; loading.value = false } else reload() }
watch(() => [route.fullPath, identity.value], reload, { immediate: true })
document.addEventListener('visibilitychange', visibility)
onUnmounted(() => { requests.invalidate(); document.removeEventListener('visibilitychange', visibility) })
</script>

<style scoped>
header, .filters { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin: 16px 0; }
.filters > .el-input, .filters > .el-select { width: 230px; }
.hint { color: var(--addp-text-secondary); }
</style>
