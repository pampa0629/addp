<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.target_principal_id" :placeholder="t('system.iam.administratorChanges.targetId')" clearable @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload"><el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" /></el-select>
        <el-button @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="canCreate" type="primary" @click="openCreate">{{ t('system.iam.administratorChanges.create') }}</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column type="expand"><template #default="{ row }"><el-descriptions :column="1" border>
        <el-descriptions-item :label="t('system.iam.common.reason')">{{ row.reason }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.administratorChanges.reviewedBy')">{{ row.review?.principal_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.common.reviewReason')">{{ row.review?.reason || '-' }}</el-descriptions-item>
      </el-descriptions></template></el-table-column>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column :label="t('system.iam.administratorChanges.changeType')" min-width="180"><template #default="{ row }">{{ typeLabel(row.change_type) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.administratorChanges.role')" min-width="140"><template #default="{ row }">{{ roleLabel(row.governance_role_key) }}</template></el-table-column>
      <el-table-column prop="target_principal_id" :label="t('system.iam.administratorChanges.targetId')" width="140" />
      <el-table-column prop="successor_principal_id" :label="t('system.iam.administratorChanges.successor')" width="140" />
      <el-table-column prop="reason" :label="t('system.iam.common.reason')" min-width="180" show-overflow-tooltip />
      <el-table-column prop="requested_by_principal_id" :label="t('system.iam.administratorChanges.requester')" width="120" />
      <el-table-column :label="t('system.iam.administratorChanges.reviewer')" min-width="140"><template #default="{ row }">{{ roleLabel(row.reviewer_role_key) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag>{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.administratorChanges.expiresAt')" width="180"><template #default="{ row }">{{ formatDate(row.expires_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="210" fixed="right"><template #default="{ row }">
        <template v-if="mayReview(row)"><el-button v-if="can('approve')" link type="success" @click="openReview(row, 'approve')">{{ t('system.iam.common.approve') }}</el-button><el-button v-if="can('reject')" link type="danger" @click="openReview(row, 'reject')">{{ t('system.iam.common.reject') }}</el-button></template>
        <el-button v-if="(row.status === 'pending' || (row.status === 'approved' && row.change_type === 'platform_credential_recovery')) && row.requested_by_principal_id === principalId && canCreate" link @click="openReview(row, 'cancel')">{{ t('system.iam.common.cancel') }}</el-button>
      </template></el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />
    <el-dialog v-model="createVisible" :title="t('system.iam.administratorChanges.create')" width="min(600px, 94vw)" :close-on-click-modal="!submitting">
      <el-form label-position="top" :disabled="submitting">
        <el-form-item :label="t('system.iam.administratorChanges.changeType')" required><el-select v-model="createForm.change_type" @change="resetSelection"><el-option v-for="type in changeTypes" :key="type" :label="typeLabel(type)" :value="type" /></el-select></el-form-item>
        <el-form-item :label="t('system.iam.administratorChanges.role')" required><el-select v-model="createForm.target_role_key" @change="resetTargets"><el-option v-for="role in targetRoles" :key="role" :label="roleLabel(role)" :value="role" /></el-select></el-form-item>
        <el-form-item :label="t('system.iam.administratorChanges.target')" required><el-select v-model="createForm.target_principal_id" filterable remote :remote-method="loadCandidates" :loading="candidatesLoading"><el-option v-for="user in targetCandidates" :key="user.id" :label="`${user.display_name} (${user.id})`" :value="user.id" /></el-select></el-form-item>
        <el-form-item v-if="createForm.change_type === 'platform_role_replace'" :label="t('system.iam.administratorChanges.successor')" required><el-select v-model="createForm.successor_principal_id" filterable remote :remote-method="loadCandidates" :loading="candidatesLoading"><el-option v-for="user in successorCandidates" :key="user.id" :label="`${user.display_name} (${user.id})`" :value="user.id" /></el-select></el-form-item>
        <el-form-item :label="t('system.iam.common.reason')" required><el-input v-model="createForm.reason" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="submitting" @click="createVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="createRequest">{{ t('system.iam.common.submit') }}</el-button></template>
    </el-dialog>
    <el-dialog v-model="reviewVisible" :title="t(`system.iam.common.${review.action}`)" width="min(560px, 94vw)" :close-on-click-modal="!submitting">
      <el-alert v-if="review.action === 'approve'" :title="t('system.iam.administratorChanges.approvalEffect')" type="warning" :closable="false" />
      <el-form label-position="top"><el-form-item :label="t('system.iam.common.reviewReason')" required><el-input v-model="review.reason" type="textarea" :disabled="submitting" :rows="3" /></el-form-item></el-form>
      <template #footer><el-button :disabled="submitting" @click="reviewVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!review.reason.trim()" @click="submitReview">{{ t('system.iam.common.confirm') }}</el-button></template>
    </el-dialog>
    <el-dialog v-model="recoveryVisible" :title="t('system.iam.administratorChanges.recoveryIssued')" width="min(640px, 94vw)" destroy-on-close @closed="recoveryToken = ''">
      <p>{{ t('system.iam.administratorChanges.recoveryDelivery') }}</p>
      <el-input :model-value="recoveryToken" type="textarea" readonly :rows="3" />
      <p><a href="/administrator-recovery" target="_blank" rel="noopener noreferrer">{{ t('system.iam.administratorChanges.recoveryEntry') }}</a></p>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
