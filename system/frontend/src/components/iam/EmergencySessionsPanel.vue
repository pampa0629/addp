<template>
  <section class="emergency-sessions">
    <p>{{ label('scope') }}</p>
    <div class="emergency-sessions__toolbar">
      <el-button v-if="canCreate" type="primary" :disabled="blocked" @click="openCreate">{{ actionLabel('create') }}</el-button>
      <el-button :loading="loading" @click="load">{{ label('refresh') }}</el-button>
      <el-button v-if="needsReadMFA || requireFreshMFA" @click="stepUp.request()">{{ label('stepUp') }}</el-button>
      <el-form class="emergency-sessions__lookup" @submit.prevent="lookup(lookupID)">
        <el-input v-model.trim="lookupID" :aria-label="label('requestID')" :placeholder="label('requestID')" maxlength="36" />
        <el-button native-type="submit" :loading="lookingUp" :disabled="needsReadMFA || !isUUID(lookupID) || submitting">{{ label('lookup') }}</el-button>
      </el-form>
    </div>
    <el-alert v-if="unresolved" type="warning" :closable="false" :title="label('unresolved', { id: unresolved })" />
    <el-alert v-if="notice" type="warning" :closable="false" :title="notice" />

    <el-table v-loading="loading" :data="rows" row-key="id">
      <el-table-column prop="incident_reference" :label="label('incident')" min-width="150" show-overflow-tooltip />
      <el-table-column :label="label('tenant')" min-width="130">
        <template #default="{ row }">{{ row.tenant_name || row.tenant_id }}</template>
      </el-table-column>
      <el-table-column :label="label('target')" min-width="150">
        <template #default="{ row }">{{ row.target_name || row.target_principal_id }}</template>
      </el-table-column>
      <el-table-column :label="label('state')" min-width="140">
        <template #default="{ row }">{{ stateLabel(row) }}</template>
      </el-table-column>
      <el-table-column :label="label('approvals')" width="105">
        <template #default="{ row }">{{ row.approvals?.length || 0 }} / 2</template>
      </el-table-column>
      <el-table-column :label="label('operation')" width="145">
        <template #default="{ row }">
          <el-button link :disabled="needsReadMFA || submitting || lookingUp" @click="lookup(row.id)">{{ label('details') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />

    <el-dialog v-model="detailOpen" :title="label('details')" width="min(680px, calc(100% - 24px))">
      <template v-if="detail">
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="label('requestID')">{{ detail.id }}</el-descriptions-item>
          <el-descriptions-item :label="label('state')">{{ stateLabel(detail) }}</el-descriptions-item>
          <el-descriptions-item :label="label('tenant')">{{ detail.tenant_name || detail.tenant_id }} ({{ detail.tenant_id }})</el-descriptions-item>
          <el-descriptions-item :label="label('executor')">{{ detail.executor_name || detail.executor_principal_id }}</el-descriptions-item>
          <el-descriptions-item :label="label('target')">{{ detail.target_name || detail.target_principal_id }} ({{ detail.target_principal_id }})</el-descriptions-item>
          <el-descriptions-item :label="label('family')">{{ detail.target_family_id }}</el-descriptions-item>
          <el-descriptions-item :label="label('incident')">{{ detail.incident_reference }}</el-descriptions-item>
          <el-descriptions-item :label="label('reason')">{{ detail.reason }}</el-descriptions-item>
          <el-descriptions-item :label="label('approvalDeadline')">{{ formatTime(detail.approval_expires_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="detail.execute_before" :label="label('executionDeadline')">{{ formatTime(detail.execute_before) }}</el-descriptions-item>
          <el-descriptions-item :label="label('approvals')">
            <div v-for="approval in detail.approvals" :key="approval.role_key">
              {{ label('roles.' + approval.role_key.split('.')[1]) }} · {{ approval.display_name || approval.principal_id }} · {{ formatTime(approval.approved_at) }} · {{ approval.reason }}
            </div>
            <span v-if="!detail.approvals?.length">—</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.close_reason" :label="label('closeReason')">{{ detail.close_reason }}</el-descriptions-item>
          <template v-if="detail.execution_id">
            <el-descriptions-item :label="label('executionID')">{{ detail.execution_id }}</el-descriptions-item>
            <el-descriptions-item :label="label('outcome')">{{ label('outcomes.' + detail.outcome) }} · {{ formatTime(detail.executed_at) }}</el-descriptions-item>
          </template>
        </el-descriptions>
      </template>
      <template #footer>
        <el-button v-for="action in availableActions" :key="action" :disabled="blocked" @click="openAction(action)">{{ actionLabel(action) }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="formOpen" :title="actionLabel(command?.action)" width="min(570px, calc(100% - 24px))" :close-on-click-modal="false">
      <el-form v-if="command" label-position="top" @submit.prevent="submit">
        <el-form-item :label="label('requestID')"><code>{{ command.id }}</code></el-form-item>
        <template v-if="command.action === 'create'">
          <p>{{ label('targetHint') }}</p>
          <el-form-item :label="label('targetID')" required><el-input v-model.trim="form.target_principal_id" inputmode="numeric" maxlength="19" /></el-form-item>
          <el-form-item :label="label('family')" required><el-input v-model.trim="form.target_family_id" inputmode="numeric" maxlength="19" /></el-form-item>
          <el-form-item :label="label('incident')" required><el-input v-model.trim="form.incident_reference" maxlength="200" /></el-form-item>
        </template>
        <p v-else>{{ label('confirmTarget', { tenant: command.row.tenant_id, target: command.row.target_principal_id, family: command.row.target_family_id }) }}</p>
        <el-form-item v-if="command.action !== 'execute'" :label="label('reason')" required>
          <el-input v-model.trim="form.reason" type="textarea" maxlength="1000" />
        </el-form-item>
        <el-alert v-if="formNotice" type="error" :closable="false" :title="formNotice" />
        <el-alert v-if="needsActionMFA" type="warning" :closable="false" :title="label('stepUpHint')" />
      </el-form>
      <template #footer>
        <el-button @click="formOpen = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button v-if="needsActionMFA" type="primary" @click="stepUp.request()">{{ label('stepUp') }}</el-button>
        <el-button v-else type="primary" :loading="submitting" :disabled="!formValid || blocked" @click="submit">{{ label('confirm') }}</el-button>
      </template>
    </el-dialog>
    <MFAStepUpDialog ref="stepUp" />
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import MFAStepUpDialog from './MFAStepUpDialog.vue'

const { t, d } = useI18n()
const label = (key, args) => t('system.iam.emergencySessions.' + key, args || {})
const actionLabel = action => action ? label('actions.' + action) : ''
const stateLabel = row => label('states.' + row.effective_state)
const formatTime = value => d(new Date(value), { dateStyle: 'short', timeStyle: 'short' })
const auth = useAuthStore()
const context = computed(() => auth.authContext)
const scope = computed(() => context.value?.context?.type)
const principal = computed(() => context.value?.principal?.id)
const can = action => auth.hasPermission('iam.emergency_session_revocation.' + action)
const canCreate = computed(() => scope.value === 'tenant' && can('create'))
const stepUp = ref(null)
const rows = ref([])
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const lookingUp = ref(false)
const submitting = ref(false)
const lookupID = ref('')
const unresolved = ref('')
const notice = ref('')
const formNotice = ref('')
const detail = ref(null)
const detailOpen = ref(false)
const formOpen = ref(false)
const command = ref(null)
const form = reactive({ target_principal_id: '', target_family_id: '', incident_reference: '', reason: '' })
const blocked = computed(() => Boolean(unresolved.value || submitting.value || lookingUp.value))
const assurance = computed(() => context.value?.authentication)
const hasMFA = computed(() => ['aal2', 'aal3'].includes(assurance.value?.assurance_level))
const needsReadMFA = computed(() => !hasMFA.value)
const requireFreshMFA = ref(false)
const needsMFA = action => requireFreshMFA.value || !hasMFA.value || (!['create', 'cancel'].includes(action) &&
  !(Date.parse(assurance.value?.step_up_expires_at) > Date.now()))
const needsActionMFA = computed(() => command.value && needsMFA(command.value.action))
const platformRole = computed(() => (context.value?.authorization?.role_assignments || []).find(assignment =>
  assignment.scope?.type === 'platform' &&
  ['platform.system_administrator', 'platform.security_administrator'].includes(assignment.role_key))?.role_key)
const availableActions = computed(() => detail.value ? actionsFor(detail.value) : [])
const formValid = computed(() => {
  const action = command.value?.action
  if (!action) return false
  if (action !== 'execute' && !fits(form.reason, 1000)) return false
  return action !== 'create' || (isID(form.target_principal_id) && isID(form.target_family_id) && fits(form.incident_reference, 200))
})
const api = iamAPI.emergencySessions
let generation = 0
let listRead = 0
let execution = null
let pendingWrite = null

function actionsFor(row) {
  const state = row.effective_state
  if (scope.value === 'tenant' && row.executor_principal_id === principal.value) {
    const actions = []
    if (['pending', 'approved'].includes(state) && can('cancel')) actions.push('cancel')
    if (state === 'approved' && can('execute')) actions.push('execute')
    return actions
  }
  if (scope.value !== 'platform' || !platformRole.value) return []
  if (state === 'approved') return can('revoke') ? ['revoke'] : []
  if (state !== 'pending' || [row.executor_principal_id, row.target_principal_id].includes(principal.value)) return []
  const actions = []
  if (can('approve') && !row.approvals?.some(a => a.principal_id === principal.value || a.role_key === platformRole.value)) actions.push('approve')
  if (can('reject')) actions.push('reject')
  return actions
}

const isID = value => /^[1-9]\d{0,18}$/.test(value) && (value.length < 19 || value <= '9223372036854775807')
const isUUID = value => /^[\da-f]{8}(?:-[\da-f]{4}){3}-[\da-f]{12}$/i.test(value)
const fits = (value, limit) => value.trim() && new TextEncoder().encode(value.trim()).length <= limit

function clearForm() {
  formOpen.value = false
  command.value = null
  formNotice.value = ''
  Object.keys(form).forEach(key => { form[key] = '' })
}

watch([() => auth.token, context], () => {
  invalidateRequests()
  requireFreshMFA.value = false
  rows.value = []
  page.value = 1
  total.value = 0
  loading.value = lookingUp.value = submitting.value = false
  lookupID.value = unresolved.value = notice.value = ''
  detail.value = null
  detailOpen.value = false
  execution = null
  clearForm()
}, { flush: 'sync' })
watch([() => auth.token, context], load, { immediate: true })
onBeforeUnmount(invalidateRequests)

function invalidateRequests() {
  generation++
  pendingWrite?.abort()
  pendingWrite = null
}

async function load() {
  if (!context.value || !can('read') || !hasMFA.value) return
  const origin = generation
  const read = ++listRead
  loading.value = true
  try {
    const result = await api.list(scope.value, { page: page.value, page_size: 20 })
    if (origin !== generation || read !== listRead) return
    rows.value = result.data || []
    total.value = result.total
  } catch (error) {
    if (origin === generation && read === listRead) notice.value = error.response?.data?.error || label('readFailed')
  } finally {
    if (origin === generation && read === listRead) loading.value = false
  }
}

async function lookup(id) {
  if (!can('read') || !hasMFA.value || !isUUID(id) || lookingUp.value || submitting.value) return
  const origin = generation
  notice.value = ''
  lookingUp.value = true
  try {
    const row = await api.get(scope.value, id)
    if (origin !== generation) return
    if (unresolved.value.toLowerCase() === id.toLowerCase()) unresolved.value = ''
    showDetail(row)
  } catch {
    if (origin === generation) notice.value = label('lookupFailed', { id })
  } finally {
    if (origin === generation) lookingUp.value = false
  }
}

function showDetail(row) {
  detail.value = row
  detailOpen.value = true
}

function openCreate() {
  if (!canCreate.value || blocked.value) return
  clearForm()
  command.value = { action: 'create', id: crypto.randomUUID() }
  formOpen.value = true
}

function openAction(action) {
  if (blocked.value || !availableActions.value.includes(action)) return
  clearForm()
  command.value = { action, id: detail.value.id, row: detail.value }
  detailOpen.value = false
  formOpen.value = true
}

async function submit() {
  if (!formOpen.value || !formValid.value || blocked.value) return
  const { action, id, row } = command.value
  if (action === 'create' ? !canCreate.value : !actionsFor(row).includes(action)) return
  if (needsMFA(action)) {
    requireFreshMFA.value = true
    return
  }
  const origin = generation
  let payload = { version: row?.version, reason: form.reason.trim() }
  if (action === 'create') payload = { id, ...form, reason: form.reason.trim() }
  if (action === 'execute') {
    if (execution?.id !== id || execution?.version !== row.version) execution = { id, version: row.version, uuid: crypto.randomUUID() }
    payload = { version: row.version, execution_id: execution.uuid }
  }
  notice.value = formNotice.value = ''
  submitting.value = true
  pendingWrite = new AbortController()
  const signal = pendingWrite.signal
  try {
    const result = action === 'create' ? await api.create(payload, signal) : await api[action](id, payload, signal)
    if (origin !== generation) return
    clearForm()
    showDetail(result)
    await load()
  } catch (error) {
    if (origin !== generation) return
    const status = error.response?.status
    if (!status || status === 401 || status === 409 || status >= 500) {
      clearForm()
      unresolved.value = lookupID.value = id
    } else if (error.response?.data?.error_code === 'step_up_required') {
      requireFreshMFA.value = true
      clearForm()
      notice.value = label('stepUpHint')
    } else {
      formNotice.value = error.response?.data?.error || label('writeFailed')
    }
  } finally {
    if (origin === generation) {
      pendingWrite = null
      submitting.value = false
    }
  }
}
</script>

<style scoped>
.emergency-sessions { display: grid; gap: var(--spacing-md, 16px); }
.emergency-sessions__toolbar, .emergency-sessions__lookup { display: flex; flex-wrap: wrap; align-items: center; gap: var(--spacing-sm, 8px); }
.emergency-sessions__lookup .el-input { width: min(290px, 100%); }
.emergency-sessions :deep(.el-descriptions__content) { overflow-wrap: anywhere; }
</style>
