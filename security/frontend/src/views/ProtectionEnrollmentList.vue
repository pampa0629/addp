<template>
  <section class="page">
    <div class="page-header">
      <div>
        <h2>{{ t('security.resources.protectionEnrollment') }}</h2>
        <p>{{ t('security.descriptions.protectionEnrollment') }}</p>
      </div>
      <div class="page-actions">
        <span class="refresh-feedback" aria-live="polite">{{ refreshFeedback }}</span>
        <el-button :icon="Refresh" :loading="manualRefreshing" @click="manualRefresh">
          {{ t('security.enrollment.refresh') }}
        </el-button>
        <el-button v-if="canCreate && activeWorkspace === 'resources'" type="primary" @click="openCreate()">
          {{ t('security.enrollment.create') }}
        </el-button>
      </div>
    </div>

    <el-tabs v-model="activeWorkspace" class="workspace-tabs" @tab-change="handleWorkspaceChange">
      <el-tab-pane name="resources" :label="t('security.enrollment.workspaces.resources')" />
      <el-tab-pane name="review-queue">
        <template #label>
          <span class="workspace-tab-label">
            {{ t('security.enrollment.workspaces.reviewQueue') }}
            <el-tag v-if="activeWorkspace === 'review-queue' && reviewQueueTotal > 0" size="small" type="warning" round>{{ reviewQueueTotal }}</el-tag>
          </span>
        </template>
      </el-tab-pane>
    </el-tabs>

    <template v-if="activeWorkspace === 'resources'">
      <AccessRequestReviewWorkspace
        v-if="canReviewAccessRequests"
        v-model:scope="accessRequestScope"
        v-model:created-range="accessRequestCreatedRange"
        v-model:page="accessRequestPage"
        v-model:page-size="accessRequestPageSize"
        :rows="accessRequestRows"
        :total="accessRequestTotal"
        :loading="accessRequestLoading"
        :filters="accessRequestFilters"
        :can-read-exemptions="canReadExemptions"
        :release-actor-label="releaseActorLabel"
        :format-date-time="formatDateTime"
        @update:filters="updateAccessRequestFilters"
        @scope-change="handleAccessRequestScopeChange"
        @apply-filters="applyAccessRequestFilters"
        @reset-filters="resetAccessRequestFilters"
        @page-change="handleAccessRequestPageChange"
        @open-authorization="openAccessRequestAuthorization"
        @decide="openAccessRequestDecision"
      />
      <EnrollmentResourceList
        v-model:scope="listScope"
        v-model:page="currentPage"
        v-model:page-size="pageSize"
        :rows="rows"
        :total="total"
        :loading="loading"
        :can-create="canCreate"
        :resource-name="resourceName"
        :resource-path="resourcePath"
        :item-type-label="itemTypeLabel"
        :engine-label="engineLabel"
        :presentation-state="presentationState"
        :owner-label="ownerLabel"
        :owner-presentation="ownerPresentation"
        :format-date-time="formatDateTime"
        :discovery-presentation="discoveryPresentation"
        @scope-change="handleScopeChange"
        @page-change="handlePageChange"
        @open="openDetail"
        @re-enroll="openReEnrollment"
        @create="openCreate"
      />
    </template>

    <FindingReviewQueueWorkspace
      v-else
      v-model:type-id="reviewQueueTypeID"
      v-model:detector-version="reviewQueueDetectorVersion"
      v-model:page="reviewQueuePage"
      v-model:page-size="reviewQueuePageSize"
      :rows="reviewQueueRows"
      :total="reviewQueueTotal"
      :loading="reviewQueueLoading"
      :sensitive-types="sensitiveTypes"
      :detector-capabilities="detectorCapabilities"
      :can-review="canReviewFindings"
      :resource-name="resourceName"
      :resource-path="resourcePath"
      :item-type-label="itemTypeLabel"
      :engine-label="engineLabel"
      :type-name="typeName"
      :capability-name="capabilityName"
      :evidence-description="evidenceDescription"
      :confidence-label="confidenceLabel"
      :format-date-time="formatDateTime"
      @filter-change="handleReviewQueueFilterChange"
      @reset-filters="resetReviewQueueFilters"
      @page-change="handleReviewQueuePageChange"
      @open-resource="openReviewQueueResource"
      @review="openFindingReview"
    />

    <EnrollmentCreateDrawer
      v-model="createDrawer"
      ref="createDrawerRef"
      :can-create="canCreate"
      :saving="saving"
      :selected-item-loading="selectedItemLoading"
      :selected-item="selectedItem"
      :existing-enrollment="existingEnrollment"
      :initial-locator="initialLocator"
      :form="createForm"
      :rules="createFormRules"
      :item-type-label="itemTypeLabel"
      :engine-label="engineLabel"
      :format-date-time="formatDateTime"
      :before-close="beforeCreateClose"
      @resource-model-update="handleResourceModelUpdate"
      @resource-select="handleResourceSelect"
      @close="closeCreate"
      @closed="handleCreateClosed"
      @submit="createEnrollment"
    />

    <EnrollmentDetailDrawer
      v-model="detailDrawer"
      ref="detailDrawerRef"
      :enrollment="detailRow"
      :refresh-state="detailRefreshState"
      :governance="detailGovernance"
      :exemptions="detailExemptions"
      :lifecycle="detailLifecycle"
      :presentation="detailPresentation"
      @update:findings-page="findingsPage = $event"
      @close="closeDetail"
      @closed="handleDetailClosed"
      @refresh="manualRefresh"
      @designate="openManualAssessment"
      @review="openFindingReview"
      @history="openAssessmentHistory"
      @revise="openAssessmentRevision"
      @configure-policy="openPolicy"
      @restore-policy="openPolicyRestore"
      @revoke-assessment="openAssessmentRevoke"
      @page-change="loadFindings"
      @revoke="openExemptionRevoke"
      @re-enroll="openReEnrollment"
      @rediscover="openRediscovery"
      @release="openRelease"
    />

    <AccessRequestDecisionDialog
      v-model="accessRequestDecisionDialog"
      ref="accessRequestDecisionDialogRef"
      :decision="accessRequestDecision"
      :request="decidingAccessRequest"
      :title="accessRequestDecisionTitle"
      :hint="accessRequestDecisionHint"
      :confirm-label="accessRequestDecisionConfirmLabel"
      :saving="accessRequestDecisionSaving"
      :conflict="accessRequestDecisionConflict"
      :reloading="accessRequestDecisionReloading"
      :form="accessRequestDecisionForm"
      :rules="accessRequestDecisionRules"
      :release-actor-label="releaseActorLabel"
      :format-date-time="formatDateTime"
      @reload="reloadAccessRequestDecisionBaseline"
      @close="closeAccessRequestDecisionDialog"
      @closed="closeAccessRequestDecisionDialog"
      @submit="submitAccessRequestDecision"
    />

    <FindingReviewDialog
      v-model="reviewDialog"
      ref="reviewDialogRef"
      v-model:basis-expanded="reviewBasisExpanded"
      :finding="reviewingFinding"
      :remaining-label="reviewRemainingLabel"
      :saving="reviewSaving"
      :form="reviewForm"
      :rules="reviewRules"
      :sensitive-types="sensitiveTypes"
      :rationale-placeholder="reviewRationalePlaceholder"
      :active-grades-for-type="activeGradesForType"
      :type-name="typeName"
      :confidence-label="confidenceLabel"
      :capability-name="capabilityName"
      :evidence-audit-description="evidenceAuditDescription"
      :decision-presentation="decisionPresentation"
      :effective-definition-summary="effectiveDefinitionSummary"
      :baseline-description="baselineDescription"
      :owner-label="ownerLabel"
      :outlet-rule-description="outletRuleDescription"
      @sensitive-type-change="applyReviewDefaultGrade"
      @close="closeFindingReview"
      @closed="closeFindingReview"
      @submit="submitFindingReview"
    />

    <ManualAssessmentDialog
      v-model="manualAssessmentDialog"
      ref="manualAssessmentDialogRef"
      :saving="manualAssessmentSaving"
      :components-loading="componentsLoading"
      :component-options="componentOptions"
      :sensitive-types="sensitiveTypes"
      :form="manualAssessmentForm"
      :rules="manualAssessmentRules"
      :active-grades-for-type="activeGradesForType"
      @sensitive-type-change="applyDefaultGrade"
      @close="closeManualAssessment"
      @closed="closeManualAssessment"
      @submit="submitManualAssessment"
    />

    <AssessmentHistoryDialog
      v-model="assessmentHistoryDialog"
      :loading="assessmentHistoryLoading"
      :assessment="assessmentHistory"
      :format-date-time="formatDateTime"
      :assessment-conclusion-label="assessmentConclusionLabel"
      :assessment-revision-source-label="assessmentRevisionSourceLabel"
      :assessment-revision-summary="assessmentRevisionSummary"
      :assessment-actor-label="assessmentActorLabel"
      @close="closeAssessmentHistory"
      @closed="closeAssessmentHistory"
    />

    <AssessmentChangeDialog
      v-model="assessmentRevisionDialog"
      ref="assessmentRevisionDialogRef"
      mode="revise"
      :assessment="revisingAssessment"
      :saving="assessmentRevisionSaving"
      :reloading="assessmentRevisionReloading"
      :conflict="assessmentRevisionConflict"
      :form="assessmentRevisionForm"
      :rules="assessmentRevisionRules"
      :sensitive-types="sensitiveTypes"
      :active-grades-for-type="activeGradesForType"
      :assessment-summary="assessmentSummary"
      :assessment-conclusion-label="assessmentConclusionLabel"
      @reload="reloadAssessmentRevisionBaseline"
      @sensitive-type-change="applyAssessmentRevisionDefaultGrade"
      @close="closeAssessmentRevision"
      @closed="closeAssessmentRevision"
      @submit="submitAssessmentRevision"
    />

    <AssessmentChangeDialog
      v-model="assessmentRevokeDialog"
      ref="assessmentRevokeDialogRef"
      mode="revoke"
      :assessment="revokingAssessment"
      :saving="assessmentRevokeSaving"
      :reloading="assessmentRevokeReloading"
      :conflict="assessmentRevokeConflict"
      :form="assessmentRevokeForm"
      :rules="assessmentRevokeRules"
      :sensitive-types="sensitiveTypes"
      :active-grades-for-type="activeGradesForType"
      :assessment-summary="assessmentSummary"
      :assessment-conclusion-label="assessmentConclusionLabel"
      @reload="reloadAssessmentRevokeBaseline"
      @close="closeAssessmentRevokeDialog"
      @closed="closeAssessmentRevokeDialog"
      @submit="submitAssessmentRevoke"
    />

    <ExemptionRevokeDialog
      v-model="exemptionRevokeDialog"
      ref="exemptionRevokeDialogRef"
      :exemption="revokingExemption"
      :saving="exemptionRevokeSaving"
      :reloading="exemptionRevokeReloading"
      :conflict="exemptionRevokeConflict"
      :form="exemptionRevokeForm"
      :rules="exemptionRevokeRules"
      :assessment-component="assessmentComponent"
      :owner-label="ownerLabel"
      :action-label="actionLabel"
      :format-date-time="formatDateTime"
      @reload="reloadExemptionRevokeBaseline"
      @close="closeExemptionRevokeDialog"
      @closed="closeExemptionRevokeDialog"
      @submit="submitExemptionRevoke"
    />

    <ProtectionPolicyDialog
      v-model="policyDialog"
      ref="policyDialogRef"
      mode="tighten"
      :assessment="policyAssessment"
      :assessment-summary="policyAssessment ? assessmentSummary(policyAssessment) : ''"
      :protection-summary="policyAssessment ? assessmentProtectionSummary(policyAssessment) : ''"
      :saving="policySaving"
      :reloading="policyReloading"
      :conflict="policyVersionConflict"
      :form="policyForm"
      :rules="policyRules"
      :effects="policyEffects"
      :effect-label="effectLabel"
      @reload="reloadPolicyBaseline"
      @close="closePolicyDialog"
      @closed="closePolicyDialog"
      @submit="savePolicy"
    />

    <ProtectionPolicyDialog
      v-model="policyRestoreDialog"
      ref="policyRestoreDialogRef"
      mode="restore"
      :assessment="policyRestoreAssessment"
      :assessment-summary="policyRestoreAssessment ? assessmentSummary(policyRestoreAssessment) : ''"
      :protection-summary="policyRestoreAssessment ? assessmentProtectionSummary(policyRestoreAssessment) : ''"
      :saving="policyRestoreSaving"
      :reloading="policyRestoreReloading"
      :conflict="policyRestoreConflict"
      :form="policyRestoreForm"
      :rules="policyRestoreRules"
      :effect-label="effectLabel"
      @reload="reloadPolicyRestoreBaseline"
      @close="closePolicyRestoreDialog"
      @closed="closePolicyRestoreDialog"
      @submit="submitPolicyRestore"
    />

    <EnrollmentLifecycleActionDialog
      v-model="reEnrollmentDialog"
      ref="reEnrollmentDialogRef"
      mode="re-enroll"
      :enrollment="reEnrollmentSource"
      :saving="reEnrollmentSaving"
      :reloading="reEnrollmentReloading"
      :conflict="reEnrollmentConflict"
      :resource-name="resourceName"
      :release-basis-label="releaseBasisLabel"
      :format-date-time="formatDateTime"
      @reload="reloadReEnrollmentBaseline"
      @close="closeReEnrollmentDialog"
      @closed="closeReEnrollmentDialog"
      @submit="submitReEnrollment"
    />

    <EnrollmentLifecycleActionDialog
      v-model="rediscoveryDialog"
      ref="rediscoveryDialogRef"
      mode="rediscover"
      :enrollment="rediscoveryEnrollment"
      :saving="rediscoverySaving"
      :reloading="rediscoveryReloading"
      :conflict="rediscoveryConflict"
      :resource-name="resourceName"
      :release-basis-label="releaseBasisLabel"
      :format-date-time="formatDateTime"
      @reload="reloadRediscoveryBaseline"
      @close="closeRediscoveryDialog"
      @closed="closeRediscoveryDialog"
      @submit="submitRediscovery"
    />

    <EnrollmentReleaseDialog
      v-model="releaseDialog"
      ref="releaseDialogRef"
      :enrollment="releasing"
      :basis="releaseBasis"
      :saving="releaseSaving"
      :reloading="releaseReloading"
      :conflict="releaseConflict"
      :form="releaseForm"
      :rules="releaseRules"
      :release-basis-label="releaseBasisLabel"
      @reload="reloadReleaseBaseline"
      @close="closeReleaseDialog"
      @closed="closeReleaseDialog"
      @submit="releaseEnrollment"
    />
  </section>
