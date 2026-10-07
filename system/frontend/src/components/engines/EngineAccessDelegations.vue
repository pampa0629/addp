<template>
  <section data-testid="engine-access-delegations" class="delegations">
    <el-alert type="info" :closable="false" show-icon :title="t('system.engine.delegations.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <el-alert v-if="created" type="success" :closable="false" :title="t('system.engine.delegations.created')" />
    <template v-if="canRead">
      <el-button :loading="loading" @click="load">{{ t('system.engine.delegations.refresh') }}</el-button>
      <el-table :data="rows" v-loading="loading">
        <el-table-column prop="display_name" :label="t('system.engine.delegations.account')" min-width="150" />
        <el-table-column :label="t('system.engine.delegations.state')" min-width="110"><template #default="{ row }">{{ t(`system.engine.delegations.states.${row.effective_state}`) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.delegations.expiresAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.expires_at) }}</template></el-table-column>
        <el-table-column prop="grant_reason" :label="t('system.engine.delegations.reason')" min-width="180" />
        <el-table-column v-if="canRevoke" width="100"><template #default="{ row }"><el-button v-if="row.status === 'active' && row.effective_state !== 'expired'" data-testid="delegation-revoke" link type="danger" @click="openRevoke(row)">{{ t('system.engine.delegations.revoke') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination layout="prev, pager, next, total" :total="total" :page-size="10" :current-page="page" @current-change="changePage" />
    </template>
    <el-alert v-else type="info" :closable="false" :title="t('system.engine.delegations.readRequired')" />
    <el-form v-if="canCreate" label-position="top" @submit.prevent="create">
      <h3>{{ t('system.engine.delegations.create') }}</h3>
      <el-alert v-if="!canReadMembers" type="info" :closable="false" :title="t('system.engine.delegations.membersRequired')" />
      <el-form-item :label="t('system.engine.delegations.account')" required>
        <TenantMemberSelect v-model="form.membershipID" :members="members" principal-type="user" active-only
          :current-membership-id="auth.authContext?.context?.tenant_membership_id" :loading="membersLoading" :disabled="saving || !canReadMembers" />
      </el-form-item>
      <el-button v-if="canReadMembers" :disabled="saving" :loading="membersLoading" @click="loadMembers">{{ t('system.engine.delegations.refreshMembers') }}</el-button>
      <el-form-item :label="t('system.engine.delegations.expiresAt')" required><el-date-picker v-model="form.expiresAt" type="datetime" :disabled="saving" /></el-form-item>
      <el-form-item :label="t('system.engine.delegations.reason')" required><el-input v-model="form.reason" data-testid="delegation-reason" type="textarea" maxlength="2000" show-word-limit :disabled="saving" /></el-form-item>
      <el-button data-testid="delegation-create" type="primary" :disabled="!canReadMembers" :loading="saving" @click="create">{{ t('system.engine.delegations.create') }}</el-button>
    </el-form>
    <el-dialog v-model="revokeVisible" :title="t('system.engine.delegations.revoke')" append-to-body width="min(600px, 95vw)">
      <p>{{ revokeRow?.display_name }}</p>
      <el-alert type="warning" :closable="false" :title="t('system.engine.delegations.revokeBoundary')" />
      <el-form label-position="top" @submit.prevent="revoke">
        <el-form-item :label="t('system.engine.delegations.reason')" required><el-input v-model="revokeReason" data-testid="delegation-revoke-reason" type="textarea" maxlength="2000" :disabled="revoking" /></el-form-item>
        <el-alert v-if="revokeError" type="error" :closable="false" :title="revokeError" />
        <el-button data-testid="delegation-revoke-confirm" type="danger" :loading="revoking" :disabled="!canRevoke" @click="revoke">{{ t('system.engine.delegations.confirmRevoke') }}</el-button>
      </el-form>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { enginesAPI } from '../../api/engines'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import TenantMemberSelect from '../iam/TenantMemberSelect.vue'
import { captureDelegation } from '../../utils/engineAccessDelegation'

