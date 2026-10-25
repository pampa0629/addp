<template>
  <section data-testid="availability-rules">
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-button v-if="canCreate" type="primary" :disabled="busy" @click="edit()">{{ t('monitor.availability.create') }}</el-button>
    <el-table :data="rows" v-loading="loading">
      <el-table-column prop="name" :label="t('monitor.platformAlerts.name')" min-width="160" />
      <el-table-column :label="t('monitor.availability.group')" min-width="220"><template #default="{ row }">{{ groupLabel(row) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.trigger')" min-width="180"><template #default="{ row }">{{ t('monitor.availability.trigger', { seconds: row.trigger_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resourceRules.recovery')" min-width="180"><template #default="{ row }">{{ t('monitor.availability.recovery', { seconds: row.recovery_seconds }) }}</template></el-table-column>
      <el-table-column :label="t('monitor.platformAlerts.enabled')" width="90"><template #default="{ row }">{{ t(`monitor.platformAlerts.facts.${row.enabled ? 'yes' : 'no'}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.availability.lastEvaluation')" min-width="210"><template #default="{ row }">
        <span>{{ row.last_error === 'waiting_confirmation' ? t('monitor.availability.waiting') : t(`monitor.availability.verdicts.${row.last_verdict || 'unknown'}`) }}</span>
        <small v-if="row.last_error">{{ reason(row.last_error) }}</small><small v-if="row.last_evaluated_at">{{ new Date(row.last_evaluated_at).toLocaleString() }}</small>
      </template></el-table-column>
      <el-table-column v-if="canUpdate || canDelete" :label="t('monitor.platformAlerts.actions')" width="150" fixed="right"><template #default="{ row }">
        <el-button v-if="canUpdate" size="small" :disabled="busy" @click="edit(row)">{{ t('monitor.platformAlerts.edit') }}</el-button>
        <el-button v-if="canDelete" size="small" :disabled="busy" @click="remove(row)">{{ t('monitor.platformAlerts.delete') }}</el-button>
      </template></el-table-column>
    </el-table>
    <el-pagination v-if="total && !loading" layout="total, prev, pager, next" :current-page="page" :page-size="20" :total="total" @current-change="$emit('page', $event)" />
    <el-dialog v-model="open" :title="t(editing ? 'monitor.availability.edit' : 'monitor.availability.create')" width="680px" :close-on-click-modal="false" :before-close="close" append-to-body>
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top">
        <el-form-item :label="t('monitor.platformAlerts.name')"><el-input v-model="draft.name" :maxlength="100" /></el-form-item>
        <el-form-item :label="t('monitor.availability.group')">
          <el-select v-model="draft.group_id" :disabled="!!editing" :loading="groupsLoading" filterable :aria-label="t('monitor.availability.group')">
            <el-option v-for="group in groups" :key="group.id" :value="group.id" :label="groupLabel(group)" />
          </el-select>
          <small>{{ t('monitor.availability.scope') }}</small>
        </el-form-item>
        <el-form-item :label="t('monitor.resourceRules.triggerSeconds')"><el-input-number v-model="draft.trigger_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.triggerSeconds')" /></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.recoverySeconds')"><el-input-number v-model="draft.recovery_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.recoverySeconds')" /></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.enabled')"><el-switch v-model="draft.enabled" :disabled="!editing" /><small>{{ t('monitor.availability.enableHint') }}</small></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="busy" @click="open = false">{{ t('common.close') }}</el-button><el-button type="primary" :disabled="!draft.group_id || busy" @click="save">{{ t('monitor.platformAlerts.save') }}</el-button></template>
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
import { validAvailabilityPolicy, validateAvailabilityGroups, validateAvailabilityRules } from '../utils/serviceAvailability'
const props = defineProps({ page: { type: Number, required: true } }); defineEmits(['page'])
const auth = useAuthStore(), { t } = useI18n(), requests = createLatestRequestScope(), candidates = createLatestRequestScope()
const prefix = 'monitor.platform_service_availability_rule'
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission(`${prefix}.read`) && auth.hasPermission('platform.module.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission(`${prefix}.create`)), canUpdate = computed(() => canRead.value && auth.hasPermission(`${prefix}.update`)), canDelete = computed(() => canRead.value && auth.hasPermission(`${prefix}.delete`))
const rows = ref([]), total = ref(0), loading = ref(false), error = ref(''), busy = ref(false), open = ref(false), editing = ref(''), draft = ref({}), groups = ref([]), groupsLoading = ref(false), editError = ref('')
let lease
const groupLabel = row => `${row.module_name} · ${t(`monitor.process.roles.${row.role}`)}${row.runtime_name ? ` · ${t(`monitor.availability.purposes.${row.runtime_name}`, row.runtime_name)}` : ''}`
const errorText = e => e?.response?.data?.error || t('monitor.platformAlerts.failed')
const reason = value => ['waiting_confirmation','warming_up','source_changed','identity_unavailable','observation_not_advanced','planned_stopped','module_disabled','module_unregistered','runtime_name_unavailable'].includes(value) ? t(`monitor.availability.reasons.${value}`) : t('monitor.platformAlerts.failed')
function close(done) { if (!busy.value) done() }
async function load() {
  const owner = requests.invalidate(); lease = owner; rows.value = []; total.value = 0; error.value = ''; loading.value = false
  if (!canRead.value) return
  loading.value = true
  try { const page = validateAvailabilityRules(await requests.request(({ signal }) => api.availabilityRules(props.page, signal)), props.page); if (requests.current(owner)) { rows.value = page.data; total.value = page.total } }
  catch (e) { if (requests.current(owner)) error.value = errorText(e) }
  finally { if (requests.current(owner)) loading.value = false }
}
async function edit(row) {
  if (busy.value || (row ? !canUpdate.value : !canCreate.value)) return
  candidates.invalidate(); editError.value = ''; editing.value = row?.id || ''; draft.value = row ? { version: row.version, name: row.name, group_id: row.group_id, trigger_seconds: row.trigger_seconds, recovery_seconds: row.recovery_seconds, enabled: row.enabled } : { name: '', group_id: '', trigger_seconds: 60, recovery_seconds: 30, enabled: false }
  groups.value = row ? [{ ...row, id: row.group_id }] : []; open.value = true
  if (row) return
  const owner = lease, ticket = candidates.invalidate(); groupsLoading.value = true
  try { const result = validateAvailabilityGroups(await candidates.request(({ signal }) => api.availabilityGroups(signal))); if (requests.current(owner) && candidates.current(ticket) && open.value) groups.value = result }
  catch (e) { if (requests.current(owner) && candidates.current(ticket)) editError.value = errorText(e) }
  finally { if (candidates.current(ticket)) groupsLoading.value = false }
}
async function save() {
  if (busy.value || (editing.value ? !canUpdate.value : !canCreate.value)) return
  if (!draft.value.name?.trim() || !draft.value.group_id || !validAvailabilityPolicy(draft.value)) { editError.value = t('monitor.availability.invalid'); return }
  const owner = lease; busy.value = true; editError.value = ''
  try { const input = { ...draft.value, name: draft.value.name.trim() }; await requests.request(() => editing.value ? api.updateAvailabilityRule(editing.value, input) : api.createAvailabilityRule({ ...input, enabled: false })); if (requests.current(owner)) { open.value = false; await load() } }
  catch (e) { if (requests.current(owner)) editError.value = e?.response?.status === 409 ? t('monitor.availability.conflict') : errorText(e) }
  finally { if (requests.current(owner) || !open.value) busy.value = false }
}
async function remove(row) {
  if (busy.value || !canDelete.value) return
  const owner = lease; busy.value = true
  try { await ElMessageBox.confirm(t('monitor.resourceRules.deleteHint', { name: row.name }), t('monitor.platformAlerts.delete')); if (!requests.current(owner)) return; await requests.request(() => api.deleteAvailabilityRule(row)); if (requests.current(owner)) await load() }
  catch (e) { if (requests.current(owner) && e !== 'cancel' && e !== 'close') error.value = e?.response?.status === 409 ? t('monitor.resourceRules.deleteConflict') : errorText(e) }
  finally { if (requests.current(owner) || !open.value) busy.value = false }
}
watch(() => [props.page, JSON.stringify([auth.authContext, auth.permissions])], () => { candidates.invalidate(); open.value = false; busy.value = false; draft.value = {}; groups.value = []; groupsLoading.value = false; load() }, { immediate: true })
watch(open, value => { if (!value) { candidates.invalidate(); groups.value = []; groupsLoading.value = false } })
onUnmounted(() => { requests.dispose(); candidates.dispose() }); defineExpose({ load })
</script>
<style scoped>
small { display:block; margin-top:6px; color:var(--addp-text-secondary); overflow-wrap:anywhere; }
.el-select { width:100%; }
</style>
