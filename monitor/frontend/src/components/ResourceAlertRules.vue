<template>
  <section data-testid="resource-rules">
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-button v-if="canCreate && canReadHosts" type="primary" :disabled="loading || !presets.length" @click="edit()">{{ t('monitor.resourceRules.create') }}</el-button>
    <el-table v-loading="loading" :data="rows">
      <el-table-column prop="name" :label="t('monitor.platformAlerts.name')" min-width="160" show-overflow-tooltip />
      <el-table-column :label="t('monitor.targets.node')" min-width="190" show-overflow-tooltip><template #default="{ row }">{{ hosts[row.node_id]?.display_name || row.node_id }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.metric')" min-width="150"><template #default="{ row }">{{ metricLabel(row.metric_key) }}</template></el-table-column>
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
    <el-dialog v-model="open" :title="t(editing ? 'monitor.resourceRules.edit' : 'monitor.resourceRules.create')" width="min(620px, 95vw)" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top" :disabled="busy" @submit.prevent="save">
        <el-form-item :label="t('monitor.platformAlerts.name')"><el-input v-model="draft.name" maxlength="100" data-testid="rule-name" /></el-form-item>
        <el-form-item :label="t('monitor.targets.node')">
          <el-input v-if="editing" :model-value="selected?.display_name || draft.node_id" readonly />
          <HostNodeSelect v-else v-model="draft.node_id" :selected="selected" @selected="selected = $event" />
        </el-form-item>
        <el-form-item :label="t('monitor.resourceRules.metric')"><el-select v-model="draft.metric_key" :disabled="!!editing" @change="applyPreset" :aria-label="t('monitor.resourceRules.metric')"><el-option v-for="preset in presets" :key="preset.metric_key" :value="preset.metric_key" :label="metricLabel(preset.metric_key)" /></el-select></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.trigger')"><div class="rule-condition"><el-input-number v-model="draft.trigger_percent" :min="0" :max="100" :aria-label="t('monitor.resourceRules.triggerPercent')" /><span>%</span><el-input-number v-model="draft.trigger_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.triggerSeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div><small>{{ t('monitor.resourceRules.triggerHint') }}</small></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.recovery')"><div class="rule-condition"><el-input-number v-model="draft.recovery_percent" :min="0" :max="100" :aria-label="t('monitor.resourceRules.recoveryPercent')" /><span>%</span><el-input-number v-model="draft.recovery_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.recoverySeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div><small>{{ t('monitor.resourceRules.recoveryHint') }}</small></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.enabled')"><el-switch v-model="draft.enabled" :disabled="!editing" :aria-label="t('monitor.platformAlerts.enabled')" /><small v-if="!editing">{{ t('monitor.resourceRules.disabledHint') }}</small></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="busy" @click="open = false">{{ t('common.close') }}</el-button><el-button type="primary" :loading="busy" :disabled="!selected" @click="save">{{ t('monitor.platformAlerts.save') }}</el-button></template>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { platformAlertsAPI as api } from '../api/platformAlerts'
