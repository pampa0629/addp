<template>
  <section class="source-grants" data-testid="engine-source-grants">
    <el-alert v-if="view === 'list'" type="info" :closable="false" :title="t(listMode === 'history' ? 'system.engine.sourceGrants.historyBoundary' : 'system.engine.sourceGrants.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <StatusAnnouncer :message="success" />
    <el-alert v-if="success" type="success" :closable="false" :title="success" />
    <div v-if="view === 'create' && requirements.length && canCreate" class="grant-form" data-testid="source-grant-form">
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
      <template v-if="view === 'list'">
        <el-radio-group v-model="listMode" data-testid="source-grant-list-mode" :disabled="revoking" @change="changeListMode">
          <el-radio-button value="current">{{ t('system.engine.sourceGrants.current') }}</el-radio-button>
          <el-radio-button value="history">{{ t('system.engine.sourceGrants.history') }}</el-radio-button>
        </el-radio-group>
        <el-form class="grant-filters" label-position="top" data-testid="source-grant-filters" @submit.prevent="applyFilters">
          <el-form-item :label="t('system.engine.dataAuthorization.target')">
            <el-input v-model="filterDraft.tableSearch" data-testid="source-grant-table-filter" :placeholder="t('system.engine.sourceGrants.tableSearch')" maxlength="200" clearable @input="scheduleFilters" />
          </el-form-item>
          <el-form-item class="recipient-type-filter" :label="t('system.engine.sourceGrants.recipientType')">
            <el-select v-model="filterDraft.recipientType" data-testid="source-grant-type-filter" clearable :placeholder="t('system.engine.sourceGrants.allRecipientTypes')" @change="changeRecipientType">
              <el-option v-for="kind in Object.keys(recipientSources)" :key="kind" :value="kind" :label="t(`system.engine.sourceGrants.types.${kind}`)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('system.engine.sourceGrants.recipient')">
            <TenantMemberSelect v-if="filterDraft.recipientType === 'user'" v-model="filterDraft.recipientID" data-testid="source-grant-recipient-filter" :members="filterMembers" principal-type="user" value-field="principal_id" stringify-value :disabled="!canReadFilterRecipients || filterRecipientStatus !== 'ready'" :loading="filterRecipientStatus === 'loading'" :placeholder="t('system.engine.sourceGrants.allAccounts')" :current-membership-id="auth.authContext?.context?.tenant_membership_id" @change="applyFilters" />
            <el-select v-else v-model="filterDraft.recipientID" data-testid="source-grant-recipient-filter" filterable clearable :disabled="!filterDraft.recipientType || !canReadFilterRecipients || filterRecipientStatus !== 'ready'" :loading="filterRecipientStatus === 'loading'" :placeholder="t(filterDraft.recipientType ? 'system.engine.sourceGrants.allRecipients' : 'system.engine.sourceGrants.selectRecipientType')" @change="applyFilters">
              <el-option v-for="item in filterOrganizations" :key="item.id" :value="String(item.id)" :label="[item.name, item.code].filter(Boolean).join(' · ')" />
            </el-select>
          </el-form-item>
        </el-form>
        <p class="filter-hint">{{ t(accountScope ? 'system.engine.sourceGrants.accountSourcesHint' : 'system.engine.sourceGrants.recipientFilterBoundary') }}</p>
        <el-alert v-if="filterDraft.recipientType && (!canReadFilterRecipients || filterRecipientStatus === 'failed')" type="warning" :closable="false" :title="t(`system.engine.sourceGrants.recipientNames.${canReadFilterRecipients ? 'failed' : 'forbidden'}`)" />
        <el-button :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
        <el-table ref="grantTable" :data="rows" v-loading="loading" data-testid="source-grant-list" :empty-text="t('system.engine.sourceGrants.noMatchingSources')">
          <el-table-column v-if="accountScope" type="expand">
            <template #default="{ row: target }">
              <div class="source-details" data-testid="account-grant-sources">
                <p>{{ t('system.engine.sourceGrants.inspection.observedAt') }}：{{ formatTime(target.inspection.observed_at) }}</p>
                <el-table :data="target.inspection.sources" :empty-text="t('system.engine.sourceGrants.inspection.noSources')">
                  <el-table-column :label="t('system.engine.sourceGrants.source')" min-width="220"><template #default="{ row }"><EngineGrantRecipient :type="row.recipient_type" :id="row.recipient_id" v-bind="recipientPresentation(row)" /></template></el-table-column>
                  <el-table-column :label="t('system.engine.dataAuthorization.mode')" min-width="150"><template #default="{ row }">{{ t(`system.engine.dataAuthorization.modes.${row.approval_mode}`) }}</template></el-table-column>
                  <el-table-column :label="t('system.engine.sourceGrants.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
                  <el-table-column :label="t('system.engine.sourceGrants.sourceCount')" prop="grant_count" min-width="100" />
                  <el-table-column v-if="canRevoke" width="100" fixed="right"><template #default="{ row }"><el-button data-testid="source-grant-revoke" link type="danger" @click="openRevoke({ ...row, catalog_path: target.catalog_path })">{{ t('system.engine.sourceGrants.revokeSource') }}</el-button></template></el-table-column>
                </el-table>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="180"><template #default="{ row }">{{ approvalPathLabel(row.catalog_path) }}</template></el-table-column>
          <el-table-column :label="t('system.engine.sourceGrants.recipient')" min-width="220"><template #default="{ row }"><EngineGrantRecipient :type="row.recipient_type" :id="row.recipient_id" v-bind="recipientPresentation(row)" /></template></el-table-column>
          <el-table-column v-if="accountScope" :label="t('system.engine.sourceGrants.source')" min-width="140"><template #default="{ row }"><el-button data-testid="account-grant-expand" link type="primary" @click="grantTable?.toggleRowExpansion(row)">{{ t('system.engine.sourceGrants.viewSources', { count: row.inspection.sources.length }) }}</el-button></template></el-table-column>
          <el-table-column v-if="!accountScope" :label="t('system.engine.dataAuthorization.mode')" min-width="150"><template #default="{ row }">{{ row.approval_mode ? t(`system.engine.dataAuthorization.modes.${row.approval_mode}`) : '—' }}</template></el-table-column>
          <el-table-column v-if="!accountScope" :label="t('system.engine.sourceGrants.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
          <el-table-column :label="t(accountScope ? 'system.engine.sourceGrants.ruleStatus' : 'system.engine.sourceGrants.state')" min-width="220"><template #default="{ row }"><span v-if="accountScope" data-testid="account-grant-status">{{ t(`system.engine.sourceGrants.inspection.reasons.${row.inspection.reason}`) }}</span><span v-else>{{ t(`system.engine.sourceGrants.states.${state(row)}`) }}</span></template></el-table-column>
          <el-table-column v-if="listMode === 'current' && !accountScope" :label="t('system.engine.sourceGrants.sourceCount')" min-width="120"><template #default="{ row }">{{ row.grant_count }}</template></el-table-column>
          <el-table-column v-if="listMode === 'history'" :label="t('system.engine.sourceGrants.command')" prop="request_id" min-width="220" show-overflow-tooltip />
          <el-table-column v-if="canRevoke && listMode === 'current' && !accountScope" width="100" fixed="right"><template #default="{ row }"><el-button data-testid="source-grant-revoke" link type="danger" @click="openRevoke(row)">{{ t('system.engine.sourceGrants.revoke') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination layout="prev, pager, next, total" :total="total" :page-size="20" :current-page="page" @current-change="changePage" />
      </template>
    </template>
    <el-alert v-else-if="view !== 'create'" type="info" :closable="false" :title="t('system.engine.sourceGrants.readRequired')" />
    <el-dialog v-model="revokeVisible" class="addp-dialog" append-to-body width="min(600px, calc(100vw - 24px))" :title="t('system.engine.sourceGrants.revoke')" :close-on-click-modal="!revoking" :close-on-press-escape="!revoking" :show-close="!revoking" @opened="revokeCancel?.$el?.focus()">
      <p>{{ approvalPathLabel(revokeRow?.catalog_path) }}</p>
      <EngineGrantRecipient v-if="revokeRow" :type="revokeRow.recipient_type" :id="revokeRow.recipient_id" v-bind="recipientPresentation(revokeRow)" />
      <el-alert type="warning" :closable="false" :title="t('system.engine.sourceGrants.revokeBoundary')" />
      <el-alert v-if="revokeRow && revokeRow.recipient_type !== 'user'" type="warning" :closable="false" :title="t('system.engine.sourceGrants.revokeOrganizationBoundary')" />
      <p>{{ t('system.engine.sourceGrants.revokeCount', { count: revokeRow?.grant_count }) }}</p>
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
import { approvalPathLabel, approvalTargetsEqual } from '../../utils/engineApprovalCatalog'
import { captureIndependentGrant } from '../../utils/independentGrant'
import TenantMemberSelect from '../iam/TenantMemberSelect.vue'
import EngineGrantRecipient from './EngineGrantRecipient.vue'

