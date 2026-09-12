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

    <el-drawer
      v-model="createDrawer"
      :title="t('security.enrollment.create')"
      size="760px"
      destroy-on-close
      @closed="handleCreateClosed"
    >
      <div class="create-flow">
        <el-alert type="info" :closable="false" :title="t('security.enrollment.createHint')" />
        <el-form ref="createFormRef" :model="createForm" :rules="createFormRules" label-position="top">
          <el-form-item class="enrollment-resource-field" :label="t('security.enrollment.resource')" prop="resource" required>
            <ResourceTreePicker
              v-model="createForm.resource"
              api-base-url="/api/v1/meta"
              mode="item"
              :initial-locator="initialLocator"
              :engine-label="t('security.enrollment.engine')"
              :engine-placeholder="t('security.enrollment.enginePlaceholder')"
              :search-placeholder="t('security.enrollment.searchCurrentEngine')"
              :search-all-engines-placeholder="t('security.enrollment.searchAllEngines')"
              :search-empty-text="t('security.enrollment.resourceNotFound')"
              tree-height="min(52vh, 520px)"
              @update:model-value="handleResourceModelUpdate"
              @select="handleResourceSelect"
            />
          </el-form-item>
        </el-form>

        <el-skeleton v-if="selectedItemLoading" :rows="3" animated />
        <section v-else-if="selectedItem" class="selection-card">
          <div class="selection-card__title">
            <div>
              <strong>{{ selectedItem.name }}</strong>
              <span>{{ selectedItem.full_name }}</span>
            </div>
            <el-tag effect="plain">{{ itemTypeLabel(selectedItem.item_type) }}</el-tag>
          </div>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item :label="t('security.enrollment.engine')">
              {{ createForm.resource?.display?.engine_name || engineLabel(selectedItem.engine_id) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.lastScanned')">
              {{ formatDateTime(selectedItem.scanned_at) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.scope')" :span="2">
              {{ t('security.enrollment.wholeResourceScope') }}
            </el-descriptions-item>
          </el-descriptions>
          <el-alert
            v-if="existingEnrollment"
            class="existing-alert"
            type="warning"
            :closable="false"
            :title="t('security.enrollment.alreadyEnrolled')"
          />
        </section>
      </div>

      <template v-if="canCreate" #footer>
        <el-button @click="createDrawer = false">{{ t('security.common.cancel') }}</el-button>
        <el-button
          type="primary"
          :loading="saving"
          :disabled="selectedItemLoading || Boolean(existingEnrollment)"
          @click="createEnrollment"
        >
          {{ t('security.enrollment.confirmCreate') }}
        </el-button>
      </template>
    </el-drawer>

    <el-drawer v-model="detailDrawer" :title="t('security.enrollment.details')" size="min(760px, 100vw)" @closed="handleDetailClosed">
      <template v-if="detailRow">
        <div class="detail-refresh">
          <span class="refresh-feedback" aria-live="polite">{{ refreshFeedback }}</span>
          <el-button link type="primary" :icon="Refresh" :loading="manualRefreshing" @click="manualRefresh">
            {{ t('security.enrollment.refresh') }}
          </el-button>
        </div>
        <section class="detail-resource">
          <div>
            <h3>{{ resourceName(detailRow) }}</h3>
            <p>{{ resourcePath(detailRow) }}</p>
          </div>
          <el-tag :type="presentationState(detailRow).type">{{ presentationState(detailRow).label }}</el-tag>
        </section>

        <template v-if="['releasing', 'released'].includes(detailRow.state)">
          <h4>{{ t('security.enrollment.releaseAudit') }}</h4>
          <el-descriptions class="release-audit" :column="2" border>
            <el-descriptions-item :label="t('security.enrollment.releaseBasisLabel')">
              {{ releaseBasisLabel(detailRow.release_basis) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.releaseRequestedBy')">
              {{ releaseActorLabel(detailRow.release_requested_by) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.releaseRequestedAt')">
              {{ formatDateTime(detailRow.release_requested_at) }}
            </el-descriptions-item>
            <el-descriptions-item v-if="detailRow.released_at" :label="t('security.enrollment.releasedAt')">
              {{ formatDateTime(detailRow.released_at) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.releaseReasonLabel')" :span="2">
              <span class="release-reason-text">{{ detailRow.release_reason || t('security.common.notAvailable') }}</span>
            </el-descriptions-item>
          </el-descriptions>
        </template>

        <h4>{{ t('security.enrollment.discovery') }}</h4>
        <el-alert
          :type="discoveryPresentation(detailRow).alertType"
          :closable="false"
          :title="discoveryPresentation(detailRow).detailTitle"
          :description="discoveryPresentation(detailRow).detailDescription"
          show-icon
        />

        <section v-if="canReadFindings || canReadAssessments" class="finding-section">
          <div class="finding-section__header">
            <div>
              <h4>{{ t('security.finding.governanceTitle') }}</h4>
              <p>{{ t('security.finding.governanceHint') }}</p>
            </div>
            <div class="finding-section__actions">
              <el-tag v-if="normalizeDiscoverySummary(detailRow).pendingReviewCount > 0" type="warning">
                {{ t('security.finding.pendingCount', { count: normalizeDiscoverySummary(detailRow).pendingReviewCount }) }}
              </el-tag>
              <el-button
                v-if="canCreateAssessments && !['releasing', 'released'].includes(detailRow.state)"
                type="primary"
                plain
                @click="openManualAssessment"
              >
                {{ t('security.assessment.designateField') }}
              </el-button>
            </div>
          </div>

          <el-skeleton v-if="governanceLoading" :rows="3" animated />
          <template v-else>
            <EnrollmentFindingList
              v-if="canReadFindings && findings.length > 0"
              v-model:page="findingsPage"
              :findings="findings"
              :total="findingsTotal"
              :page-size="findingsPageSize"
              :can-review="canReviewFindings"
              :can-update-assessments="canUpdateAssessments"
              :can-revoke-policies="canRevokePolicies"
              :type-name="typeName"
              :finding-state-presentation="findingStatePresentation"
              :capability-name="capabilityName"
              :evidence-description="evidenceDescription"
              :evidence-audit-description="evidenceAuditDescription"
              :capability-text="capabilityText"
              :capability-scope="capabilityScope"
              :confidence-label="confidenceLabel"
              :decision-presentation="decisionPresentation"
              :effective-definition-summary="effectiveDefinitionSummary"
              :baseline-description="baselineDescription"
              :assessment-for-finding="assessmentForFinding"
              :active-assessment-for-finding="activeAssessmentForFinding"
              :assessment-protection-summary="assessmentProtectionSummary"
              :owner-label="ownerLabel"
              :outlet-rule-description="outletRuleDescription"
              :outlet-acknowledgement-presentation="outletAcknowledgementPresentation"
              :format-date-time="formatDateTime"
              :can-configure-policy="canConfigurePolicy"
              :policy-for-assessment="policyForAssessment"
              @review="openFindingReview"
              @history="openAssessmentHistory"
              @revise="openAssessmentRevision"
              @configure-policy="openPolicy"
              @restore-policy="openPolicyRestore"
              @revoke-assessment="openAssessmentRevoke"
              @page-change="loadFindings"
            />

            <EnrollmentAssessmentList
              v-if="manualAssessments.length > 0"
              :assessments="manualAssessments"
              :can-update="canUpdateAssessments"
              :can-revoke-policies="canRevokePolicies"
              :assessment-summary="assessmentSummary"
              :assessment-protection-summary="assessmentProtectionSummary"
              :assessment-conclusion-label="assessmentConclusionLabel"
              :can-configure-policy="canConfigurePolicy"
              :policy-for-assessment="policyForAssessment"
              @history="openAssessmentHistory"
              @revise="openAssessmentRevision"
              @configure-policy="openPolicy"
              @restore-policy="openPolicyRestore"
              @revoke="openAssessmentRevoke"
            />

            <el-empty
              v-if="findings.length === 0 && manualAssessments.length === 0"
              :description="t('security.finding.noGovernanceConclusions')"
              :image-size="72"
            />
          </template>
        </section>

        <EnrollmentExemptionSection
          v-if="canReadExemptions"
          ref="exemptionSectionRef"
          :loading="exemptionsLoading"
          :exemptions="exemptions"
          :focused-exemption-id="focusedExemptionID"
          :can-revoke="canRevokeExemptions"
          :assessment-component="assessmentComponent"
          :exemption-state-presentation="exemptionStatePresentation"
          :owner-label="ownerLabel"
          :action-label="actionLabel"
          :format-date-time="formatDateTime"
          @revoke="openExemptionRevoke"
        />

        <h4>{{ t('security.enrollment.ownerProtection') }}</h4>
        <el-alert
          class="owner-protection-hint"
          type="info"
          :closable="false"
          :title="t('security.enrollment.ownerProtectionHint')"
        />
        <div class="owner-detail-list">
          <div v-for="owner in detailRow.owner_progress" :key="owner.consumer_owner" class="owner-detail">
            <div>
              <strong>{{ ownerLabel(owner.consumer_owner) }}</strong>
              <span>{{ ownerEffectDescription(owner) }}</span>
            </div>
            <el-tag :type="ownerPresentation(detailRow, owner).type">{{ ownerPresentation(detailRow, owner).label }}</el-tag>
          </div>
        </div>

        <el-descriptions class="detail-facts" :column="1" border>
          <el-descriptions-item :label="t('security.enrollment.lastDiscovered')">{{ formatDateTime(detailRow.last_discovered_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.createdAt')">{{ formatDateTime(detailRow.created_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.scope')">{{ t('security.enrollment.wholeResourceScope') }}</el-descriptions-item>
        </el-descriptions>

        <el-collapse class="technical-details">
          <el-collapse-item :title="t('security.enrollment.technicalDetails')" name="technical">
            <el-descriptions :column="1" size="small">
              <el-descriptions-item :label="t('security.enrollment.fingerprint')">
                <span class="technical-value">{{ detailRow.target.resource_identity }}</span>
              </el-descriptions-item>
              <el-descriptions-item :label="t('security.enrollment.enrollmentId')">
                <span class="technical-value">{{ detailRow.id }}</span>
              </el-descriptions-item>
              <el-descriptions-item v-if="detailRow.release_source_snapshot_hash" :label="t('security.enrollment.releaseSourceSnapshot')">
                <span class="technical-value">{{ detailRow.release_source_snapshot_hash }}</span>
              </el-descriptions-item>
            </el-descriptions>
          </el-collapse-item>
        </el-collapse>

        <div class="detail-actions">
          <el-button
            v-if="canCreate && detailRow.state === 'released'"
            type="primary"
            @click="openReEnrollment(detailRow)"
          >
            {{ t('security.enrollment.reEnroll') }}
          </el-button>
          <el-button
            v-if="canRelease && ['enrolling', 'active'].includes(detailRow.state)"
            @click="openRediscovery(detailRow)"
          >
            {{ t('security.enrollment.rediscover') }}
          </el-button>
          <el-button
            v-if="canRelease && !['releasing', 'released'].includes(detailRow.state)"
            type="danger"
            plain
            @click="openRelease(detailRow, isZeroFindingDiscovery(detailRow) ? 'no_supported_findings' : 'manual')"
          >
            {{ isZeroFindingDiscovery(detailRow) ? t('security.enrollment.confirmNoProtectionNeeded') : t('security.enrollment.release') }}
          </el-button>
        </div>
      </template>
    </el-drawer>

    <el-dialog
      v-model="accessRequestDecisionDialog"
      class="addp-dialog"
      :title="accessRequestDecisionTitle"
      width="min(600px, calc(100vw - 24px))"
      :close-on-click-modal="!accessRequestDecisionSaving"
      :close-on-press-escape="!accessRequestDecisionSaving"
      :show-close="!accessRequestDecisionSaving"
      @opened="focusAccessRequestDecisionCancel"
      @closed="closeAccessRequestDecisionDialog"
    >
      <AccessRequestDecisionForm
        v-if="decidingAccessRequest"
        ref="accessRequestDecisionFormRef"
        :request="decidingAccessRequest"
        :hint="accessRequestDecisionHint"
        :conflict="accessRequestDecisionConflict"
        :reloading="accessRequestDecisionReloading"
        :form="accessRequestDecisionForm"
        :rules="accessRequestDecisionRules"
        :release-actor-label="releaseActorLabel"
        :format-date-time="formatDateTime"
        @reload="reloadAccessRequestDecisionBaseline"
      />
      <template #footer>
        <el-button ref="accessRequestDecisionCancelButton" :disabled="accessRequestDecisionSaving" @click="accessRequestDecisionDialog = false">
          {{ t('security.common.cancel') }}
        </el-button>
        <el-button
          :type="accessRequestDecision === 'reject' ? 'danger' : 'primary'"
          :loading="accessRequestDecisionSaving"
          :disabled="accessRequestDecisionConflict"
          @click="submitAccessRequestDecision"
        >
          {{ t(`security.accessRequest.confirmActions.${accessRequestDecision}`) }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="reviewDialog"
      class="addp-dialog"
      :title="t('security.finding.reviewTitle')"
      width="min(640px, calc(100vw - 24px))"
      :close-on-click-modal="!reviewSaving"
      :close-on-press-escape="!reviewSaving"
      :show-close="!reviewSaving"
      @opened="focusReviewRationale"
      @closed="closeFindingReview"
    >
      <FindingReviewForm
        v-if="reviewingFinding"
        ref="reviewFormRef"
        v-model:basis-expanded="reviewBasisExpanded"
        :finding="reviewingFinding"
        :remaining-label="reviewRemainingLabel"
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
      />
      <template #footer>
        <el-button :disabled="reviewSaving" @click="closeFindingReview">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="reviewSaving" @click="submitFindingReview">{{ t('security.finding.submitReview') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="manualAssessmentDialog"
      class="addp-dialog"
      :title="t('security.assessment.designateTitle')"
      width="min(640px, calc(100vw - 24px))"
      @opened="focusManualRationale"
    >
      <el-alert type="info" :closable="false" :title="t('security.assessment.designateHint')" />
      <el-form ref="manualAssessmentFormRef" :model="manualAssessmentForm" :rules="manualAssessmentRules" class="manual-assessment-form" label-position="top">
        <el-form-item :label="t('security.assessment.component')" prop="componentKey" required>
          <el-select
            v-model="manualAssessmentForm.componentKey"
            class="wide"
            filterable
            :placeholder="t('security.assessment.selectComponent')"
            :loading="componentsLoading"
          >
            <el-option
              v-for="option in componentOptions"
              :key="option.component.key"
              :value="option.component.key"
              :label="option.component.key"
            >
              <div class="component-option">
                <span>{{ option.component.key }}</span>
                <small>{{ option.component.value_type }}</small>
              </div>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
          <el-select v-model="manualAssessmentForm.sensitiveDataTypeID" class="wide" :placeholder="t('security.finding.selectSensitiveDataType')" @change="applyDefaultGrade">
            <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
          <el-select v-model="manualAssessmentForm.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
            <el-option v-for="item in activeGradesForType(manualAssessmentForm.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.assessment.rationale')" prop="rationale" required>
          <el-input
            ref="manualRationaleInput"
            v-model="manualAssessmentForm.rationale"
            type="textarea"
            :rows="4"
            maxlength="2000"
            show-word-limit
            :placeholder="t('security.assessment.rationalePlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="manualAssessmentDialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="manualAssessmentSaving" @click="submitManualAssessment">
          {{ t('security.assessment.confirmDesignation') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="assessmentHistoryDialog"
      class="addp-dialog"
      :title="t('security.assessment.historyTitle')"
      width="min(720px, calc(100vw - 24px))"
      @closed="closeAssessmentHistory"
    >
      <div v-loading="assessmentHistoryLoading" class="assessment-history">
        <template v-if="assessmentHistory">
          <div class="policy-target">
            <strong>{{ assessmentHistory.component_key }}</strong>
            <span>{{ t('security.assessment.historyHint') }}</span>
          </div>
          <el-timeline v-if="assessmentHistory.history?.length">
            <el-timeline-item
              v-for="revision in assessmentHistory.history"
              :key="revision.id"
              :timestamp="formatDateTime(revision.created_at)"
              :type="isCurrentAssessmentRevision(revision) ? 'primary' : undefined"
              placement="top"
            >
              <article class="assessment-history__item">
                <div class="assessment-history__header">
                  <strong>{{ t('security.assessment.revisionLabel', { revision: revision.revision }) }}</strong>
                  <el-tag v-if="isCurrentAssessmentRevision(revision)" size="small" type="primary">
                    {{ t('security.assessment.currentRevision') }}
                  </el-tag>
                  <el-tag size="small" :type="revision.conclusion === 'sensitive' ? 'success' : 'info'">
                    {{ assessmentConclusionLabel(revision.conclusion) }}
                  </el-tag>
                  <el-tag size="small" effect="plain">
                    {{ assessmentRevisionSourceLabel(revision.source_kind) }}
                  </el-tag>
                </div>
                <p class="assessment-history__summary">{{ assessmentRevisionSummary(revision) }}</p>
                <p>{{ revision.rationale }}</p>
                <small>{{ t('security.assessment.historyMeta', { actor: assessmentActorLabel(revision.created_by), time: formatDateTime(revision.created_at) }) }}</small>
              </article>
            </el-timeline-item>
          </el-timeline>
          <el-empty v-else :description="t('security.assessment.emptyHistory')" :image-size="64" />
        </template>
      </div>
      <template #footer>
        <el-button @click="assessmentHistoryDialog = false">{{ t('security.common.close') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="assessmentRevisionDialog"
      class="addp-dialog"
      :title="t('security.assessment.reviseTitle')"
      width="min(640px, calc(100vw - 24px))"
      :close-on-click-modal="!assessmentRevisionSaving"
      :close-on-press-escape="!assessmentRevisionSaving"
      :show-close="!assessmentRevisionSaving"
      @opened="focusAssessmentRevisionRationale"
      @closed="closeAssessmentRevision"
    >
      <template v-if="revisingAssessment">
        <el-alert type="info" :closable="false" :title="t('security.assessment.reviseHint')" />
        <div v-if="assessmentRevisionConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.assessment.versionConflict') }}</span>
          <el-button link type="primary" :loading="assessmentRevisionReloading" @click="reloadAssessmentRevisionBaseline">
            {{ t('security.assessment.reloadLatest') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ revisingAssessment.component_key }}</strong>
          <span>{{ assessmentConclusionLabel(revisingAssessment.current?.conclusion) }}</span>
          <span>{{ assessmentSummary(revisingAssessment) }}</span>
        </div>
        <el-form ref="assessmentRevisionFormRef" :model="assessmentRevisionForm" :rules="assessmentRevisionRules" label-position="top">
          <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
            <el-select
              v-model="assessmentRevisionForm.sensitiveDataTypeID"
              class="wide"
              :placeholder="t('security.finding.selectSensitiveDataType')"
              @change="applyAssessmentRevisionDefaultGrade"
            >
              <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
            <el-select v-model="assessmentRevisionForm.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
              <el-option v-for="item in activeGradesForType(assessmentRevisionForm.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('security.assessment.revisionRationale')" prop="rationale" required>
            <el-input
              ref="assessmentRevisionRationaleInput"
              v-model="assessmentRevisionForm.rationale"
              type="textarea"
              :rows="4"
              maxlength="2000"
              show-word-limit
              :placeholder="t('security.assessment.revisionRationalePlaceholder')"
            />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button :disabled="assessmentRevisionSaving" @click="closeAssessmentRevision">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="assessmentRevisionSaving" :disabled="assessmentRevisionConflict" @click="submitAssessmentRevision">
          {{ t('security.assessment.confirmRevision') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="assessmentRevokeDialog"
      class="addp-dialog"
      :title="t('security.assessment.revokeTitle')"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!assessmentRevokeSaving"
      :close-on-press-escape="!assessmentRevokeSaving"
      :show-close="!assessmentRevokeSaving"
      @opened="focusAssessmentRevokeCancel"
      @closed="closeAssessmentRevokeDialog"
    >
      <template v-if="revokingAssessment">
        <el-alert type="warning" :closable="false" :title="t('security.assessment.revokeHint')" />
        <div v-if="assessmentRevokeConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.assessment.revokeVersionConflict') }}</span>
          <el-button link type="primary" :loading="assessmentRevokeReloading" @click="reloadAssessmentRevokeBaseline">
            {{ t('security.assessment.reloadLatestForRevoke') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ revokingAssessment.component_key }}</strong>
          <span>{{ assessmentConclusionLabel(revokingAssessment.current?.conclusion) }}</span>
          <span>{{ assessmentSummary(revokingAssessment) }}</span>
        </div>
        <el-form ref="assessmentRevokeFormRef" :model="assessmentRevokeForm" :rules="assessmentRevokeRules" label-position="top">
          <el-form-item :label="t('security.assessment.revokeRationale')" prop="rationale" required>
            <el-input
              v-model="assessmentRevokeForm.rationale"
              type="textarea"
              :rows="4"
              maxlength="2000"
              show-word-limit
              :placeholder="t('security.assessment.revokeRationalePlaceholder')"
            />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button ref="assessmentRevokeCancelButton" :disabled="assessmentRevokeSaving" @click="closeAssessmentRevokeDialog">
          {{ t('security.common.cancel') }}
        </el-button>
        <el-button type="danger" :loading="assessmentRevokeSaving" :disabled="assessmentRevokeConflict" @click="submitAssessmentRevoke">
          {{ t('security.assessment.confirmRevoke') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="exemptionRevokeDialog"
      class="addp-dialog"
      :title="t('security.exemption.revokeTitle')"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!exemptionRevokeSaving"
      :close-on-press-escape="!exemptionRevokeSaving"
      :show-close="!exemptionRevokeSaving"
      @opened="focusExemptionRevokeCancel"
      @closed="closeExemptionRevokeDialog"
    >
      <template v-if="revokingExemption">
        <el-alert type="warning" :closable="false" :title="t('security.exemption.revokeHint')" />
        <div v-if="exemptionRevokeConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.exemption.revokeVersionConflict') }}</span>
          <el-button link type="primary" :loading="exemptionRevokeReloading" @click="reloadExemptionRevokeBaseline">
            {{ t('security.exemption.reloadLatestForRevoke') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ assessmentComponent(revokingExemption.assessment_id) }}</strong>
          <span>{{ t('security.exemption.subject') }}：{{ revokingExemption.subject_id }}</span>
          <span>{{ ownerLabel(revokingExemption.consumer_owner) }} · {{ actionLabel(revokingExemption.action) }}</span>
          <span>{{ t('security.exemption.expiresAt') }}：{{ formatDateTime(revokingExemption.current?.expires_at) }}</span>
        </div>
        <el-form ref="exemptionRevokeFormRef" :model="exemptionRevokeForm" :rules="exemptionRevokeRules" label-position="top">
          <el-form-item :label="t('security.exemption.revokeRationale')" prop="rationale" required>
            <el-input
              v-model="exemptionRevokeForm.rationale"
              type="textarea"
              :rows="4"
              maxlength="2000"
              show-word-limit
              :placeholder="t('security.exemption.revokeRationalePlaceholder')"
            />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button ref="exemptionRevokeCancelButton" :disabled="exemptionRevokeSaving" @click="exemptionRevokeDialog = false">
          {{ t('security.common.cancel') }}
        </el-button>
        <el-button type="danger" :loading="exemptionRevokeSaving" :disabled="exemptionRevokeConflict" @click="submitExemptionRevoke">
          {{ t('security.exemption.confirmRevoke') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="policyDialog"
      class="addp-dialog"
      :title="t('security.policy.title')"
      width="min(600px, calc(100vw - 24px))"
      :close-on-click-modal="!policySaving"
      :close-on-press-escape="!policySaving"
      :show-close="!policySaving"
      @open="clearPolicyValidation"
      @closed="closePolicyDialog"
    >
      <ProtectionPolicyForm
        v-if="policyAssessment"
        ref="policyFormRef"
        mode="tighten"
        :assessment="policyAssessment"
        :assessment-summary="assessmentSummary(policyAssessment)"
        :protection-summary="assessmentProtectionSummary(policyAssessment)"
        :conflict="policyVersionConflict"
        :reloading="policyReloading"
        :form="policyForm"
        :rules="policyRules"
        :effects="policyEffects"
        :effect-label="effectLabel"
        @reload="reloadPolicyBaseline"
      />
      <template #footer>
        <el-button :disabled="policySaving" @click="closePolicyDialog">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="policySaving" :disabled="policyVersionConflict" @click="savePolicy">{{ t('security.policy.confirm') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="policyRestoreDialog"
      class="addp-dialog"
      :title="t('security.policy.restoreDefault')"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!policyRestoreSaving"
      :close-on-press-escape="!policyRestoreSaving"
      :show-close="!policyRestoreSaving"
      @opened="focusPolicyRestoreCancel"
      @closed="closePolicyRestoreDialog"
    >
      <ProtectionPolicyForm
        v-if="policyRestoreAssessment"
        ref="policyRestoreFormRef"
        mode="restore"
        :assessment="policyRestoreAssessment"
        :assessment-summary="assessmentSummary(policyRestoreAssessment)"
        :protection-summary="assessmentProtectionSummary(policyRestoreAssessment)"
        :conflict="policyRestoreConflict"
        :reloading="policyRestoreReloading"
        :form="policyRestoreForm"
        :rules="policyRestoreRules"
        :effect-label="effectLabel"
        @reload="reloadPolicyRestoreBaseline"
      />
      <template #footer>
        <el-button ref="policyRestoreCancelButton" :disabled="policyRestoreSaving" @click="closePolicyRestoreDialog">{{ t('security.common.cancel') }}</el-button>
        <el-button type="danger" :loading="policyRestoreSaving" :disabled="policyRestoreConflict" @click="submitPolicyRestore">
          {{ t('security.policy.confirmRestore') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="reEnrollmentDialog"
      class="addp-dialog"
      :title="t('security.enrollment.reEnrollTitle')"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!reEnrollmentSaving"
      :close-on-press-escape="!reEnrollmentSaving"
      :show-close="!reEnrollmentSaving"
      @opened="focusReEnrollmentCancel"
      @closed="closeReEnrollmentDialog"
    >
      <template v-if="reEnrollmentSource">
        <el-alert
          type="warning"
          :closable="false"
          :title="t('security.enrollment.reEnrollWarning', { resource: resourceName(reEnrollmentSource) })"
        />
        <div v-if="reEnrollmentConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.enrollment.reEnrollmentVersionConflict') }}</span>
          <el-button link type="primary" :loading="reEnrollmentReloading" @click="reloadReEnrollmentBaseline">
            {{ t('security.enrollment.reloadLatestForReEnrollment') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ reEnrollmentSource.target_snapshot?.full_name || t('security.common.notAvailable') }}</strong>
          <span>{{ t('security.enrollment.releaseBasisLabel') }}：{{ releaseBasisLabel(reEnrollmentSource.release_basis) }}</span>
          <span>{{ t('security.enrollment.releasedAt') }}：{{ formatDateTime(reEnrollmentSource.released_at) }}</span>
          <span>{{ t('security.enrollment.releaseReasonLabel') }}：{{ reEnrollmentSource.release_reason || t('security.common.notAvailable') }}</span>
        </div>
      </template>
      <template #footer>
        <el-button ref="reEnrollmentCancelButton" :disabled="reEnrollmentSaving" @click="closeReEnrollmentDialog">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="reEnrollmentSaving" :disabled="reEnrollmentConflict" @click="submitReEnrollment">
          {{ t('security.enrollment.confirmReEnroll') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="rediscoveryDialog"
      class="addp-dialog"
      :title="t('security.enrollment.rediscoverTitle')"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!rediscoverySaving"
      :close-on-press-escape="!rediscoverySaving"
      :show-close="!rediscoverySaving"
      @opened="focusRediscoveryCancel"
      @closed="closeRediscoveryDialog"
    >
      <template v-if="rediscoveryEnrollment">
        <el-alert type="info" :closable="false" :title="t('security.enrollment.rediscoverHint')" />
        <div v-if="rediscoveryConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.enrollment.rediscoveryVersionConflict') }}</span>
          <el-button link type="primary" :loading="rediscoveryReloading" @click="reloadRediscoveryBaseline">
            {{ t('security.enrollment.reloadLatestForRediscovery') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ rediscoveryEnrollment.target_snapshot?.full_name || t('security.common.notAvailable') }}</strong>
          <span>{{ t('security.enrollment.lastDiscovered') }}：{{ formatDateTime(rediscoveryEnrollment.last_discovered_at) }}</span>
        </div>
      </template>
      <template #footer>
        <el-button ref="rediscoveryCancelButton" :disabled="rediscoverySaving" @click="closeRediscoveryDialog">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="rediscoverySaving" :disabled="rediscoveryConflict" @click="submitRediscovery">
          {{ t('security.enrollment.confirmRediscover') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="releaseDialog"
      class="addp-dialog"
      :title="releaseDialogTitle"
      width="min(560px, calc(100vw - 24px))"
      :close-on-click-modal="!releaseSaving"
      :close-on-press-escape="!releaseSaving"
      :show-close="!releaseSaving"
      @opened="focusReleaseCancel"
      @closed="closeReleaseDialog"
    >
      <el-alert type="warning" :closable="false" :title="releaseDialogWarning" />
      <div v-if="releaseConflict" class="version-conflict-notice" role="alert">
        <span>{{ t('security.enrollment.releaseVersionConflict') }}</span>
        <el-button link type="primary" :loading="releaseReloading" @click="reloadReleaseBaseline">
          {{ t('security.enrollment.reloadLatestForRelease') }}
        </el-button>
      </div>
      <el-form ref="releaseFormRef" :model="releaseForm" :rules="releaseRules" label-position="top" class="release-form">
        <el-form-item :label="t('security.enrollment.resource')">
          <span>{{ releasing?.target_snapshot?.full_name || t('security.common.notAvailable') }}</span>
        </el-form-item>
        <el-form-item :label="t('security.enrollment.releaseBasisLabel')">
          <el-tag type="info" effect="plain">{{ releaseBasisLabel(releaseBasis) }}</el-tag>
        </el-form-item>
        <el-form-item :label="t('security.enrollment.releaseReasonLabel')" prop="reason" required>
          <el-input
            v-model="releaseForm.reason"
            type="textarea"
            :rows="3"
            :placeholder="releaseReasonPlaceholder"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button ref="releaseCancelButton" :disabled="releaseSaving" @click="closeReleaseDialog">{{ t('security.common.cancel') }}</el-button>
        <el-button type="danger" :loading="releaseSaving" :disabled="releaseConflict" @click="releaseEnrollment">{{ releaseConfirmLabel }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  ResourceTreePicker,
  listResourceTreeEngines,
  navigateConsoleModuleRoute,
  openMonitorExecution,
  resolveCanonicalTabRouteState
} from '@common-ui'
import { assessmentAPI, classificationAPI, detectorCapabilityAPI, findingAPI, gradeAPI, metaAPI, protectionAccessRequestAPI, protectionBaselineAPI, protectionEnrollmentAPI, protectionExemptionAPI, protectionPolicyAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { useProtectionAccessRequestReview } from '../composables/useProtectionAccessRequestReview.mjs'
import { useProtectionAssessmentChange } from '../composables/useProtectionAssessmentChange.mjs'
import { useProtectionEnrollmentCollection } from '../composables/useProtectionEnrollmentCollection.mjs'
import { useProtectionEnrollmentReEnrollment } from '../composables/useProtectionEnrollmentReEnrollment.mjs'
import { useProtectionEnrollmentRediscovery } from '../composables/useProtectionEnrollmentRediscovery.mjs'
import { useProtectionEnrollmentRelease } from '../composables/useProtectionEnrollmentRelease.mjs'
import { useProtectionFindingReview } from '../composables/useProtectionFindingReview.mjs'
import { resolveFindingReviewQueueRouteState, useProtectionFindingReviewQueue } from '../composables/useProtectionFindingReviewQueue.mjs'
import { useProtectionEnrollmentGovernance } from '../composables/useProtectionEnrollmentGovernance.mjs'
import { useProtectionPolicyChange } from '../composables/useProtectionPolicyChange.mjs'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import EnrollmentResourceList from '../components/protection-enrollment/EnrollmentResourceList.vue'
import {
  discoveryRefreshMarker,
  findingDecisionState,
  findingOutletRules,
  findingReviewState,
  isProtectionEffectStricter,
  isResourceVersionConflict,
  isZeroFindingDiscovery,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'

const { t, locale } = useI18n()
const AccessRequestDecisionForm = defineAsyncComponent(() => import('../components/protection-enrollment/AccessRequestDecisionForm.vue'))
const AccessRequestReviewWorkspace = defineAsyncComponent(() => import('../components/protection-enrollment/AccessRequestReviewWorkspace.vue'))
const EnrollmentAssessmentList = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentAssessmentList.vue'))
const EnrollmentExemptionSection = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentExemptionSection.vue'))
const EnrollmentFindingList = defineAsyncComponent(() => import('../components/protection-enrollment/EnrollmentFindingList.vue'))
const FindingReviewQueueWorkspace = defineAsyncComponent(() => import('../components/protection-enrollment/FindingReviewQueueWorkspace.vue'))
const FindingReviewForm = defineAsyncComponent(() => import('../components/protection-enrollment/FindingReviewForm.vue'))
const ProtectionPolicyForm = defineAsyncComponent(() => import('../components/protection-enrollment/ProtectionPolicyForm.vue'))
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const WORKSPACE_TABS = ['resources', 'review-queue']
function resolveWorkspaceRouteState(routeQuery) {
  const requested = resolveCanonicalTabRouteState({ allowedTabs: WORKSPACE_TABS, defaultTab: 'resources', routeQuery })
  const reviewQueue = resolveFindingReviewQueueRouteState(routeQuery)
  const preservedQuery = requested.tab === 'review-queue'
    ? reviewQueue.query
    : (String(routeQuery.action || '') === 'enroll' ? { action: 'enroll', locator: routeQuery.locator } : {})
  return {
    ...resolveCanonicalTabRouteState({ allowedTabs: WORKSPACE_TABS, defaultTab: 'resources', routeQuery, preservedQuery }),
    reviewQueue
  }
}

const initialWorkspaceRoute = resolveWorkspaceRouteState(route.query)
const activeWorkspace = ref(initialWorkspaceRoute.tab)

const manualRefreshing = ref(false)
const saving = ref(false)
const createDrawer = ref(false)
const createFormRef = ref(null)
const detailDrawer = ref(false)
const manualAssessmentDialog = ref(false)
const manualAssessmentFormRef = ref(null)
const assessmentHistoryDialog = ref(false)
const exemptionRevokeDialog = ref(false)
const exemptionRevokeFormRef = ref(null)
const createForm = reactive({ resource: null })
const selectedItem = ref(null)
const selectedItemLoading = ref(false)
const initialLocator = ref('')
const detailRow = ref(null)
const engineNames = ref(new Map())
const focusedExemptionID = ref('')
const exemptionSectionRef = ref(null)
const componentOptions = ref([])
const componentsLoading = ref(false)
const sensitiveTypes = ref([])
const securityClassifications = ref([])
const securityGrades = ref([])
const protectionBaselines = ref([])
const detectorCapabilities = ref([])
const accessRequestDecisionFormRef = ref(null)
const accessRequestDecisionCancelButton = ref(null)
const manualRationaleInput = ref(null)
const manualAssessmentSaving = ref(false)
const manualAssessmentForm = reactive({ componentKey: '', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
const assessmentHistory = ref(null)
const assessmentHistoryLoading = ref(false)
const exemptionRevokeCancelButton = ref(null)
const exemptionRevokeSaving = ref(false)
const exemptionRevokeReloading = ref(false)
const exemptionRevokeConflict = ref(false)
const revokingExemption = ref(null)
const exemptionRevokeForm = reactive({ rationale: '' })

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
  onRowsLoaded: syncLoadedEnrollmentRows,
  onLoadError: error => ElMessage.error(error.message || t('security.common.failed')),
  onAutoRefreshTimeout: () => ElMessage.warning(t('security.enrollment.autoRefreshTimedOut'))
})

function requiredFieldRule(labelKey, options = {}) {
  return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
}

const accessRequestDecisionRules = computed(() => ({
  rationale: [requiredFieldRule('security.accessRequest.decisionRationaleLabel', { trigger: 'blur', whitespace: true })]
}))
const manualAssessmentRules = computed(() => ({
  componentKey: [requiredFieldRule('security.assessment.component')],
  sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
  securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
  rationale: [requiredFieldRule('security.assessment.rationale', { trigger: 'blur', whitespace: true })]
}))
const exemptionRevokeRules = computed(() => ({
  rationale: [requiredFieldRule('security.exemption.revokeRationale', { trigger: 'blur', whitespace: true })]
}))
async function validateRequiredForm(instanceRef) {
  if (!instanceRef.value) return false
  return instanceRef.value.validate().catch(() => false)
}

let selectedItemRequest = 0
let assessmentHistoryRequest = 0
let exemptionRevokeReloadRequest = 0
let workspaceMounted = false

const canCreate = computed(() => auth.hasPermission('security.enrollment.create'))
const createFormRules = computed(() => ({
  resource: [{
    validator: (_rule, selection, callback) => {
      const locator = String(selection?.identity?.locator || '').trim()
      if (locator && selectedItem.value) {
        callback()
        return
      }
      callback(new Error(t('security.enrollment.resourceRequired')))
    },
    trigger: 'change'
  }]
}))
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
  decisionSaving: accessRequestDecisionSaving,
  decisionReloading: accessRequestDecisionReloading,
  decisionConflict: accessRequestDecisionConflict,
  decidingRequest: decidingAccessRequest,
  decision: accessRequestDecision,
  decisionForm: accessRequestDecisionForm,
  loadQueue: loadAccessRequestQueue,
  changeScope: handleAccessRequestScopeChange,
  updateFilters: updateAccessRequestFilters,
  applyFilters: applyAccessRequestFilters,
  resetFilters: resetAccessRequestFilters,
  openDecision: openAccessRequestDecision,
  closeDecision: closeAccessRequestDecisionState,
  reloadDecisionBaseline,
  submitDecision: submitAccessRequestDecisionCommand,
  dispose: disposeAccessRequestReview
} = useProtectionAccessRequestReview({
  canReview: canReviewAccessRequests,
  listRequests: params => protectionAccessRequestAPI.reviewQueue(params),
  getRequest: id => protectionAccessRequestAPI.getForReview(id),
  decideRequest: (id, payload) => protectionAccessRequestAPI.decide(id, payload),
  onQueueLoadError: error => ElMessage.error(error.message || t('security.accessRequest.loadFailed'))
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
  revisionDialog: assessmentRevisionDialog,
  revisionFormRef: assessmentRevisionFormRef,
  revisionRationaleInput: assessmentRevisionRationaleInput,
  revisionSaving: assessmentRevisionSaving,
  revisionReloading: assessmentRevisionReloading,
  revisionConflict: assessmentRevisionConflict,
  revisionAssessment: revisingAssessment,
  revisionForm: assessmentRevisionForm,
  revisionRules: assessmentRevisionRules,
  openRevision: openAssessmentRevision,
  closeRevision: closeAssessmentRevision,
  focusRevisionRationale: focusAssessmentRevisionRationale,
  applyRevisionDefaultGrade: applyAssessmentRevisionDefaultGrade,
  reloadRevisionBaseline: reloadAssessmentRevisionBaseline,
  submitRevision: submitAssessmentRevision,
  revokeDialog: assessmentRevokeDialog,
  revokeFormRef: assessmentRevokeFormRef,
  revokeCancelButton: assessmentRevokeCancelButton,
  revokeSaving: assessmentRevokeSaving,
  revokeReloading: assessmentRevokeReloading,
  revokeConflict: assessmentRevokeConflict,
  revokeAssessment: revokingAssessment,
  revokeForm: assessmentRevokeForm,
  revokeRules: assessmentRevokeRules,
  openRevoke: openAssessmentRevoke,
  closeRevoke: closeAssessmentRevokeDialog,
  focusRevokeCancel: focusAssessmentRevokeCancel,
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
  formRef: reviewFormRef,
  finding: reviewingFinding,
  saving: reviewSaving,
  basisExpanded: reviewBasisExpanded,
  form: reviewForm,
  rules: reviewRules,
  rationalePlaceholder: reviewRationalePlaceholder,
  open: openFindingReview,
  close: closeFindingReview,
  focusRationale: focusReviewRationale,
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
  editFormRef: policyFormRef,
  editSaving: policySaving,
  editReloading: policyReloading,
  editConflict: policyVersionConflict,
  editAssessment: policyAssessment,
  editForm: policyForm,
  editRules: policyRules,
  editEffects: policyEffects,
  openEdit: openPolicy,
  closeEdit: closePolicyDialog,
  clearEditValidation: clearPolicyValidation,
  reloadEditBaseline: reloadPolicyBaseline,
  submitEdit: savePolicy,
  restoreDialog: policyRestoreDialog,
  restoreFormRef: policyRestoreFormRef,
  restoreCancelButton: policyRestoreCancelButton,
  restoreSaving: policyRestoreSaving,
  restoreReloading: policyRestoreReloading,
  restoreConflict: policyRestoreConflict,
  restoreAssessment: policyRestoreAssessment,
  restoreForm: policyRestoreForm,
  restoreRules: policyRestoreRules,
  openRestore: openPolicyRestore,
  closeRestore: closePolicyRestoreDialog,
  focusRestoreCancel: focusPolicyRestoreCancel,
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
    protectionBaselines.value = Array.isArray(context.baselines) ? context.baselines : []
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
  cancelButton: reEnrollmentCancelButton,
  saving: reEnrollmentSaving,
  reloading: reEnrollmentReloading,
  conflict: reEnrollmentConflict,
  source: reEnrollmentSource,
  open: openReEnrollment,
  close: closeReEnrollmentDialog,
  focusCancel: focusReEnrollmentCancel,
  reloadBaseline: reloadReEnrollmentBaseline,
  submit: submitReEnrollment,
  dispose: disposeEnrollmentReEnrollment
} = useProtectionEnrollmentReEnrollment({
  getEnrollment: id => protectionEnrollmentAPI.get(id),
  createReEnrollment: (id, payload) => protectionEnrollmentAPI.reEnroll(id, payload),
  replaceEnrollment,
  showCurrentCollection: enrollment => {
    detailDrawer.value = false
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
  cancelButton: rediscoveryCancelButton,
  saving: rediscoverySaving,
  reloading: rediscoveryReloading,
  conflict: rediscoveryConflict,
  enrollment: rediscoveryEnrollment,
  open: openRediscovery,
  close: closeRediscoveryDialog,
  focusCancel: focusRediscoveryCancel,
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
  formRef: releaseFormRef,
  cancelButton: releaseCancelButton,
  saving: releaseSaving,
  reloading: releaseReloading,
  conflict: releaseConflict,
  enrollment: releasing,
  basis: releaseBasis,
  form: releaseForm,
  rules: releaseRules,
  dialogTitle: releaseDialogTitle,
  dialogWarning: releaseDialogWarning,
  reasonPlaceholder: releaseReasonPlaceholder,
  confirmLabel: releaseConfirmLabel,
  open: openRelease,
  close: closeReleaseDialog,
  focusCancel: focusReleaseCancel,
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
const manualAssessments = computed(() => assessments.value.filter(item => item.current?.source_kind === 'manual'))
const refreshFeedback = computed(() => {
  if (autoRefreshActive.value) return t('security.enrollment.autoRefreshing')
  if (!lastRefreshedAt.value) return ''
  const language = locale.value === 'en' ? 'en-US' : 'zh-CN'
  return t('security.enrollment.lastRefreshed', { time: lastRefreshedAt.value.toLocaleTimeString(language) })
})
const accessRequestDecisionTitle = computed(() => t(`security.accessRequest.${accessRequestDecision.value}`))
const accessRequestDecisionHint = computed(() => t(`security.accessRequest.${accessRequestDecision.value}Hint`))
const reviewRemainingLabel = computed(() => {
  if (activeWorkspace.value === 'review-queue') {
    return t('security.finding.reviewRemainingQueue', { count: reviewQueueTotal.value })
  }
  return t('security.finding.reviewRemainingResource', { count: normalizeDiscoverySummary(detailRow.value).pendingReviewCount })
})
const existingEnrollment = computed(() => {
  const fingerprint = String(selectedItem.value?.fingerprint || '')
  return rows.value.find(row => row.target?.resource_identity === fingerprint && row.state !== 'released') || null
})

const ownerLabel = owner => t(`security.enrollment.owners.${owner}`)
const effectLabel = effect => t(`security.enrollment.effects.${effect}`)

function resourceName(row) {
  const fullName = String(row.target_snapshot?.full_name || '').trim()
  if (!fullName) return t('security.enrollment.unknownResource')
  const separator = ['object', 'file', 'directory'].includes(String(row.target_snapshot?.item_type || '').toLowerCase()) ? '/' : '.'
  return fullName.split(separator).filter(Boolean).at(-1) || fullName
}

function resourcePath(row) {
  return String(row.target_snapshot?.full_name || '').trim() || t('security.enrollment.snapshotUnavailable')
}

function engineLabel(engineID) {
  const id = Number(engineID || 0)
  return engineNames.value.get(id) || t('security.enrollment.engineId', { id: id || '-' })
}

function itemTypeLabel(itemType) {
  const key = String(itemType || 'unknown').toLowerCase()
  const translated = t(`security.enrollment.itemTypes.${key}`)
  return translated === `security.enrollment.itemTypes.${key}` ? key : translated
}

function formatDateTime(value) {
  if (!value) return t('security.common.notAvailable')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? t('security.common.notAvailable') : date.toLocaleString()
}

function releaseBasisLabel(basis) {
  const normalized = String(basis || '').trim()
  if (!['manual', 'no_supported_findings'].includes(normalized)) return t('security.common.notAvailable')
  return t(`security.enrollment.releaseBases.${normalized}`)
}

function releaseActorLabel(actorID) {
  const normalized = Number(actorID)
  return Number.isInteger(normalized) && normalized > 0
    ? t('security.enrollment.userId', { id: normalized })
    : t('security.common.notAvailable')
}

function discoveryPresentation(row) {
  const summary = normalizeDiscoverySummary(row)
  if (summary.status !== 'completed') {
    return {
      type: 'info', alertType: 'info', label: t('security.enrollment.discoveryNotCompleted'),
      detailTitle: t('security.enrollment.discoveryNotCompletedTitle'),
      detailDescription: t('security.enrollment.discoveryNotCompletedDescription')
    }
  }
  if (summary.findingCount === 0) {
    return {
      type: 'success', alertType: 'info', label: t('security.enrollment.discoveryZeroFindings'),
      detailTitle: t('security.enrollment.discoveryZeroFindingsTitle'),
      detailDescription: t('security.enrollment.discoveryZeroFindingsDescription')
    }
  }
  if (summary.pendingReviewCount === 0) {
    return {
      type: 'success', alertType: 'success', label: t('security.enrollment.discoveryReviewCompleted'),
      detailTitle: t('security.enrollment.discoveryReviewCompletedTitle', { count: summary.findingCount }),
      detailDescription: t('security.enrollment.discoveryReviewCompletedDescription')
    }
  }
  return {
    type: 'warning', alertType: 'warning', label: t('security.enrollment.discoveryPendingCount', { count: summary.pendingReviewCount }),
    detailTitle: t('security.enrollment.discoveryFindingCountTitle', { count: summary.findingCount }),
    detailDescription: t('security.enrollment.discoveryFindingCountDescription', { count: summary.pendingReviewCount })
  }
}

function presentationState(row) {
  if (row.state === 'released') return { type: 'info', label: t('security.enrollment.states.released'), description: t('security.enrollment.stateDescriptions.released') }
  if (row.state === 'releasing') return { type: 'warning', label: t('security.enrollment.states.releasing'), description: t('security.enrollment.stateDescriptions.releasing') }
  const owners = Array.isArray(row.owner_progress) ? row.owner_progress : []
  if (owners.length > 0 && owners.every(owner => owner.acknowledged && owner.projection_state === 'active')) {
    return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
  }
  if (row.state === 'active') return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
  const activeOwners = owners.filter(owner => owner.acknowledged && owner.projection_state === 'active').length
  if (activeOwners > 0) return { type: 'primary', label: t('security.enrollment.states.partiallyActive'), description: t('security.enrollment.stateDescriptions.partiallyActive', { count: activeOwners }) }
  if (row.state === 'activating') return { type: 'warning', label: t('security.enrollment.states.activating'), description: t('security.enrollment.stateDescriptions.activating') }
  return { type: 'primary', label: t('security.enrollment.states.enrolling'), description: t('security.enrollment.stateDescriptions.enrolling') }
}

function ownerPresentation(row, owner) {
  if (row.state === 'released') return { type: 'info', label: t('security.enrollment.ownerStates.released') }
  if (!owner.acknowledged) return { type: 'warning', label: t('security.enrollment.ownerStates.waiting') }
  if (owner.projection_state === 'active') return { type: 'success', label: t('security.enrollment.ownerStates.active') }
  return { type: 'info', label: t('security.enrollment.ownerStates.denied') }
}

function ownerEffectDescription(owner) {
  const rules = Array.isArray(owner.rules) ? owner.rules : []
  if (!owner.acknowledged) return t('security.enrollment.ownerEffectWaiting')
  if (owner.projection_state === 'enrolling') {
    return owner.consumer_owner === 'manager'
      ? t('security.enrollment.ownerEffectDeniedPendingRule')
      : t('security.enrollment.ownerEffectDeniedUnsupported')
  }
  return rules.length
    ? t('security.enrollment.ownerEffectRequirements', {
        rules: rules.map(rule => t('security.finding.outletRule', {
          action: actionLabel(rule.action), effect: effectLabel(rule.effect)
        })).join('；')
      })
    : t('security.enrollment.ownerEffectActive')
}

function typeName(id) {
  const match = sensitiveTypes.value.find(item => String(item.id) === String(id))
  return match?.name || t('security.finding.typeId', { id })
}

function classificationName(id) {
  const match = securityClassifications.value.find(item => String(item.id) === String(id))
  return match?.name || t('security.finding.classificationId', { id })
}

function gradeName(id) {
  const match = securityGrades.value.find(item => String(item.id) === String(id))
  return match?.name || t('security.finding.gradeId', { id })
}

function confidenceLabel(value) {
  const confidence = Number(value)
  return Number.isFinite(confidence) ? `${Math.round(confidence * 100)}%` : t('security.common.notAvailable')
}

function evidenceDescription(finding) {
  const rule = String(finding?.evidence?.matched_rule || '')
  if (rule === 'terminal_field_name') return t('security.finding.evidenceRules.terminalFieldName', { type: finding?.evidence?.field_type || '-' })
  if (rule === 'exact_ascii_digit_run') return t('security.finding.evidenceRules.exactDigitRun', { count: Number(finding?.evidence?.match_count || 0) })
  return t('security.finding.evidenceRules.detectorMatch')
}

function capabilityText(finding, key) {
  const i18nKey = String(finding?.explanation?.capability?.[key] || '')
  if (!i18nKey) return t('security.common.notAvailable')
  const translated = t(i18nKey)
  return translated === i18nKey ? i18nKey : translated
}

function capabilityScope(finding) {
  const capability = finding?.explanation?.capability || {}
  const itemTypes = Array.isArray(capability.supported_item_types)
    ? capability.supported_item_types.map(itemTypeLabel).join('、')
    : t('security.common.notAvailable')
  const fieldTypes = Array.isArray(capability.supported_field_types) && capability.supported_field_types.length
    ? capability.supported_field_types.map(type => t(`security.detector.fieldTypes.${type}`)).join('、')
    : t('security.finding.ruleAudit.notApplicable')
  return t('security.finding.ruleAudit.scopeValue', {
    target: t(`security.detector.targets.${capability.target_kind}`),
    evidence: t(`security.detector.evidenceSources.${capability.evidence_source}`),
    itemTypes,
    fieldTypes
  })
}

function evidenceAuditDescription(finding) {
  const evidence = finding?.evidence || {}
  if (evidence.matched_rule === 'terminal_field_name') {
    return t('security.finding.ruleAudit.metadataEvidence', {
      component: evidence.component_key || finding.component_key,
      terminal: evidence.semantic_terminal || '-',
      normalized: evidence.normalized_terminal || '-',
      alias: evidence.matched_alias || '-',
      fieldType: evidence.field_type || '-'
    })
  }
  if (evidence.matched_rule === 'exact_ascii_digit_run') {
    return t('security.finding.ruleAudit.documentEvidence', {
      count: Number(evidence.match_count || 0),
      rule: evidence.matched_rule
    })
  }
  return JSON.stringify(evidence)
}

function capabilityName(finding) {
  const key = String(finding?.explanation?.capability?.name_i18n_key || '')
  if (key) {
    const translated = t(key)
    if (translated !== key) return translated
  }
  return String(finding?.detector_version || t('security.common.notAvailable'))
}

function decisionPresentation(finding) {
  const state = findingDecisionState(finding)
  const types = {
    automatic: 'success', formal: 'success', awaiting_review: 'warning', detector_inactive: 'info',
    baseline_missing: 'danger', rejected: 'info', revoked: 'info', superseded: 'info'
  }
  return { type: types[state], label: t(`security.finding.decisionStates.${state}`) }
}

function effectiveDefinitionSummary(finding) {
  const explanation = finding?.explanation || {}
  if (!explanation.effective_sensitive_data_type_id || !explanation.effective_security_classification_id || !explanation.effective_security_grade_id) {
    return t('security.finding.noEffectiveDefinition')
  }
  return t('security.finding.effectiveDefinition', {
    type: typeName(explanation.effective_sensitive_data_type_id),
    classification: classificationName(explanation.effective_security_classification_id),
    grade: gradeName(explanation.effective_security_grade_id)
  })
}

function baselineDescription(finding) {
  const baseline = finding?.explanation?.baseline
  if (!baseline) return t('security.finding.noEffectiveBaseline')
  if (baseline.effect === 'mask') {
    return t('security.finding.baselineMask', { prefix: baseline.keep_prefix, suffix: baseline.keep_suffix })
  }
  return t('security.finding.baselineEffect', { effect: effectLabel(baseline.effect) })
}

function actionLabel(action) {
  const normalized = String(action || '')
  const translated = t(`security.finding.actions.${normalized}`)
  return translated === `security.finding.actions.${normalized}` ? normalized : translated
}

function assessmentComponent(assessmentID) {
  return assessments.value.find(item => item.id === assessmentID)?.component_key || t('security.exemption.unknownField')
}

function exemptionStatePresentation(exemption) {
  const state = String(exemption?.effective_state || 'expired')
  const types = { active: 'warning', expired: 'info', revoked: 'info' }
  return { type: types[state] || 'info', label: t(`security.exemption.states.${state}`) }
}

function outletRuleDescription(finding, owner) {
  const outlet = findingOutletRules(finding, owner)
  if (!outlet) return t('security.finding.outletUnavailable')
  if (!outlet.rules.length) {
    return outlet.projectionState === 'enrolling'
      ? t('security.finding.outletConservativeDeny')
      : t('security.finding.outletNoFieldRule')
  }
  return outlet.rules.map(rule => t('security.finding.outletRule', {
    action: actionLabel(rule.action), effect: effectLabel(rule.effect)
  })).join('；')
}

function outletAcknowledgementPresentation(outlet) {
  return outlet?.acknowledged
    ? { type: 'success', label: t('security.finding.outletAcknowledged') }
    : { type: 'warning', label: t('security.finding.outletWaiting') }
}

function findingStatePresentation(finding) {
  const state = findingReviewState(finding)
  const types = { pending: 'warning', confirm: 'success', adjust: 'primary', reject: 'info' }
  return { type: types[state], label: t(`security.finding.states.${state}`) }
}

function activeAssessmentForFinding(finding) {
  const assessment = assessmentForFinding(finding)
  return assessment?.current?.conclusion === 'sensitive' ? assessment : null
}

function assessmentForFinding(finding) {
  const id = String(finding?.explanation?.assessment_id || '')
  if (!id) return null
  return assessments.value.find(item => item.id === id) || null
}

function assessmentSummary(assessment) {
  return assessmentRevisionSummary(assessment.current)
}

function assessmentRevisionSummary(revision) {
  return t('security.assessment.summary', {
    type: typeName(revision?.sensitive_data_type_id),
    classification: classificationName(revision?.security_classification_id),
    grade: gradeName(revision?.security_grade_id)
  })
}

function assessmentConclusionLabel(conclusion) {
  const normalized = conclusion === 'sensitive' ? 'sensitive' : 'not_sensitive'
  return t(`security.assessment.conclusions.${normalized}`)
}

function assessmentRevisionSourceLabel(sourceKind) {
  const normalized = sourceKind === 'finding' ? 'finding' : 'manual'
  return t(`security.assessment.sources.${normalized}`)
}

function assessmentActorLabel(actorID) {
  const normalized = Number(actorID)
  return Number.isInteger(normalized) && normalized > 0
    ? t('security.enrollment.userId', { id: normalized })
    : t('security.common.notAvailable')
}

function isCurrentAssessmentRevision(revision) {
  return Number(revision?.revision) === Number(assessmentHistory.value?.current_revision)
}

function baselineForAssessment(assessment) {
  return protectionBaselines.value.find(item => item.enabled
    && String(item.sensitive_data_type_id) === String(assessment?.current?.sensitive_data_type_id)
    && String(item.security_grade_id) === String(assessment?.current?.security_grade_id)) || null
}

function policyForAssessment(assessment) {
  return policies.value.find(item => item.assessment_id === assessment?.id
    && item.consumer_owner === 'manager'
    && item.action === 'preview') || null
}

function stricterPolicyEffects(assessment) {
  const baseline = baselineForAssessment(assessment)
  return ['mask', 'suppress', 'deny'].filter(effect => isProtectionEffectStricter(effect, baseline?.effect))
}

function canConfigurePolicy(assessment) {
  if (!canReadPolicies.value || stricterPolicyEffects(assessment).length === 0) return false
  return policyForAssessment(assessment) ? canUpdatePolicies.value : canCreatePolicies.value
}

function assessmentProtectionSummary(assessment) {
  const baseline = baselineForAssessment(assessment)
  if (!baseline) return t('security.policy.baselineMissing')
  const policy = policyForAssessment(assessment)
  if (policy?.state === 'active' && isProtectionEffectStricter(policy.current?.effect, baseline.effect)) {
    return t('security.policy.activeSummary', { baseline: effectLabel(baseline.effect), policy: effectLabel(policy.current?.effect) })
  }
  return t('security.policy.defaultSummary', { baseline: effectLabel(baseline.effect) })
}

function activeGradesForType(typeID) {
  const activeGradeIDs = new Set(protectionBaselines.value
    .filter(item => item.enabled && String(item.sensitive_data_type_id) === String(typeID))
    .map(item => String(item.security_grade_id)))
  return securityGrades.value.filter(item => activeGradeIDs.has(String(item.id)))
}

async function loadFindingDefinitions() {
  if (sensitiveTypes.value.length && securityClassifications.value.length && securityGrades.value.length && protectionBaselines.value.length) return
  const [types, classifications, grades, baselines] = await Promise.all([sensitiveDataTypeAPI.list(), classificationAPI.list(), gradeAPI.list(), protectionBaselineAPI.list()])
  sensitiveTypes.value = Array.isArray(types) ? types : []
  securityClassifications.value = Array.isArray(classifications) ? classifications : []
  securityGrades.value = Array.isArray(grades) ? grades : []
  protectionBaselines.value = Array.isArray(baselines) ? baselines : []
}

async function loadDetectorCapabilities() {
  if (detectorCapabilities.value.length) return
  const response = await detectorCapabilityAPI.list()
  detectorCapabilities.value = Array.isArray(response) ? response : []
}

async function handleAccessRequestPageChange() {
  await loadAccessRequestQueue(accessRequestPage.value)
}

async function openAccessRequestAuthorization(row) {
  try {
    const enrollment = await protectionEnrollmentAPI.get(row.enrollment_id)
    await openDetail(enrollment, row.exemption_id)
  } catch (error) {
    ElMessage.error(error.message || t('security.exemption.loadFailed'))
  }
}

function focusAccessRequestDecisionCancel() {
  nextTick(() => {
    accessRequestDecisionFormRef.value?.clearValidate()
    const cancelButton = accessRequestDecisionCancelButton.value?.$el || accessRequestDecisionCancelButton.value
    cancelButton?.focus?.()
  })
}

function closeAccessRequestDecisionDialog() {
  closeAccessRequestDecisionState()
}

async function reloadAccessRequestDecisionBaseline() {
  const result = await reloadDecisionBaseline()
  if (result.status === 'reloaded') {
    ElMessage.success(t('security.accessRequest.decisionReloadedLatest'))
    focusAccessRequestDecisionCancel()
    return
  }
  if (result.status === 'already_processed') {
    await loadAccessRequestQueue(accessRequestPage.value)
    ElMessage.info(t('security.accessRequest.alreadyProcessed'))
    return
  }
  if (result.status === 'failed') ElMessage.error(result.error.message || t('security.accessRequest.loadFailed'))
}

async function submitAccessRequestDecision() {
  if (!decidingAccessRequest.value) return
  if (!await validateRequiredForm(accessRequestDecisionFormRef)) return
  const result = await submitAccessRequestDecisionCommand()
  if (result.status === 'succeeded') {
    await Promise.all([loadAccessRequestQueue(), load({ background: true })])
    ElMessage.success(t(`security.accessRequest.${result.decision}d`))
    return
  }
  if (result.status === 'version_conflict') {
    ElMessage.warning(t('security.accessRequest.decisionVersionConflict'))
    return
  }
  if (result.status === 'expired') {
    await loadAccessRequestQueue(accessRequestPage.value)
    ElMessage.info(t('security.accessRequest.expiredBeforeDecision'))
    return
  }
  if (result.status === 'failed') ElMessage.error(result.error.message || t('security.accessRequest.decisionFailed'))
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

async function loadGovernance(page = findingsPage.value) {
  await Promise.all([
    loadGovernanceData(page),
    loadFindingDefinitions().catch(error => ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed')))
  ])
}

function focusExemptionRevokeCancel() {
  nextTick(() => {
    exemptionRevokeFormRef.value?.clearValidate()
    const cancelButton = exemptionRevokeCancelButton.value?.$el || exemptionRevokeCancelButton.value
    cancelButton?.focus?.()
  })
}

function openExemptionRevoke(exemption) {
  if (!exemption?.id || exemption.effective_state !== 'active') return
  exemptionRevokeReloadRequest += 1
  revokingExemption.value = exemption
  exemptionRevokeForm.rationale = ''
  exemptionRevokeConflict.value = false
  exemptionRevokeDialog.value = true
}

function closeExemptionRevokeDialog() {
  exemptionRevokeReloadRequest += 1
  revokingExemption.value = null
  exemptionRevokeForm.rationale = ''
  exemptionRevokeConflict.value = false
  exemptionRevokeReloading.value = false
}

async function reloadExemptionRevokeBaseline() {
  if (!revokingExemption.value?.id) return
  const request = ++exemptionRevokeReloadRequest
  exemptionRevokeReloading.value = true
  try {
    const latest = await protectionExemptionAPI.get(revokingExemption.value.id)
    if (request !== exemptionRevokeReloadRequest) return
    replaceExemption(latest)
    if (latest?.effective_state !== 'active') {
      exemptionRevokeDialog.value = false
      ElMessage.info(t('security.exemption.alreadyInactive'))
      return
    }
    revokingExemption.value = latest
    exemptionRevokeForm.rationale = ''
    exemptionRevokeConflict.value = false
    ElMessage.success(t('security.exemption.revokeReloadedLatest'))
    focusExemptionRevokeCancel()
  } catch (error) {
    if (request !== exemptionRevokeReloadRequest) return
    ElMessage.error(error.message || t('security.exemption.loadFailed'))
  } finally {
    if (request === exemptionRevokeReloadRequest) exemptionRevokeReloading.value = false
  }
}

async function submitExemptionRevoke() {
  if (!revokingExemption.value?.id) return
  if (!await validateRequiredForm(exemptionRevokeFormRef)) return
  exemptionRevokeSaving.value = true
  try {
    await protectionExemptionAPI.revoke(revokingExemption.value.id, {
      version: Number(revokingExemption.value.version),
      rationale: exemptionRevokeForm.rationale.trim()
    })
    exemptionRevokeDialog.value = false
    await Promise.all([loadExemptions(), load({ background: true })])
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.exemption.revoked'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      exemptionRevokeConflict.value = true
      ElMessage.warning(t('security.exemption.revokeVersionConflict'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    exemptionRevokeSaving.value = false
  }
}

function applyDefaultGrade(typeID) {
  const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
  manualAssessmentForm.securityGradeID = String(selectedType?.default_security_grade_id || '')
}

async function openManualAssessment() {
  manualAssessmentForm.componentKey = ''
  manualAssessmentForm.sensitiveDataTypeID = ''
  manualAssessmentForm.securityGradeID = ''
  manualAssessmentForm.rationale = ''
  componentOptions.value = []
  manualAssessmentDialog.value = true
  componentsLoading.value = true
  try {
    const [response] = await Promise.all([
      protectionEnrollmentAPI.components(detailRow.value.id),
      loadFindingDefinitions(),
      loadAssessments()
    ])
    componentOptions.value = Array.isArray(response?.data) ? response.data : []
  } catch (error) {
    ElMessage.error(error.message || t('security.assessment.componentsLoadFailed'))
  } finally {
    componentsLoading.value = false
  }
}

function focusManualRationale() {
  nextTick(() => {
    manualAssessmentFormRef.value?.clearValidate()
    manualRationaleInput.value?.focus?.()
  })
}

async function submitManualAssessment() {
  if (!await validateRequiredForm(manualAssessmentFormRef)) return
  manualAssessmentSaving.value = true
  try {
    await assessmentAPI.create({
      enrollment_id: detailRow.value.id,
      enrollment_version: Number(detailRow.value.version),
      component_key: manualAssessmentForm.componentKey,
      sensitive_data_type_id: Number(manualAssessmentForm.sensitiveDataTypeID),
      security_grade_id: Number(manualAssessmentForm.securityGradeID),
      rationale: manualAssessmentForm.rationale.trim()
    })
    manualAssessmentDialog.value = false
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.assessment.designated'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    manualAssessmentSaving.value = false
  }
}

async function openAssessmentHistory(assessment) {
  if (!assessment?.id) return
  const request = ++assessmentHistoryRequest
  assessmentHistory.value = null
  assessmentHistoryDialog.value = true
  assessmentHistoryLoading.value = true
  try {
    const detail = await assessmentAPI.get(assessment.id)
    if (request !== assessmentHistoryRequest) return
    assessmentHistory.value = detail
  } catch (error) {
    if (request !== assessmentHistoryRequest) return
    assessmentHistoryDialog.value = false
    ElMessage.error(error.message || t('security.assessment.historyLoadFailed'))
  } finally {
    if (request === assessmentHistoryRequest) assessmentHistoryLoading.value = false
  }
}

function closeAssessmentHistory() {
  assessmentHistoryRequest += 1
  assessmentHistory.value = null
  assessmentHistoryLoading.value = false
}

async function nextDetailReviewFinding() {
  const row = detailRow.value
  if (!row?.id || !row.latest_source_snapshot_hash || !row.latest_discovery_execution_id) return null
  const response = await findingAPI.list({
    enrollment_id: row.id,
    source_snapshot_hash: row.latest_source_snapshot_hash,
    discovery_execution_id: row.latest_discovery_execution_id,
    review_state: 'pending',
    page: 1,
    page_size: 1
  })
  return Array.isArray(response?.data) ? response.data[0] || null : null
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

async function loadEngines() {
  try {
    const engines = await listResourceTreeEngines('/api/v1/meta')
    engineNames.value = new Map(engines.map(engine => [Number(engine.id), engine.name]))
  } catch {
    engineNames.value = new Map()
  }
}

async function syncLoadedEnrollmentRows({ rows: loadedRows, options }) {
  const previousDetailMarker = discoveryRefreshMarker(detailRow.value)
  if (detailRow.value) {
    const visibleDetail = loadedRows.find(row => row.id === detailRow.value.id)
    detailRow.value = visibleDetail || await protectionEnrollmentAPI.get(detailRow.value.id)
  }
  if (options?.syncFindings && detailRow.value && discoveryRefreshMarker(detailRow.value) !== previousDetailMarker) {
    findingsPage.value = 1
    await loadGovernance(1)
  }
}

async function manualRefresh() {
  if (manualRefreshing.value) return
  manualRefreshing.value = true
  try {
    if (activeWorkspace.value === 'review-queue') {
      await loadRoutedReviewQueue(reviewQueuePage.value)
    } else {
      await Promise.all([load({ background: true }), loadAccessRequestQueue()])
      await loadGovernance(findingsPage.value)
      scheduleAutoRefresh({ reset: true })
    }
  } finally {
    manualRefreshing.value = false
  }
}

async function handlePageChange() {
  await refreshCurrentPage()
}

async function handleScopeChange() {
  detailDrawer.value = false
  await refreshChangedScope()
}

async function navigateWorkspace(workspace, history = 'replace') {
  const query = workspace === 'review-queue' ? reviewQueueRouteQuery() : {}
  const location = { path: '/protection-enrollments', query }
  if (router.resolve(location).fullPath === route.fullPath) return
  await navigateConsoleModuleRoute(router, 'security', location, { history })
}

async function loadRoutedReviewQueue(requestedPage = reviewQueuePage.value) {
  const expectedPage = Number(requestedPage) || 1
  const loaded = await loadReviewQueue(expectedPage)
  if (loaded && reviewQueuePage.value !== expectedPage) await navigateWorkspace('review-queue')
  return loaded
}

async function handleWorkspaceChange(workspace) {
  stopAutoRefresh()
  await navigateWorkspace(workspace)
}

async function handleReviewQueueFilterChange() {
  prepareReviewQueueFilterChange()
  await navigateWorkspace('review-queue')
}

async function resetReviewQueueFilters() {
  resetReviewQueueFilterState()
  await navigateWorkspace('review-queue')
}

async function handleReviewQueuePageChange() {
  await navigateWorkspace('review-queue')
}

async function openReviewQueueResource(finding) {
  try {
    const enrollment = await protectionEnrollmentAPI.get(finding.enrollment_id)
    await navigateWorkspace('resources', 'push')
    openDetail(enrollment)
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  }
}

function openCreate(locator = '') {
  createForm.resource = null
  selectedItem.value = null
  initialLocator.value = String(locator || '').trim()
  createDrawer.value = true
}

function handleResourceModelUpdate(selection) {
  if (String(selection?.identity?.locator || '').trim()) return
  selectedItemRequest += 1
  selectedItem.value = null
  selectedItemLoading.value = false
}

async function handleResourceSelect(selection) {
  const request = ++selectedItemRequest
  selectedItem.value = null
  const itemID = Number(selection?.identity?.item_id || 0)
  if (!itemID) return
  selectedItemLoading.value = true
  try {
    const item = await metaAPI.getItem(itemID)
    if (request === selectedItemRequest) {
      selectedItem.value = item
      await nextTick()
      await createFormRef.value?.validateField('resource').catch(() => false)
    }
  } catch (error) {
    if (request === selectedItemRequest) ElMessage.error(error.message || t('security.enrollment.itemLoadFailed'))
  } finally {
    if (request === selectedItemRequest) selectedItemLoading.value = false
  }
}

async function createEnrollment() {
  const valid = await createFormRef.value?.validate().catch(() => false)
  if (valid === false) return
  const locator = String(createForm.resource?.identity?.locator || '').trim()
  if (existingEnrollment.value) return ElMessage.warning(t('security.enrollment.alreadyEnrolled'))
  saving.value = true
  try {
    await protectionEnrollmentAPI.create({ locator })
    listScope.value = 'current'
    currentPage.value = 1
    await load()
    scheduleAutoRefresh({ reset: true })
    createDrawer.value = false
    ElMessage.success(t('security.enrollment.created'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    saving.value = false
  }
}

async function focusExemptionCard() {
  if (!focusedExemptionID.value) return
  await nextTick()
  await exemptionSectionRef.value?.focusFocused()
}

async function openDetail(row, exemptionID = '') {
  detailRow.value = row
  resetGovernance()
  focusedExemptionID.value = exemptionID
  detailDrawer.value = true
  await loadGovernance(1)
  await focusExemptionCard()
}

function handleDetailClosed() {
  resetGovernance()
  focusedExemptionID.value = ''
  detailRow.value = null
}

function replaceEnrollment(latest) {
  replaceCollectionRow(latest)
  if (detailRow.value?.id === latest?.id) detailRow.value = latest
}

async function handleCreateClosed() {
  selectedItemRequest += 1
  createForm.resource = null
  selectedItem.value = null
  selectedItemLoading.value = false
  initialLocator.value = ''
  if (route.query.action === 'enroll' || route.query.locator) {
    await navigateConsoleModuleRoute(router, 'security', { path: '/protection-enrollments' }, { history: 'replace' })
  }
}

watch(() => route.query, async routeQuery => {
  const routeState = resolveWorkspaceRouteState(routeQuery)
  if (routeState.changed) {
    const location = { path: '/protection-enrollments', query: routeState.query }
    await navigateConsoleModuleRoute(router, 'security', location, { history: 'replace' })
    return
  }
  activeWorkspace.value = routeState.tab
  applyReviewQueueRouteState(routeState.reviewQueue)
  if (!workspaceMounted) return
  if (routeState.tab === 'review-queue') {
    stopAutoRefresh()
    await Promise.all([loadRoutedReviewQueue(routeState.reviewQueue.page), loadFindingDefinitions(), loadDetectorCapabilities()])
  } else {
    await Promise.all([load(), loadAccessRequestQueue()])
    scheduleAutoRefresh({ reset: true })
  }
}, { immediate: true })

watch(
  () => [route.query.action, route.query.locator],
  ([action, locator]) => {
    if (action === 'enroll' && canCreate.value) openCreate(locator)
  },
  { immediate: true }
)

onMounted(async () => {
  startVisibilityTracking()
  workspaceMounted = true
  if (activeWorkspace.value === 'review-queue') {
    await Promise.all([loadRoutedReviewQueue(reviewQueuePage.value), loadFindingDefinitions(), loadDetectorCapabilities()])
  } else {
    await Promise.all([load(), loadAccessRequestQueue()])
    scheduleAutoRefresh({ reset: true })
  }
  await loadEngines()
})

onBeforeUnmount(() => {
  disposeAccessRequestReview()
  disposeEnrollmentCollection()
  disposeEnrollmentReEnrollment()
  disposeEnrollmentRediscovery()
  disposeEnrollmentRelease()
  disposeFindingReview()
  disposeFindingReviewQueue()
  disposeProtectionPolicyChange()
  disposeProtectionAssessmentChange()
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
.create-flow { display: flex; flex-direction: column; gap: 18px; }
.selection-card { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.selection-card__title { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.selection-card__title div { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
.selection-card__title strong { font-size: 16px; }
.selection-card__title span { overflow: hidden; color: var(--addp-text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.existing-alert { margin-top: 14px; }
.detail-resource { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; padding-bottom: 18px; border-bottom: 1px solid var(--addp-border-color); }
.detail-refresh { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin: -8px 0 8px; }
.detail-resource h3 { margin: 0; font-size: 20px; }
.detail-resource p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.release-audit { margin-bottom: 4px; }
.release-reason-text { white-space: pre-wrap; overflow-wrap: anywhere; }
h4 { margin: 24px 0 12px; }
.owner-protection-hint { margin-bottom: 12px; }
.owner-detail-list { display: flex; flex-direction: column; gap: 10px; }
.owner-detail { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.owner-detail div { display: flex; flex-direction: column; gap: 5px; }
.owner-detail span { color: var(--addp-text-secondary); font-size: 12px; }
.finding-section { margin-top: 24px; }
.finding-section__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-section__header h4 { margin: 0; }
.finding-section__header p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.finding-section__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.assessment-history { min-height: 120px; }
.assessment-history :deep(.el-timeline) { padding-left: 6px; }
.assessment-history__item { padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.assessment-history__header { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; }
.assessment-history__item p { margin: 7px 0 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.assessment-history__item .assessment-history__summary { color: var(--addp-text-primary); font-weight: 600; }
.assessment-history__item small { display: block; margin-top: 8px; color: var(--addp-text-tertiary); }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
.form-help { display: block; margin-top: 6px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.manual-assessment-form { margin-top: 16px; }
.component-option { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.component-option small { color: var(--addp-text-tertiary); }
.wide { width: 100%; }
.detail-facts { margin-top: 22px; }
.technical-details { margin-top: 18px; }
.technical-value { overflow-wrap: anywhere; font-family: monospace; color: var(--addp-text-secondary); }
.detail-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
.release-form { margin-top: 16px; }
:deep(.el-drawer__body) { padding-top: 8px; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
  .finding-section__header { flex-direction: column; }
  .finding-section__actions { width: 100%; justify-content: space-between; }
}
</style>