</template>

<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listResourceTreeEngines, openMonitorExecution } from '@common-ui'
import { assessmentAPI, classificationAPI, detectorCapabilityAPI, findingAPI, gradeAPI, metaAPI, protectionAccessRequestAPI, protectionBaselineAPI, protectionEnrollmentAPI, protectionExemptionAPI, protectionPolicyAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { useProtectionAccessRequestReview } from '../composables/useProtectionAccessRequestReview.mjs'
import { useProtectionAssessmentChange } from '../composables/useProtectionAssessmentChange.mjs'
import { useProtectionAssessmentWorkspace } from '../composables/useProtectionAssessmentWorkspace.mjs'
import { useProtectionEnrollmentCollection } from '../composables/useProtectionEnrollmentCollection.mjs'
import { useProtectionEnrollmentCreation } from '../composables/useProtectionEnrollmentCreation.mjs'
import { useProtectionEnrollmentDetail } from '../composables/useProtectionEnrollmentDetail.mjs'
import { useProtectionEnrollmentReEnrollment } from '../composables/useProtectionEnrollmentReEnrollment.mjs'
import { useProtectionEnrollmentRediscovery } from '../composables/useProtectionEnrollmentRediscovery.mjs'
import { useProtectionEnrollmentRelease } from '../composables/useProtectionEnrollmentRelease.mjs'
import { useProtectionEnrollmentPresentation } from '../composables/useProtectionEnrollmentPresentation.mjs'
import { resolveProtectionEnrollmentWorkspaceRouteState, useProtectionEnrollmentWorkspace } from '../composables/useProtectionEnrollmentWorkspace.mjs'
import { useProtectionReferenceCatalog } from '../composables/useProtectionReferenceCatalog.mjs'
import { useProtectionFindingReview } from '../composables/useProtectionFindingReview.mjs'
import { useProtectionFindingReviewQueue } from '../composables/useProtectionFindingReviewQueue.mjs'
import { useProtectionEnrollmentGovernance } from '../composables/useProtectionEnrollmentGovernance.mjs'
import { useProtectionExemptionRevocation } from '../composables/useProtectionExemptionRevocation.mjs'
import { useProtectionPolicyChange } from '../composables/useProtectionPolicyChange.mjs'
import EnrollmentResourceList from '../components/protection-enrollment/EnrollmentResourceList.vue'

const { t, locale } = useI18n()
const EnrollmentCreateDrawer = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentCreateDrawer.vue'))
const EnrollmentDetailDrawer = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentDetailDrawer.vue'))
const AccessRequestDecisionDialog = defineAsyncComponent(() => import('../components/protection-enrollment/AccessRequestDecisionDialog.vue'))
const AssessmentChangeDialog = defineAsyncComponent(() => import('../components/protection-enrollment/AssessmentChangeDialog.vue'))
const ExemptionRevokeDialog = defineAsyncComponent(() => import('../components/protection-enrollment/ExemptionRevokeDialog.vue'))
const EnrollmentLifecycleActionDialog = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentLifecycleActionDialog.vue'))
const EnrollmentReleaseDialog = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentReleaseDialog.vue'))
const ProtectionPolicyDialog = defineAsyncComponent(() => import('../components/protection-enrollment/ProtectionPolicyDialog.vue'))
const ManualAssessmentDialog = defineAsyncComponent(() => import('../components/protection-enrollment/ManualAssessmentDialog.vue'))
const AssessmentHistoryDialog = defineAsyncComponent(() => import('../components/protection-enrollment/AssessmentHistoryDialog.vue'))
const AccessRequestReviewWorkspace = defineAsyncComponent(() => import('../components/protection-enrollment/AccessRequestReviewWorkspace.vue'))
const FindingReviewQueueWorkspace = defineAsyncComponent(() => import('../components/protection-enrollment/FindingReviewQueueWorkspace.vue'))
const FindingReviewDialog = defineAsyncComponent(() => import('../components/protection-enrollment/FindingReviewDialog.vue'))
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const initialWorkspaceRoute = resolveProtectionEnrollmentWorkspaceRouteState(route.query)
const activeWorkspace = ref(initialWorkspaceRoute.tab)

const detailRow = ref(null)
const {
  engineNames,
  sensitiveTypes,
  securityClassifications,
  securityGrades,
  protectionBaselines,
  detectorCapabilities,
  loadDefinitions: loadFindingDefinitions,
  loadDetectorCapabilities,
  loadEngines,
  setProtectionBaselines,
  dispose: disposeProtectionReferenceCatalog
} = useProtectionReferenceCatalog({
  listSensitiveTypes: () => sensitiveDataTypeAPI.list(),
  listClassifications: () => classificationAPI.list(),
  listGrades: () => gradeAPI.list(),
  listBaselines: () => protectionBaselineAPI.list(),
  listDetectorCapabilities: () => detectorCapabilityAPI.list(),
  listEngines: () => listResourceTreeEngines('/api/v1/meta')
})

const {
  rows,
  total,
  currentPage,
  pageSize,
  listScope,
  loading,
  autoRefreshActive,
  lastRefreshedAt,
  load,
  markRefreshed,
  replaceRow: replaceCollectionRow,
  setCollection,
  watchDiscovery,
  scheduleAutoRefresh,
  stopAutoRefresh,
  refreshCurrentPage,
  refreshChangedScope,
  startVisibilityTracking,
  dispose: disposeEnrollmentCollection
} = useProtectionEnrollmentCollection({
  listEnrollments: params => protectionEnrollmentAPI.list(params),
  onRowsLoaded: context => syncDetailEnrollmentRows(context),
  onLoadError: error => ElMessage.error(error.message || t('security.common.failed')),
  onAutoRefreshTimeout: () => ElMessage.warning(t('security.enrollment.autoRefreshTimedOut'))
})

const canCreate = computed(() => auth.hasPermission('security.enrollment.create'))
const {
  drawer: createDrawer,
  drawerRef: createDrawerRef,
  form: createForm,
  formRules: createFormRules,
  selectedItem,
  selectedItemLoading,
  initialLocator,
  existingEnrollment,
  saving,
  open: openCreate,
  updateResource: handleResourceModelUpdate,
  selectResource: handleResourceSelect,
  requestClose: closeCreate,
  beforeClose: beforeCreateClose,
  closed: handleCreateClosed,
  submit: createEnrollment,
  dispose: disposeEnrollmentCreation
} = useProtectionEnrollmentCreation({
  enrollments: rows,
  getItem: id => metaAPI.getItem(id),
  createEnrollment: payload => protectionEnrollmentAPI.create(payload),
  showCurrentCollection: () => {
    listScope.value = 'current'
    currentPage.value = 1
  },
  refreshCollection: () => load(),
  scheduleAutoRefresh,
  t,
  onItemLoadError: error => ElMessage.error(error.message || t('security.enrollment.itemLoadFailed')),
  onAlreadyEnrolled: () => ElMessage.warning(t('security.enrollment.alreadyEnrolled')),
  onCreated: () => ElMessage.success(t('security.enrollment.created')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed')),
  onClosed: async () => {
    if (route.query.action === 'enroll' || route.query.locator) {
      await navigateWorkspace('resources')
    }
  }
})
const canRelease = computed(() => auth.hasPermission('security.enrollment.update'))
const canReadFindings = computed(() => auth.hasPermission('security.finding.read'))
const canReviewFindings = computed(() => auth.hasPermission('security.finding.update'))
const canReadAssessments = computed(() => auth.hasPermission('security.assessment.read'))
const canCreateAssessments = computed(() => auth.hasPermission('security.assessment.create'))
const canUpdateAssessments = computed(() => auth.hasPermission('security.assessment.update'))
const canReadPolicies = computed(() => auth.hasPermission('security.policy.read'))
const canCreatePolicies = computed(() => auth.hasPermission('security.policy.create'))
const canUpdatePolicies = computed(() => auth.hasPermission('security.policy.update'))
const canRevokePolicies = computed(() => auth.hasPermission('security.policy.delete'))
const canReadExemptions = computed(() => auth.hasPermission('security.protection_exemption.read'))
const canRevokeExemptions = computed(() => auth.hasPermission('security.protection_exemption.delete'))
const canReviewAccessRequests = computed(() => auth.hasPermission('security.protection_access_request.update'))
const {
  rows: reviewQueueRows,
  total: reviewQueueTotal,
  page: reviewQueuePage,
  pageSize: reviewQueuePageSize,
  sensitiveDataTypeID: reviewQueueTypeID,
  detectorVersion: reviewQueueDetectorVersion,
  loading: reviewQueueLoading,
  applyRouteState: applyReviewQueueRouteState,
  routeQuery: reviewQueueRouteQuery,
  loadQueue: loadReviewQueue,
  prepareFilterChange: prepareReviewQueueFilterChange,
  resetFilters: resetReviewQueueFilterState,
  nextFindingAfterReview: nextReviewQueueFinding,
  dispose: disposeFindingReviewQueue
} = useProtectionFindingReviewQueue({
  canRead: canReadFindings,
  initialRouteState: initialWorkspaceRoute.reviewQueue,
  listFindings: params => findingAPI.list(params),
  onRefreshed: markRefreshed,
  onLoadError: error => ElMessage.error(error.message || t('security.reviewQueue.loadFailed'))
})
const {
  rows: accessRequestRows,
  total: accessRequestTotal,
  page: accessRequestPage,
  pageSize: accessRequestPageSize,
  loading: accessRequestLoading,
  scope: accessRequestScope,
  filters: accessRequestFilters,
  createdRange: accessRequestCreatedRange,
  decisionDialog: accessRequestDecisionDialog,
  decisionDialogRef: accessRequestDecisionDialogRef,
  decisionSaving: accessRequestDecisionSaving,
  decisionReloading: accessRequestDecisionReloading,
  decisionConflict: accessRequestDecisionConflict,
  decidingRequest: decidingAccessRequest,
  decision: accessRequestDecision,
  decisionForm: accessRequestDecisionForm,
  decisionRules: accessRequestDecisionRules,
  decisionTitle: accessRequestDecisionTitle,
  decisionHint: accessRequestDecisionHint,
  decisionConfirmLabel: accessRequestDecisionConfirmLabel,
  loadQueue: loadAccessRequestQueue,
  changeScope: handleAccessRequestScopeChange,
  updateFilters: updateAccessRequestFilters,
  applyFilters: applyAccessRequestFilters,
  resetFilters: resetAccessRequestFilters,
  openDecision: openAccessRequestDecision,
  closeDecision: closeAccessRequestDecisionDialog,
  reloadDecisionBaseline: reloadAccessRequestDecisionBaseline,
  submitDecision: submitAccessRequestDecision,
  dispose: disposeAccessRequestReview
} = useProtectionAccessRequestReview({
  canReview: canReviewAccessRequests,
  listRequests: params => protectionAccessRequestAPI.reviewQueue(params),
  getRequest: id => protectionAccessRequestAPI.getForReview(id),
  decideRequest: (id, payload) => protectionAccessRequestAPI.decide(id, payload),
  refreshCollection: options => load(options),
  t,
  onQueueLoadError: error => ElMessage.error(error.message || t('security.accessRequest.loadFailed')),
  onDecisionReloaded: () => ElMessage.success(t('security.accessRequest.decisionReloadedLatest')),
  onAlreadyProcessed: () => ElMessage.info(t('security.accessRequest.alreadyProcessed')),
  onDecisionLoadError: error => ElMessage.error(error.message || t('security.accessRequest.loadFailed')),
  onDecisionSucceeded: decision => ElMessage.success(t(`security.accessRequest.${decision}d`)),
  onDecisionConflict: () => ElMessage.warning(t('security.accessRequest.decisionVersionConflict')),
  onDecisionExpired: () => ElMessage.info(t('security.accessRequest.expiredBeforeDecision')),
  onDecisionError: error => ElMessage.error(error.message || t('security.accessRequest.decisionFailed'))
})
const {
  findings,
  findingsTotal,
  findingsPage,
  findingsPageSize,
  findingsLoading,
  assessments,
  policies,
  exemptions,
  exemptionsLoading,
  governanceLoading,
  loadFindings,
  loadAssessments,
  loadPolicies,
  loadExemptions,
  loadGovernance: loadGovernanceData,
  replaceAssessment,
  replacePolicy,
  replaceExemption,
  reset: resetGovernance
} = useProtectionEnrollmentGovernance({
  enrollment: detailRow,
  canReadFindings,
  canReadAssessments,
  canReadPolicies,
  canReadExemptions,
  listFindings: params => findingAPI.list(params),
  listAssessments: params => assessmentAPI.list(params),
  listPolicies: params => protectionPolicyAPI.list(params),
  listExemptions: params => protectionExemptionAPI.list(params),
  onLoadError: handleGovernanceLoadError
})
const {
  manualAssessments,
  refreshFeedback,
  reviewRemainingLabel,
  normalizeDiscoverySummary,
  isZeroFindingDiscovery,
  ownerLabel,
  effectLabel,
  resourceName,
  resourcePath,
  engineLabel,
  itemTypeLabel,
  formatDateTime,
  releaseBasisLabel,
  releaseActorLabel,
  discoveryPresentation,
  presentationState,
  ownerPresentation,
  ownerEffectDescription,
  typeName,
  classificationName,
  gradeName,
  confidenceLabel,
  evidenceDescription,
  capabilityText,
  capabilityScope,
  evidenceAuditDescription,
  capabilityName,
  decisionPresentation,
  effectiveDefinitionSummary,
  baselineDescription,
  actionLabel,
  assessmentComponent,
  exemptionStatePresentation,
  outletRuleDescription,
  outletAcknowledgementPresentation,
  findingStatePresentation,
  findingAssessmentView,
  assessmentSummary,
  assessmentRevisionSummary,
  assessmentConclusionLabel,
  assessmentRevisionSourceLabel,
  assessmentActorLabel,
  policyForAssessment,
  stricterPolicyEffects,
  canConfigurePolicy,
  assessmentProtectionSummary,
  activeGradesForType
} = useProtectionEnrollmentPresentation({
  t,
  locale,
  activeWorkspace,
  reviewQueueTotal,
  detailRow,
  autoRefreshActive,
  lastRefreshedAt,
  engineNames,
  sensitiveTypes,
  securityClassifications,
  securityGrades,
  protectionBaselines,
  assessments,
  policies,
  canReadPolicies,
  canCreatePolicies,
  canUpdatePolicies
})
const {
  drawer: detailDrawer,
  focusedExemptionID,
  drawerRef: detailDrawerRef,
  load: loadGovernance,
  open: openDetail,
  openAuthorization: openAccessRequestAuthorization,
  openFinding: openReviewQueueResource,
  requestClose: closeDetail,
  closed: handleDetailClosed,
  replace: replaceDetailEnrollment,
  syncLoadedRows: syncDetailEnrollmentRows,
  nextPendingFinding: nextDetailReviewFinding,
  dispose: disposeEnrollmentDetail
} = useProtectionEnrollmentDetail({
  enrollment: detailRow,
  getEnrollment: id => protectionEnrollmentAPI.get(id),
  loadGovernance: page => loadGovernanceData(page),
  resetGovernance,
  loadDefinitions: loadFindingDefinitions,
  navigateToResources: () => navigateWorkspace('resources', 'push'),
  listPendingFindings: params => findingAPI.list(params),
  onAuthorizationLoadError: error => ElMessage.error(error.message || t('security.exemption.loadFailed')),
  onResourceLoadError: error => ElMessage.error(error.message || t('security.common.failed')),
  onDefinitionsLoadError: error => ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed'))
})
const {
  designationDialog: manualAssessmentDialog,
  designationDialogRef: manualAssessmentDialogRef,
  designationSaving: manualAssessmentSaving,
  componentsLoading,
  componentOptions,
  designationForm: manualAssessmentForm,
  designationRules: manualAssessmentRules,
  openDesignation: openManualAssessment,
  closeDesignation: closeManualAssessment,
  applyDesignationDefaultGrade: applyDefaultGrade,
  submitDesignation: submitManualAssessment,
  historyDialog: assessmentHistoryDialog,
  historyLoading: assessmentHistoryLoading,
  history: assessmentHistory,
  openHistory: openAssessmentHistory,
  closeHistory: closeAssessmentHistory,
  dispose: disposeProtectionAssessmentWorkspace
} = useProtectionAssessmentWorkspace({
  getEnrollment: () => detailRow.value,
  loadComponents: enrollmentID => protectionEnrollmentAPI.components(enrollmentID),
  loadDefinitions: loadFindingDefinitions,
  refreshAssessments: loadAssessments,
  createAssessment: payload => assessmentAPI.create(payload),
  getAssessment: id => assessmentAPI.get(id),
  refreshCollection: options => load(options),
  refreshGovernance: () => loadGovernance(findingsPage.value),
  scheduleAutoRefresh,
  defaultGradeID: typeID => {
    const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
    return selectedType?.default_security_grade_id
  },
  t,
  onComponentsLoadError: error => ElMessage.error(error.message || t('security.assessment.componentsLoadFailed')),
  onDesignationSaved: () => ElMessage.success(t('security.assessment.designated')),
  onDesignationError: error => ElMessage.error(error.message || t('security.common.failed')),
  onHistoryLoadError: error => ElMessage.error(error.message || t('security.assessment.historyLoadFailed'))
})
const {
  dialog: exemptionRevokeDialog,
  dialogRef: exemptionRevokeDialogRef,
  saving: exemptionRevokeSaving,
  reloading: exemptionRevokeReloading,
  conflict: exemptionRevokeConflict,
  exemption: revokingExemption,
  form: exemptionRevokeForm,
  rules: exemptionRevokeRules,
  open: openExemptionRevoke,
  close: closeExemptionRevokeDialog,
  reloadBaseline: reloadExemptionRevokeBaseline,
  submit: submitExemptionRevoke,
  dispose: disposeProtectionExemptionRevocation
} = useProtectionExemptionRevocation({
  getExemption: id => protectionExemptionAPI.get(id),
  revokeExemption: (id, payload) => protectionExemptionAPI.revoke(id, payload),
  replaceExemption,
  refreshExemptions: loadExemptions,
  refreshCollection: options => load(options),
  scheduleAutoRefresh,
  t,
  onReloaded: () => ElMessage.success(t('security.exemption.revokeReloadedLatest')),
  onAlreadyInactive: () => ElMessage.info(t('security.exemption.alreadyInactive')),
  onLoadError: error => ElMessage.error(error.message || t('security.exemption.loadFailed')),
  onRevoked: () => ElMessage.success(t('security.exemption.revoked')),
  onVersionConflict: () => ElMessage.warning(t('security.exemption.revokeVersionConflict')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
const {
  revisionDialog: assessmentRevisionDialog,
  revisionDialogRef: assessmentRevisionDialogRef,
  revisionSaving: assessmentRevisionSaving,
  revisionReloading: assessmentRevisionReloading,
  revisionConflict: assessmentRevisionConflict,
  revisionAssessment: revisingAssessment,
  revisionForm: assessmentRevisionForm,
  revisionRules: assessmentRevisionRules,
  openRevision: openAssessmentRevision,
  closeRevision: closeAssessmentRevision,
  applyRevisionDefaultGrade: applyAssessmentRevisionDefaultGrade,
  reloadRevisionBaseline: reloadAssessmentRevisionBaseline,
  submitRevision: submitAssessmentRevision,
  revokeDialog: assessmentRevokeDialog,
  revokeDialogRef: assessmentRevokeDialogRef,
  revokeSaving: assessmentRevokeSaving,
  revokeReloading: assessmentRevokeReloading,
  revokeConflict: assessmentRevokeConflict,
  revokeAssessment: revokingAssessment,
  revokeForm: assessmentRevokeForm,
  revokeRules: assessmentRevokeRules,
  openRevoke: openAssessmentRevoke,
  closeRevoke: closeAssessmentRevokeDialog,
  reloadRevokeBaseline: reloadAssessmentRevokeBaseline,
  submitRevoke: submitAssessmentRevoke,
  dispose: disposeProtectionAssessmentChange
} = useProtectionAssessmentChange({
  loadDefinitions: loadFindingDefinitions,
  activeGradesForType,
  defaultGradeID: typeID => {
    const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
    return selectedType?.default_security_grade_id
  },
  getAssessment: id => assessmentAPI.get(id),
  reviseAssessment: (id, payload) => assessmentAPI.revise(id, payload),
  revokeAssessment: (id, payload) => assessmentAPI.revoke(id, payload),
  replaceAssessment,
  refreshCollection: options => load(options),
  refreshGovernance: () => loadGovernance(findingsPage.value),
  scheduleAutoRefresh,
  t,
  onDefinitionsError: error => ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed')),
  onRevisionReloaded: () => ElMessage.success(t('security.assessment.reloadedLatest')),
  onRevokeReloaded: () => ElMessage.success(t('security.assessment.revokeReloadedLatest')),
  onAlreadyRevoked: () => ElMessage.info(t('security.assessment.alreadyRevoked')),
  onLoadError: error => ElMessage.error(error.message || t('security.assessment.reloadFailed')),
  onRevised: () => ElMessage.success(t('security.assessment.revised')),
  onRevoked: () => ElMessage.success(t('security.assessment.revoked')),
  onRevisionConflict: () => ElMessage.warning(t('security.assessment.versionConflict')),
  onRevokeConflict: () => ElMessage.warning(t('security.assessment.revokeVersionConflict')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
const {
  dialog: reviewDialog,
  dialogRef: reviewDialogRef,
  finding: reviewingFinding,
  saving: reviewSaving,
  basisExpanded: reviewBasisExpanded,
  form: reviewForm,
  rules: reviewRules,
  rationalePlaceholder: reviewRationalePlaceholder,
  open: openFindingReview,
  close: closeFindingReview,
  applyDefaultGrade: applyReviewDefaultGrade,
  submit: submitFindingReview,
  dispose: disposeFindingReview
} = useProtectionFindingReview({
  sensitiveTypes,
  loadDefinitions: loadFindingDefinitions,
  reviewFinding: (id, payload) => findingAPI.review(id, payload),
  resolveContext: () => activeWorkspace.value === 'review-queue' ? 'queue' : 'resource',
  continueAfterReview: continueFindingReview,
  t,
  onDefinitionsError: error => ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed')),
  onContinuationError: error => ElMessage.error(error.message || t('security.finding.loadFailed')),
  onSaved: () => ElMessage.success(t('security.finding.reviewSaved')),
  onContinued: () => ElMessage.success(t('security.finding.reviewSavedAndContinued'))
})
const {
  editDialog: policyDialog,
  editDialogRef: policyDialogRef,
  editSaving: policySaving,
  editReloading: policyReloading,
  editConflict: policyVersionConflict,
  editAssessment: policyAssessment,
  editForm: policyForm,
  editRules: policyRules,
  editEffects: policyEffects,
  openEdit: openPolicy,
  closeEdit: closePolicyDialog,
  reloadEditBaseline: reloadPolicyBaseline,
  submitEdit: savePolicy,
  restoreDialog: policyRestoreDialog,
  restoreDialogRef: policyRestoreDialogRef,
  restoreSaving: policyRestoreSaving,
  restoreReloading: policyRestoreReloading,
  restoreConflict: policyRestoreConflict,
  restoreAssessment: policyRestoreAssessment,
  restoreForm: policyRestoreForm,
  restoreRules: policyRestoreRules,
  openRestore: openPolicyRestore,
  closeRestore: closePolicyRestoreDialog,
  reloadRestoreBaseline: reloadPolicyRestoreBaseline,
  submitRestore: submitPolicyRestore,
  dispose: disposeProtectionPolicyChange
} = useProtectionPolicyChange({
  policyForAssessment,
  stricterEffects: stricterPolicyEffects,
  getPolicy: id => protectionPolicyAPI.get(id),
  getAssessment: id => assessmentAPI.get(id),
  listBaselines: () => protectionBaselineAPI.list(),
  applyLatestContext: context => {
    setProtectionBaselines(context.baselines)
    replacePolicy(context.policy)
    replaceAssessment(context.assessment)
  },
  createPolicy: payload => protectionPolicyAPI.create(payload),
  updatePolicy: (id, payload) => protectionPolicyAPI.update(id, payload),
  revokePolicy: (id, payload) => protectionPolicyAPI.revoke(id, payload),
  refreshCollection: options => load(options),
  refreshGovernance: () => loadGovernance(findingsPage.value),
  scheduleAutoRefresh,
  t,
  onAlreadyStrictest: () => ElMessage.info(t('security.policy.alreadyStrictest')),
  onEditReloaded: () => ElMessage.success(t('security.policy.reloadedLatest')),
  onRestoreReloaded: () => ElMessage.success(t('security.policy.restoreReloadedLatest')),
  onAlreadyRestored: () => ElMessage.info(t('security.policy.alreadyRestored')),
  onLoadError: error => ElMessage.error(error.message || t('security.policy.reloadFailed')),
  onSaved: () => ElMessage.success(t('security.policy.saved')),
  onRestored: () => ElMessage.success(t('security.policy.restored')),
  onEditConflict: () => ElMessage.warning(t('security.policy.versionConflict')),
  onRestoreConflict: () => ElMessage.warning(t('security.policy.restoreVersionConflict')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
const {
  dialog: reEnrollmentDialog,
  dialogRef: reEnrollmentDialogRef,
  saving: reEnrollmentSaving,
  reloading: reEnrollmentReloading,
  conflict: reEnrollmentConflict,
  source: reEnrollmentSource,
  open: openReEnrollment,
  close: closeReEnrollmentDialog,
  reloadBaseline: reloadReEnrollmentBaseline,
  submit: submitReEnrollment,
  dispose: disposeEnrollmentReEnrollment
} = useProtectionEnrollmentReEnrollment({
  getEnrollment: id => protectionEnrollmentAPI.get(id),
  createReEnrollment: (id, payload) => protectionEnrollmentAPI.reEnroll(id, payload),
  replaceEnrollment,
  showCurrentCollection: enrollment => {
    closeDetail()
    listScope.value = 'current'
    currentPage.value = 1
    if (enrollment) setCollection([enrollment], 1)
  },
  refreshCollection: options => load(options),
  scheduleAutoRefresh,
  onUnavailable: () => ElMessage.info(t('security.enrollment.reEnrollmentUnavailable')),
  onReloaded: () => ElMessage.success(t('security.enrollment.reEnrollmentReloadedLatest')),
  onLoadError: error => ElMessage.error(error.message || t('security.enrollment.reEnrollmentLoadFailed')),
  onCreated: () => ElMessage.success(t('security.enrollment.reEnrolled')),
  onVersionConflict: () => ElMessage.warning(t('security.enrollment.reEnrollmentVersionConflict')),
  onAlreadyActive: () => ElMessage.info(t('security.enrollment.enrollmentAlreadyActive')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
const {
  dialog: rediscoveryDialog,
  dialogRef: rediscoveryDialogRef,
  saving: rediscoverySaving,
  reloading: rediscoveryReloading,
  conflict: rediscoveryConflict,
  enrollment: rediscoveryEnrollment,
  open: openRediscovery,
  close: closeRediscoveryDialog,
  reloadBaseline: reloadRediscoveryBaseline,
  submit: submitRediscovery,
  dispose: disposeEnrollmentRediscovery
} = useProtectionEnrollmentRediscovery({
  getEnrollment: id => protectionEnrollmentAPI.get(id),
  createDiscoveryExecution: (id, payload) => protectionEnrollmentAPI.rediscover(id, payload),
  replaceEnrollment,
  watchDiscovery,
  refreshCollection: options => load(options),
  scheduleAutoRefresh,
  openExecution: executionID => openMonitorExecution(executionID),
  onUnavailable: () => ElMessage.info(t('security.enrollment.rediscoveryUnavailable')),
  onReloaded: () => ElMessage.success(t('security.enrollment.rediscoveryReloadedLatest')),
  onLoadError: error => ElMessage.error(error.message || t('security.enrollment.rediscoveryLoadFailed')),
  onCreated: () => ElMessage.success(t('security.enrollment.rediscoveryCreated')),
  onVersionConflict: () => ElMessage.warning(t('security.enrollment.rediscoveryVersionConflict')),
  onExecutionInProgress: () => ElMessage.info(t('security.enrollment.discoveryExecutionInProgress')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
const {
  dialog: releaseDialog,
  dialogRef: releaseDialogRef,
  saving: releaseSaving,
  reloading: releaseReloading,
  conflict: releaseConflict,
  enrollment: releasing,
  basis: releaseBasis,
  form: releaseForm,
  rules: releaseRules,
  open: openRelease,
  close: closeReleaseDialog,
  reloadBaseline: reloadReleaseBaseline,
  submit: releaseEnrollment,
  dispose: disposeEnrollmentRelease
} = useProtectionEnrollmentRelease({
  getEnrollment: id => protectionEnrollmentAPI.get(id),
  releaseEnrollment: (id, payload) => protectionEnrollmentAPI.release(id, payload),
  replaceEnrollment,
  refreshCollection: options => load(options),
  scheduleAutoRefresh,
  t,
  onAlreadyReleasing: () => ElMessage.info(t('security.enrollment.alreadyReleasing')),
  onNoSupportedFindingsUnavailable: () => ElMessage.info(t('security.enrollment.noSupportedFindingsReleaseUnavailable')),
  onReloaded: () => ElMessage.success(t('security.enrollment.releaseReloadedLatest')),
  onLoadError: error => ElMessage.error(error.message || t('security.enrollment.releaseLoadFailed')),
  onStarted: () => ElMessage.success(t('security.enrollment.releaseStarted')),
  onVersionConflict: () => ElMessage.warning(t('security.enrollment.releaseVersionConflict')),
  onSubmitError: error => ElMessage.error(error.message || t('security.common.failed'))
})
async function handleAccessRequestPageChange() {
  await loadAccessRequestQueue(accessRequestPage.value)
}

function handleGovernanceLoadError(kind, error) {
  const messageKeys = {
    findings: 'security.finding.loadFailed',
    assessments: 'security.assessment.loadFailed',
    policies: 'security.policy.loadFailed',
    exemptions: 'security.exemption.loadFailed'
  }
  ElMessage.error(error.message || t(messageKeys[kind]))
}

async function continueFindingReview(reviewedFinding, reviewContext) {
  if (reviewContext === 'queue') {
    const continuation = await nextReviewQueueFinding(reviewedFinding)
    if (continuation.pageChanged) await navigateWorkspace('review-queue')
    return continuation.finding
  }
  await load()
  await loadFindings(findingsPage.value)
  scheduleAutoRefresh({ reset: true })
  return nextDetailReviewFinding()
}

async function handlePageChange() {
  await refreshCurrentPage()
}

async function handleScopeChange() {
  closeDetail()
  await refreshChangedScope()
}

function replaceEnrollment(latest) {
  replaceCollectionRow(latest)
  replaceDetailEnrollment(latest)
}

const {
  manualRefreshing,
  handleRouteChange: handleWorkspaceRouteChange,
  mount: mountWorkspace,
  manualRefresh,
  navigateWorkspace,
  handleWorkspaceChange,
  handleReviewQueueFilterChange,
  resetReviewQueueFilters,
  handleReviewQueuePageChange,
  dispose: disposeProtectionEnrollmentWorkspace
} = useProtectionEnrollmentWorkspace({
  route,
  router,
  activeWorkspace,
  reviewQueue: {
    page: reviewQueuePage,
    applyRouteState: applyReviewQueueRouteState,
    routeQuery: reviewQueueRouteQuery,
    load: loadReviewQueue,
    prepareFilterChange: prepareReviewQueueFilterChange,
    resetFilters: resetReviewQueueFilterState
  },
  resources: {
    findingsPage,
    load,
    loadAccessRequests: loadAccessRequestQueue,
    loadGovernance,
    scheduleAutoRefresh,
    stopAutoRefresh,
    startVisibilityTracking
  },
  references: {
    loadDefinitions: loadFindingDefinitions,
    loadDetectorCapabilities,
    loadEngines
  },
  creation: { canCreate, open: openCreate }
})

const detailRefreshState = computed(() => ({
  feedback: refreshFeedback.value,
  loading: manualRefreshing.value
}))
const detailGovernance = computed(() => ({
  visible: canReadFindings.value || canReadAssessments.value,
  loading: governanceLoading.value,
  findings: findings.value,
  findingsPage: findingsPage.value,
  findingsTotal: findingsTotal.value,
  findingsPageSize,
  manualAssessments: manualAssessments.value,
  readFindings: canReadFindings.value,
  createAssessments: canCreateAssessments.value,
  reviewFindings: canReviewFindings.value,
  updateAssessments: canUpdateAssessments.value,
  revokePolicies: canRevokePolicies.value
}))
const detailExemptions = computed(() => ({
  visible: canReadExemptions.value,
  loading: exemptionsLoading.value,
  items: exemptions.value,
  focusedId: focusedExemptionID.value,
  canRevoke: canRevokeExemptions.value
}))
const detailLifecycle = computed(() => ({
  canCreate: canCreate.value,
  canUpdate: canRelease.value
}))
const detailPresentation = Object.freeze({
  resource: Object.freeze({
    name: resourceName,
    path: resourcePath,
    state: presentationState,
    releaseBasisLabel,
    releaseActorLabel,
    formatDateTime,
    discovery: discoveryPresentation,
    isZeroFindingDiscovery
  }),
  governance: Object.freeze({
    summary: normalizeDiscoverySummary,
    findings: Object.freeze({
      typeName,
      state: findingStatePresentation,
      capabilityName,
      evidenceDescription,
      evidenceAuditDescription,
      capabilityText,
      capabilityScope,
      confidenceLabel,
      decision: decisionPresentation,
      effectiveDefinitionSummary,
      baselineDescription,
      assessmentView: findingAssessmentView,
      ownerLabel,
      outletRuleDescription,
      outletAcknowledgement: outletAcknowledgementPresentation,
      formatDateTime
    }),
    assessments: Object.freeze({
      summary: assessmentSummary,
      protectionSummary: assessmentProtectionSummary,
      conclusionLabel: assessmentConclusionLabel,
      canConfigurePolicy,
      policyForAssessment
    })
  }),
  exemption: Object.freeze({
    assessmentComponent,
    state: exemptionStatePresentation,
    ownerLabel,
    actionLabel,
    formatDateTime
  }),
  owner: Object.freeze({
    label: ownerLabel,
    effectDescription: ownerEffectDescription,
    state: ownerPresentation
  })
})

watch(() => route.query, async routeQuery => {
  await handleWorkspaceRouteChange(routeQuery)
}, { immediate: true })

onMounted(async () => {
  await mountWorkspace()
})

onBeforeUnmount(() => {
  disposeProtectionEnrollmentWorkspace()
  disposeProtectionReferenceCatalog()
  disposeEnrollmentDetail()
  disposeAccessRequestReview()
  disposeEnrollmentCollection()
  disposeEnrollmentCreation()
  disposeEnrollmentReEnrollment()
  disposeEnrollmentRediscovery()
  disposeEnrollmentRelease()
  disposeFindingReview()
  disposeFindingReviewQueue()
  disposeProtectionPolicyChange()
  disposeProtectionAssessmentChange()
  disposeProtectionExemptionRevocation()
  disposeProtectionAssessmentWorkspace()
})
</script>

<style scoped>
.page { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.page-header p { max-width: 780px; margin: 10px 0 0; color: var(--addp-text-secondary); }
.page-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 10px; }
.refresh-feedback { color: var(--addp-text-tertiary); font-size: 12px; white-space: nowrap; }
.workspace-tabs { margin: -4px 0 10px; }
.workspace-tab-label { display: inline-flex; align-items: center; gap: 7px; }
:deep(.workspace-tabs .el-tabs__header) { margin-bottom: 0; }
</style>