const { t } = useI18n()
const auth = useAuthStore()
const principalId = computed(() => auth.authContext?.principal?.id || '')
const actorRole = computed(() => (auth.authContext?.authorization?.role_assignments || []).find(a => a.role_key?.endsWith('_administrator') && a.scope?.type === 'platform')?.role_key || '')
const can = action => auth.hasPermission(`iam.platform_administrator_change.${action}`)
const canCreate = computed(() => can('create'))
const targetRoles = computed(() => actorRole.value === 'platform.system_administrator' ? ['platform.security_administrator'] : actorRole.value === 'platform.security_administrator' ? ['platform.system_administrator', 'platform.audit_administrator'] : [])
const statuses = ['pending', 'approved', 'rejected', 'cancelled', 'applied']
const changeTypes = ['platform_identity_suspend', 'platform_identity_reactivate', 'platform_role_grant', 'platform_role_replace', 'platform_role_revoke', 'platform_credential_recovery']
const rows = ref([]), loading = ref(false), submitting = ref(false), page = ref(1), pageSize = ref(20), total = ref(0)
const filters = reactive({ status: '', target_principal_id: '' })
const createVisible = ref(false), reviewVisible = ref(false), recoveryVisible = ref(false), recoveryToken = ref('')
const createForm = reactive({ change_type: changeTypes[0], target_role_key: '', target_principal_id: '', successor_principal_id: '', reason: '' })
const review = reactive({ row: null, action: 'approve', reason: '' })
const candidates = ref([]), candidatesLoading = ref(false)
let loadVersion = 0, candidateVersion = 0, alive = true
const successorCandidates = computed(() => candidates.value.filter(u => u.id !== principalId.value && u.status === 'active' && u.mfa_enrolled && !u.role_key))
const targetCandidates = computed(() => createForm.change_type === 'platform_role_grant' ? successorCandidates.value : candidates.value.filter(u => u.role_key === createForm.target_role_key && u.id !== principalId.value && (createForm.change_type === 'platform_identity_suspend' ? u.status === 'active' : createForm.change_type === 'platform_identity_reactivate' ? u.status === 'suspended' : true)))
const canSubmit = computed(() => canCreate.value && createForm.target_principal_id && createForm.target_role_key && createForm.reason.trim() && (createForm.change_type !== 'platform_role_replace' || createForm.successor_principal_id))
const statusLabel = value => t(`system.iam.status.${value}`)
const typeLabel = value => t(`system.iam.administratorChanges.types.${value}`)
const roleLabel = value => t(`system.iam.administratorChanges.roles.${value?.split('.')[1] || 'unknown'}`)
const formatDate = value => value ? new Date(value).toLocaleString() : '-'
function mayReview(row) { return row.status === 'pending' && row.reviewer_role_key === actorRole.value && ![row.requested_by_principal_id, row.target_principal_id, row.successor_principal_id].includes(principalId.value) && Date.parse(row.expires_at) > Date.now() }
async function load() {
  const version = ++loadVersion
  loading.value = true
  try { const result = await iamAPI.administratorChanges.list({ page: page.value, page_size: pageSize.value, status: filters.status || undefined, target_principal_id: filters.target_principal_id || undefined }); if (alive && version === loadVersion) { rows.value = result.data || []; total.value = result.total || 0 } }
  catch (error) { if (alive && version === loadVersion) ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { if (alive && version === loadVersion) loading.value = false }
}
function reload() { page.value = 1; return load() }
function resetTargets() { createForm.target_principal_id = ''; createForm.successor_principal_id = '' }
function resetSelection() { resetTargets(); loadCandidates('') }
async function loadCandidates(search = '') {
  const version = ++candidateVersion
  candidatesLoading.value = true
  try { const result = await iamAPI.administratorChanges.candidates({ search, page_size: 100 }); if (alive && version === candidateVersion) candidates.value = result.data || [] }
  catch (error) { if (alive && version === candidateVersion) ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { if (alive && version === candidateVersion) candidatesLoading.value = false }
}
function openCreate() { Object.assign(createForm, { change_type: changeTypes[0], target_role_key: targetRoles.value[0] || '', target_principal_id: '', successor_principal_id: '', reason: '' }); createVisible.value = true; loadCandidates() }
async function createRequest() {
  if (!canSubmit.value || submitting.value) return
  submitting.value = true
  try {
    const data = { change_type: createForm.change_type, target_principal_id: createForm.target_principal_id, reason: createForm.reason.trim() }
    if (createForm.change_type.startsWith('platform_role_')) data.target_role_key = createForm.target_role_key
    if (createForm.change_type === 'platform_role_replace') data.successor_principal_id = createForm.successor_principal_id
    await iamAPI.administratorChanges.create(data)
    if (!alive) return
    createVisible.value = false; ElMessage.success(t('system.iam.common.submitted')); await load()
  } catch (error) { if (alive) ElMessage.error(error.response?.data?.error || t('system.iam.common.submitFailed')) }
  finally { if (alive) submitting.value = false }
}
function openReview(row, action) { Object.assign(review, { row, action, reason: '' }); reviewVisible.value = true }
async function submitReview() {
  if (submitting.value || !review.row) return
  submitting.value = true
  try {
    const result = await iamAPI.administratorChanges[review.action](review.row.id, review.row.version, review.reason.trim())
    if (!alive) return
    reviewVisible.value = false
    if (result.recovery_token) { recoveryToken.value = result.recovery_token; recoveryVisible.value = true }
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (alive) ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
  finally { if (alive) submitting.value = false }
}
let stableIdentity = `${principalId.value}/${auth.contextType}/${actorRole.value}`
watch(() => `${principalId.value}/${auth.contextType}/${actorRole.value}`, identity => {
  if (auth.isAuthenticated && !principalId.value) return
  if (identity === stableIdentity) return
  stableIdentity = identity
  loadVersion++; candidateVersion++; rows.value = []; candidates.value = []; createVisible.value = false; reviewVisible.value = false; recoveryVisible.value = false; recoveryToken.value = ''
})
onMounted(load)
onBeforeUnmount(() => { alive = false; loadVersion++; candidateVersion++; recoveryToken.value = '' })
</script>