import { nodeResourcesAPI } from '../api/nodeResources'
import HostNodeSelect from './HostNodeSelect.vue'
import { loadHostReferences } from '../utils/hostReferences'
const props = defineProps({ page: { type: Number, required: true } })
defineEmits(['page'])
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.platform_resource_rule.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.create'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.update'))
const canDelete = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.delete'))
const canReadHosts = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const rows = ref([]), total = ref(0), presets = ref([]), hosts = ref({}), loading = ref(false), error = ref(''), busy = ref(false)
const open = ref(false), editing = ref(''), draft = ref({}), selected = ref(null), editError = ref('')
const metricLabel = value => t(value === 'node.cpu.busy_percent' ? 'monitor.platformAlerts.cpuPressure' : 'monitor.platformAlerts.memoryPressure')
const reason = value => t(`monitor.resourceRules.reasons.${['warming_up','no_data','source_changed','collection_paused','collection_disabled','identity_unavailable','query_unavailable','query_budget','query_policy','unconfigured'].includes(value) ? value : 'unconfigured'}`)
const errorText = e => e.response?.data?.error || t('monitor.platformAlerts.failed')
let ticket = 0, epoch = 0, controller, disposed = false
const abandoned = Symbol('abandoned rule operation')
function check(lease) { if (disposed || lease !== epoch) throw abandoned }
function clear() { epoch++; ticket++; controller?.abort(); rows.value = []; total.value = 0; presets.value = []; hosts.value = {}; error.value = ''; open.value = false; draft.value = {}; selected.value = null; loading.value = false; busy.value = false }
async function load() {
  controller?.abort(); controller = new AbortController(); const own = ++ticket, lease = epoch
  rows.value = []; total.value = 0; hosts.value = {}; error.value = ''
  if (!canRead.value) return
  loading.value = true
  try {
    const result = await api.rules(props.page, controller.signal); check(lease); if (own !== ticket) return
    if (!Array.isArray(result.data) || !Array.isArray(result.presets) || !Number.isSafeInteger(result.total)) throw new Error('invalid_rules')
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
function applyPreset() { const preset = presets.value.find(item => item.metric_key === draft.value.metric_key); if (preset) Object.assign(draft.value, preset.policy) }
async function edit(row) {
  if (busy.value || !canReadHosts.value || (row ? !canUpdate.value : !canCreate.value)) return
  const lease = epoch; editError.value = ''; selected.value = row ? hosts.value[row.node_id] : null
  editing.value = row?.id || ''
  draft.value = row ? Object.fromEntries(['version','name','node_id','metric_key','trigger_percent','recovery_percent','trigger_seconds','recovery_seconds','enabled'].map(key => [key, row[key]])) : { name: '', node_id: '', metric_key: presets.value[0]?.metric_key, enabled: false }
  if (!row) applyPreset()
  open.value = true
  if (row && !selected.value) {
    busy.value = true
    try { const host = await nodeResourcesAPI.node(row.node_id); check(lease); selected.value = host }
    catch (e) { if (lease === epoch && e !== abandoned) editError.value = errorText(e) }
    finally { if (lease === epoch) busy.value = false }
  }
}
async function save() {
  if (busy.value || !selected.value || (editing.value ? !canUpdate.value : !canCreate.value)) return
  const d = draft.value
  if (!d.name.trim() || ![d.recovery_percent,d.trigger_percent].every(value => typeof value === 'number' && Number.isFinite(value)) || !(d.recovery_percent > 0 && d.recovery_percent < d.trigger_percent && d.trigger_percent <= 100) || ![d.trigger_seconds,d.recovery_seconds].every(value => Number.isInteger(value) && value >= 15 && value <= 300 && value % 15 === 0)) { editError.value = t('monitor.resourceRules.invalid'); return }
  const lease = epoch; busy.value = true; editError.value = ''
  try { if (editing.value) await api.updateRule(editing.value, { ...d }); else await api.createRule({ ...d, enabled: false }); check(lease); open.value = false; await load() }
  catch (e) { if (lease === epoch && e !== abandoned) editError.value = e.response?.status === 409 ? t('monitor.resourceRules.conflict') : errorText(e) }
  finally { if (lease === epoch) busy.value = false }
}
async function remove(row) {
  if (busy.value || !canDelete.value) return
  const lease = epoch; busy.value = true
  try { await ElMessageBox.confirm(t('monitor.resourceRules.deleteHint', { name: row.name }), t('monitor.platformAlerts.delete')); check(lease); await api.deleteRule(row); check(lease); await load() }
  catch (e) { if (lease === epoch && ![abandoned,'cancel','close'].includes(e)) error.value = e.response?.status === 409 ? t('monitor.resourceRules.deleteConflict') : errorText(e) }
  finally { if (lease === epoch) busy.value = false }
}
watch(() => [props.page, JSON.stringify([auth.authContext, auth.permissions])], () => { clear(); load() }, { immediate: true })
onUnmounted(() => { disposed = true; clear() })
defineExpose({ load })
</script>
<style scoped>
.rule-condition { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
small { display: block; color: var(--addp-text-secondary); margin-top: 6px; width: 100%; }
</style>
