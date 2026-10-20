<template>
  <section data-testid="http-rules">
    <p>{{ t('monitor.httpRules.scope') }}</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-button v-if="canCreate && canReadInstances" type="primary" @click="edit()">{{ t('monitor.httpRules.create') }}</el-button>
    <el-table :data="rows" v-loading="loading">
      <el-table-column prop="name" :label="t('monitor.platformAlerts.name')" min-width="160" />
      <el-table-column :label="t('monitor.httpRules.subject')" min-width="200"><template #default="{ row }">{{ row.module_name }} · {{ t(`monitor.http.layers.${row.role}`) }}<small>{{ row.route === null ? t('monitor.http.allRoutes') : `${row.method} ${row.route || t('monitor.http.unmatched')}` }}</small></template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.metric')" min-width="160"><template #default="{ row }">{{ metricLabel(row.metric_key) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.trigger')" min-width="180"><template #default="{ row }">{{ t('monitor.httpRules.above', { value: row.trigger_value, seconds: row.trigger_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.recovery')" min-width="180"><template #default="{ row }">{{ t('monitor.httpRules.below', { value: row.recovery_value, seconds: row.recovery_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.platformAlerts.enabled')"><template #default="{ row }">{{ t(`monitor.platformAlerts.facts.${row.enabled ? 'yes' : 'no'}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.observation')" min-width="180"><template #default="{ row }"><el-tag v-if="row.enabled && row.route !== null && row.metric_key === 'http.duration.mean_seconds' && row.last_verdict === 'risk'" type="danger">{{ t('monitor.httpRules.slow') }}</el-tag><span v-else>{{ t(`monitor.resourceRules.verdicts.${row.last_verdict || 'unknown'}`) }}</span><small v-if="row.last_error">{{ reason(row.last_error) }}</small><small v-if="row.last_value !== null">{{ row.last_value }}</small><small v-if="row.last_evaluated_at">{{ new Date(row.last_evaluated_at).toLocaleString() }}</small></template></el-table-column>
      <el-table-column v-if="canUpdate || canDelete" :label="t('monitor.platformAlerts.actions')" min-width="160"><template #default="{ row }"><el-button v-if="canUpdate && canReadInstances" :disabled="busy" @click="edit(row)">{{ t('monitor.platformAlerts.edit') }}</el-button><el-button v-if="canDelete" :disabled="busy" @click="remove(row)">{{ t('monitor.platformAlerts.delete') }}</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="total > 0" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="$emit('page', $event)" />
    <el-dialog v-model="open" :title="t(editing ? 'monitor.httpRules.edit' : 'monitor.httpRules.create')" width="min(680px, 95vw)" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top" :disabled="busy" @submit.prevent="save">
        <el-form-item :label="t('monitor.platformAlerts.name')"><el-input v-model="draft.name" maxlength="100" data-testid="http-rule-name" /></el-form-item>
        <el-form-item :label="t('monitor.httpRules.anchor')">
          <div class="filters"><el-input v-model="moduleFilter" :readonly="!!editing" :placeholder="t('monitor.process.module')" /><el-select v-model="roleFilter" :disabled="!!editing"><el-option v-for="role in ['backend','ingress']" :key="role" :value="role" :label="t(`monitor.http.layers.${role}`)" /></el-select><el-button @click="loadInstances(1)">{{ t('common.search') }}</el-button></div>
          <el-select v-model="draft.instance_id" :loading="instancesLoading" :aria-label="t('monitor.httpRules.anchor')"><el-option v-for="row in candidates" :key="row.id" :value="row.id" :label="`${row.module_name} · ${row.instance_id}`" /></el-select>
          <el-pagination v-if="candidateTotal > 10" layout="prev, pager, next" :total="candidateTotal" :page-size="10" :current-page="candidatePage" @current-change="loadInstances" />
          <small>{{ t('monitor.httpRules.anchorHint') }}</small>
        </el-form-item>
        <el-form-item :label="t('monitor.http.route')"><el-switch v-model="perRoute" :disabled="!!editing" :active-text="t('monitor.httpRules.perRoute')" /><el-input v-if="perRoute" v-model="draft.route" :readonly="!!editing" maxlength="1024" :placeholder="t('monitor.httpRules.template')" /><el-select v-if="perRoute" v-model="draft.method" :disabled="!!editing"><el-option v-for="method in methods" :key="method" :value="method" /></el-select></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.metric')"><el-select v-model="draft.metric_key" :disabled="!!editing" :aria-label="t('monitor.resourceRules.metric')"><el-option v-for="metric in httpRuleMetrics" :key="metric.key" :value="metric.key" :label="metricLabel(metric.key)" /></el-select></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.trigger')"><div class="filters"><el-input-number v-model="draft.trigger_value" :min="0" :max="draft.metric_key === 'http.failures.percent' ? 100 : 1e9" :aria-label="t('monitor.httpRules.triggerValue')" /><el-input-number v-model="draft.trigger_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.triggerSeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.recovery')"><div class="filters"><el-input-number v-model="draft.recovery_value" :min="0" :max="1e9" :aria-label="t('monitor.httpRules.recoveryValue')" /><el-input-number v-model="draft.recovery_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.recoverySeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.enabled')"><el-switch v-model="draft.enabled" :disabled="!editing" /><small>{{ t('monitor.httpRules.policyHint') }}</small></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="busy" @click="open = false">{{ t('common.close') }}</el-button><el-button type="primary" :loading="busy" :disabled="!selected" @click="save">{{ t('monitor.platformAlerts.save') }}</el-button></template>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { useAuthStore } from '../store/auth'