const props = defineProps({ engineId: { type: [String, Number], required: true } })
const auth = useAuthStore(), { t, locale } = useI18n()
const canRead = computed(() => auth.hasPermission('system.engine_access_delegation.read'))
const canCreate = computed(() => auth.hasPermission('system.engine_access_delegation.create'))
const canRevoke = computed(() => auth.hasPermission('system.engine_access_delegation.revoke'))
const canReadMembers = computed(() => auth.hasPermission('iam.tenant_membership.read'))
const rows = ref([]), total = ref(0), page = ref(1), loading = ref(false), error = ref('')
const members = ref([]), membersLoading = ref(false), saving = ref(false), created = ref(false)
const form = reactive({ membershipID: '', expiresAt: null, reason: '' })
const revokeVisible = ref(false), revokeRow = ref(null), revokeReason = ref(''), revokeError = ref(''), revoking = ref(false)
let generation = 0, readSequence = 0, memberSequence = 0
const message = e => e?.response?.data?.error || t('system.engine.delegations.failed')
const formatTime = value => value ? new Date(value).toLocaleString(locale.value) : '—'
async function load() {
  if (!canRead.value) return
  const epoch = generation, seq = ++readSequence
  loading.value = true; error.value = ''; rows.value = []; total.value = 0
  try {
    const result = await enginesAPI.listAccessDelegations(props.engineId, { page: page.value, page_size: 10 })
    if (epoch === generation && seq === readSequence) { rows.value = result.data; total.value = result.total }
  } catch (e) { if (epoch === generation && seq === readSequence) error.value = message(e) }
  finally { if (epoch === generation && seq === readSequence) loading.value = false }
}
function changePage(value) { page.value = value; load() }
async function loadMembers() {
  if (!canCreate.value || !canReadMembers.value) return
  const epoch = generation, seq = ++memberSequence
  membersLoading.value = true
  try {
    const result = await iamAPI.memberships.listAll({ principal_type: 'user', status: 'active' })
    if (epoch === generation && seq === memberSequence) members.value = result.filter(member => member.principal_status === 'active' && !member.ended_at && (!member.expires_at || new Date(member.expires_at).getTime() > Date.now()))
  } catch (e) { if (epoch === generation && seq === memberSequence) { members.value = []; error.value = message(e) } }
  finally { if (epoch === generation && seq === memberSequence) membersLoading.value = false }
}
async function create() {
  if (saving.value || !canCreate.value || !canReadMembers.value) return
  let payload
  try { payload = captureDelegation(form, members.value) }
  catch { error.value = t('system.engine.delegations.invalid'); return }
  const epoch = generation
  saving.value = true; error.value = ''; created.value = false
  try {
    await enginesAPI.createAccessDelegation(props.engineId, payload)
    if (epoch !== generation) return
    form.membershipID = ''; form.expiresAt = null; form.reason = ''; created.value = true
    await load()
  } catch (e) { if (epoch === generation) error.value = message(e) }
  finally { if (epoch === generation) saving.value = false }
}
function openRevoke(row) {
  if (!canRevoke.value || row.status !== 'active' || row.effective_state === 'expired' || !Number.isSafeInteger(row.version) || row.version <= 0) return
  revokeRow.value = row; revokeReason.value = ''; revokeError.value = ''; revokeVisible.value = true
}
async function revoke() {
  if (revoking.value || !canRevoke.value || !revokeRow.value) return
  const reason = revokeReason.value.trim()
  if (!reason || Array.from(reason).length > 2000) { revokeError.value = t('system.engine.delegations.invalidReason'); return }
  const epoch = generation, row = revokeRow.value
  revoking.value = true; revokeError.value = ''
  try {
    await enginesAPI.revokeAccessDelegation(props.engineId, row.id, { version: row.version, reason })
    if (epoch !== generation) return
    revokeVisible.value = false; await load()
  } catch (e) { if (epoch === generation) revokeError.value = message(e) }
  finally { if (epoch === generation) revoking.value = false }
}
watch(() => [props.engineId, auth.authContext?.principal?.id, auth.authContext?.context?.tenant_id,
  auth.authContext?.context?.tenant_membership_id, auth.authContext?.authorization?.authorization_version,
  canRead.value, canCreate.value, canRevoke.value, canReadMembers.value], () => {
  generation++; page.value = 1; rows.value = []; total.value = 0; members.value = []
  loading.value = membersLoading.value = saving.value = revoking.value = revokeVisible.value = created.value = false
  revokeRow.value = null; error.value = revokeError.value = ''; form.membershipID = ''; form.expiresAt = null; form.reason = ''
  load(); loadMembers()
}, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.delegations { display: grid; gap: 16px; }
.delegations :deep(.el-date-editor) { width: 100%; }
</style>
