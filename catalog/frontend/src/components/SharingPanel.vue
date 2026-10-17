<template>
  <section class="sharing-panel" data-testid="catalog-sharing-panel">
    <div v-if="eligibility.confirm || eligibility.history" class="workspace-toolbar">
      <el-radio-group :model-value="workspace" :disabled="switchingBlocked" data-testid="sharing-workspace" :aria-label="t('catalog.sharing.title')" @change="changeWorkspace">
        <el-radio-button value="create" :disabled="!eligibility.create" :title="!eligibility.create ? t('catalog.sharing.ownerRequired') : undefined">{{ t('catalog.sharing.workspace.create') }}</el-radio-button>
        <el-radio-button value="handling" :disabled="!eligibility.history" :title="!eligibility.history ? t('catalog.sharing.noPermission') : undefined">{{ t('catalog.sharing.workspace.handling') }}</el-radio-button>
        <el-radio-button value="records">{{ t('catalog.sharing.workspace.records') }}</el-radio-button>
      </el-radio-group>
      <el-button v-if="systemAuthorizationRoute" class="system-authorization-link" data-testid="sharing-open-system" :disabled="switchingBlocked" @click="openSystemAuthorization">{{ t('catalog.sharing.openSystem') }}</el-button>
    </div>
    <p v-if="eligibility.confirm || eligibility.history" class="sharing-help workspace-help">{{ t(`catalog.sharing.workspaceHelp.${workspace}`) }}</p>
    <el-alert v-if="switchingBlocked" type="warning" :closable="false" :title="t('catalog.sharing.resolveBeforeSwitch')" />
    <div v-if="workspace === 'records' && detailOpen" class="record-heading">
      <el-button data-testid="sharing-back-records" :disabled="switchingBlocked" @click="backToRecords">{{ t('catalog.sharing.backRecords') }}</el-button>
      <strong>{{ t(recordKind === 'confirmations' ? 'catalog.sharing.confirmationDetail' : 'catalog.sharing.requestDetail') }}</strong>
    </div>
    <el-card v-if="(eligibility.confirm || eligibility.history) && ((workspace === 'records' && detailOpen) || (workspace === 'handling' && requestID))" shadow="never" data-testid="sharing-progress">
      <template #header><strong>{{ t('catalog.sharing.progressTitle') }}</strong></template>
      <el-steps v-if="progress.step >= 0" :active="progress.step" :process-status="['duplicate', 'expired'].includes(progress.status) ? 'error' : 'process'" finish-status="success" align-center>
        <el-step v-for="phase in ['confirmation', 'handling', 'issuance']" :key="phase" :title="t(`catalog.sharing.phases.${phase}`)" />
      </el-steps>
      <p data-testid="sharing-progress-status" role="status" aria-live="polite">{{ t(`catalog.sharing.progress.${progress.status}`) }}</p>
      <p class="sharing-help">{{ t('catalog.sharing.nextStep') }}{{ t(`catalog.sharing.next.${progress.next}`) }}</p>
      <el-button v-if="workspace === 'records' && recordKind === 'confirmations' && confirmation && canPrepare && !requestID" data-testid="sharing-continue-handling" type="primary" :disabled="switchingBlocked" @click="changeWorkspace('handling')">{{ t('catalog.sharing.continueHandling') }}</el-button>
    </el-card>
    <div v-if="workspace === 'records' && !detailOpen && (eligibility.confirm || eligibility.history)" class="record-filter">
      <span>{{ t('catalog.sharing.recordType') }}</span>
      <el-select :model-value="recordKind" :aria-label="t('catalog.sharing.recordType')" data-testid="sharing-record-kind" @change="changeRecordKind">
        <el-option v-if="eligibility.confirm" value="confirmations" :label="t('catalog.sharing.confirmationsTitle')" />
        <el-option v-if="eligibility.history" value="requests" :label="t('catalog.sharing.historyTitle')" />
      </el-select>
    </div>
    <el-card v-if="workspace === 'records' && !detailOpen && recordKind === 'confirmations' && eligibility.confirm" shadow="never">
      <template #header><div class="record-heading"><strong>{{ t('catalog.sharing.confirmationsTitle') }}</strong>
        <el-button data-testid="sharing-refresh-history" :loading="confirmationsLoading" @click="loadConfirmations">{{ t('catalog.common.refresh') }}</el-button>
      </div></template>
      <el-alert type="info" :closable="false" :title="t('catalog.sharing.reviewScope')" />
      <el-alert v-if="confirmationsError" type="error" :closable="false" :title="confirmationsError" />
      <el-table data-testid="sharing-confirmations" :data="confirmations" v-loading="confirmationsLoading">
        <el-table-column :label="t('catalog.sharing.recipient')" min-width="230" show-overflow-tooltip><template #default="{ row }">{{ recipientLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.confirmedAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
        <el-table-column prop="reason" :label="t('catalog.sharing.reason')" min-width="160" show-overflow-tooltip />
        <el-table-column width="140"><template #default="{ row }"><el-button data-testid="sharing-open-confirmation" link type="primary" :disabled="switchingBlocked" @click="openConfirmation(row.id)">{{ t('catalog.sharing.viewConfirmation') }}</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!confirmationsLoading && !confirmationsError && !confirmationTotal" :description="t('catalog.sharing.noConfirmations')" />
      <el-pagination data-testid="sharing-confirmation-pagination" layout="prev, pager, next, total" :current-page="confirmationPage" :page-size="20" :total="confirmationTotal" @current-change="changeConfirmationPage" />
    </el-card>
    <el-card v-if="workspace === 'create' && eligibility.confirm" shadow="never" data-testid="sharing-create-panel">
      <template #header><strong>{{ t('catalog.sharing.confirmTitle') }}</strong></template>
      <el-alert v-if="!eligibility.create" type="info" :closable="false" :title="t('catalog.sharing.ownerRequired')" />
      <el-alert v-if="confirmationError" type="error" :closable="false" :title="confirmationError" />
      <el-form v-if="eligibility.create && !decisionID" class="confirmation-form" label-position="top" @submit.prevent="submitConfirmation">
        <el-form-item :label="t('catalog.sharing.recipientType')" required>
          <el-select v-model="form.recipientType" :disabled="!!attempt" @change="changeRecipientType">
            <el-option v-for="type in ['user', 'project_group']" :key="type" :value="type" :label="t(`catalog.sharing.${type}`)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.sharing.recipient')" required>
          <el-select v-model="form.recipientID" filterable remote :disabled="!!attempt" :loading="recipientLoading"
            :remote-method="searchRecipients" :placeholder="t('catalog.sharing.selectRecipient')"
            @visible-change="visible => visible && searchRecipients('')">
            <el-option v-for="option in recipients" :key="option.id" :value="option.id" :label="recipientOptionLabel(option)">
              <div class="approval-option"><span>{{ recipientOptionLabel(option) }}</span><span v-if="option.approvals?.length" class="approval-status">{{ t('catalog.sharing.alreadyApproved') }}</span></div>
            </el-option>
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
        <el-form-item v-if="!matchingApproval" class="form-full" :label="t('catalog.sharing.reason')" required>
          <el-input v-model="form.reason" type="textarea" :rows="3" maxlength="2000" show-word-limit :disabled="!!attempt" />
        </el-form-item>
        <div v-if="matchingApproval" class="form-full">
          <el-alert data-testid="sharing-already-approved" type="success" :closable="false" :title="t('catalog.sharing.matchingApproval')" />
          <el-button data-testid="sharing-use-approval" :disabled="saving" @click="useExistingApproval(false)">{{ t('catalog.sharing.viewConfirmation') }}</el-button>
          <el-button v-if="canPrepare" data-testid="sharing-use-approval-handle" type="primary" :disabled="saving" @click="useExistingApproval(true)">{{ t('catalog.sharing.continueHandling') }}</el-button>
        </div>
        <div v-else class="form-full"><el-button data-testid="sharing-confirm" type="primary" :loading="saving" @click="submitConfirmation">{{ t('catalog.sharing.confirm') }}</el-button></div>
      </el-form>
      <template v-if="attempt && !confirmation">
        <p>{{ t('catalog.sharing.uncertain') }}</p>
        <el-button data-testid="sharing-retry" :loading="saving" :disabled="!eligibility.create" @click="submitConfirmation">{{ t('catalog.sharing.retrySame') }}</el-button>
      </template>
      <el-button v-if="decisionID" data-testid="sharing-query-confirmation" :loading="confirmationLoading" @click="refreshConfirmation">{{ t('catalog.sharing.refreshConfirmation') }}</el-button>
      <el-button v-if="confirmation" data-testid="sharing-created-detail" @click="showConfirmationDetail">{{ t('catalog.sharing.viewConfirmation') }}</el-button>
    </el-card>
    <el-card v-if="workspace === 'records' && detailOpen && recordKind === 'confirmations' && eligibility.confirm" shadow="never" data-testid="sharing-confirmation-detail">
      <el-alert v-if="confirmationError" type="error" :closable="false" :title="confirmationError" />
      <div v-if="decisionID" class="record-heading">
        <span class="break-all">{{ t('catalog.sharing.decisionID') }}: {{ decisionID }}</span>
        <el-button data-testid="sharing-query-confirmation" :loading="confirmationLoading" @click="refreshConfirmation">{{ t('catalog.sharing.refreshConfirmation') }}</el-button>
      </div>
      <el-descriptions v-if="confirmation" :column="1" border>
        <el-descriptions-item :label="t('catalog.sharing.confirmedAt')">{{ formatTime(confirmation.created_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(confirmation.target) }}</el-descriptions-item>
        <el-descriptions-item :label="t('catalog.sharing.recipient')">{{ recipientLabel(confirmation) }}</el-descriptions-item>
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
          <el-table-column :label="t('catalog.sharing.acceptance')" min-width="140"><template #default="{ row }"><el-tag :type="stateType(row.state)">{{ t(`catalog.sharing.stateLabel.${row.state}`) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('catalog.sharing.recordedAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.recorded_at) }}</template></el-table-column>
          <el-table-column :label="t('catalog.sharing.issuance')" min-width="210"><template #default="{ row }">{{ issuanceLabel(row) }}</template></el-table-column>
        </el-table>
        <el-empty v-if="!resultsLoading && !resultsError && !resultsTotal" :description="t('catalog.sharing.noResults')" />
        <el-pagination data-testid="sharing-results-pagination" layout="prev, pager, next, total" :current-page="resultsPage" :page-size="20" :total="resultsTotal" @current-change="changeResultsPage" />
      </template>
    </el-card>

    <el-card v-if="workspace === 'handling' && eligibility.history" shadow="never" data-testid="sharing-handling-panel">
      <template #header><strong>{{ t('catalog.sharing.prepareTitle') }}</strong></template>
      <el-alert type="info" :closable="false" :title="t('catalog.sharing.prepareBoundary')" />
      <p class="sharing-help">{{ t('catalog.sharing.handlerBoundary') }}</p>
      <el-alert v-if="handlingError" type="error" :closable="false" :title="handlingError" />
      <el-alert v-if="confirmationError && decisionID" type="error" :closable="false" :title="confirmationError" />
      <template v-if="canPrepare && !requestID">
        <el-form label-position="top" @submit.prevent="submitRequest">
          <el-form-item :label="t('catalog.sharing.selectDecision')" required>
            <el-select v-model="selectedDecision" filterable :loading="candidateLoading" :disabled="initializing || handlingSaving || !!handlingAttempt"
              data-testid="sharing-decision-select"
              :placeholder="t('catalog.sharing.selectDecision')" @visible-change="visible => visible && loadCandidates()" @change="observeRequirement">
              <el-option v-for="option in candidates" :key="option.id" :value="option.id" :label="candidateLabel(option)">
                <div class="approval-option"><span>{{ candidateLabel(option) }}</span><span :class="['approval-status', { 'approval-status-pending': candidateGrantStatuses[option.id] !== 'existing' }]">{{ candidateStatusLabel(option) }}</span></div>
              </el-option>
            </el-select>
            <el-pagination layout="prev, pager, next, total" :current-page="candidatePage" :page-size="20" :total="candidateTotal" @current-change="changeCandidatePage" />
          </el-form-item>
        </el-form>
        <p v-if="selectedDecision && selectedDecision === decisionID" class="sharing-help" data-testid="sharing-current-selected">{{ t('catalog.sharing.currentSelected') }}</p>
        <el-descriptions v-if="selectedCandidate" :column="1" border>
          <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(selectedCandidate.target) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.recipient')">{{ candidateLabel(selectedCandidate) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.expiry')">{{ expiryLabel(selectedCandidate) }}</el-descriptions-item>
        </el-descriptions>
        <p v-if="grantObservationLoading" role="status">{{ t('catalog.sharing.checkingExistingGrant') }}</p>
        <el-alert v-if="existingGrant" data-testid="sharing-existing-grant" type="warning" :closable="false" :title="t('catalog.sharing.existingGrant')"
          :description="t('catalog.sharing.existingGrantDetail', { expiry: expiryLabel(existingGrant) })" />
        <el-alert v-if="grantObservationFailed" data-testid="sharing-grant-check-unavailable" type="info" :closable="false" :title="t('catalog.sharing.grantCheckUnavailable')" />
        <el-button v-if="selectedCandidate" data-testid="sharing-observe-requirement" :disabled="initializing || !!handlingAttempt || handlingSaving" :loading="requirementLoading" @click="observeRequirement">{{ t('catalog.sharing.observeRequirement') }}</el-button>
        <el-form v-if="canInitialize && selectedCandidate && requirementMissing && !requirementLoading && !handlingAttempt" class="initialization-form" label-position="top" @submit.prevent="initializeRequirement">
          <h4>{{ t('catalog.sharing.setupTitle') }}</h4>
          <el-alert type="info" :closable="false" :title="t('catalog.sharing.initializeBoundary')" />
          <el-form-item :label="t('catalog.sharing.initializeReason')" required>
            <el-input v-model="initializationReason" data-testid="sharing-initialize-reason" type="textarea" maxlength="2000" :disabled="initializing" />
          </el-form-item>
          <el-button data-testid="sharing-initialize" :loading="initializing" @click="initializeRequirement">{{ t('catalog.sharing.initialize') }}</el-button>
        </el-form>
        <el-button data-testid="sharing-prepare" type="primary" :disabled="!requirement || requirementLoading || grantObservationLoading || !!existingGrant" :loading="handlingSaving" @click="submitRequest">{{ t('catalog.sharing.prepare') }}</el-button>
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

    <el-card v-if="workspace === 'records' && !detailOpen && recordKind === 'requests' && eligibility.history" shadow="never" data-testid="sharing-request-records">
      <template #header>
        <div class="record-heading"><strong>{{ t('catalog.sharing.historyTitle') }}</strong>
          <el-button :loading="historyLoading" @click="loadHistory">{{ t('catalog.common.refresh') }}</el-button></div>
      </template>
      <el-alert v-if="historyError" type="error" :closable="false" :title="historyError" />
      <el-table :data="history" v-loading="historyLoading">
        <el-table-column prop="request_id" :label="t('catalog.sharing.requestID')" min-width="240" />
        <el-table-column :label="t('catalog.sharing.createdAt')" min-width="170"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column :label="t('catalog.sharing.expiry')" min-width="180"><template #default="{ row }">{{ expiryLabel(row) }}</template></el-table-column>
        <el-table-column width="130"><template #default="{ row }"><el-button data-testid="sharing-open-request" link type="primary" @click="openRequest(row)">{{ t('catalog.sharing.queryResult') }}</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!historyLoading && !historyError && !total" :description="t('catalog.sharing.noRequests')" />
      <el-pagination layout="prev, pager, next, total" :current-page="page" :page-size="20" :total="total" @current-change="changePage" />
    </el-card>
    <el-card v-if="eligibility.history && ((workspace === 'handling' && requestID) || (workspace === 'records' && detailOpen && recordKind === 'requests'))" shadow="never" data-testid="sharing-request-detail">
      <template #header><div class="record-heading"><strong>{{ t('catalog.sharing.requestDetail') }}</strong>
        <el-button data-testid="sharing-refresh-request" :loading="requestLoading" @click="loadRequest(requestID)">{{ t('catalog.common.refresh') }}</el-button>
      </div></template>
      <el-alert v-if="requestError" type="error" :closable="false" :title="requestError" />
      <div v-if="request" v-loading="requestLoading">
        <el-alert type="info" :closable="false" :title="t(`catalog.sharing.state.${request.state}`)" />
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="t('catalog.sharing.requestID')">{{ request.request_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.decisionID')">{{ request.decision_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.target')">{{ targetLabel(request.target) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.recordedAt')">{{ formatTime(request.recorded_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.deadline')">{{ formatTime(request.deadline) }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.sharing.issuance')">{{ issuanceLabel(request) }}</el-descriptions-item>
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
import { createSharingDecision, getSharingDecision, listSharingConfirmations, listSharingConfirmationResults, listSharingRecipients, listSharingRequests, getSharingRequest, listSharingDecisions, observeSharingRequirement, findExistingSharingGrant, observeSharingCandidateGrants, prepareSharingRequest, initializeSharingRequirement } from '../api/catalog'
import { captureSharingConfirmation, captureSharingRequest, sharingEligibility, sharingProgress, validateHandlingRequirement, matchingSharingApproval } from '../utils/sharingConfirmation'
import { openConsoleRoute, serializeEngineApprovalInitialization } from '@common-ui'
import { getSharingBoundary } from '../api/catalog'
import { sharingRecipientLabel, sharingRecipientOptionLabel } from '../utils/sharingRecipientPresentation'

const props = defineProps({ entry: { type: Object, required: true }, decisionID: { type: String, default: '' }, recordDecision: { type: Function, required: true }, requestID: { type: String, default: '' }, recordRequest: { type: Function, required: true } })
const { t, locale } = useI18n()
const auth = useAuthStore()
const eligibility = computed(() => sharingEligibility(props.entry, auth))
const workspace = ref('records'), recordKind = ref('confirmations'), detailOpen = ref(false)
const systemAuthorizationRoute = computed(() => {
  const engine = props.entry.source_engine
  return engine?.status === 'current' && typeof engine.id === 'string' && /^[1-9]\d*$/.test(engine.id) &&
    auth.hasPermission('system.engine.read') && auth.hasPermission('system.engine_access_grant.read')
    ? `/system/engines/${engine.id}?tab=data-authorization` : ''
})
const canPrepare = computed(() => eligibility.value.history && props.entry?.entry_status === 'active' && props.entry?.governance_status !== 'deprecated' &&
  props.entry?.entry_type === 'data_item' && props.entry?.source?.source_module === 'meta' && props.entry?.source?.source_status === 'active')
const candidates = ref([]), candidatePage = ref(1), candidateTotal = ref(0), candidateLoading = ref(false), selectedDecision = ref('')
const candidateGrantStatuses = ref({})
const selectedCandidate = computed(() => candidates.value.find(item => item.id === selectedDecision.value))
const canInitialize = computed(() => canPrepare.value && auth.hasPermission('system.engine_access_approval_requirement.initialize'))
const initializationReason = ref(''), initializing = ref(false)
const requirement = ref(null), requirementMissing = ref(false), requirementLoading = ref(false), handlingAttempt = ref(null), handlingSaving = ref(false), handlingError = ref(''), handlingSucceeded = ref(false)
const canReadGrants = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const existingGrant = ref(null), grantObservationLoading = ref(false), grantObservationFailed = ref(false)
const form = reactive({ recipientType: 'user', recipientID: '', expiryMode: '', expiresAt: null, reason: '' })
const recipients = ref([]), recipientLoading = ref(false), recipientError = ref('')
const matchingApproval = computed(() => matchingSharingApproval(recipients.value.find(row => row.id === form.recipientID), form))
const attempt = ref(null), saving = ref(false), confirmation = ref(null), confirmationError = ref(''), confirmationLoading = ref(false)
const history = ref([]), page = ref(1), total = ref(0), historyLoading = ref(false), historyError = ref('')
const request = ref(null), requestLoading = ref(false), requestError = ref('')
const confirmations = ref([]), confirmationPage = ref(1), confirmationTotal = ref(0), confirmationsLoading = ref(false), confirmationsError = ref('')
const results = ref([]), resultsPage = ref(1), resultsTotal = ref(0), resultsLoading = ref(false), resultsError = ref('')
const switchingBlocked = computed(() => saving.value || handlingSaving.value || initializing.value ||
  (!!attempt.value && !confirmation.value) || (!!handlingAttempt.value && !handlingSucceeded.value))
const progress = computed(() => sharingProgress({ decisionID: props.decisionID, requestID: props.requestID,
  confirmation: confirmation.value, request: request.value, candidate: selectedCandidate.value,
  loading: props.requestID && eligibility.value.history ? requestLoading.value : props.decisionID ? confirmationLoading.value || resultsLoading.value || candidateLoading.value : false,
  error: props.requestID && eligibility.value.history ? !!requestError.value : props.decisionID ? !!confirmationError.value || !!resultsError.value : false,
  results: results.value, resultsPage: resultsPage.value, canCreate: eligibility.value.create,
  canHandle: selectedCandidate.value?.id === props.decisionID }))
let confirmationsSequence = 0, resultsSequence = 0, grantObservationSequence = 0, candidateGrantSequence = 0
let generation = 0, recipientSequence = 0, historySequence = 0, requestSequence = 0, confirmationSequence = 0, candidateSequence = 0, requirementSequence = 0
const message = error => error?.response?.data?.error || t('catalog.sharing.queryFailed')
const formatTime = value => value ? new Date(value).toLocaleString(locale.value) : '—'
const expiryLabel = row => row.expiry_mode === 'until_revoked' ? t('catalog.sharing.until_revoked') : formatTime(row.expires_at)
const issuanceLabel = row => row.granted_at ? t('catalog.sharing.issued') + ' · ' + formatTime(row.granted_at)
  : row.unissued_reason ? t(`catalog.sharing.unissued.${row.unissued_reason}`) : t('catalog.sharing.noIssuance')
const targetLabel = target => (target?.segments || []).map(segment => segment.name).join(' / ')
const recipientLabel = row => sharingRecipientLabel(row, auth.authContext, t)
const recipientOptionLabel = option => sharingRecipientOptionLabel(option, form.recipientType, auth.authContext, t)
const candidateLabel = row => `${recipientLabel(row)} · ${expiryLabel(row)}`
const candidateStatusLabel = row => t(`catalog.sharing.candidateState.${candidateGrantStatuses.value[row.id] || 'unknown'}`)
const stateType = state => ({ accepted: 'success', pending: 'warning', closed: 'info' }[state])
function openSystemAuthorization() {
  if (systemAuthorizationRoute.value && !switchingBlocked.value) return openConsoleRoute(systemAuthorizationRoute.value)
}

async function changeWorkspace(value) {
  if (switchingBlocked.value || !['create', 'handling', 'records'].includes(value) ||
    (value === 'create' && !eligibility.value.create) || (value === 'handling' && !eligibility.value.history)) return
  const epoch = generation
  if (value === 'create') {
    const resetDraft = !!props.decisionID
    await props.recordDecision('')
    if (epoch !== generation) return
    if (resetDraft) Object.assign(form, { recipientType: 'user', recipientID: '', expiryMode: '', expiresAt: null, reason: '' })
    attempt.value = null
  } else if (value === 'handling' && props.requestID) {
    await props.recordDecision(props.decisionID)
    if (epoch !== generation) return
  }
  workspace.value = value; detailOpen.value = false
  if (value === 'handling') loadCandidates()
}
function changeRecordKind(value) {
  if (switchingBlocked.value || (value === 'confirmations' ? !eligibility.value.confirm : value !== 'requests' || !eligibility.value.history)) return
  recordKind.value = value; detailOpen.value = false
}
function showConfirmationDetail() {
  workspace.value = 'records'; recordKind.value = 'confirmations'; detailOpen.value = true
}
async function backToRecords() {
  if (switchingBlocked.value) return
  detailOpen.value = false
  await props.recordDecision('')
}
async function openRequest(row) {
  if (switchingBlocked.value || !eligibility.value.history) return
  workspace.value = 'records'; recordKind.value = 'requests'; detailOpen.value = true
  if (row.request_id === props.requestID) await loadRequest(row.request_id)
  else await props.recordRequest(row.request_id, row.decision_id || '')
}

async function loadCandidates() {
  if (!canPrepare.value || initializing.value || handlingAttempt.value || props.requestID) return
  const seq = ++candidateSequence, epoch = generation
  const preferred = selectedDecision.value || props.decisionID
  const originalDecision = props.decisionID
  candidates.value = []; candidateTotal.value = 0; selectedDecision.value = ''; requirement.value = null; requirementMissing.value = false; requirementSequence++
  candidateGrantSequence++; candidateGrantStatuses.value = {}
  clearGrantObservation()
  candidateLoading.value = true; requirementLoading.value = false; handlingError.value = ''
  try {
    const result = await listSharingDecisions(props.entry.id, { page: candidatePage.value, page_size: 20 })
    if (seq === candidateSequence && epoch === generation && originalDecision === props.decisionID) {
      candidates.value = result.data; candidateTotal.value = result.total
      loadCandidateGrantStatuses(result.data)
      if (result.data.some(row => row.id === preferred)) {
        selectedDecision.value = preferred
        await observeRequirement()
      }
    }
  } catch (error) { if (seq === candidateSequence && epoch === generation) handlingError.value = message(error) }
  finally { if (seq === candidateSequence && epoch === generation) candidateLoading.value = false }
}
function changeCandidatePage(value) { candidatePage.value = value; loadCandidates() }

async function loadCandidateGrantStatuses(rows) {
  const seq = ++candidateGrantSequence, epoch = generation
  candidateGrantStatuses.value = {}
  if (!canReadGrants.value || !rows.length) return
  try {
    const statuses = await observeSharingCandidateGrants(rows)
    if (seq === candidateGrantSequence && epoch === generation) candidateGrantStatuses.value = statuses
  } catch { /* Unknown never becomes a claim that no Grant exists. */ }
}

async function observeRequirement() {
  const candidate = selectedCandidate.value
  const seq = ++requirementSequence, epoch = generation
  requirement.value = null; requirementMissing.value = false; requirementLoading.value = false; handlingError.value = ''
  clearGrantObservation()
  if (!canPrepare.value || handlingAttempt.value || !candidate) return
  requirementLoading.value = true
  try {
    const result = await observeSharingRequirement(candidate.target)
    // Validate the lossless response before enabling an explicit command.
    validateHandlingRequirement(candidate, result)
    if (seq === requirementSequence && epoch === generation) {
      requirement.value = result
      await observeExistingGrant(candidate)
    }
  } catch (error) {
    if (seq === requirementSequence && epoch === generation) {
      requirementMissing.value = error?.response?.status === 404 && error?.response?.data?.error_code === 'approval_requirement_not_configured'
      handlingError.value = error.message === 'invalidHandlingRequirement' ? t('catalog.sharing.invalidRequirement') : message(error)
    }
  }
  finally { if (seq === requirementSequence && epoch === generation) requirementLoading.value = false }
}

function clearGrantObservation() {
  grantObservationSequence++; existingGrant.value = null; grantObservationLoading.value = false; grantObservationFailed.value = false
}
async function observeExistingGrant(candidate) {
  clearGrantObservation()
  if (!canReadGrants.value) return true
  const seq = grantObservationSequence, epoch = generation
  grantObservationLoading.value = true
  try {
    const result = await findExistingSharingGrant(candidate)
    if (seq !== grantObservationSequence || epoch !== generation || candidate !== selectedCandidate.value) return false
    existingGrant.value = result
    return !result
  } catch {
    if (seq !== grantObservationSequence || epoch !== generation || candidate !== selectedCandidate.value) return false
    // Preflight failure is not an absence claim or a new permission gate.
    grantObservationFailed.value = true
    return true
  } finally {
    if (seq === grantObservationSequence && epoch === generation) grantObservationLoading.value = false
  }
}

async function initializeRequirement() {
  if (initializing.value || !canInitialize.value || !requirementMissing.value || requirement.value || requirementLoading.value || handlingAttempt.value || props.requestID) return
  const candidate = selectedCandidate.value
  let payload
  try { payload = serializeEngineApprovalInitialization(candidate?.target, 'catalog', initializationReason.value,
    { item_fingerprint: candidate?.item_fingerprint, boundary_hash: candidate?.boundary_hash, native_target: candidate?.target }) }
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
  if (handlingSaving.value || !canPrepare.value || handlingSucceeded.value ||
      (!handlingAttempt.value && (requirementLoading.value || grantObservationLoading.value || existingGrant.value))) return
  handlingError.value = ''
  const epoch = generation
  handlingSaving.value = true
  try {
    if (!handlingAttempt.value) {
      const candidate = selectedCandidate.value
      // Reobserve before freezing an ID. Transport retries after capture always
      // recover the original command, without an unrelated new preflight gate.
      validateHandlingRequirement(candidate, requirement.value)
      if (!await observeExistingGrant(candidate) || epoch !== generation) return
      handlingAttempt.value = captureSharingRequest(candidate, requirement.value, crypto.randomUUID())
    }
    if (props.decisionID !== handlingAttempt.value.decision_id) {
      confirmationSequence++; confirmation.value = null; confirmationError.value = ''; confirmationLoading.value = false
      clearConfirmationResults(); resultsPage.value = 1
    }
    await props.recordRequest(handlingAttempt.value.request_id, handlingAttempt.value.decision_id)
    if (epoch !== generation) return
    if (eligibility.value.confirm && !confirmation.value) refreshConfirmation()
    const result = await prepareSharingRequest(props.entry.id, handlingAttempt.value)
    if (epoch !== generation) return
    if (result.request_id !== handlingAttempt.value.request_id || !['pending', 'accepted', 'closed'].includes(result.state)) throw new Error('invalidResult')
    handlingSucceeded.value = result.state !== 'pending'
    await loadHistory()
    if (epoch === generation) await loadRequest(handlingAttempt.value.request_id)
  } catch (error) { if (epoch === generation) handlingError.value = error.message === 'invalidHandlingRequirement' ? t('catalog.sharing.invalidRequirement') : message(error) }
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

async function useExistingApproval(handling) {
  const approval = matchingApproval.value
  if (!approval || saving.value || attempt.value) return
  const epoch = generation
  await props.recordDecision(approval.id)
  if (epoch !== generation) return
  if (handling && canPrepare.value) await changeWorkspace('handling')
  else showConfirmationDetail()
}

async function submitConfirmation() {
  if (saving.value || !eligibility.value.create) return
  if (!attempt.value && matchingApproval.value) { await useExistingApproval(false); return }
  const epoch = generation, entry = { ...props.entry }, draft = { ...form }, options = [...recipients.value]
  saving.value = true
  confirmationError.value = ''
  if (!attempt.value) {
    try {
      const boundary = await getSharingBoundary(entry.id)
      if (epoch !== generation || !eligibility.value.create) { saving.value = false; return }
      attempt.value = captureSharingConfirmation(entry, draft, options, crypto.randomUUID(), boundary)
    }
    catch (error) { if (epoch === generation) { confirmationError.value = message(error); saving.value = false }; return }
  }
  try {
    // Persist only the opaque ID in the public route before sending. A reload
    // uses GET, never reconstructs a command from browser history or storage.
    await props.recordDecision(attempt.value.decision_id)
    if (epoch !== generation) return
    const result = await createSharingDecision(props.entry.id, attempt.value)
    if (epoch === generation) { confirmation.value = result; showConfirmationDetail(); loadConfirmations(); loadConfirmationResults(); if (canPrepare.value) loadCandidates() }
  } catch (error) {
    if (epoch === generation) {
      confirmationError.value = message(error)
      if (error?.response?.data?.error_code === 'catalog_sharing_decision_exists') {
        attempt.value = null
        await props.recordDecision('')
        if (epoch === generation) { workspace.value = 'create'; await searchRecipients('') }
      }
    }
  }
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
  if (switchingBlocked.value) return
  showConfirmationDetail()
  if (id && id === props.decisionID && !props.requestID) { await refreshConfirmation(); return }
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
  clearGrantObservation()
  candidateGrantSequence++; candidateGrantStatuses.value = {}
  confirmations.value = []; confirmationTotal.value = 0; confirmationPage.value = 1; confirmationsError.value = ''; confirmationsLoading.value = false
  clearConfirmationResults(); resultsPage.value = 1
  recipientLoading.value = saving.value = confirmationLoading.value = requestLoading.value = historyLoading.value = false
  confirmationError.value = recipientError.value = requestError.value = historyError.value = ''
  candidates.value = []; candidateTotal.value = 0; candidatePage.value = 1; selectedDecision.value = ''; requirement.value = null; handlingAttempt.value = null
  candidateLoading.value = requirementLoading.value = handlingSaving.value = handlingSucceeded.value = false; handlingError.value = ''
  initializing.value = false; initializationReason.value = ''
  requirementMissing.value = false
  workspace.value = !eligibility.value.confirm && canPrepare.value && props.decisionID && !props.requestID ? 'handling' : 'records'
  recordKind.value = props.requestID && eligibility.value.history ? 'requests' : eligibility.value.confirm ? 'confirmations' : 'requests'
  detailOpen.value = workspace.value === 'records' && !!(props.decisionID || props.requestID) && (eligibility.value.confirm || eligibility.value.history)
  page.value = 1
  if (eligibility.value.history) loadHistory()
  if (eligibility.value.confirm) loadConfirmations()
  if (eligibility.value.confirm && props.decisionID) refreshConfirmation()
  if (canPrepare.value && props.decisionID && !props.requestID) loadCandidates()
  if (eligibility.value.history && props.requestID) loadRequest(props.requestID)
}, { immediate: true })
watch(() => props.decisionID, value => {
  if (saving.value || handlingSaving.value) return
  candidateSequence++; requirementSequence++; requestSequence++
  candidateGrantSequence++; candidateGrantStatuses.value = {}
  clearGrantObservation()
  candidates.value = []; candidateTotal.value = 0; candidatePage.value = 1; selectedDecision.value = ''; requirement.value = null; requirementMissing.value = false
  candidateLoading.value = requirementLoading.value = false; handlingError.value = ''; initializationReason.value = ''
  request.value = null; requestError.value = ''; requestLoading.value = false; handlingAttempt.value = null; handlingSucceeded.value = false
  confirmationSequence++; confirmation.value = null; confirmationError.value = ''; confirmationLoading.value = false; clearConfirmationResults(); resultsPage.value = 1
  if (attempt.value && value !== attempt.value.decision_id) attempt.value = null
  if (value) refreshConfirmation()
  if (value && canPrepare.value && !props.requestID) loadCandidates()
})
watch(() => props.requestID, value => {
  if (handlingSaving.value) return
  if (handlingAttempt.value && value !== handlingAttempt.value.request_id) {
    handlingAttempt.value = null; handlingSucceeded.value = false; requirement.value = null; selectedDecision.value = ''
    handlingError.value = ''; requirementSequence++
  }
  if (value) loadRequest(value)
})
watch(canReadGrants, () => {
  clearGrantObservation()
  loadCandidateGrantStatuses(candidates.value)
  if (canPrepare.value && selectedCandidate.value && requirement.value && !handlingAttempt.value && !handlingSaving.value && !props.requestID) observeExistingGrant(selectedCandidate.value)
})
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.sharing-panel { display: grid; gap: 16px; }
.sharing-panel .el-select, .sharing-panel :deep(.el-date-editor) { width: 100%; }
.sharing-panel .el-alert, .sharing-panel .el-pagination { margin-bottom: 12px; }
.record-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-bottom: 12px; }
.approval-option { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.approval-option > span:first-child { overflow: hidden; text-overflow: ellipsis; }
.approval-status { flex-shrink: 0; color: var(--el-color-success); font-size: 12px; }
.approval-status-pending { color: var(--addp-text-secondary); }
.break-all { overflow-wrap: anywhere; }
.sharing-help { color: var(--addp-text-secondary); overflow-wrap: anywhere; line-height: 1.6; }
.workspace-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.system-authorization-link { margin-inline-start: auto; }
.workspace-help { margin: 0; }
.record-filter { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.record-filter .el-select { width: 260px; max-width: 100%; }
.confirmation-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 20px; max-width: 960px; }
.form-full { grid-column: 1 / -1; }
.initialization-form { border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 16px; margin: 16px 0; }
.sharing-panel :deep(.el-card__header) { padding: 16px 20px; }
.sharing-panel :deep(.el-descriptions__content) { overflow-wrap: anywhere; }
@media (max-width: 640px) {
  .confirmation-form { grid-template-columns: minmax(0, 1fr); }
  .workspace-toolbar :deep(.el-radio-button__inner) { padding: 9px 12px; }
}
</style>
