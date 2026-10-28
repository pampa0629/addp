<template>
  <section class="direct-permissions" data-testid="direct-permissions">
    <el-alert type="info" :closable="false" :title="t('system.engine.directPermission.boundary')" show-icon />
    <el-form inline @submit.prevent="search">
      <el-form-item :label="t('system.engine.directPermission.search')"><el-input v-model="query" clearable :disabled="busy" data-testid="direct-permission-search" /></el-form-item>
      <el-form-item :label="t('system.engine.directPermission.state')"><el-select v-model="state" :disabled="busy" data-testid="direct-permission-state">
        <el-option value="" :label="t('system.engine.directPermission.all')" />
        <el-option v-for="value in ['allowed', 'denied', 'unknown']" :key="value" :value="value" :label="t(`system.engine.directPermission.states.${value}`)" />
      </el-select></el-form-item>
      <el-button :disabled="busy" :loading="loading" native-type="submit">{{ t('common.refresh') }}</el-button>
    </el-form>
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <el-table :data="rows" v-loading="loading" data-testid="direct-permission-list">
      <el-table-column prop="full_name" :label="t('system.engine.dataAuthorization.target')" min-width="220" />
      <el-table-column :label="t('system.engine.directPermission.state')" min-width="140"><template #default="{ row }"><el-tag :type="row.state === 'allowed' ? 'success' : 'info'">{{ t(`system.engine.directPermission.states.${row.state}`) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.engine.directPermission.source')" min-width="160"><template #default="{ row }">{{ t(`system.engine.directPermission.sources.${row.source}`) }}</template></el-table-column>
      <el-table-column prop="version" :label="t('system.engine.dataAuthorization.version')" width="100" />
      <el-table-column v-if="canManage" :label="t('common.actions')" min-width="140"><template #default="{ row }"><el-button link type="primary" :disabled="busy" data-testid="direct-permission-edit" @click="edit(row)">{{ t('system.engine.directPermission.edit') }}</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error" v-model:current-page="page" :disabled="busy" :page-size="pageSize" :total="total" layout="prev, pager, next, total" @current-change="load" />
    <el-dialog v-if="draft && authorizationReady" :model-value="true" :title="t('system.engine.directPermission.edit')" append-to-body width="min(640px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" :before-close="close">
      <el-alert type="warning" :closable="false" :title="t('system.engine.directPermission.changeBoundary')" />
      <p>{{ draft.label }}</p>
      <p>{{ t(`system.engine.directPermission.sources.${draft.row.source}`) }} · {{ t('system.engine.dataAuthorization.version') }} {{ draft.row.version }}</p>
      <el-alert v-if="draft.error" type="error" :closable="false" :title="draft.error" />
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item :label="t('system.engine.directPermission.state')"><el-radio-group v-model="draft.allowed" :disabled="saving" data-testid="direct-permission-value">
          <el-radio :value="true">{{ t('system.engine.directPermission.states.allowed') }}</el-radio><el-radio :value="false">{{ t('system.engine.directPermission.states.denied') }}</el-radio>
        </el-radio-group></el-form-item>
        <el-form-item :label="t('system.engine.dataAuthorization.reason')" required><el-input v-model="draft.reason" type="textarea" maxlength="2000" :disabled="saving" data-testid="direct-permission-reason" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="close()">{{ t('common.cancel') }}</el-button>
        <el-button v-if="draft.needsReview" :disabled="saving" @click="review">{{ t('system.engine.dataAuthorization.reviewCurrent') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="saving || draft.needsReview || !draft.reason.trim()" data-testid="direct-permission-save" @click="save">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { captureDirectPermission, readPermissionTarget, validateDirectPermission } from '../../utils/directPermission'
const props = defineProps({ engine: { type: Object, required: true }, authorizationKey: { type: String, required: true }, authorizationReady: Boolean })
const { t } = useI18n(), auth = useAuthStore()
const canManage = computed(() => auth.hasPermission('system.engine_access_direct_permission.update') && auth.hasPermission('system.engine_access_delegation.create'))
const rows = ref([]), total = ref(0), page = ref(1), pageSize = 20, query = ref(''), state = ref(''), error = ref(''), loading = ref(false), saving = ref(false), draft = ref(null)
const busy = computed(() => !props.authorizationReady || loading.value || saving.value || !!draft.value)
let epoch = 0, sequence = 0, applied = { search: '', state: '' }
const message = e => e?.response?.data?.error || t('system.engine.dataAuthorization.failed')
async function load() {
  if (!props.authorizationReady || saving.value || draft.value) return
  const generation = epoch, request = ++sequence, requestedPage = page.value
  loading.value = true; error.value = ''; rows.value = []
  try {
    const result = await enginesAPI.listDirectPermissions(props.engine.id, { ...applied, page: requestedPage, page_size: pageSize })
    if (epoch !== generation || request !== sequence) return
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.page !== requestedPage || result.page_size !== pageSize ||
      result.data.length !== Math.max(0, Math.min(pageSize, result.total - (requestedPage - 1) * pageSize))) throw new Error('invalidPermissionPage')
    result.data.forEach(row => validateDirectPermission(row, props.engine.id))
    rows.value = result.data; total.value = result.total
  } catch (e) { if (epoch === generation && request === sequence) { error.value = message(e); total.value = 0 } }
  finally { if (epoch === generation && request === sequence) loading.value = false }
}
function search() { if (busy.value) return; applied = { search: query.value.trim(), state: state.value }; page.value = 1; load() }
async function observe(row) {
  const [permission, target] = await Promise.all([
    enginesAPI.getDirectPermission(props.engine.id, row.item_fingerprint),
    readPermissionTarget(props.engine.id, row.item_fingerprint, enginesAPI.getAuthorizationFacts)
  ])
  return { row: validateDirectPermission(permission, props.engine.id, row.item_fingerprint), target }
}
async function edit(row) {
  if (busy.value || !canManage.value) return
  const generation = epoch
  loading.value = true; error.value = ''
  try { const current = await observe(row); if (generation === epoch) draft.value = { ...current, label: row.full_name, allowed: current.row.state === 'allowed', reason: '', error: '', needsReview: false } }
  catch (e) { if (generation === epoch) error.value = message(e) }
  finally { if (generation === epoch) loading.value = false }
}
function close(done) { if (saving.value) return; draft.value = null; if (typeof done === 'function') done(); load() }
async function review() {
  if (saving.value || !draft.value || !props.authorizationReady) return
  const current = draft.value, generation = epoch
  saving.value = true
  try { const observed = await observe(current.row); if (generation === epoch && draft.value === current) Object.assign(current, observed, { needsReview: false, error: '' }) }
  catch (e) { if (generation === epoch && draft.value === current) current.error = message(e) }
  finally { if (generation === epoch) saving.value = false }
}
async function save() {
  if (saving.value || !props.authorizationReady || !canManage.value || !draft.value || draft.value.needsReview) return
  const current = draft.value, generation = epoch
  saving.value = true; current.error = ''; let submitted = false, saved = false
  try {
    const payload = captureDirectPermission(current.row, current.target, current.allowed, current.reason)
    await ElMessageBox.confirm(t('system.engine.directPermission.changeBoundary'), t('system.engine.directPermission.edit'), { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), customClass: 'addp-message-box' })
    if (generation !== epoch || draft.value !== current || !props.authorizationReady || !canManage.value) return
    submitted = true
    const result = validateDirectPermission(await enginesAPI.setDirectPermission(props.engine.id, current.row.item_fingerprint, payload), props.engine.id, current.row.item_fingerprint)
    if (generation !== epoch || draft.value !== current) return
    const expected = current.row.source === 'explicit' ? BigInt(current.row.version) + BigInt((current.row.state === 'allowed') !== current.allowed) : 1n
    if (result.source !== 'explicit' || result.state !== (current.allowed ? 'allowed' : 'denied') || BigInt(result.version) !== expected) throw new Error('invalidPermissionWrite')
    draft.value = null; saved = true
  } catch (e) {
    if (generation === epoch && draft.value === current && e !== 'cancel' && e !== 'close') { current.needsReview = submitted; current.error = message(e) }
  } finally { if (generation === epoch) { saving.value = false; if (saved) load() } }
}
watch(() => props.authorizationKey, () => {
  epoch++; sequence++; rows.value = []; total.value = 0; draft.value = null; loading.value = saving.value = false; error.value = ''
  ElMessageBox.close()
  if (props.authorizationReady) load()
}, { immediate: true })
watch(() => props.authorizationReady, ready => {
  epoch++; sequence++; loading.value = saving.value = false
  ElMessageBox.close()
  // Token refresh suspends authority without changing the actor. Keep input,
  // but require a fresh facts/permission observation before another command.
  if (!ready) {
    if (draft.value) draft.value.needsReview = true
    return
  }
  load()
})
onBeforeUnmount(() => { epoch++; sequence++; ElMessageBox.close() })
</script>
<style scoped>
.direct-permissions { display: grid; gap: 16px; min-width: 0; }
.el-form--inline { display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-start; }
.el-select { width: 180px; }
p { overflow-wrap: anywhere; }
</style>
