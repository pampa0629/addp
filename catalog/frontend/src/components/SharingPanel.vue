<template>
  <section class="sharing-panel" data-testid="catalog-sharing-panel">
    <el-alert type="info" :closable="false" show-icon :title="t('catalog.sharing.boundary')" />
    <el-card v-if="eligibility.confirm" shadow="never">
      <template #header><div class="record-heading"><strong>{{ t('catalog.sharing.confirmationsTitle') }}</strong>
        <el-button data-testid="sharing-refresh-history" :loading="confirmationsLoading" @click="loadConfirmations">{{ t('catalog.common.refresh') }}</el-button>
      </div></template>
      <el-alert type="info" :closable="false" :title="t('catalog.sharing.reviewScope')" />
      <el-alert v-if="confirmationsError" type="error" :closable="false" :title="confirmationsError" />
      <el-table data-testid="sharing-confirmations" :data="confirmations" v-loading="confirmationsLoading">
        <el-table-column prop="id" :label="t('catalog.sharing.decisionID')" min-width="240" />
        <el-table-column :label="t('catalog.sharing.recipient')" min-width="130"><template #default="{ row }">{{ t(`catalog.sharing.${row.recipient_type}`) }} · {{ row.recipient_id }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.confirmedAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
        <el-table-column width="140"><template #default="{ row }"><el-button data-testid="sharing-open-confirmation" link type="primary" :disabled="saving || (!!attempt && !confirmation)" @click="openConfirmation(row.id)">{{ t('catalog.sharing.viewConfirmation') }}</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!confirmationsLoading && !confirmationsError && !confirmationTotal" :description="t('catalog.sharing.noConfirmations')" />
      <el-pagination data-testid="sharing-confirmation-pagination" layout="prev, pager, next, total" :current-page="confirmationPage" :page-size="20" :total="confirmationTotal" @current-change="changeConfirmationPage" />
    </el-card>
    <el-card v-if="eligibility.confirm" shadow="never">
      <template #header><strong>{{ t('catalog.sharing.confirmTitle') }}</strong></template>
      <el-alert v-if="!eligibility.create" type="info" :closable="false" :title="t('catalog.sharing.ownerRequired')" />
      <el-alert v-if="confirmationError" type="error" :closable="false" :title="confirmationError" />
      <el-form v-if="eligibility.create && !decisionID" label-position="top" @submit.prevent="submitConfirmation">
        <el-form-item :label="t('catalog.sharing.recipientType')" required>
          <el-select v-model="form.recipientType" :disabled="!!attempt" @change="changeRecipientType">
            <el-option v-for="type in ['user', 'project_group']" :key="type" :value="type" :label="t(`catalog.sharing.${type}`)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.sharing.recipient')" required>
          <el-select v-model="form.recipientID" filterable remote :disabled="!!attempt" :loading="recipientLoading"
            :remote-method="searchRecipients" :placeholder="t('catalog.sharing.selectRecipient')"
            @visible-change="visible => visible && searchRecipients('')">
            <el-option v-for="option in recipients" :key="option.id" :value="option.id" :label="option.name + (option.code ? ` · ${option.code}` : '')" />
          </el-select>
          <el-alert v-if="recipientError" type="error" :closable="false" :title="recipientError" />
        </el-form-item>
        <el-form-item :label="t('catalog.sharing.expiry')" required>
          <el-select v-model="form.expiryMode" :disabled="!!attempt" :placeholder="t('catalog.sharing.selectExpiry')" @change="form.expiresAt = null">
            <el-option v-for="mode in ['at_time', 'until_revoked']" :key="mode" :value="mode" :label="t(`catalog.sharing.${mode}`)" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.expiryMode === 'at_time'" :label="t('catalog.sharing.expiresAt')" required>
          <el-date-picker v-model="form.expiresAt" type="datetime" :disabled="!!attempt" />
        </el-form-item>
        <el-form-item :label="t('catalog.sharing.reason')" required>
          <el-input v-model="form.reason" type="textarea" :rows="3" maxlength="2000" show-word-limit :disabled="!!attempt" />
        </el-form-item>
        <el-button data-testid="sharing-confirm" type="primary" :loading="saving" @click="submitConfirmation">{{ t('catalog.sharing.confirm') }}</el-button>
      </el-form>
      <template v-if="attempt && !confirmation">
        <p>{{ t('catalog.sharing.uncertain') }}</p>
        <el-button data-testid="sharing-retry" :loading="saving" :disabled="!eligibility.create" @click="submitConfirmation">{{ t('catalog.sharing.retrySame') }}</el-button>
      </template>
      <div v-if="decisionID" class="record-heading">
        <span class="break-all">{{ t('catalog.sharing.decisionID') }}: {{ decisionID }}</span>
        <el-button data-testid="sharing-query-confirmation" :loading="confirmationLoading" @click="refreshConfirmation">{{ t('catalog.sharing.refreshConfirmation') }}</el-button>
        <el-button v-if="eligibility.create" data-testid="sharing-new-confirmation" :disabled="saving || (!!attempt && !confirmation)" @click="openConfirmation('')">{{ t('catalog.sharing.newConfirmation') }}</el-button>
      </div>
      <el-descriptions v-if="confirmation" :column="1" border>
        <el-descriptions-item :label="t('catalog.sharing.confirmedAt')">{{ formatTime(confirmation.created_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(confirmation.target) }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.recipient')">{{ t(`catalog.sharing.${confirmation.recipient_type}`) }} · {{ confirmation.recipient_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.expiry')">{{ expiryLabel(confirmation) }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.reason')">{{ confirmation.reason }}</el-descriptions-item>
      </el-descriptions>
      <template v-if="confirmation">
        <div class="record-heading"><strong>{{ t('catalog.sharing.resultsTitle') }}</strong>
          <el-button data-testid="sharing-refresh-results" :loading="resultsLoading" @click="loadConfirmationResults">{{ t('catalog.common.refresh') }}</el-button></div>
        <el-alert type="info" :closable="false" :title="t('catalog.sharing.issuanceBoundary')" />
        <el-alert v-if="resultsError" type="error" :closable="false" :title="resultsError" />
        <el-table data-testid="sharing-results" :data="results" v-loading="resultsLoading">
          <el-table-column prop="request_id" :label="t('catalog.sharing.requestID')" min-width="240" />
          <el-table-column :label="t('catalog.sharing.acceptance')" min-width="240"><template #default="{ row }">{{ t(`catalog.sharing.state.${row.state}`) }}</template></el-table-column>
          <el-table-column :label="t('catalog.sharing.recordedAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.recorded_at) }}</template></el-table-column>
          <el-table-column :label="t('catalog.sharing.issuance')" min-width="210"><template #default="{ row }">{{ row.granted_at ? t('catalog.sharing.issued') + ' · ' + formatTime(row.granted_at) : t('catalog.sharing.noIssuance') }}</template></el-table-column>
        </el-table>
        <el-empty v-if="!resultsLoading && !resultsError && !resultsTotal" :description="t('catalog.sharing.noResults')" />
        <el-pagination data-testid="sharing-results-pagination" layout="prev, pager, next, total" :current-page="resultsPage" :page-size="20" :total="resultsTotal" @current-change="changeResultsPage" />
      </template>
    </el-card>

    <el-card v-if="eligibility.history" shadow="never">
      <template #header><strong>{{ t('catalog.sharing.prepareTitle') }}</strong></template>
      <el-alert type="info" :closable="false" :title="t('catalog.sharing.prepareBoundary')" />
      <el-alert v-if="handlingError" type="error" :closable="false" :title="handlingError" />
      <template v-if="canPrepare && !requestID">
        <el-form label-position="top" @submit.prevent="submitRequest">
          <el-form-item :label="t('catalog.sharing.selectDecision')" required>
            <el-select v-model="selectedDecision" filterable :loading="candidateLoading" :disabled="initializing || handlingSaving || !!handlingAttempt"
              :placeholder="t('catalog.sharing.selectDecision')" @visible-change="visible => visible && loadCandidates()" @change="observeRequirement">
              <el-option v-for="option in candidates" :key="option.id" :value="option.id" :label="candidateLabel(option)" />
            </el-select>
            <el-pagination layout="prev, pager, next, total" :current-page="candidatePage" :page-size="20" :total="candidateTotal" @current-change="changeCandidatePage" />
          </el-form-item>
        </el-form>
        <el-descriptions v-if="selectedCandidate" :column="1" border>
          <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(selectedCandidate.target) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.recipient')">{{ candidateLabel(selectedCandidate) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.expiry')">{{ expiryLabel(selectedCandidate) }}</el-descriptions-item>
        </el-descriptions>
        <el-button v-if="selectedCandidate" data-testid="sharing-observe-requirement" :disabled="initializing || !!handlingAttempt" :loading="requirementLoading" @click="observeRequirement">{{ t('catalog.sharing.observeRequirement') }}</el-button>
        <p v-if="requirement">{{ t('catalog.sharing.observedVersion', { version: requirement.requirement_version }) }}</p>
        <el-form v-if="canInitialize && selectedCandidate && !requirement && !requirementLoading && !handlingAttempt" label-position="top" @submit.prevent="initializeRequirement">
          <el-alert type="info" :closable="false" :title="t('catalog.sharing.initializeBoundary')" />
          <el-form-item :label="t('catalog.sharing.initializeReason')" required>
            <el-input v-model="initializationReason" data-testid="sharing-initialize-reason" type="textarea" maxlength="2000" :disabled="initializing" />
          </el-form-item>
          <el-button data-testid="sharing-initialize" :loading="initializing" @click="initializeRequirement">{{ t('catalog.sharing.initialize') }}</el-button>
        </el-form>
        <el-button data-testid="sharing-prepare" type="primary" :disabled="!requirement || requirementLoading" :loading="handlingSaving" @click="submitRequest">{{ t('catalog.sharing.prepare') }}</el-button>
      </template>
      <el-alert v-else-if="!requestID" type="info" :closable="false" :title="t('catalog.sharing.noNewHandling')" />
      <div v-if="requestID" class="record-heading">
        <span class="break-all">{{ t('catalog.sharing.requestID') }}: {{ requestID }}</span>
        <el-button data-testid="sharing-query-request" :loading="requestLoading" @click="loadRequest(requestID)">{{ t('catalog.sharing.queryResult') }}</el-button>
      </div>
      <template v-if="handlingAttempt && !handlingSucceeded">
        <p>{{ t('catalog.sharing.handlingUncertain') }}</p>
        <el-button data-testid="sharing-retry-request" :disabled="!canPrepare" :loading="handlingSaving" @click="submitRequest">{{ t('catalog.sharing.retryRequest') }}</el-button>
      </template>
    </el-card>

    <el-card v-if="eligibility.history" shadow="never">
      <template #header>
        <div class="record-heading"><strong>{{ t('catalog.sharing.historyTitle') }}</strong>
          <el-button :loading="historyLoading" @click="loadHistory">{{ t('catalog.common.refresh') }}</el-button></div>
      </template>
      <el-alert v-if="historyError" type="error" :closable="false" :title="historyError" />
      <el-table :data="history" v-loading="historyLoading">
        <el-table-column prop="request_id" :label="t('catalog.sharing.requestID')" min-width="240" />
        <el-table-column :label="t('catalog.sharing.createdAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
        <el-table-column width="130"><template #default="{ row }"><el-button link type="primary" @click="loadRequest(row.request_id)">{{ t('catalog.sharing.queryResult') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination layout="prev, pager, next, total" :current-page="page" :page-size="20" :total="total" @current-change="changePage" />
      <el-alert v-if="requestError" type="error" :closable="false" :title="requestError" />
      <div v-if="request" v-loading="requestLoading">
        <el-alert type="info" :closable="false" :title="t(`catalog.sharing.state.${request.state}`)" />
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="t('catalog.sharing.requestID')">{{ request.request_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(request.target) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.recordedAt')">{{ formatTime(request.recorded_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.deadline')">{{ formatTime(request.deadline) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.issuance')">{{ request.granted_at ? t('catalog.sharing.issued') + ' · ' + formatTime(request.granted_at) : t('catalog.sharing.noIssuance') }}</el-descriptions-item>
        </el-descriptions>
        <el-alert type="info" :closable="false" :title="t('catalog.sharing.issuanceBoundary')" />
      </div>
    </el-card>
    <el-empty v-if="!eligibility.confirm && !eligibility.history" :description="t('catalog.sharing.noPermission')" />
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { createSharingDecision, getSharingDecision, listSharingConfirmations, listSharingConfirmationResults, listSharingRecipients, listSharingRequests, getSharingRequest, listSharingDecisions, observeSharingRequirement, prepareSharingRequest, initializeSharingRequirement } from '../api/catalog'
import { captureSharingConfirmation, captureSharingRequest, sharingEligibility, validateHandlingRequirement, serializeApprovalInitialization } from '../utils/sharingConfirmation'

const props = defineProps({ entry: { type: Object, required: true }, decisionID: { type: String, default: '' }, recordDecision: { type: Function, required: true }, requestID: { type: String, default: '' }, recordRequest: { type: Function, required: true } })
const { t, locale } = useI18n()
const auth = useAuthStore()
const eligibility = computed(() => sharingEligibility(props.entry, auth))
const canPrepare = computed(() => eligibility.value.history && props.entry?.entry_status === 'active' && props.entry?.governance_status !== 'deprecated' &&
  props.entry?.entry_type === 'data_item' && props.entry?.source?.source_module === 'meta' && props.entry?.source?.source_status === 'active')
const candidates = ref([]), candidatePage = ref(1), candidateTotal = ref(0), candidateLoading = ref(false), selectedDecision = ref('')
const selectedCandidate = computed(() => candidates.value.find(item => item.id === selectedDecision.value))
const canInitialize = computed(() => canPrepare.value && auth.hasPermission('system.engine_access_approval_requirement.initialize'))
const initializationReason = ref(''), initializing = ref(false)
const requirement = ref(null), requirementLoading = ref(false), handlingAttempt = ref(null), handlingSaving = ref(false), handlingError = ref(''), handlingSucceeded = ref(false)
const form = reactive({ recipientType: 'user', recipientID: '', expiryMode: '', expiresAt: null, reason: '' })
const recipients = ref([]), recipientLoading = ref(false), recipientError = ref('')
const attempt = ref(null), saving = ref(false), confirmation = ref(null), confirmationError = ref(''), confirmationLoading = ref(false)
const history = ref([]), page = ref(1), total = ref(0), historyLoading = ref(false), historyError = ref('')
const request = ref(null), requestLoading = ref(false), requestError = ref('')
const confirmations = ref([]), confirmationPage = ref(1), confirmationTotal = ref(0), confirmationsLoading = ref(false), confirmationsError = ref('')
const results = ref([]), resultsPage = ref(1), resultsTotal = ref(0), resultsLoading = ref(false), resultsError = ref('')
let confirmationsSequence = 0, resultsSequence = 0
let generation = 0, recipientSequence = 0, historySequence = 0, requestSequence = 0, confirmationSequence = 0, candidateSequence = 0, requirementSequence = 0
const message = error => error?.response?.data?.error || t('catalog.sharing.queryFailed')
const formatTime = value => value ? new Date(value).toLocaleString(locale.value) : '—'
const expiryLabel = row => row.expiry_mode === 'until_revoked' ? t('catalog.sharing.until_revoked') : formatTime(row.expires_at)
const targetLabel = target => (target?.segments || []).map(segment => segment.name).join(' / ')
const candidateLabel = row => `${row.recipient_name || t(`catalog.sharing.${row.recipient_type}`)} · ${row.recipient_id} · ${formatTime(row.confirmed_at)}`

async function loadCandidates() {
  if (!canPrepare.value || initializing.value || handlingAttempt.value || props.requestID) return
  const seq = ++candidateSequence, epoch = generation
  candidates.value = []; candidateTotal.value = 0; selectedDecision.value = ''; requirement.value = null; requirementSequence++
  candidateLoading.value = true; requirementLoading.value = false; handlingError.value = ''
  try {
    const result = await listSharingDecisions(props.entry.id, { page: candidatePage.value, page_size: 20 })
    if (seq === candidateSequence && epoch === generation) { candidates.value = result.data; candidateTotal.value = result.total }
  } catch (error) { if (seq === candidateSequence && epoch === generation) handlingError.value = message(error) }
  finally { if (seq === candidateSequence && epoch === generation) candidateLoading.value = false }
}
function changeCandidatePage(value) { candidatePage.value = value; loadCandidates() }

async function observeRequirement() {
  const candidate = selectedCandidate.value
  const seq = ++requirementSequence, epoch = generation
  requirement.value = null; requirementLoading.value = false; handlingError.value = ''
  if (!canPrepare.value || handlingAttempt.value || !candidate) return
  requirementLoading.value = true
  try {
    const result = await observeSharingRequirement(candidate.target)
    // Validate the lossless response before enabling an explicit command.
    validateHandlingRequirement(candidate, result)
    if (seq === requirementSequence && epoch === generation) requirement.value = result
  } catch (error) { if (seq === requirementSequence && epoch === generation) handlingError.value = error.message === 'invalidHandlingRequirement' ? t('catalog.sharing.invalidRequirement') : message(error) }
  finally { if (seq === requirementSequence && epoch === generation) requirementLoading.value = false }
}

async function initializeRequirement() {
  if (initializing.value || !canInitialize.value || requirement.value || requirementLoading.value || handlingAttempt.value || props.requestID) return
  const candidate = selectedCandidate.value
  let payload
  try { payload = serializeApprovalInitialization(candidate?.target, initializationReason.value) }
  catch { handlingError.value = t('catalog.sharing.invalidInitialization'); return }
  const epoch = generation, decision = selectedDecision.value
  initializing.value = true; handlingError.value = ''
  try {
    await initializeSharingRequirement(candidate.target, payload)
    // A configuration response is not a handling version or a Grant. Reobserve
    // the selected target through the existing qualification boundary.
    if (epoch === generation && decision === selectedDecision.value) await observeRequirement()
  } catch (error) { if (epoch === generation && decision === selectedDecision.value) handlingError.value = message(error) }
  finally { if (epoch === generation) initializing.value = false }
}

async function submitRequest() {
  if (handlingSaving.value || !canPrepare.value || handlingSucceeded.value) return
  handlingError.value = ''
  if (!handlingAttempt.value) {
    try { handlingAttempt.value = captureSharingRequest(selectedCandidate.value, requirement.value, crypto.randomUUID()) }
    catch { handlingError.value = t('catalog.sharing.invalidRequirement'); return }
  }
  const epoch = generation
  handlingSaving.value = true
  try {
    await props.recordRequest(handlingAttempt.value.request_id)
    if (epoch !== generation) return
    const result = await prepareSharingRequest(props.entry.id, handlingAttempt.value)
    if (epoch !== generation) return
    if (result.request_id !== handlingAttempt.value.request_id || !['pending', 'accepted', 'closed'].includes(result.state)) throw new Error('invalidResult')
    handlingSucceeded.value = result.state !== 'pending'
    await loadHistory()
    if (epoch === generation) await loadRequest(handlingAttempt.value.request_id)
  } catch (error) { if (epoch === generation) handlingError.value = message(error) }
  finally { if (epoch === generation) handlingSaving.value = false }
}

async function searchRecipients(search) {
  if (!eligibility.value.create || attempt.value) return
  const seq = ++recipientSequence, epoch = generation
  recipientLoading.value = true; recipientError.value = ''; recipients.value = []
  try {
    const result = await listSharingRecipients(props.entry.id, { recipient_type: form.recipientType, search, page: 1, page_size: 50 })
    if (seq === recipientSequence && epoch === generation) recipients.value = result.data
  } catch (error) { if (seq === recipientSequence && epoch === generation) recipientError.value = message(error) }
  finally { if (seq === recipientSequence && epoch === generation) recipientLoading.value = false }
}
function changeRecipientType() { form.recipientID = ''; searchRecipients('') }

async function submitConfirmation() {
  if (saving.value || !eligibility.value.create) return
  confirmationError.value = ''
  if (!attempt.value) {
    try { attempt.value = captureSharingConfirmation(props.entry, form, recipients.value, crypto.randomUUID()) }
    catch { confirmationError.value = t('catalog.sharing.invalidConfirmation'); return }
  }
  const epoch = generation
  saving.value = true
  try {
    // Persist only the opaque ID in the public route before sending. A reload
    // uses GET, never reconstructs a command from browser history or storage.
    await props.recordDecision(attempt.value.decision_id)
    if (epoch !== generation) return
    const result = await createSharingDecision(props.entry.id, attempt.value)
    if (epoch === generation) { confirmation.value = result; loadConfirmations(); loadConfirmationResults() }
  } catch (error) { if (epoch === generation) confirmationError.value = message(error) }
  finally { if (epoch === generation) saving.value = false }
}

async function refreshConfirmation() {
  if (!props.decisionID || !eligibility.value.confirm) return
  const seq = ++confirmationSequence, epoch = generation
  confirmation.value = null; confirmationError.value = ''; confirmationLoading.value = true
  clearConfirmationResults(); resultsPage.value = 1
  try {
    const result = await getSharingDecision(props.entry.id, props.decisionID)
    if (result.id !== props.decisionID) throw new Error('invalid confirmation response')
    if (seq === confirmationSequence && epoch === generation) { confirmation.value = result; loadConfirmationResults() }
  } catch (error) { if (seq === confirmationSequence && epoch === generation) confirmationError.value = message(error) }
  finally { if (seq === confirmationSequence && epoch === generation) confirmationLoading.value = false }
}

async function loadConfirmations() {
  if (!eligibility.value.confirm) return
  const seq = ++confirmationsSequence, epoch = generation
  confirmations.value = []; confirmationTotal.value = 0; confirmationsError.value = ''; confirmationsLoading.value = true
  try {
    const result = await listSharingConfirmations(props.entry.id, { page: confirmationPage.value, page_size: 20 })
    if (seq === confirmationsSequence && epoch === generation) { confirmations.value = result.data; confirmationTotal.value = result.total }
  } catch (error) { if (seq === confirmationsSequence && epoch === generation) confirmationsError.value = message(error) }
  finally { if (seq === confirmationsSequence && epoch === generation) confirmationsLoading.value = false }
}
function changeConfirmationPage(value) { confirmationPage.value = value; loadConfirmations() }
async function openConfirmation(id) {
  if (saving.value || (attempt.value && !confirmation.value)) return
  if (id && id === props.decisionID) { await refreshConfirmation(); return }
  await props.recordDecision(id)
}
function clearConfirmationResults() {
  resultsSequence++; results.value = []; resultsTotal.value = 0; resultsLoading.value = false; resultsError.value = ''
}
async function loadConfirmationResults() {
  if (!eligibility.value.confirm || !confirmation.value || confirmation.value.id !== props.decisionID) return
  const id = props.decisionID, seq = ++resultsSequence, epoch = generation
  results.value = []; resultsTotal.value = 0; resultsError.value = ''; resultsLoading.value = true
  try {
    const result = await listSharingConfirmationResults(props.entry.id, id, { page: resultsPage.value, page_size: 20 })
    if (result.data.some(row => row.decision_id !== id || !['pending', 'accepted', 'closed'].includes(row.state))) throw new Error('invalid result binding')
    if (seq === resultsSequence && epoch === generation) { results.value = result.data; resultsTotal.value = result.total }
  } catch (error) { if (seq === resultsSequence && epoch === generation) resultsError.value = message(error) }
  finally { if (seq === resultsSequence && epoch === generation) resultsLoading.value = false }
}
function changeResultsPage(value) { resultsPage.value = value; loadConfirmationResults() }

async function loadHistory() {
  if (!eligibility.value.history) return
  const seq = ++historySequence, epoch = generation
  history.value = []; total.value = 0; request.value = null; historyError.value = ''; historyLoading.value = true
  requestSequence++; requestError.value = ''; requestLoading.value = false
  try {
    const result = await listSharingRequests(props.entry.id, { page: page.value, page_size: 20 })
    if (seq === historySequence && epoch === generation) { history.value = result.data; total.value = result.total }
  } catch (error) { if (seq === historySequence && epoch === generation) historyError.value = message(error) }
  finally { if (seq === historySequence && epoch === generation) historyLoading.value = false }
}
function changePage(value) { page.value = value; loadHistory() }

async function loadRequest(id) {
  if (!eligibility.value.history) return
  const seq = ++requestSequence, epoch = generation
  request.value = null; requestError.value = ''; requestLoading.value = true
  try {
    const result = await getSharingRequest(props.entry.id, id)
    if (result.request_id !== id || !['pending', 'accepted', 'closed'].includes(result.state)) throw new Error('invalidResult')
    if (seq === requestSequence && epoch === generation) {
      request.value = result
      if (handlingAttempt.value?.request_id === id && result.state !== 'pending') handlingSucceeded.value = true
    }
  } catch (error) { if (seq === requestSequence && epoch === generation) requestError.value = message(error) }
  finally { if (seq === requestSequence && epoch === generation) requestLoading.value = false }
}

watch(() => [props.entry.id, auth.authContext?.principal?.id, auth.authContext?.context?.tenant_id, auth.authContext?.context?.tenant_membership_id, auth.authContext?.authorization?.authorization_version, eligibility.value.owner, eligibility.value.confirm, eligibility.value.create, eligibility.value.history, canPrepare.value, canInitialize.value], () => {
  generation++; attempt.value = null; confirmation.value = null; request.value = null; recipients.value = []; history.value = []; total.value = 0
  confirmations.value = []; confirmationTotal.value = 0; confirmationPage.value = 1; confirmationsError.value = ''; confirmationsLoading.value = false
  clearConfirmationResults(); resultsPage.value = 1
  recipientLoading.value = saving.value = confirmationLoading.value = requestLoading.value = historyLoading.value = false
  confirmationError.value = recipientError.value = requestError.value = historyError.value = ''
  candidates.value = []; candidateTotal.value = 0; candidatePage.value = 1; selectedDecision.value = ''; requirement.value = null; handlingAttempt.value = null
  candidateLoading.value = requirementLoading.value = handlingSaving.value = handlingSucceeded.value = false; handlingError.value = ''
  initializing.value = false; initializationReason.value = ''
  page.value = 1
  if (eligibility.value.history) loadHistory()
  if (eligibility.value.confirm) loadConfirmations()
  if (eligibility.value.confirm && props.decisionID) refreshConfirmation()
  if (eligibility.value.history && props.requestID) loadRequest(props.requestID)
}, { immediate: true })
watch(() => props.decisionID, value => {
  if (saving.value) return
  confirmationSequence++; confirmation.value = null; confirmationError.value = ''; confirmationLoading.value = false; clearConfirmationResults()
  if (attempt.value && value !== attempt.value.decision_id) attempt.value = null
  if (value) refreshConfirmation()
})
watch(() => props.requestID, value => {
  if (handlingSaving.value) return
  if (handlingAttempt.value && value !== handlingAttempt.value.request_id) {
    handlingAttempt.value = null; handlingSucceeded.value = false; requirement.value = null; selectedDecision.value = ''
    handlingError.value = ''; requirementSequence++
  }
  if (value) loadRequest(value)
})
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.sharing-panel { display: grid; gap: 16px; }
.sharing-panel .el-select, .sharing-panel :deep(.el-date-editor) { width: 100%; }
.sharing-panel .el-alert, .sharing-panel .el-pagination { margin-bottom: 12px; }
.record-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-bottom: 12px; }
.break-all { overflow-wrap: anywhere; }
</style>