import { platformAlertsAPI as api } from '../api/platformAlerts'
import { processResourcesAPI } from '../api/processResources'
import { validateProcessInstances } from '../utils/processResources'
import { httpRuleMetrics, validHTTPPolicy, validHTTPSubject, validateHTTPRules } from '../utils/httpRules'
const props = defineProps({ page: { type: Number, required: true } })
defineEmits(['page'])
const auth = useAuthStore(), { t } = useI18n(), requests = createLatestRequestScope(), candidatesRequest = createLatestRequestScope()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.platform_http_rule.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.platform_http_rule.create'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('monitor.platform_http_rule.update'))
const canDelete = computed(() => canRead.value && auth.hasPermission('monitor.platform_http_rule.delete'))
const canReadInstances = computed(() => canRead.value && auth.hasPermission('platform.module.read'))
const rows = ref([]), total = ref(0), loading = ref(false), error = ref(''), busy = ref(false), open = ref(false), editing = ref(''), draft = ref({}), editError = ref(''), perRoute = ref(false)
const candidates = ref([]), candidateTotal = ref(0), candidatePage = ref(1), instancesLoading = ref(false), moduleFilter = ref(''), roleFilter = ref('backend')
const selected = computed(() => candidates.value.find(row => row.id === draft.value.instance_id))
const methods = ['GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS','CONNECT','TRACE','QUERY','_OTHER']
const metricLabel = key => t(`monitor.http.metrics.${httpRuleMetrics.find(item => item.key === key)?.name || 'qps'}`)
const reason = code => t(`monitor.resourceRules.reasons.${['warming_up','no_data','source_changed','collection_paused','collection_disabled','identity_unavailable','query_unavailable','query_budget','query_policy'].includes(code) ? code : 'unconfigured'}`)
const errorText = e => e.response?.data?.error || t('monitor.platformAlerts.failed')
let lease = 0
async function load() {
  const ticket = requests.invalidate(); lease = ticket
  open.value = false; candidatesRequest.invalidate(); candidates.value = []; rows.value = []; total.value = 0; error.value = ''; loading.value = false
  if (!canRead.value) return
  loading.value = true
  try { const page = validateHTTPRules(await requests.request(config => api.httpRules(props.page, config.signal)), props.page); if (requests.current(ticket)) { rows.value = page.data; total.value = page.total } }
  catch (e) { if (requests.current(ticket)) error.value = errorText(e) }
  finally { if (requests.current(ticket)) loading.value = false }
}
async function loadInstances(page = 1) {
  const ticket = candidatesRequest.invalidate(), owner = lease
  candidates.value = []; candidateTotal.value = 0; draft.value.instance_id = null; instancesLoading.value = false
  if (!open.value || !canReadInstances.value) return
  instancesLoading.value = true; editError.value = ''
  const params = { page, page_size: 10, status: 'up', role: roleFilter.value, ...(moduleFilter.value.trim() ? { module_name: moduleFilter.value.trim() } : {}) }
  try {
    const result = validateProcessInstances(await candidatesRequest.request(config => processResourcesAPI.instances(params, config)), page, params, 10)
    if (!candidatesRequest.current(ticket) || !requests.current(owner) || !open.value) return
    if (result.data.some(row => row.role !== params.role || (params.module_name && row.module_name !== params.module_name))) throw new Error('invalid_instance_scope')
    candidates.value = result.data; candidatePage.value = page; candidateTotal.value = result.total
  } catch (e) { if (candidatesRequest.current(ticket) && requests.current(owner)) editError.value = errorText(e) }
  finally { if (candidatesRequest.current(ticket)) instancesLoading.value = false }
}
function edit(row) {
  if (busy.value || !canReadInstances.value || (row ? !canUpdate.value : !canCreate.value)) return
  editing.value = row?.id || ''; editError.value = ''; perRoute.value = row ? row.route !== null : false
  draft.value = row ? Object.fromEntries(['version','name','metric_key','route','method','trigger_value','recovery_value','trigger_seconds','recovery_seconds','enabled'].map(key => [key, row[key]])) : { name: '', metric_key: 'http.failures.percent', route: '', method: 'GET', trigger_value: null, recovery_value: null, trigger_seconds: 60, recovery_seconds: 60, enabled: false }
  moduleFilter.value = row?.module_name || ''; roleFilter.value = row?.role || 'backend'; open.value = true; loadInstances()
}
async function save() {
  if (busy.value || !selected.value || (editing.value ? !canUpdate.value : !canCreate.value)) return
  const input = { ...draft.value, name: draft.value.name.trim(), route: perRoute.value ? draft.value.route : null, method: perRoute.value ? draft.value.method : '' }
  if (!input.name || !validHTTPPolicy(input, input.metric_key) || !validHTTPSubject({ ...input, module_name: selected.value.module_name, role: selected.value.role })) { editError.value = t('monitor.httpRules.invalid'); return }
  const owner = lease; busy.value = true; editError.value = ''
  try {
    await requests.request(() => editing.value ? api.updateHTTPRule(editing.value, input) : api.createHTTPRule({ ...input, enabled: false }))
    if (!requests.current(owner)) return
    open.value = false; await load()
  } catch (e) { if (requests.current(owner)) editError.value = e.response?.status === 409 ? t('monitor.httpRules.conflict') : errorText(e) }
  finally { if (requests.current(owner) || !open.value) busy.value = false }
}
async function remove(row) {
  if (busy.value || !canDelete.value) return
  const owner = lease; busy.value = true
  try { await ElMessageBox.confirm(t('monitor.resourceRules.deleteHint', { name: row.name }), t('monitor.platformAlerts.delete')); if (!requests.current(owner)) return; await api.deleteHTTPRule(row); if (requests.current(owner)) await load() }
  catch (e) { if (requests.current(owner) && !['cancel','close'].includes(e)) error.value = e.response?.status === 409 ? t('monitor.resourceRules.deleteConflict') : errorText(e) }
  finally { if (requests.current(owner) || !open.value) busy.value = false }
}
watch(open, value => { if (!value) { candidatesRequest.invalidate(); candidates.value = []; draft.value = {}; editError.value = '' } })
watch(() => [props.page, JSON.stringify([auth.authContext, auth.permissions])], () => { open.value = false; busy.value = false; candidatesRequest.invalidate(); load() }, { immediate: true })
onUnmounted(() => { requests.dispose(); candidatesRequest.dispose() })
defineExpose({ load })
</script>
<style scoped>
.filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; width: 100%; margin-bottom: 8px; }
.filters .el-input, .filters .el-select { width: 180px; }
small { display: block; width: 100%; margin-top: 6px; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
</style>
