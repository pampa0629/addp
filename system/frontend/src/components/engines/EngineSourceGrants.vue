<template>
  <section class="source-grants" data-testid="engine-source-grants">
    <el-alert type="info" :closable="false" :title="t('system.engine.sourceGrants.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <StatusAnnouncer :message="success" />
    <el-alert v-if="success" type="success" :closable="false" :title="success" />
    <div v-if="requirements.length && canCreate" class="grant-form" data-testid="source-grant-form">
      <h3>{{ t('system.engine.sourceGrants.create') }} · {{ requirements.length }}</h3>
      <el-alert v-if="formError" type="error" :closable="false" :title="formError" />
      <el-alert v-if="!canReadCandidates" type="warning" :closable="false" :title="t('system.engine.sourceGrants.candidatesRequired')" />
      <el-form label-position="top" @submit.prevent="create">
        <div :inert="saving || !!attempt || undefined">
          <el-form-item :label="t('system.engine.sourceGrants.recipientType')" required>
            <el-select ref="recipientTypeSelect" v-model="form.recipientType" data-testid="source-grant-recipient-type" @change="loadCandidates"><el-option v-for="kind in ['user','department','project_group']" :key="kind" :value="kind" :label="t(`system.engine.sourceGrants.types.${kind}`)" /></el-select>
          </el-form-item>
          <el-form-item :label="t('system.engine.sourceGrants.recipient')" required>
            <TenantMemberSelect v-if="form.recipientType === 'user'" v-model="form.recipientID" :members="members" principal-type="user" active-only value-field="principal_id" stringify-value :loading="candidateLoading" :disabled="!canReadCandidates" :current-membership-id="auth.authContext?.context?.tenant_membership_id" />
            <el-select v-else v-model="form.recipientID" filterable clearable :loading="candidateLoading" :disabled="!canReadCandidates"><el-option v-for="item in candidates" :key="item.id" :value="item.id" :label="`${item.name} · ${item.id}`" /></el-select>
          </el-form-item>
          <el-form-item :label="t('system.engine.sourceGrants.expiry')" required><el-select v-model="form.expiryMode" data-testid="source-grant-expiry"><el-option v-for="mode in ['at_time','until_revoked']" :key="mode" :value="mode" :label="t(`system.engine.sourceGrants.${mode}`)" /></el-select></el-form-item>
          <el-form-item v-if="form.expiryMode === 'at_time'" :label="t('system.engine.sourceGrants.at_time')" required><el-date-picker v-model="form.expiresAt" type="datetime" /></el-form-item>
          <el-form-item :label="t('system.engine.sourceGrants.reason')" required><el-input v-model="form.reason" data-testid="source-grant-reason" type="textarea" maxlength="2000" show-word-limit /></el-form-item>
        </div>
      </el-form>
      <el-table v-if="outcomes.length" :data="outcomes" data-testid="source-grant-outcomes">
        <el-table-column :label="t('system.engine.dataAuthorization.target')"><template #default="{ row }">{{ row.targetLabel }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.state')"><template #default="{ row }">{{ row.done ? t(row.recovered ? 'system.engine.sourceGrants.historyRecovered' : 'system.engine.sourceGrants.created') : row.error }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.command')" prop="requestID" show-overflow-tooltip />
      </el-table>
      <div><el-button :disabled="saving" @click="closeGrant">{{ t('common.cancel') }}</el-button><el-button type="primary" data-testid="source-grant-confirm" :loading="saving" :disabled="!canCreate || !canReadCandidates" @click="create">{{ t(attempt ? 'system.engine.sourceGrants.retry' : 'system.engine.sourceGrants.create') }}</el-button></div>
    </div>
    <template v-if="canRead">
      <h3>{{ t('system.engine.sourceGrants.history') }}</h3>
      <el-button :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="180"><template #default="{ row }">{{ approvalPathLabel(row.catalog_path) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.recipient')" min-width="160"><template #default="{ row }">{{ t(`system.engine.sourceGrants.types.${row.recipient_type}`) }} · {{ row.recipient_id }}</template></el-table-column>
        <el-table-column :label="t('system.engine.dataAuthorization.mode')" min-width="150"><template #default="{ row }">{{ t(`system.engine.dataAuthorization.modes.${row.approval_mode}`) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.state')" min-width="110"><template #default="{ row }">{{ t(`system.engine.sourceGrants.states.${state(row)}`) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.sourceGrants.command')" prop="request_id" min-width="220" show-overflow-tooltip />
        <el-table-column v-if="canRevoke" width="100"><template #default="{ row }"><el-button v-if="state(row) === 'issued'" data-testid="source-grant-revoke" link type="danger" @click="openRevoke(row)">{{ t('system.engine.sourceGrants.revoke') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination layout="prev, pager, next, total" :total="total" :page-size="20" :current-page="page" @current-change="changePage" />
    </template>
    <el-alert v-else type="info" :closable="false" :title="t('system.engine.sourceGrants.readRequired')" />
    <el-dialog v-model="revokeVisible" class="addp-dialog" append-to-body width="min(600px, calc(100vw - 24px))" :title="t('system.engine.sourceGrants.revoke')" :close-on-click-modal="!revoking" :close-on-press-escape="!revoking" :show-close="!revoking" @opened="revokeCancel?.$el?.focus()">
      <p>{{ approvalPathLabel(revokeRow?.catalog_path) }} · {{ revokeRow?.recipient_id }}</p>
      <el-alert type="warning" :closable="false" :title="t('system.engine.sourceGrants.revokeBoundary')" />
      <el-alert v-if="revokeError" type="error" :closable="false" :title="revokeError" />
      <el-form label-position="top"><el-form-item :label="t('system.engine.sourceGrants.reason')" required><el-input v-model="revokeReason" data-testid="source-grant-revoke-reason" type="textarea" maxlength="2000" :disabled="revoking" /></el-form-item></el-form>
      <template #footer><el-button ref="revokeCancel" :disabled="revoking" @click="revokeVisible = false">{{ t('common.cancel') }}</el-button><el-button type="danger" data-testid="source-grant-revoke-confirm" :loading="revoking" :disabled="!canRevoke" @click="revoke">{{ t('system.engine.sourceGrants.revoke') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { StatusAnnouncer } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel } from '../../utils/engineApprovalCatalog'