const props = defineProps({ engine: { type: Object, required: true }, view: { type: String, required: true, validator: value => ['list', 'create'].includes(value) }, requirements: { type: Array, default: () => [] } })
const recipientTypeSelect = ref(null), revokeCancel = ref(null), grantTable = ref(null)
const emit = defineEmits(['close', 'busy', 'issued']), auth = useAuthStore(), { t, locale } = useI18n()
const canCreate = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canRead = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const canRevoke = computed(() => auth.hasPermission('system.engine_access_grant.revoke'))
const recipientSources = {
  user: { permission: 'iam.tenant_membership.read', api: iamAPI.memberships, id: 'principal_id' },
  department: { permission: 'iam.department.read', api: iamAPI.departments, id: 'id' },
  project_group: { permission: 'iam.project_group.read', api: iamAPI.projectGroups, id: 'id' }
}
const form = reactive({ recipientType: 'user', recipientID: '', expiryMode: '', expiresAt: null, reason: '' })
const canReadCandidates = computed(() => auth.hasPermission(recipientSources[form.recipientType]?.permission))
const rows = ref([]), total = ref(0), page = ref(1), loading = ref(false), error = ref(''), success = ref('')
const listMode = ref('current')
const accountScope = computed(() => listMode.value === 'current' && appliedFilters.value.recipient_type === 'user' && !!appliedFilters.value.recipient_id)
const filterDraft = reactive({ tableSearch: '', recipientType: '', recipientID: '' })
const appliedFilters = ref({})
const filterMembers = ref([])
const recipientNames = ref({})
const canReadFilterRecipients = computed(() => auth.hasPermission(recipientSources[filterDraft.recipientType]?.permission))
const filterRecipientStatus = computed(() => recipientNames.value[filterDraft.recipientType]?.status)
const filterOrganizations = computed(() => filterDraft.recipientType === 'user' ? [] : Object.values(recipientNames.value[filterDraft.recipientType]?.items || {}))
const candidates = ref([]), members = ref([]), candidateLoading = ref(false), formError = ref(''), saving = ref(false), attempt = ref(null)
const outcomes = ref([])
const revokeVisible = ref(false), revokeRow = ref(null), revokeReason = ref(''), revokeError = ref(''), revoking = ref(false)
let generation = 0, readSequence = 0, candidateSequence = 0
let filterTimer = null
const message = e => e?.response?.data?.error || t('system.engine.sourceGrants.failed')
const formatTime = value => new Date(value).toLocaleString(locale.value)
const expiryLabel = row => row.expiry_mode === 'until_revoked' ? t('system.engine.sourceGrants.until_revoked') : row.expires_at ? formatTime(row.expires_at) : '—'
const state = row => row.revocation ? 'revoked' : row.expires_at && new Date(row.expires_at).getTime() <= Date.now() ? 'expired' : 'issued'
function recipientPresentation(row, cache = recipientNames.value) {
  const names = cache[row.recipient_type]
  const identity = names?.items?.[String(row.recipient_id)]
  return { identity, status: identity ? 'ready' : names?.status === 'ready' ? 'unavailable' : names?.status || 'loading' }
}
async function fetchRecipientNames(kind) {
  const source = recipientSources[kind]
  if (!source || !auth.hasPermission(source.permission)) return { status: 'forbidden' }
  try {
    const items = await source.api.listAll(kind === 'user' ? { principal_type: 'user' } : {})
    return { status: 'ready', items: Object.fromEntries(items.map(item => [String(item[source.id]), item])), members: items }
  } catch { return { status: 'failed' } }
}
async function loadRecipientNames(epoch, seq) {
  const kinds = Object.keys(recipientSources)
  await Promise.all(kinds.map(async kind => {
    const source = recipientSources[kind]
    if (!source) return
    recipientNames.value[kind] = { status: auth.hasPermission(source.permission) ? 'loading' : 'forbidden' }
    if (!auth.hasPermission(source.permission)) return
    // Include inactive history identities; eligibility belongs to the grant form.
    const names = await fetchRecipientNames(kind)
    if (epoch !== generation || seq !== readSequence) return
    recipientNames.value[kind] = names
    if (kind === 'user' && names.status === 'ready') filterMembers.value = names.members
  }))
}
function validAccountObservation(row) {
  const observation = row.inspection
  return observation?.account_id === appliedFilters.value.recipient_id && approvalTargetsEqual(observation.catalog_path, row.catalog_path) && Array.isArray(observation.sources) &&
    ['grant', 'no_grant', 'explicit_deny', 'source_unavailable', 'target_unavailable'].includes(observation.reason) &&
    observation.rule_covered === (observation.reason === 'grant') && Number.isFinite(new Date(observation.observed_at).getTime())
}
async function load() {
  if (props.view === 'create') return
  if (filterTimer !== null) captureFilters()
  if (!canRead.value) return
  const epoch = generation, seq = ++readSequence
  // Preserve total while loading: resetting it makes Element Plus clamp page 2
  // to page 1 and issue a competing request before the response arrives.
  loading.value = true; error.value = ''; rows.value = []; recipientNames.value = {}
  try {
    const list = listMode.value === 'history' ? enginesAPI.listSourceGrantHistory : enginesAPI.listSourceGrants
    const result = await list(props.engine.id, { page: page.value, page_size: 20, ...appliedFilters.value })
    if (epoch !== generation || seq !== readSequence) return
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0) throw new Error('invalidHistory')
    if (accountScope.value && result.data.some(row => !validAccountObservation(row))) throw new Error('invalidInspection')
    rows.value = result.data; total.value = result.total
    await loadRecipientNames(epoch, seq)
  } catch (e) { if (epoch === generation && seq === readSequence) error.value = message(e) }
  finally { if (epoch === generation && seq === readSequence) loading.value = false }
}
function changePage(value) { if (filterTimer !== null) return applyFilters(); if (value === page.value) return; page.value = value; load() }
function changeListMode() { applyFilters() }
function cancelFilterTimer() { clearTimeout(filterTimer); filterTimer = null }
function captureFilters() {
  cancelFilterTimer()
  const tableSearch = filterDraft.tableSearch.trim()
  appliedFilters.value = { ...(tableSearch ? { table_search: tableSearch } : {}),
    ...(filterDraft.recipientType ? { recipient_type: filterDraft.recipientType, ...(filterDraft.recipientID ? { recipient_id: filterDraft.recipientID } : {}) } : {}) }
  page.value = 1
}
function applyFilters() { captureFilters(); load() }
function changeRecipientType() { filterDraft.recipientID = ''; applyFilters() }
function scheduleFilters() {
  cancelFilterTimer()
  // Invalidate in-flight results as soon as input changes, before the debounce.
  readSequence++; rows.value = []; error.value = ''
  if (!filterDraft.tableSearch.trim()) return applyFilters()
  loading.value = true
  filterTimer = setTimeout(applyFilters, 300)
}
async function loadCandidates() {
  const epoch = generation, seq = ++candidateSequence, kind = form.recipientType
  candidates.value = []; members.value = []; form.recipientID = ''; candidateLoading.value = false; formError.value = ''
  if (!props.requirements.length || !canCreate.value || !canReadCandidates.value) return
  candidateLoading.value = true
  try {
    const result = await recipientSources[kind].api.listAll({ status: 'active', ...(kind === 'user' ? { principal_type: 'user' } : {}) })
    if (epoch !== generation || seq !== candidateSequence) return
    if (kind === 'user') {
      members.value = result.filter(item => item.principal_type === 'user' && item.status === 'active' && item.principal_status === 'active' && !item.ended_at && (!item.expires_at || new Date(item.expires_at).getTime() > Date.now()))
      candidates.value = members.value.map(item => ({ id: String(item.principal_id), name: item.display_name || item.username || String(item.principal_id) }))
    } else candidates.value = result.filter(item => item.status === 'active').map(item => ({ id: String(item.id), name: item.name }))
  } catch (e) { if (epoch === generation && seq === candidateSequence) formError.value = message(e) }
  finally { if (epoch === generation && seq === candidateSequence) candidateLoading.value = false }
}
function closeGrant() {
  if (saving.value) return
  attempt.value = null; emit('busy', false); emit('close')
}
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
    if (!recovering) outcomes.value = []
    attempt.value = captured
    for (const command of captured) {
      if (outcomes.value.some(row => row.requestID === command.requestID && (row.done || row.terminal))) continue
      let outcome
      try {
        const result = await enginesAPI.createSourceGrant(props.engine.id, command.payload)
        if (epoch !== generation) return
        if (result?.request_id !== command.requestID || result.approval_mode !== 'independent' || !result.granted_at) throw new Error('invalidIssuance')
        outcome = { ...command, done: true, error: '', recovered: recovering || !!result.revocation }
      } catch (e) {
        if (epoch !== generation) return
        outcome = { ...command, done: false, terminal: e?.response?.data?.error_code === 'engine_access_grant_relation_exists', error: message(e) }
      }
      outcomes.value = [...outcomes.value.filter(row => row.requestID !== command.requestID), outcome]
      if (!canCreate.value) return
    }
    if (outcomes.value.every(row => row.done)) {
      success.value = t(recovering ? 'system.engine.sourceGrants.historyRecovered' : 'system.engine.sourceGrants.created')
      // Locate only this batch's recipient; keep list filtering server-side.
      filterDraft.recipientType = first.recipientType
      filterDraft.recipientID = first.recipientID
      filterDraft.tableSearch = captured.length === 1 ? first.targetLabel : ''
      captureFilters(); listMode.value = 'current'
      saving.value = false; attempt.value = null; emit('busy', false); emit('issued')
    } else {
      formError.value = t('system.engine.sourceGrants.partialFailure')
      // A definite duplicate rejection is not an unknown issuance. Do not keep
      // an immutable retry command that could later grant after withdrawal.
      if (outcomes.value.every(row => row.done || row.terminal)) attempt.value = null
    }
    await load()
  } catch (e) { if (epoch === generation && e !== 'cancel' && e !== 'close') formError.value = e?.message === 'invalidGrant' || e?.message === 'invalidEngineCatalogTarget' ? t('system.engine.sourceGrants.invalid') : message(e) }
  finally { if (epoch === generation) saving.value = false }
}
function openRevoke(row) { if (!canRevoke.value || listMode.value !== 'current') return; revokeRow.value = row; revokeReason.value = ''; revokeError.value = ''; revokeVisible.value = true }
async function revoke() {
  if (revoking.value || !canRevoke.value || !revokeRow.value) return
  const reason = revokeReason.value.trim(), row = revokeRow.value, epoch = generation
  if (!reason || Array.from(reason).length > 2000) { revokeError.value = t('system.engine.sourceGrants.invalidReason'); return }
  revoking.value = true; revokeError.value = ''
  try {
    const result = await enginesAPI.revokeSourceGrant(props.engine.id, row.request_id, reason)
    if (epoch !== generation) return
    if (result?.request_id !== row.request_id || !result.revoked_at || !Array.isArray(result.revoked_request_ids) || !result.revoked_request_ids.includes(row.request_id)) throw new Error('invalidRevocation')
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
  ...Object.values(recipientSources).map(source => () => auth.hasPermission(source.permission))], () => {
  cancelFilterTimer(); generation++; attempt.value = null; outcomes.value = []; candidates.value = []; members.value = []; rows.value = []; recipientNames.value = {}; total.value = 0; page.value = 1

  filterDraft.tableSearch = ''; filterDraft.recipientType = ''; filterDraft.recipientID = ''; appliedFilters.value = {}; filterMembers.value = []
  saving.value = revoking.value = loading.value = false; revokeVisible.value = false; error.value = formError.value = success.value = ''; load()
}, { immediate: true })
watch(() => props.view, view => {
  cancelFilterTimer(); readSequence++; error.value = ''; loading.value = false
  if (view !== 'list') success.value = ''
  if (view === 'list') { listMode.value = 'current'; captureFilters() }
  load()
})
watch([saving, attempt, revoking], () => emit('busy', saving.value || !!attempt.value || revoking.value), { immediate: true })
onBeforeUnmount(() => { cancelFilterTimer(); generation++ })
</script>

<style scoped>
.source-grants { display: grid; gap: 16px; min-width: 0; }
h3 { margin: 0; }
.el-select { width: 100%; }
.grant-form { display: grid; gap: 16px; }
.grant-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; }
.grant-filters .el-form-item { flex: 1 1 260px; min-width: 0; margin-bottom: 0; }
.grant-filters .recipient-type-filter { flex: 0 1 180px; }
.grant-filters :deep(.iam-member-select) { min-width: 0; }
.source-details { padding: 12px; display: grid; gap: 12px; }
.filter-hint { margin: 0; color: var(--addp-text-secondary); }
p { overflow-wrap: anywhere; }
</style>
