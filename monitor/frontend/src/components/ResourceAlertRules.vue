<template>
  <section data-testid="resource-rules">
    <el-alert v-if="filter" :title="`${filter.params.node_id} · ${mountLabel(filter.params)}`" type="info" :closable="false" data-testid="resource-rule-filter" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-button v-if="canCreate && canReadHosts && (!filter || (filter.valid && auth.hasPermission('monitor.resource_observation.read')))" type="primary" data-testid="resource-rule-create" :disabled="loading || !presets.length" @click="edit()">{{ t('monitor.resourceRules.create') }}</el-button>
    <el-table v-loading="loading" :data="rows" :empty-text="filter && !error && total === 0 ? t('monitor.resourceRules.noMatching') : undefined">
      <el-table-column prop="name" :label="t('monitor.platformAlerts.name')" min-width="160" show-overflow-tooltip />
      <el-table-column :label="t('monitor.targets.node')" min-width="190" show-overflow-tooltip><template #default="{ row }">{{ hosts[row.node_id]?.display_name || row.node_id }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.metric')" min-width="150"><template #default="{ row }">{{ metricLabel(row.metric_key) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.mount')" min-width="250" show-overflow-tooltip><template #default="{ row }">{{ row.metric_key === filesystemMetric ? mountLabel(row.dimensions) : '—' }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.trigger')" min-width="180"><template #default="{ row }">{{ t('monitor.resourceRules.above', { percent: row.trigger_percent, seconds: row.trigger_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.recovery')" min-width="180"><template #default="{ row }">{{ t('monitor.resourceRules.below', { percent: row.recovery_percent, seconds: row.recovery_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.platformAlerts.enabled')" width="100"><template #default="{ row }">{{ t(`monitor.platformAlerts.facts.${row.enabled ? 'yes' : 'no'}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.observation')" min-width="180"><template #default="{ row }">
        <span>{{ t(`monitor.resourceRules.verdicts.${row.last_verdict || 'unknown'}`) }}</span>
        <small v-if="row.last_error">{{ reason(row.last_error) }}</small>
        <small v-if="row.last_sample_at">{{ t('monitor.resourceRules.latest', { value: row.last_value, time: new Date(row.last_sample_at).toLocaleString() }) }}</small>
      </template></el-table-column>
      <el-table-column v-if="canUpdate || canDelete" :label="t('monitor.platformAlerts.actions')" width="160" fixed="right"><template #default="{ row }">
        <el-button v-if="canUpdate && canReadHosts" size="small" :disabled="busy" @click="edit(row)">{{ t('monitor.platformAlerts.edit') }}</el-button>
        <el-button v-if="canDelete" size="small" :disabled="busy" @click="remove(row)">{{ t('monitor.platformAlerts.delete') }}</el-button>
      </template></el-table-column>
    </el-table>
    <el-pagination v-if="total > 0 && !loading" layout="total, prev, pager, next" :current-page="page" :page-size="20" :total="total" @current-change="$emit('page', $event)" />
    <ResourceAlertRuleEditor ref="editor" :presets="presets" @saved="load" />
  </section>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { filesystemAlertMetric as filesystemMetric, resourceMountLabel as mountLabel, resourceRuleMetricLabelKey } from '../utils/platformAlerts'
import { platformAlertsAPI as api } from '../api/platformAlerts'
import { nodeResourcesAPI } from '../api/nodeResources'
import ResourceAlertRuleEditor from './ResourceAlertRuleEditor.vue'
import { resourceDimensionID } from '../utils/nodeResources'
import { loadHostReferences } from '../utils/hostReferences'
const props = defineProps({ page: { type: Number, required: true }, filter: { type: Object, default: null } })
defineEmits(['page'])
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.platform_resource_rule.read'))

const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.create'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.update'))
const canDelete = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.delete'))
const canReadHosts = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const rows = ref([]), total = ref(0), presets = ref([]), hosts = ref({}), loading = ref(false), error = ref(''), busy = ref(false)
const editor = ref(null)
const metricLabel = value => t(resourceRuleMetricLabelKey(value))
const reason = value => t(`monitor.resourceRules.reasons.${['warming_up','no_data','source_changed','collection_paused','collection_disabled','identity_unavailable','query_unavailable','query_budget','query_policy','unconfigured','mount_missing'].includes(value) ? value : 'unconfigured'}`)
const errorText = e => e.response?.data?.error || t('monitor.platformAlerts.failed')
let ticket = 0, epoch = 0, controller, disposed = false
const abandoned = Symbol('abandoned rule operation')
function check(lease) { if (disposed || lease !== epoch) throw abandoned }
function clear() { editor.value?.close(); epoch++; ticket++; controller?.abort(); rows.value = []; total.value = 0; presets.value = []; hosts.value = {}; error.value = ''; loading.value = false; busy.value = false }
async function load() {
  controller?.abort(); controller = new AbortController(); const own = ++ticket, lease = epoch
  rows.value = []; total.value = 0; hosts.value = {}; error.value = ''
  if (!canRead.value) return
  if (props.filter && !props.filter.valid) { error.value = t('monitor.resourceRules.invalidFilter'); return }
  loading.value = true
  try {
    const result = await api.rules(props.page, controller.signal, props.filter?.params); check(lease); if (own !== ticket) return
    if (!Array.isArray(result.data) || !Array.isArray(result.presets) || !Number.isSafeInteger(result.total)) throw new Error('invalid_rules')
    if (props.filter && (result.total > 1 || result.data.some(row => row.node_id !== props.filter.params.node_id || row.metric_key !== props.filter.params.metric_key || resourceDimensionID(row.dimensions || {}) !== resourceDimensionID(props.filter.params)))) throw new Error('invalid_rules')
    rows.value = result.data; total.value = result.total; presets.value = result.presets
    if (canReadHosts.value) {
      const signal = controller.signal
      const names = await loadHostReferences(result.data.map(row => row.node_id), id => nodeResourcesAPI.node(id, { signal }), () => !disposed && lease === epoch && own === ticket)
      check(lease); if (own !== ticket) return
      hosts.value = names
    }
  } catch (e) { if (lease === epoch && own === ticket && e !== abandoned && e.code !== 'ERR_CANCELED') { rows.value = []; total.value = 0; error.value = errorText(e) } }
  finally { if (lease === epoch && own === ticket) loading.value = false }
}
function edit(row) {
  if (!row && props.filter?.valid) { const {node_id,metric_key,...dimensions} = props.filter.params; editor.value?.show(null,{node_id},dimensions) }
  else editor.value?.show(row, row ? hosts.value[row.node_id] : null)
}
async function remove(row) {
  if (busy.value || !canDelete.value) return
  const lease = epoch; busy.value = true
  try { await ElMessageBox.confirm(t('monitor.resourceRules.deleteHint', { name: row.name }), t('monitor.platformAlerts.delete')); check(lease); await api.deleteRule(row); check(lease); await load() }
  catch (e) { if (lease === epoch && ![abandoned,'cancel','close'].includes(e)) error.value = e.response?.status === 409 ? t('monitor.resourceRules.deleteConflict') : errorText(e) }
  finally { if (lease === epoch) busy.value = false }
}
watch(() => [props.page, JSON.stringify(props.filter), JSON.stringify([auth.authContext, auth.permissions])], () => { clear(); load() }, { immediate: true })
onUnmounted(() => { disposed = true; clear() })
defineExpose({ load })
</script>
<style scoped>
small { display: block; color: var(--addp-text-secondary); margin-top: 6px; width: 100%; }
</style>