import { captureIndependentGrant } from '../../utils/independentGrant'
import TenantMemberSelect from '../iam/TenantMemberSelect.vue'

const props = defineProps({ engine: { type: Object, required: true }, requirements: { type: Array, default: () => [] } })
const recipientTypeSelect = ref(null), revokeCancel = ref(null)
const emit = defineEmits(['close', 'busy', 'issued']), auth = useAuthStore(), { t, locale } = useI18n()
const canCreate = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canRead = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const canRevoke = computed(() => auth.hasPermission('system.engine_access_grant.revoke'))
const form = reactive({ recipientType: 'user', recipientID: '', expiryMode: '', expiresAt: null, reason: '' })
const canReadCandidates = computed(() => auth.hasPermission(({ user: 'iam.tenant_membership.read', department: 'iam.department.read', project_group: 'iam.project_group.read' })[form.recipientType]))
const rows = ref([]), total = ref(0), page = ref(1), loading = ref(false), error = ref(''), success = ref('')
const candidates = ref([]), members = ref([]), candidateLoading = ref(false), formError = ref(''), saving = ref(false), attempt = ref(null)
const outcomes = ref([])
const revokeVisible = ref(false), revokeRow = ref(null), revokeReason = ref(''), revokeError = ref(''), revoking = ref(false)
let generation = 0, readSequence = 0, candidateSequence = 0
const message = e => e?.response?.data?.error || t('system.engine.sourceGrants.failed')
const formatTime = value => new Date(value).toLocaleString(locale.value)
const expiryLabel = row => row.expiry_mode === 'until_revoked' ? t('system.engine.sourceGrants.until_revoked') : formatTime(row.expires_at)
const state = row => row.revocation ? 'revoked' : row.expires_at && new Date(row.expires_at).getTime() <= Date.now() ? 'expired' : 'issued'
async function load() {
  if (!canRead.value) return
  const epoch = generation, seq = ++readSequence
  loading.value = true; error.value = ''; rows.value = []; total.value = 0
  try {
    const result = await enginesAPI.listSourceGrants(props.engine.id, { page: page.value, page_size: 20 })
    if (epoch !== generation || seq !== readSequence) return
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0) throw new Error('invalidHistory')
    rows.value = result.data; total.value = result.total
  } catch (e) { if (epoch === generation && seq === readSequence) error.value = message(e) }
  finally { if (epoch === generation && seq === readSequence) loading.value = false }
}
function changePage(value) { page.value = value; load() }
async function loadCandidates() {
  const epoch = generation, seq = ++candidateSequence, kind = form.recipientType
  candidates.value = []; members.value = []; form.recipientID = ''; candidateLoading.value = false; formError.value = ''
  if (!props.requirements.length || !canCreate.value || !canReadCandidates.value) return
  candidateLoading.value = true
  try {
    const result = await (kind === 'user' ? iamAPI.memberships : kind === 'department' ? iamAPI.departments : iamAPI.projectGroups).listAll({ status: 'active', ...(kind === 'user' ? { principal_type: 'user' } : {}) })
    if (epoch !== generation || seq !== candidateSequence) return
    if (kind === 'user') {
      members.value = result.filter(item => item.principal_type === 'user' && item.status === 'active' && item.principal_status === 'active' && !item.ended_at && (!item.expires_at || new Date(item.expires_at).getTime() > Date.now()))
      candidates.value = members.value.map(item => ({ id: String(item.principal_id), name: item.display_name || item.username || String(item.principal_id) }))
    } else candidates.value = result.filter(item => item.status === 'active').map(item => ({ id: String(item.id), name: item.name }))
  } catch (e) { if (epoch === generation && seq === candidateSequence) formError.value = message(e) }
  finally { if (epoch === generation && seq === candidateSequence) candidateLoading.value = false }
}
function closeGrant() { if (!saving.value) emit('close') }
async function create() {
  if (saving.value || !canCreate.value || !props.requirements.length || !canReadCandidates.value) return
  const epoch = generation
  saving.value = true; formError.value = ''; success.value = ''
  try {
    const recovering = !!attempt.value
    const captured = attempt.value || props.requirements.map(requirement => captureIndependentGrant(props.engine.id, requirement, form, candidates.value))
    const first = captured[0]
    await ElMessageBox.confirm(t('system.engine.sourceGrants.confirmBody', { target: captured.map(command => command.targetLabel).join('；'), recipient: first.recipientLabel, expiry: first.expiryMode === 'until_revoked' ? t('system.engine.sourceGrants.until_revoked') : formatTime(first.expiresAt) }), t('system.engine.sourceGrants.create'), { customClass: 'addp-message-box', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') })
    if (epoch !== generation || !canCreate.value) return
    attempt.value = captured
    for (const command of captured) {
      if (outcomes.value.some(row => row.requestID === command.requestID && row.done)) continue
      let outcome
      try {
        const result = await enginesAPI.createSourceGrant(props.engine.id, command.payload)
        if (epoch !== generation) return
        if (result?.request_id !== command.requestID || result.approval_mode !== 'independent' || !result.granted_at) throw new Error('invalidIssuance')
        outcome = { ...command, done: true, error: '', recovered: recovering || !!result.revocation }
      } catch (e) {
        if (epoch !== generation) return
        outcome = { ...command, done: false, error: message(e) }
      }
      outcomes.value = [...outcomes.value.filter(row => row.requestID !== command.requestID), outcome]
      if (!canCreate.value) return
    }
    if (outcomes.value.every(row => row.done)) {
      success.value = t(recovering ? 'system.engine.sourceGrants.historyRecovered' : 'system.engine.sourceGrants.created')
      saving.value = false; emit('close'); emit('issued')
    } else formError.value = t('system.engine.sourceGrants.partialFailure')
    await load()
  } catch (e) { if (epoch === generation && e !== 'cancel' && e !== 'close') formError.value = e?.message === 'invalidGrant' || e?.message === 'invalidEngineCatalogTarget' ? t('system.engine.sourceGrants.invalid') : message(e) }
  finally { if (epoch === generation) saving.value = false }
}
function openRevoke(row) { if (!canRevoke.value) return; revokeRow.value = row; revokeReason.value = ''; revokeError.value = ''; revokeVisible.value = true }
async function revoke() {
  if (revoking.value || !canRevoke.value || !revokeRow.value) return
  const reason = revokeReason.value.trim(), row = revokeRow.value, epoch = generation
  if (!reason || Array.from(reason).length > 2000) { revokeError.value = t('system.engine.sourceGrants.invalidReason'); return }
  revoking.value = true; revokeError.value = ''
  try {
    const result = await enginesAPI.revokeSourceGrant(props.engine.id, row.request_id, reason)
    if (epoch !== generation) return
    if (result?.request_id !== row.request_id || !result.revoked_at) throw new Error('invalidRevocation')
    revokeVisible.value = false; success.value = t('system.engine.sourceGrants.revoked'); await load()
  } catch (e) { if (epoch === generation) revokeError.value = message(e) }
  finally { if (epoch === generation) revoking.value = false }
}
watch(() => JSON.stringify(props.requirements), () => {
  generation++; attempt.value = null; outcomes.value = []; saving.value = false; formError.value = ''; form.recipientType = 'user'; form.recipientID = ''; form.expiryMode = ''; form.expiresAt = null; form.reason = ''
  loadCandidates(); load()
})
watch([() => props.engine.id, () => props.engine.lifecycle_state, () => auth.authContext?.principal?.id, () => auth.authContext?.context?.tenant_id,
  () => auth.authContext?.context?.tenant_membership_id, () => auth.authContext?.authorization?.authorization_version, canRead, canCreate, canRevoke,
  ...['iam.tenant_membership.read', 'iam.department.read', 'iam.project_group.read'].map(permission => () => auth.hasPermission(permission))], () => {
  generation++; attempt.value = null; outcomes.value = []; candidates.value = []; members.value = []; rows.value = []; total.value = 0; page.value = 1
  saving.value = revoking.value = loading.value = false; revokeVisible.value = false; error.value = formError.value = success.value = ''; emit('close'); load()
}, { immediate: true })
watch([saving, attempt], () => emit('busy', saving.value || !!attempt.value), { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.source-grants { display: grid; gap: 16px; min-width: 0; }
h3 { margin: 0; }
.el-select { width: 100%; }
.grant-form { display: grid; gap: 16px; }
p { overflow-wrap: anywhere; }
</style>
