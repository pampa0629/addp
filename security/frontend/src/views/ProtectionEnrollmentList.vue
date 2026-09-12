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
    <el-card v-if="canReviewAccessRequests" class="access-review-card" shadow="never">
      <template #header>
        <div class="access-review-card__header">
          <div>
            <strong>{{ t('security.accessRequest.reviewTitle') }}</strong>
            <p>{{ t('security.accessRequest.reviewDescription') }}</p>
          </div>
          <div class="access-review-card__scope">
            <el-radio-group v-model="accessRequestScope" size="small" @change="handleAccessRequestScopeChange">
              <el-radio-button value="pending">{{ t('security.accessRequest.scopes.pending') }}</el-radio-button>
              <el-radio-button value="history">{{ t('security.accessRequest.scopes.history') }}</el-radio-button>
            </el-radio-group>
            <el-tag v-if="accessRequestTotal > 0" :type="accessRequestScope === 'pending' ? 'warning' : 'info'" effect="plain">{{ accessRequestTotal }}</el-tag>
          </div>
        </div>
      </template>
      <div class="access-review-filters">
        <el-input
          v-model.trim="accessRequestFilters.resourceSearch"
          clearable
          :placeholder="t('security.accessRequest.filters.resourcePlaceholder')"
          @keyup.enter="applyAccessRequestFilters"
        />
        <el-input
          v-model.trim="accessRequestFilters.requesterSearch"
          clearable
          :placeholder="t('security.accessRequest.filters.requesterPlaceholder')"
          @keyup.enter="applyAccessRequestFilters"
        />
        <el-select
          v-if="accessRequestScope === 'history'"
          v-model="accessRequestFilters.state"
          clearable
          :placeholder="t('security.accessRequest.filters.allStates')"
        >
          <el-option v-for="state in ['approved', 'rejected', 'expired']" :key="state" :label="t(`security.accessRequest.states.${state}`)" :value="state" />
        </el-select>
        <el-select
          v-if="accessRequestScope === 'history'"
          v-model="accessRequestFilters.authorizationState"
          clearable
          :placeholder="t('security.accessRequest.filters.allAuthorizationStates')"
        >
          <el-option v-for="state in ['active', 'expired', 'revoked', 'superseded']" :key="state" :label="t(`security.accessRequest.authorizationStates.${state}`)" :value="state" />
        </el-select>
        <el-date-picker
          v-model="accessRequestCreatedRange"
          type="datetimerange"
          unlink-panels
          :range-separator="t('security.accessRequest.filters.to')"
          :start-placeholder="t('security.accessRequest.filters.createdFrom')"
          :end-placeholder="t('security.accessRequest.filters.createdTo')"
        />
        <el-button type="primary" @click="applyAccessRequestFilters">{{ t('security.accessRequest.filters.search') }}</el-button>
        <el-button v-if="hasAccessRequestFilters" @click="resetAccessRequestFilters">{{ t('security.accessRequest.filters.reset') }}</el-button>
      </div>
      <el-table v-loading="accessRequestLoading" :data="accessRequestRows" size="small">
        <el-table-column :label="t('security.accessRequest.resourceField')" min-width="260">
          <template #default="{ row }"><strong>{{ row.target_full_name }}</strong><br><span>{{ row.component?.key }}</span></template>
        </el-table-column>
        <el-table-column :label="t('security.accessRequest.requester')" min-width="150">
          <template #default="{ row }">
            <div class="access-actor">
              <strong>{{ row.requester?.display_name }}</strong>
              <span>{{ releaseActorLabel(row.requester?.id) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.accessRequest.requestedUntil')" width="190">
          <template #default="{ row }">{{ formatDateTime(row.requested_expires_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.accessRequest.createdAt')" width="190">
          <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="rationale" :label="t('security.accessRequest.rationale')" min-width="240" show-overflow-tooltip />
        <el-table-column v-if="accessRequestScope === 'history'" :label="t('security.accessRequest.state')" width="110">
          <template #default="{ row }"><el-tag size="small" :type="accessRequestStateType(row.state)">{{ t(`security.accessRequest.states.${row.state}`) }}</el-tag></template>
        </el-table-column>
        <el-table-column v-if="accessRequestScope === 'history'" :label="t('security.accessRequest.authorizationState')" min-width="170">
          <template #default="{ row }">
            <div v-if="row.authorization_state" class="access-authorization-state">
              <el-tag size="small" :type="accessAuthorizationStateType(row.authorization_state)">
                {{ t(`security.accessRequest.authorizationStates.${row.authorization_state}`) }}
              </el-tag>
              <span v-if="row.authorized_until">
                {{ t('security.accessRequest.authorizedUntil', { time: formatDateTime(row.authorized_until) }) }}
              </span>
              <el-button
                v-if="canReadExemptions && row.enrollment_id && row.exemption_id"
                class="access-authorization-link"
                link
                type="primary"
                size="small"
                @click="openAccessRequestAuthorization(row)"
              >
                {{ t('security.accessRequest.viewAuthorization') }}
              </el-button>
            </div>
            <span v-else>{{ t('security.accessRequest.authorizationStates.not_granted') }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="accessRequestScope === 'history'" :label="t('security.accessRequest.reviewer')" width="130">
          <template #default="{ row }">
            <div v-if="row.reviewer" class="access-actor">
              <strong>{{ row.reviewer.display_name }}</strong>
              <span>{{ releaseActorLabel(row.reviewer.id) }}</span>
            </div>
            <span v-else>{{ t('security.common.notAvailable') }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="accessRequestScope === 'history'" :label="t('security.accessRequest.processedAt')" width="190">
          <template #default="{ row }">{{ formatDateTime(row.decided_at || row.requested_expires_at) }}</template>
        </el-table-column>
        <el-table-column v-if="accessRequestScope === 'history'" prop="decision_rationale" :label="t('security.accessRequest.decisionRationaleLabel')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.decision_rationale || t('security.common.notAvailable') }}</template>
        </el-table-column>
        <el-table-column v-if="accessRequestScope === 'pending'" :label="t('security.common.actions')" width="190" fixed="right">
          <template #default="{ row }">
            <div v-if="row.can_decide" class="access-decision-actions">
              <el-button link type="primary" @click="openAccessRequestDecision(row, 'approve')">{{ t('security.accessRequest.approve') }}</el-button>
              <el-button link type="danger" @click="openAccessRequestDecision(row, 'reject')">{{ t('security.accessRequest.reject') }}</el-button>
            </div>
            <div v-else-if="row.decision_unavailable_reason" class="access-decision-unavailable">
              <el-tag size="small" type="info">{{ t(`security.accessRequest.unavailableLabels.${row.decision_unavailable_reason}`) }}</el-tag>
              <span>{{ t(`security.accessRequest.unavailableReasons.${row.decision_unavailable_reason}`) }}</span>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!accessRequestLoading && accessRequestRows.length === 0" :description="t(`security.accessRequest.emptyStates.${accessRequestScope}`)" :image-size="48" />
      <div v-if="accessRequestTotal > accessRequestPageSize" class="pagination">
        <el-pagination
          v-model:current-page="accessRequestPage"
          v-model:page-size="accessRequestPageSize"
          background
          layout="total, sizes, prev, pager, next"
          :page-sizes="[10, 20, 50, 100]"
          :total="accessRequestTotal"
          @change="handleAccessRequestPageChange"
        />
      </div>
    </el-card>
    <div class="list-scope-bar">
      <el-radio-group v-model="listScope" size="small" @change="handleScopeChange">
        <el-radio-button value="current">{{ t('security.enrollment.listScopes.current') }}</el-radio-button>
        <el-radio-button value="released">{{ t('security.enrollment.listScopes.released') }}</el-radio-button>
        <el-radio-button value="all">{{ t('security.enrollment.listScopes.all') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-card class="enrollment-card" shadow="never">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.enrollment.resource')" min-width="320">
          <template #default="{ row }">
            <button type="button" class="resource-cell" @click="openDetail(row)">
              <span class="resource-name">{{ resourceName(row) }}</span>
              <span class="resource-path">{{ resourcePath(row) }}</span>
              <span class="resource-meta">
                <el-tag size="small" effect="plain">{{ itemTypeLabel(row.target_snapshot?.item_type) }}</el-tag>
                <span>{{ engineLabel(row.target_snapshot?.engine_id) }}</span>
              </span>
            </button>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.enrollment.state')" width="190">
          <template #default="{ row }">
            <div class="state-cell">
              <el-tag :type="presentationState(row).type">{{ presentationState(row).label }}</el-tag>
              <span>{{ presentationState(row).description }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.enrollment.progress')" min-width="430">
          <template #default="{ row }">
            <div class="owner-grid">
              <div v-for="owner in row.owner_progress" :key="owner.consumer_owner" class="owner-item">
                <span class="owner-name">{{ ownerLabel(owner.consumer_owner) }}</span>
                <el-tag size="small" :type="ownerPresentation(row, owner).type">
                  {{ ownerPresentation(row, owner).label }}
                </el-tag>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="listScope === 'released' ? t('security.enrollment.releaseCompletedAt') : t('security.enrollment.discovery')" width="210">
          <template #default="{ row }">
            <span v-if="listScope === 'released'" class="release-time">{{ formatDateTime(row.released_at) }}</span>
            <div v-else class="discovery-cell">
              <el-tag size="small" :type="discoveryPresentation(row).type">{{ discoveryPresentation(row).label }}</el-tag>
              <span>{{ formatDateTime(row.last_discovered_at) }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.common.actions')" width="210" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button
                v-if="canCreate && row.state === 'released'"
                link
                type="primary"
                @click="openReEnrollment(row)"
              >
                {{ t('security.enrollment.reEnroll') }}
              </el-button>
              <el-button link type="primary" @click="openDetail(row)">{{ t('security.enrollment.viewDetails') }}</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && rows.length === 0" :description="emptyDescription">
        <el-button v-if="canCreate && listScope === 'current'" type="primary" @click="openCreate()">{{ t('security.enrollment.create') }}</el-button>
      </el-empty>

      <div v-if="total > pageSize" class="pagination">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          background
          layout="total, sizes, prev, pager, next"
          :page-sizes="[20, 50, 100]"
          :total="total"
          @change="handlePageChange"
        />
      </div>
    </el-card>
    </template>

    <template v-else>
      <div class="review-queue-intro">
        <div>
          <strong>{{ t('security.reviewQueue.title') }}</strong>
          <p>{{ t('security.reviewQueue.description') }}</p>
        </div>
        <el-tag type="warning" effect="plain">{{ t('security.reviewQueue.pendingTotal', { count: reviewQueueTotal }) }}</el-tag>
      </div>

      <div class="review-queue-filters">
        <el-select
          v-model="reviewQueueTypeID"
          clearable
          :placeholder="t('security.reviewQueue.allSensitiveTypes')"
          @change="handleReviewQueueFilterChange"
        >
          <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
        <el-select
          v-model="reviewQueueDetectorVersion"
          clearable
          filterable
          :placeholder="t('security.reviewQueue.allRecognitionMethods')"
          @change="handleReviewQueueFilterChange"
        >
          <el-option v-for="item in reviewQueueCapabilities" :key="item.key" :label="capabilityOptionLabel(item)" :value="item.key" />
        </el-select>
        <el-button v-if="reviewQueueTypeID || reviewQueueDetectorVersion" @click="resetReviewQueueFilters">
          {{ t('security.reviewQueue.clearFilters') }}
        </el-button>
      </div>

      <el-card class="enrollment-card review-queue-card" shadow="never">
        <el-table v-loading="reviewQueueLoading" :data="reviewQueueRows" row-key="id">
          <el-table-column :label="t('security.reviewQueue.resource')" min-width="260">
            <template #default="{ row }">
              <button type="button" class="resource-cell" @click="openReviewQueueResource(row)">
                <span class="resource-name">{{ findingResourceName(row) }}</span>
                <span class="resource-path">{{ row.target_snapshot?.full_name || t('security.enrollment.snapshotUnavailable') }}</span>
                <span class="resource-meta">
                  <el-tag size="small" effect="plain">{{ itemTypeLabel(row.target_snapshot?.item_type) }}</el-tag>
                  <span>{{ engineLabel(row.target_snapshot?.engine_id) }}</span>
                </span>
              </button>
            </template>
          </el-table-column>
          <el-table-column :label="t('security.reviewQueue.candidate')" min-width="250">
            <template #default="{ row }">
              <div class="queue-candidate">
                <strong>{{ row.component_key }}</strong>
                <span>{{ typeName(row.sensitive_data_type_id) }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('security.reviewQueue.recognition')" min-width="270">
            <template #default="{ row }">
              <div class="queue-recognition">
                <span>{{ capabilityName(row) }}</span>
                <code>{{ row.detector_version }}</code>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('security.finding.evidence')" min-width="250">
            <template #default="{ row }">
              <div class="queue-evidence">
                <span>{{ evidenceDescription(row) }}</span>
                <small>{{ t('security.finding.confidenceValue', { value: confidenceLabel(row.confidence) }) }} · {{ formatDateTime(row.observed_at) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('security.common.actions')" width="210" fixed="right">
            <template #default="{ row }">
              <div class="queue-actions">
                <el-button v-if="canReviewFindings" link type="primary" @click="openFindingReview(row, 'confirm')">{{ t('security.finding.review') }}</el-button>
                <el-button v-if="canReviewFindings" link type="danger" @click="openFindingReview(row, 'reject')">{{ t('security.finding.markFalsePositive') }}</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <el-empty v-if="!reviewQueueLoading && reviewQueueRows.length === 0" :description="t('security.reviewQueue.empty')" />

        <div v-if="reviewQueueTotal > reviewQueuePageSize" class="pagination">
          <el-pagination
            v-model:current-page="reviewQueuePage"
            v-model:page-size="reviewQueuePageSize"
            background
            layout="total, sizes, prev, pager, next"
            :page-sizes="[20, 50, 100]"
            :total="reviewQueueTotal"
            @change="handleReviewQueuePageChange"
          />
        </div>
      </el-card>
    </template>

    <el-drawer
      v-model="createDrawer"
      :title="t('security.enrollment.create')"
      size="760px"
      destroy-on-close
      @closed="handleCreateClosed"
    >
      <div class="create-flow">
        <el-alert type="info" :closable="false" :title="t('security.enrollment.createHint')" />
        <ResourceTreePicker
          v-model="selectedResource"
          api-base-url="/api/v1/meta"
          mode="item"
          :initial-locator="initialLocator"
          :engine-label="t('security.enrollment.engine')"
          :engine-placeholder="t('security.enrollment.enginePlaceholder')"
          :search-placeholder="t('security.enrollment.searchCurrentEngine')"
          :search-all-engines-placeholder="t('security.enrollment.searchAllEngines')"
          :search-empty-text="t('security.enrollment.resourceNotFound')"
          tree-height="min(52vh, 520px)"
          @select="handleResourceSelect"
        />

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
              {{ selectedResource?.display?.engine_name || engineLabel(selectedItem.engine_id) }}
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
          :disabled="!selectedItem || Boolean(existingEnrollment)"
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
          <div v-if="canReadFindings && findings.length > 0" class="finding-list">
            <article v-for="finding in findings" :key="finding.id" class="finding-card">
              <div class="finding-card__header">
                <div>
                  <strong>{{ finding.component_key }}</strong>
                  <span>{{ typeName(finding.sensitive_data_type_id) }}</span>
                </div>
                <el-tag size="small" :type="findingStatePresentation(finding).type">
                  {{ findingStatePresentation(finding).label }}
                </el-tag>
              </div>
              <div class="finding-explanation">
                <section class="explanation-stage">
                  <div class="explanation-stage__title">
                    <span>1</span>
                    <strong>{{ t('security.finding.explanationStages.detection') }}</strong>
                  </div>
                  <p class="explanation-primary">{{ capabilityName(finding) }}</p>
                  <p>{{ evidenceDescription(finding) }}</p>
                  <dl class="detection-rule-audit">
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.actualEvidence') }}</dt>
                      <dd>{{ evidenceAuditDescription(finding) }}</dd>
                    </div>
                    <div class="detection-rule-audit__details">
                      <dt>{{ t('security.finding.ruleAudit.details') }}</dt>
                      <dd>
                        <el-popover
                          placement="top-start"
                          trigger="click"
                          :width="420"
                          popper-class="security-rule-popover"
                        >
                          <template #reference>
                            <el-button
                              class="rule-help-button"
                              link
                              type="primary"
                              :icon="QuestionFilled"
                              :aria-label="t('security.finding.ruleAudit.viewDetails')"
                            />
                          </template>
                          <dl class="recognition-rule-details">
                            <div>
                              <dt>{{ t('security.finding.ruleAudit.method') }}</dt>
                              <dd>{{ capabilityText(finding, 'method_i18n_key') }}</dd>
                            </div>
                            <div>
                              <dt>{{ t('security.finding.ruleAudit.scope') }}</dt>
                              <dd>{{ capabilityScope(finding) }}</dd>
                            </div>
                            <div>
                              <dt>{{ t('security.finding.ruleAudit.privacy') }}</dt>
                              <dd>{{ capabilityText(finding, 'privacy_i18n_key') }}</dd>
                            </div>
                            <div>
                              <dt>{{ t('security.finding.ruleAudit.limitations') }}</dt>
                              <dd>{{ capabilityText(finding, 'limitations_i18n_key') }}</dd>
                            </div>
                            <div>
                              <dt>{{ t('security.finding.ruleAudit.version') }}</dt>
                              <dd class="technical-value">{{ finding.explanation?.capability?.key || finding.detector_version }}</dd>
                            </div>
                          </dl>
                        </el-popover>
                      </dd>
                    </div>
                  </dl>
                  <div class="explanation-tags">
                    <el-tag size="small" effect="plain">{{ t('security.finding.confidenceValue', { value: confidenceLabel(finding.confidence) }) }}</el-tag>
                    <el-tag
                      v-if="finding.explanation?.automatic_adoption_threshold != null"
                      size="small"
                      :type="finding.explanation.meets_automatic_threshold ? 'success' : 'warning'"
                    >
                      {{ t('security.finding.thresholdValue', { value: confidenceLabel(finding.explanation.automatic_adoption_threshold) }) }}
                    </el-tag>
                  </div>
                </section>

                <section class="explanation-stage">
                  <div class="explanation-stage__title">
                    <span>2</span>
                    <strong>{{ t('security.finding.explanationStages.governance') }}</strong>
                  </div>
                  <el-tag size="small" :type="decisionPresentation(finding).type">
                    {{ decisionPresentation(finding).label }}
                  </el-tag>
                  <p class="explanation-primary">{{ effectiveDefinitionSummary(finding) }}</p>
                  <p>{{ baselineDescription(finding) }}</p>
                  <p v-if="activeAssessmentForFinding(finding)" class="resource-policy-summary">
                    {{ assessmentProtectionSummary(activeAssessmentForFinding(finding)) }}
                  </p>
                </section>

                <section class="explanation-stage">
                  <div class="explanation-stage__title">
                    <span>3</span>
                    <strong>{{ t('security.finding.explanationStages.execution') }}</strong>
                  </div>
                  <div class="finding-outlets">
                    <div v-for="outlet in finding.explanation.outlets" :key="outlet.consumer_owner" class="finding-outlet">
                      <span>{{ ownerLabel(outlet.consumer_owner) }}</span>
                      <strong>{{ outletRuleDescription(finding, outlet.consumer_owner) }}</strong>
                      <el-tag size="small" :type="outletAcknowledgementPresentation(outlet).type">
                        {{ outletAcknowledgementPresentation(outlet).label }}
                      </el-tag>
                    </div>
                  </div>
                </section>
              </div>
              <p class="finding-observed-at">{{ t('security.finding.observedAt') }}：{{ formatDateTime(finding.observed_at) }}</p>
              <div v-if="finding.review" class="review-result">
                <span>{{ t('security.finding.reviewRationale') }}</span>
                <p>{{ finding.review.rationale }}</p>
              </div>
              <div v-if="!finding.review && canReviewFindings" class="finding-card__actions">
                <el-button type="danger" plain @click="openFindingReview(finding, 'reject')">{{ t('security.finding.markFalsePositive') }}</el-button>
                <el-button type="primary" plain @click="openFindingReview(finding, 'confirm')">{{ t('security.finding.review') }}</el-button>
              </div>
              <div v-else-if="assessmentForFinding(finding)" class="finding-card__actions">
                <el-button plain @click="openAssessmentHistory(assessmentForFinding(finding))">
                  {{ t('security.assessment.history') }}
                </el-button>
                <el-button
                  v-if="canUpdateAssessments"
                  plain
                  @click="openAssessmentRevision(assessmentForFinding(finding))"
                >
                  {{ t('security.assessment.reviseConclusion') }}
                </el-button>
                <el-button
                  v-if="activeAssessmentForFinding(finding) && canConfigurePolicy(activeAssessmentForFinding(finding))"
                  type="primary"
                  plain
                  @click="openPolicy(activeAssessmentForFinding(finding))"
                >
                  {{ policyForAssessment(activeAssessmentForFinding(finding))?.state === 'active' ? t('security.policy.adjust') : t('security.policy.tighten') }}
                </el-button>
                <el-button
                  v-if="policyForAssessment(activeAssessmentForFinding(finding))?.state === 'active' && canRevokePolicies"
                  plain
                  @click="openPolicyRestore(activeAssessmentForFinding(finding))"
                >
                  {{ t('security.policy.restoreDefault') }}
                </el-button>
                <el-button v-if="activeAssessmentForFinding(finding) && canUpdateAssessments" type="danger" plain @click="openAssessmentRevoke(activeAssessmentForFinding(finding))">
                  {{ t('security.assessment.revokeConclusion') }}
                </el-button>
              </div>
            </article>
          </div>
          <el-pagination
            v-if="findingsTotal > findingsPageSize"
            v-model:current-page="findingsPage"
            class="finding-pagination"
            small
            background
            layout="total, prev, pager, next"
            :page-size="findingsPageSize"
            :total="findingsTotal"
            @current-change="loadFindings"
          />

          <section v-if="manualAssessments.length > 0" class="manual-assessment-list">
            <h5>{{ t('security.assessment.manualConclusions') }}</h5>
            <article v-for="assessment in manualAssessments" :key="assessment.id" class="manual-assessment-card">
              <div>
                <strong>{{ assessment.component_key }}</strong>
                <span>{{ assessmentSummary(assessment) }}</span>
                <p>{{ assessment.current?.rationale }}</p>
                <p v-if="assessment.current?.conclusion === 'sensitive'" class="resource-policy-summary">
                  {{ assessmentProtectionSummary(assessment) }}
                </p>
              </div>
              <div class="manual-assessment-card__actions">
                <el-tag :type="assessment.current?.conclusion === 'sensitive' ? 'success' : 'info'">
                  {{ assessmentConclusionLabel(assessment.current?.conclusion) }}
                </el-tag>
                <el-button link @click="openAssessmentHistory(assessment)">
                  {{ t('security.assessment.history') }}
                </el-button>
                <el-button
                  v-if="canUpdateAssessments"
                  link
                  @click="openAssessmentRevision(assessment)"
                >
                  {{ t('security.assessment.reviseConclusion') }}
                </el-button>
                <el-button
                  v-if="assessment.current?.conclusion === 'sensitive' && canConfigurePolicy(assessment)"
                  link
                  type="primary"
                  @click="openPolicy(assessment)"
                >
                  {{ policyForAssessment(assessment)?.state === 'active' ? t('security.policy.adjust') : t('security.policy.tighten') }}
                </el-button>
                <el-button
                  v-if="assessment.current?.conclusion === 'sensitive' && policyForAssessment(assessment)?.state === 'active' && canRevokePolicies"
                  link
                  @click="openPolicyRestore(assessment)"
                >
                  {{ t('security.policy.restoreDefault') }}
                </el-button>
                <el-button
                  v-if="assessment.current?.conclusion === 'sensitive' && canUpdateAssessments"
                  link
                  type="danger"
                  @click="openAssessmentRevoke(assessment)"
                >
                  {{ t('security.assessment.revokeConclusion') }}
                </el-button>
              </div>
            </article>
          </section>

          <el-empty
            v-if="findings.length === 0 && manualAssessments.length === 0"
            :description="t('security.finding.noGovernanceConclusions')"
            :image-size="72"
          />
          </template>
        </section>

        <section v-if="canReadExemptions" class="exemption-section">
          <div class="exemption-section__header">
            <div>
              <h4>{{ t('security.exemption.title') }}</h4>
              <p>{{ t('security.exemption.hint') }}</p>
            </div>
          </div>
          <el-skeleton v-if="exemptionsLoading" class="exemption-loading" :rows="2" animated />
          <div v-else-if="exemptions.length > 0" class="exemption-list">
            <article
              v-for="exemption in exemptions"
              :key="exemption.id"
              :ref="element => setExemptionCardRef(exemption.id, element)"
              class="exemption-card"
              :class="{ 'is-focused': exemption.id === focusedExemptionID }"
            >
              <div class="exemption-card__main">
                <div class="exemption-card__title">
                  <strong>{{ assessmentComponent(exemption.assessment_id) }}</strong>
                  <el-tag size="small" :type="exemptionStatePresentation(exemption).type">
                    {{ exemptionStatePresentation(exemption).label }}
                  </el-tag>
                </div>
                <span>{{ t('security.exemption.subject') }}：{{ exemption.subject_id }}</span>
                <span>{{ ownerLabel(exemption.consumer_owner) }} · {{ actionLabel(exemption.action) }}</span>
                <span>{{ t('security.exemption.expiresAt') }}：{{ formatDateTime(exemption.current?.expires_at) }}</span>
                <p>{{ exemption.current?.rationale }}</p>
              </div>
              <div v-if="canRevokeExemptions" class="exemption-card__actions">
                <el-button
                  v-if="canRevokeExemptions && exemption.effective_state === 'active'"
                  link
                  type="danger"
                  @click="openExemptionRevoke(exemption)"
                >
                  {{ t('security.exemption.revoke') }}
                </el-button>
              </div>
            </article>
          </div>
          <el-empty v-else :description="t('security.exemption.empty')" :image-size="64" />
        </section>

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
      <template v-if="decidingAccessRequest">
        <el-alert type="warning" :closable="false" :title="accessRequestDecisionHint" />
        <div v-if="accessRequestDecisionConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.accessRequest.decisionVersionConflict') }}</span>
          <el-button link type="primary" :loading="accessRequestDecisionReloading" @click="reloadAccessRequestDecisionBaseline">
            {{ t('security.accessRequest.reloadLatestForDecision') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ decidingAccessRequest.target_full_name }} · {{ decidingAccessRequest.component?.key }}</strong>
          <span>{{ t('security.accessRequest.requester') }}：{{ decidingAccessRequest.requester?.display_name }}（{{ releaseActorLabel(decidingAccessRequest.requester?.id) }}）</span>
          <span>{{ t('security.accessRequest.requestedUntil') }}：{{ formatDateTime(decidingAccessRequest.requested_expires_at) }}</span>
          <span>{{ t('security.accessRequest.rationale') }}：{{ decidingAccessRequest.rationale }}</span>
        </div>
        <el-form ref="accessRequestDecisionFormRef" :model="accessRequestDecisionForm" :rules="accessRequestDecisionRules" label-position="top">
          <el-form-item :label="t('security.accessRequest.decisionRationaleLabel')" prop="rationale" required>
            <el-input
              v-model="accessRequestDecisionForm.rationale"
              type="textarea"
              :rows="4"
              maxlength="2000"
              show-word-limit
              :placeholder="t('security.accessRequest.decisionRationale')"
            />
          </el-form-item>
        </el-form>
      </template>
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

    <el-dialog v-model="reviewDialog" class="addp-dialog" :title="t('security.finding.reviewTitle')" width="min(640px, calc(100vw - 24px))" @opened="focusReviewRationale">
      <template v-if="reviewingFinding">
        <div class="review-target">
          <div class="review-target__header">
            <strong>{{ reviewingFinding.component_key }}</strong>
            <el-tag size="small" type="warning" effect="plain">{{ reviewRemainingLabel }}</el-tag>
          </div>
          <span>{{ typeName(reviewingFinding.sensitive_data_type_id) }} · {{ confidenceLabel(reviewingFinding.confidence) }}</span>
        </div>
        <el-collapse v-model="reviewBasisExpanded" class="review-basis">
          <el-collapse-item name="basis">
            <template #title>
              <div class="review-basis__title">
                <QuestionFilled />
                <span>{{ t('security.finding.reviewBasis.title') }}</span>
                <small>{{ t('security.finding.reviewBasis.hint') }}</small>
              </div>
            </template>
            <dl class="review-basis__facts">
              <div>
                <dt>{{ t('security.finding.reviewBasis.recognitionMethod') }}</dt>
                <dd>{{ capabilityName(reviewingFinding) }}</dd>
              </div>
              <div>
                <dt>{{ t('security.finding.reviewBasis.actualMatch') }}</dt>
                <dd>{{ evidenceAuditDescription(reviewingFinding) }}</dd>
              </div>
              <div>
                <dt>{{ t('security.finding.reviewBasis.governanceDecision') }}</dt>
                <dd>
                  <el-tag size="small" :type="decisionPresentation(reviewingFinding).type">
                    {{ decisionPresentation(reviewingFinding).label }}
                  </el-tag>
                  <span>{{ effectiveDefinitionSummary(reviewingFinding) }}</span>
                  <span>{{ baselineDescription(reviewingFinding) }}</span>
                </dd>
              </div>
              <div>
                <dt>{{ t('security.finding.reviewBasis.currentEnforcement') }}</dt>
                <dd v-if="reviewingFinding.explanation?.outlets?.length" class="review-basis__outlets">
                  <span v-for="outlet in reviewingFinding.explanation.outlets" :key="outlet.consumer_owner">
                    <strong>{{ ownerLabel(outlet.consumer_owner) }}</strong>
                    {{ outletRuleDescription(reviewingFinding, outlet.consumer_owner) }}
                  </span>
                </dd>
                <dd v-else>{{ t('security.finding.outletUnavailable') }}</dd>
              </div>
            </dl>
          </el-collapse-item>
        </el-collapse>
        <el-form ref="reviewFormRef" :model="reviewForm" :rules="reviewRules" label-position="top">
          <el-form-item :label="t('security.finding.decision')" prop="decision" required>
            <el-radio-group v-model="reviewForm.decision" class="decision-group">
              <el-radio-button value="confirm">{{ t('security.finding.decisions.confirm') }}</el-radio-button>
              <el-radio-button value="adjust">{{ t('security.finding.decisions.adjust') }}</el-radio-button>
              <el-radio-button value="reject">{{ t('security.finding.decisions.reject') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <template v-if="reviewForm.decision === 'adjust'">
            <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
              <el-select v-model="reviewForm.sensitiveDataTypeID" class="wide" :placeholder="t('security.finding.selectSensitiveDataType')" @change="applyReviewDefaultGrade">
                <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
              <el-select v-model="reviewForm.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
                <el-option v-for="item in activeGradesForType(reviewForm.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
              </el-select>
            </el-form-item>
          </template>
          <el-form-item :label="t('security.finding.rationale')" prop="rationale" required>
            <el-input ref="reviewRationaleInput" v-model="reviewForm.rationale" type="textarea" :rows="4" maxlength="2000" show-word-limit :placeholder="reviewRationalePlaceholder" />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="reviewDialog = false">{{ t('security.common.cancel') }}</el-button>
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
        <el-button @click="assessmentRevisionDialog = false">{{ t('security.common.cancel') }}</el-button>
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
        <el-button ref="assessmentRevokeCancelButton" :disabled="assessmentRevokeSaving" @click="assessmentRevokeDialog = false">
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
      @opened="clearPolicyValidation"
      @closed="closePolicyDialog"
    >
      <template v-if="policyAssessment">
        <el-alert type="info" :closable="false" :title="t('security.policy.hint')" />
        <div v-if="policyVersionConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.policy.versionConflict') }}</span>
          <el-button link type="primary" :loading="policyReloading" @click="reloadPolicyBaseline">
            {{ t('security.policy.reloadLatest') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ policyAssessment.component_key }}</strong>
          <span>{{ assessmentSummary(policyAssessment) }}</span>
          <span>{{ assessmentProtectionSummary(policyAssessment) }}</span>
        </div>
        <el-form ref="policyFormRef" :model="policyForm" :rules="policyRules" label-position="top">
          <el-form-item :label="t('security.policy.scope')">
            <el-input :model-value="t('security.policy.managerPreview')" disabled />
          </el-form-item>
          <el-form-item :label="t('security.policy.effect')" prop="effect" required>
            <el-radio-group v-model="policyForm.effect">
              <el-radio-button v-for="effect in stricterPolicyEffects(policyAssessment)" :key="effect" :value="effect">
                {{ effectLabel(effect) }}
              </el-radio-button>
            </el-radio-group>
            <div class="field-help">{{ t(`security.baseline.effectImpact.${policyForm.effect}`) }}</div>
          </el-form-item>
          <el-form-item :label="t('security.policy.rationale')" prop="rationale" required>
            <el-input v-model="policyForm.rationale" type="textarea" :rows="4" maxlength="2000" show-word-limit :placeholder="t('security.policy.rationalePlaceholder')" />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="policyDialog = false">{{ t('security.common.cancel') }}</el-button>
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
      <template v-if="policyRestoreAssessment">
        <el-alert type="warning" :closable="false" :title="t('security.policy.restoreHint')" />
        <div v-if="policyRestoreConflict" class="version-conflict-notice" role="alert">
          <span>{{ t('security.policy.restoreVersionConflict') }}</span>
          <el-button link type="primary" :loading="policyRestoreReloading" @click="reloadPolicyRestoreBaseline">
            {{ t('security.policy.reloadLatestForRestore') }}
          </el-button>
        </div>
        <div class="policy-target">
          <strong>{{ policyRestoreAssessment.component_key }}</strong>
          <span>{{ assessmentSummary(policyRestoreAssessment) }}</span>
          <span>{{ assessmentProtectionSummary(policyRestoreAssessment) }}</span>
        </div>
        <el-form ref="policyRestoreFormRef" :model="policyRestoreForm" :rules="policyRestoreRules" label-position="top">
          <el-form-item :label="t('security.policy.restoreRationale')" prop="rationale" required>
            <el-input
              v-model="policyRestoreForm.rationale"
              type="textarea"
              :rows="4"
              maxlength="2000"
              show-word-limit
              :placeholder="t('security.policy.restoreRationalePlaceholder')"
            />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button ref="policyRestoreCancelButton" :disabled="policyRestoreSaving" @click="policyRestoreDialog = false">{{ t('security.common.cancel') }}</el-button>
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
        <el-button ref="reEnrollmentCancelButton" :disabled="reEnrollmentSaving" @click="reEnrollmentDialog = false">{{ t('security.common.cancel') }}</el-button>
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
        <el-button ref="rediscoveryCancelButton" :disabled="rediscoverySaving" @click="rediscoveryDialog = false">{{ t('security.common.cancel') }}</el-button>
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
        <el-button ref="releaseCancelButton" :disabled="releaseSaving" @click="releaseDialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="danger" :loading="releaseSaving" :disabled="releaseConflict" @click="releaseEnrollment">{{ releaseConfirmLabel }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { QuestionFilled, Refresh } from '@element-plus/icons-vue'
import {
  ResourceTreePicker,
  listResourceTreeEngines,
  navigateConsoleModuleRoute,
  openMonitorExecution,
  resolveCanonicalTabRouteState
} from '@common-ui'
import { assessmentAPI, classificationAPI, detectorCapabilityAPI, findingAPI, gradeAPI, metaAPI, protectionAccessRequestAPI, protectionBaselineAPI, protectionEnrollmentAPI, protectionExemptionAPI, protectionPolicyAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import {
  buildAssessmentRevisionPayload,
  buildFindingReviewPayload,
  discoveryRefreshMarker,
  findingDecisionState,
  findingOutletRules,
  findingReviewState,
  isDiscoveryExecutionInProgress,
  isNoSupportedFindingsReleaseUnavailable,
  isProtectionAccessRequestExpired,
  isProtectionEnrollmentAlreadyActive,
  isProtectionEffectStricter,
  isResourceVersionConflict,
  isZeroFindingDiscovery,
  needsEnrollmentRefresh,
  normalizeDiscoverySummary,
  resolvePendingReviewContinuation,
  resolveReviewQueueFilters
} from '../utils/protectionEnrollment.mjs'

const AUTO_REFRESH_FAST_INTERVAL_MS = 2000
const AUTO_REFRESH_SLOW_INTERVAL_MS = 5000
const AUTO_REFRESH_FAST_WINDOW_MS = 30000
const AUTO_REFRESH_TIMEOUT_MS = 120000

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const WORKSPACE_TABS = ['resources', 'review-queue']
function resolveWorkspaceRouteState(routeQuery) {
  const requested = resolveCanonicalTabRouteState({ allowedTabs: WORKSPACE_TABS, defaultTab: 'resources', routeQuery })
  const reviewQueue = resolveReviewQueueFilters(routeQuery)
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

const rows = ref([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const listScope = ref('current')
const loading = ref(false)
const backgroundRefreshing = ref(false)
const manualRefreshing = ref(false)
const autoRefreshActive = ref(false)
const lastRefreshedAt = ref(null)
const saving = ref(false)
const createDrawer = ref(false)
const detailDrawer = ref(false)
const releaseDialog = ref(false)
const releaseFormRef = ref(null)
const releaseCancelButton = ref(null)
const releaseSaving = ref(false)
const releaseReloading = ref(false)
const releaseConflict = ref(false)
const rediscoveryDialog = ref(false)
const rediscoveryCancelButton = ref(null)
const rediscoverySaving = ref(false)
const rediscoveryReloading = ref(false)
const rediscoveryConflict = ref(false)
const rediscoveryEnrollment = ref(null)
const reEnrollmentDialog = ref(false)
const reEnrollmentCancelButton = ref(null)
const reEnrollmentSaving = ref(false)
const reEnrollmentReloading = ref(false)
const reEnrollmentConflict = ref(false)
const reEnrollmentSource = ref(null)
const reviewDialog = ref(false)
const reviewFormRef = ref(null)
const manualAssessmentDialog = ref(false)
const manualAssessmentFormRef = ref(null)
const assessmentHistoryDialog = ref(false)
const assessmentRevisionDialog = ref(false)
const assessmentRevisionFormRef = ref(null)
const assessmentRevokeDialog = ref(false)
const assessmentRevokeFormRef = ref(null)
const exemptionRevokeDialog = ref(false)
const exemptionRevokeFormRef = ref(null)
const policyDialog = ref(false)
const policyFormRef = ref(null)
const policyRestoreDialog = ref(false)
const policyRestoreFormRef = ref(null)
const selectedResource = ref(null)
const selectedItem = ref(null)
const selectedItemLoading = ref(false)
const initialLocator = ref('')
const detailRow = ref(null)
const releasing = ref(null)
const releaseForm = reactive({ reason: '' })
const releaseBasis = ref('manual')
const engineNames = ref(new Map())
const findings = ref([])
const findingsTotal = ref(0)
const findingsPage = ref(1)
const findingsPageSize = 20
const findingsLoading = ref(false)
const assessments = ref([])
const assessmentsLoading = ref(false)
const policies = ref([])
const policiesLoading = ref(false)
const exemptions = ref([])
const exemptionsLoading = ref(false)
const focusedExemptionID = ref('')
const exemptionCardRefs = new Map()
const componentOptions = ref([])
const componentsLoading = ref(false)
const sensitiveTypes = ref([])
const securityClassifications = ref([])
const securityGrades = ref([])
const protectionBaselines = ref([])
const detectorCapabilities = ref([])
const reviewQueueRows = ref([])
const reviewQueueTotal = ref(0)
const reviewQueuePage = ref(initialWorkspaceRoute.reviewQueue.page)
const reviewQueuePageSize = ref(initialWorkspaceRoute.reviewQueue.pageSize)
const reviewQueueTypeID = ref(initialWorkspaceRoute.reviewQueue.sensitiveDataTypeID)
const reviewQueueDetectorVersion = ref(initialWorkspaceRoute.reviewQueue.detectorVersion)
const reviewQueueLoading = ref(false)
const accessRequestRows = ref([])
const accessRequestTotal = ref(0)
const accessRequestPage = ref(1)
const accessRequestPageSize = ref(10)
const accessRequestLoading = ref(false)
const accessRequestScope = ref('pending')
const accessRequestFilters = reactive({ resourceSearch: '', requesterSearch: '', state: '', authorizationState: '' })
const accessRequestCreatedRange = ref([])
const accessRequestDecisionDialog = ref(false)
const accessRequestDecisionFormRef = ref(null)
const accessRequestDecisionCancelButton = ref(null)
const accessRequestDecisionSaving = ref(false)
const accessRequestDecisionReloading = ref(false)
const accessRequestDecisionConflict = ref(false)
const decidingAccessRequest = ref(null)
const accessRequestDecision = ref('approve')
const accessRequestDecisionForm = reactive({ rationale: '' })
const reviewingFinding = ref(null)
const reviewSaving = ref(false)
const reviewBasisExpanded = ref([])
const reviewRationaleInput = ref(null)
const reviewForm = reactive({ decision: 'confirm', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
const manualRationaleInput = ref(null)
const manualAssessmentSaving = ref(false)
const manualAssessmentForm = reactive({ componentKey: '', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
const assessmentHistory = ref(null)
const assessmentHistoryLoading = ref(false)
const revisingAssessment = ref(null)
const assessmentRevisionRationaleInput = ref(null)
const assessmentRevisionSaving = ref(false)
const assessmentRevisionReloading = ref(false)
const assessmentRevisionConflict = ref(false)
const assessmentRevisionForm = reactive({ sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
const assessmentRevokeCancelButton = ref(null)
const assessmentRevokeSaving = ref(false)
const assessmentRevokeReloading = ref(false)
const assessmentRevokeConflict = ref(false)
const revokingAssessment = ref(null)
const assessmentRevokeForm = reactive({ rationale: '' })
const exemptionRevokeCancelButton = ref(null)
const exemptionRevokeSaving = ref(false)
const exemptionRevokeReloading = ref(false)
const exemptionRevokeConflict = ref(false)
const revokingExemption = ref(null)
const exemptionRevokeForm = reactive({ rationale: '' })
const policySaving = ref(false)
const policyReloading = ref(false)
const policyVersionConflict = ref(false)
const policyAssessment = ref(null)
const policyForm = reactive({ effect: '', rationale: '' })
const policyRestoreCancelButton = ref(null)
const policyRestoreSaving = ref(false)
const policyRestoreReloading = ref(false)
const policyRestoreConflict = ref(false)
const policyRestoreAssessment = ref(null)
const policyRestoreForm = reactive({ rationale: '' })

function requiredFieldRule(labelKey, options = {}) {
  return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
}

const accessRequestDecisionRules = computed(() => ({
  rationale: [requiredFieldRule('security.accessRequest.decisionRationaleLabel', { trigger: 'blur', whitespace: true })]
}))
const reviewRules = computed(() => ({
  decision: [requiredFieldRule('security.finding.decision')],
  sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
  securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
  rationale: [requiredFieldRule('security.finding.rationale', { trigger: 'blur', whitespace: true })]
}))
const manualAssessmentRules = computed(() => ({
  componentKey: [requiredFieldRule('security.assessment.component')],
  sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
  securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
  rationale: [requiredFieldRule('security.assessment.rationale', { trigger: 'blur', whitespace: true })]
}))
const assessmentRevisionRules = computed(() => ({
  sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
  securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
  rationale: [requiredFieldRule('security.assessment.revisionRationale', { trigger: 'blur', whitespace: true })]
}))
const assessmentRevokeRules = computed(() => ({
  rationale: [requiredFieldRule('security.assessment.revokeRationale', { trigger: 'blur', whitespace: true })]
}))
const exemptionRevokeRules = computed(() => ({
  rationale: [requiredFieldRule('security.exemption.revokeRationale', { trigger: 'blur', whitespace: true })]
}))
const policyRules = computed(() => ({
  effect: [requiredFieldRule('security.policy.effect')],
  rationale: [requiredFieldRule('security.policy.rationale', { trigger: 'blur', whitespace: true })]
}))
const policyRestoreRules = computed(() => ({
  rationale: [requiredFieldRule('security.policy.restoreRationale', { trigger: 'blur', whitespace: true })]
}))
const releaseRules = computed(() => ({
  reason: [requiredFieldRule('security.enrollment.releaseReasonLabel', { trigger: 'blur', whitespace: true })]
}))

async function validateRequiredForm(instanceRef) {
  if (!instanceRef.value) return false
  return instanceRef.value.validate().catch(() => false)
}

let selectedItemRequest = 0
let findingsRequest = 0
let assessmentHistoryRequest = 0
let accessRequestDecisionReloadRequest = 0
let assessmentRevisionReloadRequest = 0
let assessmentRevokeReloadRequest = 0
let exemptionRevokeReloadRequest = 0
let policyReloadRequest = 0
let policyRestoreReloadRequest = 0
let releaseReloadRequest = 0
let rediscoveryReloadRequest = 0
let reEnrollmentReloadRequest = 0
let refreshTimer = null
let autoRefreshStartedAt = 0
let autoRefreshTimedOut = false
let refreshHiddenAt = 0
let workspaceMounted = false
const discoveryRefreshWatches = new Map()

const canCreate = computed(() => auth.hasPermission('security.enrollment.create'))
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
const hasAccessRequestFilters = computed(() => Boolean(
  accessRequestFilters.resourceSearch ||
  accessRequestFilters.requesterSearch ||
  accessRequestFilters.state ||
  accessRequestFilters.authorizationState ||
  accessRequestCreatedRange.value?.length
))
const governanceLoading = computed(() => findingsLoading.value || assessmentsLoading.value || policiesLoading.value)
const manualAssessments = computed(() => assessments.value.filter(item => item.current?.source_kind === 'manual'))
const reviewQueueCapabilities = computed(() => {
  const capabilities = new Map(detectorCapabilities.value.map(item => [String(item.key || ''), item]))
  for (const finding of reviewQueueRows.value) {
    const capability = finding?.explanation?.capability
    const key = String(finding?.detector_version || '')
    if (key && !capabilities.has(key)) capabilities.set(key, capability?.key ? capability : { key })
  }
  return [...capabilities.values()].filter(item => item.key).sort((left, right) => String(left.key).localeCompare(String(right.key)))
})
const refreshFeedback = computed(() => {
  if (autoRefreshActive.value) return t('security.enrollment.autoRefreshing')
  if (!lastRefreshedAt.value) return ''
  const language = locale.value === 'en' ? 'en-US' : 'zh-CN'
  return t('security.enrollment.lastRefreshed', { time: lastRefreshedAt.value.toLocaleTimeString(language) })
})
const accessRequestDecisionTitle = computed(() => t(`security.accessRequest.${accessRequestDecision.value}`))
const accessRequestDecisionHint = computed(() => t(`security.accessRequest.${accessRequestDecision.value}Hint`))
const emptyDescription = computed(() => t(`security.enrollment.emptyStates.${listScope.value}`))
const reviewRationalePlaceholder = computed(() => t(`security.finding.rationalePlaceholders.${reviewForm.decision}`))
const reviewRemainingLabel = computed(() => {
  if (activeWorkspace.value === 'review-queue') {
    return t('security.finding.reviewRemainingQueue', { count: reviewQueueTotal.value })
  }
  return t('security.finding.reviewRemainingResource', { count: normalizeDiscoverySummary(detailRow.value).pendingReviewCount })
})
const releaseDialogTitle = computed(() => releaseBasis.value === 'no_supported_findings'
  ? t('security.enrollment.confirmNoProtectionNeeded')
  : t('security.enrollment.release'))
const releaseDialogWarning = computed(() => releaseBasis.value === 'no_supported_findings'
  ? t('security.enrollment.noFindingsReleaseWarning')
  : t('security.enrollment.releaseWarning'))
const releaseReasonPlaceholder = computed(() => releaseBasis.value === 'no_supported_findings'
  ? t('security.enrollment.noFindingsReleaseReason')
  : t('security.enrollment.releaseReason'))
const releaseConfirmLabel = computed(() => releaseBasis.value === 'no_supported_findings'
  ? t('security.enrollment.confirmNoProtectionNeededAction')
  : t('security.enrollment.confirmRelease'))
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

function findingResourceName(finding) {
  return resourceName({ target_snapshot: finding?.target_snapshot })
}

function capabilityOptionLabel(capability) {
  const key = String(capability?.key || '')
  const i18nKey = String(capability?.name_i18n_key || '')
  const translated = i18nKey ? t(i18nKey) : ''
  const name = translated && translated !== i18nKey ? translated : key
  return name === key ? key : `${name}（${key}）`
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

async function loadReviewQueue(page = reviewQueuePage.value) {
  if (!canReadFindings.value) {
    reviewQueueRows.value = []
    reviewQueueTotal.value = 0
    return false
  }
  reviewQueuePage.value = Number(page) || 1
  reviewQueueLoading.value = true
  try {
    const response = await findingAPI.list({
      snapshot_scope: 'current',
      review_state: 'pending',
      sensitive_data_type_id: reviewQueueTypeID.value || undefined,
      detector_version: reviewQueueDetectorVersion.value || undefined,
      page: reviewQueuePage.value,
      page_size: reviewQueuePageSize.value
    })
    reviewQueueRows.value = Array.isArray(response?.data) ? response.data : []
    reviewQueueTotal.value = Number(response?.total || 0)
    lastRefreshedAt.value = new Date()
    return true
  } catch (error) {
    ElMessage.error(error.message || t('security.reviewQueue.loadFailed'))
    return false
  } finally {
    reviewQueueLoading.value = false
  }
}

function accessRequestQueueParams(page) {
  const range = Array.isArray(accessRequestCreatedRange.value) ? accessRequestCreatedRange.value : []
  return {
    scope: accessRequestScope.value,
    state: accessRequestScope.value === 'history' ? accessRequestFilters.state || undefined : undefined,
    authorization_state: accessRequestScope.value === 'history' ? accessRequestFilters.authorizationState || undefined : undefined,
    requester_search: accessRequestFilters.requesterSearch || undefined,
    resource_search: accessRequestFilters.resourceSearch || undefined,
    created_from: range[0] instanceof Date ? range[0].toISOString() : undefined,
    created_to: range[1] instanceof Date ? range[1].toISOString() : undefined,
    page,
    page_size: accessRequestPageSize.value
  }
}

async function loadAccessRequestQueue(page = accessRequestPage.value) {
  if (!canReviewAccessRequests.value) {
    accessRequestRows.value = []
    accessRequestTotal.value = 0
    return
  }
  accessRequestPage.value = Number(page) || 1
  accessRequestLoading.value = true
  try {
    let response = await protectionAccessRequestAPI.reviewQueue(accessRequestQueueParams(accessRequestPage.value))
    const totalPages = Number(response?.total_pages || 0)
    if (totalPages > 0 && accessRequestPage.value > totalPages) {
      accessRequestPage.value = totalPages
      response = await protectionAccessRequestAPI.reviewQueue(accessRequestQueueParams(accessRequestPage.value))
    }
    accessRequestRows.value = Array.isArray(response?.data) ? response.data : []
    accessRequestTotal.value = Number(response?.total || 0)
  } catch (error) {
    ElMessage.error(error.message || t('security.accessRequest.loadFailed'))
  } finally {
    accessRequestLoading.value = false
  }
}

async function handleAccessRequestScopeChange() {
  accessRequestPage.value = 1
  accessRequestFilters.state = ''
  accessRequestFilters.authorizationState = ''
  await loadAccessRequestQueue(1)
}

async function applyAccessRequestFilters() {
  accessRequestPage.value = 1
  await loadAccessRequestQueue(1)
}

async function resetAccessRequestFilters() {
  accessRequestFilters.resourceSearch = ''
  accessRequestFilters.requesterSearch = ''
  accessRequestFilters.state = ''
  accessRequestFilters.authorizationState = ''
  accessRequestCreatedRange.value = []
  accessRequestPage.value = 1
  await loadAccessRequestQueue(1)
}

async function handleAccessRequestPageChange() {
  await loadAccessRequestQueue(accessRequestPage.value)
}

function accessRequestStateType(state) {
  if (state === 'approved') return 'success'
  if (state === 'rejected') return 'danger'
  return 'info'
}

function accessAuthorizationStateType(state) {
  if (state === 'active') return 'success'
  if (state === 'revoked') return 'danger'
  return 'info'
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

function openAccessRequestDecision(row, decision) {
  if (!row?.id || !row.can_decide || !['approve', 'reject'].includes(decision)) return
  accessRequestDecisionReloadRequest += 1
  decidingAccessRequest.value = row
  accessRequestDecision.value = decision
  accessRequestDecisionForm.rationale = ''
  accessRequestDecisionConflict.value = false
  accessRequestDecisionDialog.value = true
}

function closeAccessRequestDecisionDialog() {
  accessRequestDecisionReloadRequest += 1
  decidingAccessRequest.value = null
  accessRequestDecision.value = 'approve'
  accessRequestDecisionForm.rationale = ''
  accessRequestDecisionConflict.value = false
  accessRequestDecisionReloading.value = false
}

function replaceAccessRequest(latest) {
  const index = accessRequestRows.value.findIndex(item => item.id === latest?.id)
  if (index >= 0) accessRequestRows.value.splice(index, 1, latest)
}

async function reloadAccessRequestDecisionBaseline() {
  if (!decidingAccessRequest.value?.id) return
  const request = ++accessRequestDecisionReloadRequest
  accessRequestDecisionReloading.value = true
  try {
    const latest = await protectionAccessRequestAPI.getForReview(decidingAccessRequest.value.id)
    if (request !== accessRequestDecisionReloadRequest) return
    replaceAccessRequest(latest)
    if (latest?.state !== 'pending' || !latest.can_decide) {
      accessRequestDecisionDialog.value = false
      await loadAccessRequestQueue(accessRequestPage.value)
      ElMessage.info(t('security.accessRequest.alreadyProcessed'))
      return
    }
    decidingAccessRequest.value = latest
    accessRequestDecisionForm.rationale = ''
    accessRequestDecisionConflict.value = false
    ElMessage.success(t('security.accessRequest.decisionReloadedLatest'))
    focusAccessRequestDecisionCancel()
  } catch (error) {
    if (request !== accessRequestDecisionReloadRequest) return
    ElMessage.error(error.message || t('security.accessRequest.loadFailed'))
  } finally {
    if (request === accessRequestDecisionReloadRequest) accessRequestDecisionReloading.value = false
  }
}

async function submitAccessRequestDecision() {
  const row = decidingAccessRequest.value
  const decision = accessRequestDecision.value
  if (!row?.id || !['approve', 'reject'].includes(decision)) return
  if (!await validateRequiredForm(accessRequestDecisionFormRef)) return
  accessRequestDecisionSaving.value = true
  try {
    await protectionAccessRequestAPI.decide(row.id, {
      version: Number(row.version),
      decision,
      expires_at: decision === 'approve' ? row.requested_expires_at : undefined,
      rationale: accessRequestDecisionForm.rationale.trim()
    })
    accessRequestDecisionDialog.value = false
    ElMessage.success(t(`security.accessRequest.${decision}d`))
    accessRequestPage.value = 1
    await Promise.all([loadAccessRequestQueue(), load({ background: true })])
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      accessRequestDecisionConflict.value = true
      ElMessage.warning(t('security.accessRequest.decisionVersionConflict'))
      return
    }
    if (isProtectionAccessRequestExpired(error)) {
      accessRequestDecisionDialog.value = false
      await loadAccessRequestQueue(accessRequestPage.value)
      ElMessage.info(t('security.accessRequest.expiredBeforeDecision'))
      return
    }
    ElMessage.error(error.message || t('security.accessRequest.decisionFailed'))
  } finally {
    accessRequestDecisionSaving.value = false
  }
}

async function loadFindings(page = findingsPage.value) {
  const row = detailRow.value
  if (!canReadFindings.value || !row?.id || !row.latest_source_snapshot_hash || normalizeDiscoverySummary(row).findingCount === 0) {
    findings.value = []
    findingsTotal.value = 0
    return
  }
  const request = ++findingsRequest
  findingsPage.value = Number(page) || 1
  findingsLoading.value = true
  try {
    const response = await findingAPI.list({ enrollment_id: row.id, source_snapshot_hash: row.latest_source_snapshot_hash, discovery_execution_id: row.latest_discovery_execution_id, page: findingsPage.value, page_size: findingsPageSize })
    if (request !== findingsRequest) return
    findings.value = Array.isArray(response?.data) ? response.data : []
    findingsTotal.value = Number(response?.total || 0)
  } catch (error) {
    if (request === findingsRequest) ElMessage.error(error.message || t('security.finding.loadFailed'))
  } finally {
    if (request === findingsRequest) findingsLoading.value = false
  }
}

async function loadAssessments() {
  const row = detailRow.value
  if (!canReadAssessments.value || !row?.id) {
    assessments.value = []
    return
  }
  assessmentsLoading.value = true
  try {
    const response = await assessmentAPI.list({ enrollment_id: row.id, page: 1, page_size: 100 })
    assessments.value = Array.isArray(response?.data) ? response.data : []
  } catch (error) {
    ElMessage.error(error.message || t('security.assessment.loadFailed'))
  } finally {
    assessmentsLoading.value = false
  }
}

async function loadPolicies() {
  const row = detailRow.value
  if (!canReadPolicies.value || !row?.id) {
    policies.value = []
    return
  }
  policiesLoading.value = true
  try {
    const response = await protectionPolicyAPI.list({ enrollment_id: row.id, page: 1, page_size: 100 })
    policies.value = Array.isArray(response?.data) ? response.data : []
  } catch (error) {
    ElMessage.error(error.message || t('security.policy.loadFailed'))
  } finally {
    policiesLoading.value = false
  }
}

async function loadExemptions() {
  const row = detailRow.value
  if (!canReadExemptions.value || !row?.id) {
    exemptions.value = []
    return
  }
  exemptionsLoading.value = true
  try {
    const response = await protectionExemptionAPI.list({ enrollment_id: row.id, page: 1, page_size: 100 })
    exemptions.value = Array.isArray(response?.data) ? response.data : []
  } catch (error) {
    ElMessage.error(error.message || t('security.exemption.loadFailed'))
  } finally {
    exemptionsLoading.value = false
  }
}

async function loadGovernance(page = findingsPage.value) {
  await Promise.all([
    loadFindings(page),
    loadAssessments(),
    loadPolicies(),
    loadExemptions(),
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

function replaceExemption(latest) {
  const index = exemptions.value.findIndex(item => item.id === latest?.id)
  if (index >= 0) exemptions.value.splice(index, 1, latest)
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

function prepareFindingReview(finding, initialDecision = 'confirm') {
  const sourceType = sensitiveTypes.value.find(item => String(item.id) === String(finding.sensitive_data_type_id))
  reviewingFinding.value = finding
  reviewBasisExpanded.value = []
  reviewForm.decision = initialDecision
  reviewForm.sensitiveDataTypeID = String(finding.sensitive_data_type_id)
  reviewForm.securityGradeID = String(sourceType?.default_security_grade_id || '')
  reviewForm.rationale = ''
  reviewDialog.value = true
  focusReviewRationale()
}

async function openFindingReview(finding, initialDecision = 'confirm') {
  try {
    await loadFindingDefinitions()
  } catch (error) {
    ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed'))
    return
  }
  prepareFindingReview(finding, initialDecision)
}

function applyDefaultGrade(typeID) {
  const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
  manualAssessmentForm.securityGradeID = String(selectedType?.default_security_grade_id || '')
}

function applyReviewDefaultGrade(typeID) {
  const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
  reviewForm.securityGradeID = String(selectedType?.default_security_grade_id || '')
}

function applyAssessmentRevisionDefaultGrade(typeID) {
  const selectedType = sensitiveTypes.value.find(item => String(item.id) === String(typeID))
  assessmentRevisionForm.securityGradeID = String(selectedType?.default_security_grade_id || '')
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

function focusAssessmentRevisionRationale() {
  nextTick(() => {
    assessmentRevisionFormRef.value?.clearValidate()
    assessmentRevisionRationaleInput.value?.focus?.()
  })
}

async function openAssessmentRevision(assessment) {
  if (!assessment?.id) return
  assessmentRevisionReloadRequest += 1
  try {
    await loadFindingDefinitions()
  } catch (error) {
    ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed'))
    return
  }
  applyAssessmentRevisionBaseline(assessment)
  assessmentRevisionConflict.value = false
  assessmentRevisionDialog.value = true
}

function applyAssessmentRevisionBaseline(assessment) {
  revisingAssessment.value = assessment
  assessmentRevisionForm.sensitiveDataTypeID = String(assessment?.current?.sensitive_data_type_id || '')
  assessmentRevisionForm.securityGradeID = String(assessment?.current?.security_grade_id || '')
  assessmentRevisionForm.rationale = ''
  if (!activeGradesForType(assessmentRevisionForm.sensitiveDataTypeID).some(item => String(item.id) === assessmentRevisionForm.securityGradeID)) {
    applyAssessmentRevisionDefaultGrade(assessmentRevisionForm.sensitiveDataTypeID)
  }
}

function closeAssessmentRevision() {
  assessmentRevisionReloadRequest += 1
  revisingAssessment.value = null
  assessmentRevisionConflict.value = false
  assessmentRevisionReloading.value = false
}

async function reloadAssessmentRevisionBaseline() {
  if (!revisingAssessment.value?.id) return
  const request = ++assessmentRevisionReloadRequest
  assessmentRevisionReloading.value = true
  try {
    const latest = await assessmentAPI.get(revisingAssessment.value.id)
    if (request !== assessmentRevisionReloadRequest) return
    replaceAssessment(latest)
    applyAssessmentRevisionBaseline(latest)
    assessmentRevisionConflict.value = false
    ElMessage.success(t('security.assessment.reloadedLatest'))
    focusAssessmentRevisionRationale()
  } catch (error) {
    if (request !== assessmentRevisionReloadRequest) return
    ElMessage.error(error.message || t('security.assessment.reloadFailed'))
  } finally {
    if (request === assessmentRevisionReloadRequest) assessmentRevisionReloading.value = false
  }
}

function replaceAssessment(latest) {
  const index = assessments.value.findIndex(item => item.id === latest?.id)
  if (index >= 0) assessments.value.splice(index, 1, latest)
}

async function submitAssessmentRevision() {
  if (!revisingAssessment.value) return
  if (!await validateRequiredForm(assessmentRevisionFormRef)) return
  assessmentRevisionSaving.value = true
  try {
    const revised = await assessmentAPI.revise(revisingAssessment.value.id, buildAssessmentRevisionPayload({
      version: revisingAssessment.value.version,
      ...assessmentRevisionForm
    }))
    const index = assessments.value.findIndex(item => item.id === revised?.id)
    if (index >= 0) assessments.value.splice(index, 1, revised)
    assessmentRevisionDialog.value = false
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.assessment.revised'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      assessmentRevisionConflict.value = true
      ElMessage.warning(t('security.assessment.versionConflict'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    assessmentRevisionSaving.value = false
  }
}

function openPolicy(assessment) {
  policyReloadRequest += 1
  policyVersionConflict.value = false
  if (!applyPolicyBaseline(assessment, policyForAssessment(assessment))) return
  policyDialog.value = true
}

function applyPolicyBaseline(assessment, policy) {
  const effects = stricterPolicyEffects(assessment)
  if (!effects.length) {
    ElMessage.info(t('security.policy.alreadyStrictest'))
    return false
  }
  policyAssessment.value = assessment
  policyForm.effect = policy?.state === 'active' && effects.includes(policy.current?.effect)
    ? policy.current.effect
    : effects[0]
  policyForm.rationale = policy?.state === 'active' ? String(policy.current?.rationale || '') : ''
  return true
}

function closePolicyDialog() {
  policyReloadRequest += 1
  policyAssessment.value = null
  policyVersionConflict.value = false
  policyReloading.value = false
}

function clearPolicyValidation() {
  nextTick(() => policyFormRef.value?.clearValidate())
}

async function reloadPolicyBaseline() {
  const assessment = policyAssessment.value
  const policy = policyForAssessment(assessment)
  if (!assessment?.id || !policy?.id) return
  const request = ++policyReloadRequest
  policyReloading.value = true
  try {
    const latestContext = await fetchLatestPolicyContext(assessment, policy)
    if (request !== policyReloadRequest) return
    applyLatestPolicyContext(latestContext)
    if (!applyPolicyBaseline(latestContext.assessment, latestContext.policy)) {
      policyDialog.value = false
      return
    }
    policyVersionConflict.value = false
    ElMessage.success(t('security.policy.reloadedLatest'))
  } catch (error) {
    if (request !== policyReloadRequest) return
    ElMessage.error(error.message || t('security.policy.reloadFailed'))
  } finally {
    if (request === policyReloadRequest) policyReloading.value = false
  }
}

async function fetchLatestPolicyContext(assessment, policy) {
  const [latestPolicy, latestAssessment, latestBaselines] = await Promise.all([
    protectionPolicyAPI.get(policy.id),
    assessmentAPI.get(assessment.id),
    protectionBaselineAPI.list()
  ])
  return { policy: latestPolicy, assessment: latestAssessment, baselines: latestBaselines }
}

function applyLatestPolicyContext(context) {
  protectionBaselines.value = Array.isArray(context.baselines) ? context.baselines : []
  const policyIndex = policies.value.findIndex(item => item.id === context.policy?.id)
  if (policyIndex >= 0) policies.value.splice(policyIndex, 1, context.policy)
  replaceAssessment(context.assessment)
}

async function savePolicy() {
  if (!policyAssessment.value) return
  if (!await validateRequiredForm(policyFormRef)) return
  policySaving.value = true
  try {
    const existing = policyForAssessment(policyAssessment.value)
    if (existing) {
      await protectionPolicyAPI.update(existing.id, {
        version: Number(existing.version),
        effect: policyForm.effect,
        rationale: policyForm.rationale.trim()
      })
    } else {
      await protectionPolicyAPI.create({
        assessment_id: policyAssessment.value.id,
        consumer_owner: 'manager',
        action: 'preview',
        effect: policyForm.effect,
        rationale: policyForm.rationale.trim()
      })
    }
    policyDialog.value = false
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.policy.saved'))
  } catch (error) {
    if (policyForAssessment(policyAssessment.value) && isResourceVersionConflict(error)) {
      policyVersionConflict.value = true
      ElMessage.warning(t('security.policy.versionConflict'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    policySaving.value = false
  }
}

function focusPolicyRestoreCancel() {
  nextTick(() => {
    policyRestoreFormRef.value?.clearValidate()
    const cancelButton = policyRestoreCancelButton.value?.$el || policyRestoreCancelButton.value
    cancelButton?.focus?.()
  })
}

function openPolicyRestore(assessment) {
  const policy = policyForAssessment(assessment)
  if (!policy || policy.state !== 'active') return
  policyRestoreReloadRequest += 1
  policyRestoreAssessment.value = assessment
  policyRestoreForm.rationale = ''
  policyRestoreConflict.value = false
  policyRestoreDialog.value = true
}

function closePolicyRestoreDialog() {
  policyRestoreReloadRequest += 1
  policyRestoreAssessment.value = null
  policyRestoreForm.rationale = ''
  policyRestoreConflict.value = false
  policyRestoreReloading.value = false
}

async function reloadPolicyRestoreBaseline() {
  const assessment = policyRestoreAssessment.value
  const policy = policyForAssessment(assessment)
  if (!assessment?.id || !policy?.id) return
  const request = ++policyRestoreReloadRequest
  policyRestoreReloading.value = true
  try {
    const latestContext = await fetchLatestPolicyContext(assessment, policy)
    if (request !== policyRestoreReloadRequest) return
    applyLatestPolicyContext(latestContext)
    if (latestContext.policy?.state !== 'active' || latestContext.assessment?.current?.conclusion !== 'sensitive') {
      policyRestoreDialog.value = false
      ElMessage.info(t('security.policy.alreadyRestored'))
      return
    }
    policyRestoreAssessment.value = latestContext.assessment
    policyRestoreForm.rationale = ''
    policyRestoreConflict.value = false
    ElMessage.success(t('security.policy.restoreReloadedLatest'))
    focusPolicyRestoreCancel()
  } catch (error) {
    if (request !== policyRestoreReloadRequest) return
    ElMessage.error(error.message || t('security.policy.reloadFailed'))
  } finally {
    if (request === policyRestoreReloadRequest) policyRestoreReloading.value = false
  }
}

async function submitPolicyRestore() {
  const assessment = policyRestoreAssessment.value
  const policy = policyForAssessment(assessment)
  if (!policy || policy.state !== 'active') return
  if (!await validateRequiredForm(policyRestoreFormRef)) return
  policyRestoreSaving.value = true
  try {
    await protectionPolicyAPI.revoke(policy.id, {
      version: Number(policy.version),
      rationale: policyRestoreForm.rationale.trim()
    })
    policyRestoreDialog.value = false
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.policy.restored'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      policyRestoreConflict.value = true
      ElMessage.warning(t('security.policy.restoreVersionConflict'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    policyRestoreSaving.value = false
  }
}

function focusAssessmentRevokeCancel() {
  nextTick(() => {
    assessmentRevokeFormRef.value?.clearValidate()
    const cancelButton = assessmentRevokeCancelButton.value?.$el || assessmentRevokeCancelButton.value
    cancelButton?.focus?.()
  })
}

function openAssessmentRevoke(assessment) {
  if (!assessment?.id || assessment.current?.conclusion !== 'sensitive') return
  assessmentRevokeReloadRequest += 1
  revokingAssessment.value = assessment
  assessmentRevokeForm.rationale = ''
  assessmentRevokeConflict.value = false
  assessmentRevokeDialog.value = true
}

function closeAssessmentRevokeDialog() {
  assessmentRevokeReloadRequest += 1
  revokingAssessment.value = null
  assessmentRevokeForm.rationale = ''
  assessmentRevokeConflict.value = false
  assessmentRevokeReloading.value = false
}

async function reloadAssessmentRevokeBaseline() {
  if (!revokingAssessment.value?.id) return
  const request = ++assessmentRevokeReloadRequest
  assessmentRevokeReloading.value = true
  try {
    const latest = await assessmentAPI.get(revokingAssessment.value.id)
    if (request !== assessmentRevokeReloadRequest) return
    replaceAssessment(latest)
    if (latest?.current?.conclusion !== 'sensitive') {
      assessmentRevokeDialog.value = false
      ElMessage.info(t('security.assessment.alreadyRevoked'))
      return
    }
    revokingAssessment.value = latest
    assessmentRevokeForm.rationale = ''
    assessmentRevokeConflict.value = false
    ElMessage.success(t('security.assessment.revokeReloadedLatest'))
    focusAssessmentRevokeCancel()
  } catch (error) {
    if (request !== assessmentRevokeReloadRequest) return
    ElMessage.error(error.message || t('security.assessment.reloadFailed'))
  } finally {
    if (request === assessmentRevokeReloadRequest) assessmentRevokeReloading.value = false
  }
}

async function submitAssessmentRevoke() {
  if (!revokingAssessment.value?.id) return
  if (!await validateRequiredForm(assessmentRevokeFormRef)) return
  assessmentRevokeSaving.value = true
  try {
    await assessmentAPI.revoke(revokingAssessment.value.id, {
      version: Number(revokingAssessment.value.version),
      rationale: assessmentRevokeForm.rationale.trim()
    })
    assessmentRevokeDialog.value = false
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.assessment.revoked'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      assessmentRevokeConflict.value = true
      ElMessage.warning(t('security.assessment.revokeVersionConflict'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    assessmentRevokeSaving.value = false
  }
}

function focusReviewRationale() {
  nextTick(() => {
    reviewFormRef.value?.clearValidate()
    reviewRationaleInput.value?.focus?.()
  })
}

async function nextQueueReviewFinding(reviewedFinding) {
  const reviewedIndex = reviewQueueRows.value.findIndex(item => item.id === reviewedFinding.id)
  const loaded = await loadReviewQueue(reviewQueuePage.value)
  if (!loaded) return null

  let continuation = resolvePendingReviewContinuation({
    rows: reviewQueueRows.value,
    total: reviewQueueTotal.value,
    page: reviewQueuePage.value,
    pageSize: reviewQueuePageSize.value,
    reviewedIndex
  })
  if (continuation.reload) {
    reviewQueuePage.value = continuation.page
    if (!await loadReviewQueue(continuation.page)) return null
    continuation = resolvePendingReviewContinuation({
      rows: reviewQueueRows.value,
      total: reviewQueueTotal.value,
      page: reviewQueuePage.value,
      pageSize: reviewQueuePageSize.value,
      reviewedIndex
    })
    await navigateWorkspace('review-queue')
  }
  return continuation.finding
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

async function submitFindingReview() {
  if (!reviewingFinding.value) return
  if (!await validateRequiredForm(reviewFormRef)) return
  reviewSaving.value = true
  const reviewedFinding = reviewingFinding.value
  const continueFromQueue = activeWorkspace.value === 'review-queue'
  try {
    const payload = buildFindingReviewPayload(reviewForm)
    await findingAPI.review(reviewedFinding.id, payload)
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
    reviewSaving.value = false
    return
  }

  try {
    let nextFinding = null
    if (continueFromQueue) {
      nextFinding = await nextQueueReviewFinding(reviewedFinding)
    } else {
      await load()
      await loadFindings(findingsPage.value)
      scheduleAutoRefresh({ reset: true })
      nextFinding = await nextDetailReviewFinding()
    }
    if (nextFinding) {
      prepareFindingReview(nextFinding)
      ElMessage.success(t('security.finding.reviewSavedAndContinued'))
    } else {
      reviewDialog.value = false
      reviewingFinding.value = null
      ElMessage.success(t('security.finding.reviewSaved'))
    }
  } catch (error) {
    reviewDialog.value = false
    reviewingFinding.value = null
    ElMessage.error(error.message || t('security.finding.loadFailed'))
  } finally {
    reviewSaving.value = false
  }
}

async function loadEngines() {
  try {
    const engines = await listResourceTreeEngines('/api/v1/meta')
    engineNames.value = new Map(engines.map(engine => [Number(engine.id), engine.name]))
  } catch {
    engineNames.value = new Map()
  }
}

function reconcileDiscoveryRefreshWatches() {
  for (const [enrollmentID, baselineMarker] of discoveryRefreshWatches) {
    const row = rows.value.find(item => item.id === enrollmentID)
    if (!row || row.state === 'released') {
      discoveryRefreshWatches.delete(enrollmentID)
      continue
    }
    if (row.last_discovered_at && discoveryRefreshMarker(row) !== baselineMarker) {
      discoveryRefreshWatches.delete(enrollmentID)
    }
  }
}

function hasPendingRefresh() {
  reconcileDiscoveryRefreshWatches()
  return discoveryRefreshWatches.size > 0 || rows.value.some(needsEnrollmentRefresh)
}

function clearRefreshTimer() {
  if (refreshTimer !== null) window.clearTimeout(refreshTimer)
  refreshTimer = null
}

function stopAutoRefresh() {
  clearRefreshTimer()
  autoRefreshActive.value = false
  autoRefreshStartedAt = 0
}

function scheduleAutoRefresh({ reset = false } = {}) {
  clearRefreshTimer()
  if (!hasPendingRefresh()) {
    stopAutoRefresh()
    return
  }

  const now = Date.now()
  if (reset || !autoRefreshStartedAt) {
    autoRefreshStartedAt = now
    autoRefreshTimedOut = false
  }
  autoRefreshActive.value = true
  if (document.hidden) return

  const elapsed = now - autoRefreshStartedAt
  if (elapsed >= AUTO_REFRESH_TIMEOUT_MS) {
    stopAutoRefresh()
    if (!autoRefreshTimedOut) {
      autoRefreshTimedOut = true
      ElMessage.warning(t('security.enrollment.autoRefreshTimedOut'))
    }
    return
  }

  const delay = elapsed < AUTO_REFRESH_FAST_WINDOW_MS
    ? AUTO_REFRESH_FAST_INTERVAL_MS
    : AUTO_REFRESH_SLOW_INTERVAL_MS
  refreshTimer = window.setTimeout(runAutoRefresh, delay)
}

async function load(options = {}) {
  const background = Boolean(options?.background)
  if (loading.value || backgroundRefreshing.value) return false
  const previousDetailMarker = discoveryRefreshMarker(detailRow.value)
  if (background) backgroundRefreshing.value = true
  else loading.value = true
  try {
    const response = await protectionEnrollmentAPI.list({ scope: listScope.value, page: currentPage.value, page_size: pageSize.value })
    rows.value = response?.data || []
    total.value = Number(response?.total || 0)
    if (detailRow.value) {
      const visibleDetail = rows.value.find(row => row.id === detailRow.value.id)
      detailRow.value = visibleDetail || await protectionEnrollmentAPI.get(detailRow.value.id)
    }
    lastRefreshedAt.value = new Date()
    if (options?.syncFindings && detailRow.value && discoveryRefreshMarker(detailRow.value) !== previousDetailMarker) {
      findingsPage.value = 1
      await loadGovernance(1)
    }
    return true
  } catch (error) {
    if (!options?.silent) ElMessage.error(error.message || t('security.common.failed'))
    return false
  } finally {
    if (background) backgroundRefreshing.value = false
    else loading.value = false
  }
}

async function runAutoRefresh() {
  refreshTimer = null
  if (document.hidden) return
  await load({ background: true, silent: true, syncFindings: true })
  scheduleAutoRefresh()
}

async function manualRefresh() {
  if (manualRefreshing.value) return
  manualRefreshing.value = true
  try {
    if (activeWorkspace.value === 'review-queue') {
      await loadReviewQueue(reviewQueuePage.value)
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
  await load()
  scheduleAutoRefresh({ reset: true })
}

async function handleScopeChange() {
  currentPage.value = 1
  detailDrawer.value = false
  await load()
  scheduleAutoRefresh({ reset: true })
}

function reviewQueueRouteQuery() {
  const filters = resolveReviewQueueFilters({
    sensitive_data_type_id: reviewQueueTypeID.value,
    detector_version: reviewQueueDetectorVersion.value,
    page: reviewQueuePage.value,
    page_size: reviewQueuePageSize.value
  })
  return { tab: 'review-queue', ...filters.query }
}

async function navigateWorkspace(workspace, history = 'replace') {
  const query = workspace === 'review-queue' ? reviewQueueRouteQuery() : {}
  const location = { path: '/protection-enrollments', query }
  if (router.resolve(location).fullPath === route.fullPath) return
  await navigateConsoleModuleRoute(router, 'security', location, { history })
}

async function handleWorkspaceChange(workspace) {
  stopAutoRefresh()
  await navigateWorkspace(workspace)
}

async function handleReviewQueueFilterChange() {
  reviewQueuePage.value = 1
  await navigateWorkspace('review-queue')
}

async function resetReviewQueueFilters() {
  reviewQueueTypeID.value = ''
  reviewQueueDetectorVersion.value = ''
  reviewQueuePage.value = 1
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
  selectedResource.value = null
  selectedItem.value = null
  initialLocator.value = String(locator || '').trim()
  createDrawer.value = true
}

async function handleResourceSelect(selection) {
  const request = ++selectedItemRequest
  selectedItem.value = null
  const itemID = Number(selection?.identity?.item_id || 0)
  if (!itemID) return
  selectedItemLoading.value = true
  try {
    const item = await metaAPI.getItem(itemID)
    if (request === selectedItemRequest) selectedItem.value = item
  } catch (error) {
    if (request === selectedItemRequest) ElMessage.error(error.message || t('security.enrollment.itemLoadFailed'))
  } finally {
    if (request === selectedItemRequest) selectedItemLoading.value = false
  }
}

async function createEnrollment() {
  const locator = String(selectedResource.value?.identity?.locator || '').trim()
  if (!locator || !selectedItem.value) return ElMessage.warning(t('security.enrollment.resourceRequired'))
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

function setExemptionCardRef(exemptionID, element) {
  if (element) exemptionCardRefs.set(exemptionID, element)
  else exemptionCardRefs.delete(exemptionID)
}

async function focusExemptionCard() {
  if (!focusedExemptionID.value) return
  await nextTick()
  exemptionCardRefs.get(focusedExemptionID.value)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

async function openDetail(row, exemptionID = '') {
  detailRow.value = row
  focusedExemptionID.value = exemptionID
  detailDrawer.value = true
  findingsPage.value = 1
  findings.value = []
  findingsTotal.value = 0
  assessments.value = []
  policies.value = []
  exemptions.value = []
  await loadGovernance(1)
  await focusExemptionCard()
}

function handleDetailClosed() {
  findingsRequest += 1
  findings.value = []
  findingsTotal.value = 0
  assessments.value = []
  policies.value = []
  exemptions.value = []
  findingsLoading.value = false
  policiesLoading.value = false
  exemptionsLoading.value = false
  focusedExemptionID.value = ''
  exemptionCardRefs.clear()
  detailRow.value = null
}

function focusRediscoveryCancel() {
  nextTick(() => (rediscoveryCancelButton.value?.$el || rediscoveryCancelButton.value)?.focus?.())
}

function openRediscovery(row) {
  if (!row?.id || !['enrolling', 'active'].includes(row.state)) return
  rediscoveryReloadRequest += 1
  rediscoveryEnrollment.value = row
  rediscoveryConflict.value = false
  rediscoveryDialog.value = true
}

function closeRediscoveryDialog() {
  rediscoveryReloadRequest += 1
  rediscoveryEnrollment.value = null
  rediscoveryConflict.value = false
  rediscoveryReloading.value = false
}

async function reloadRediscoveryBaseline() {
  if (!rediscoveryEnrollment.value?.id) return
  const request = ++rediscoveryReloadRequest
  rediscoveryReloading.value = true
  try {
    const latest = await protectionEnrollmentAPI.get(rediscoveryEnrollment.value.id)
    if (request !== rediscoveryReloadRequest) return
    replaceEnrollment(latest)
    if (!['enrolling', 'active'].includes(latest?.state)) {
      rediscoveryDialog.value = false
      await load({ background: true })
      ElMessage.info(t('security.enrollment.rediscoveryUnavailable'))
      return
    }
    rediscoveryEnrollment.value = latest
    rediscoveryConflict.value = false
    ElMessage.success(t('security.enrollment.rediscoveryReloadedLatest'))
    focusRediscoveryCancel()
  } catch (error) {
    if (request !== rediscoveryReloadRequest) return
    ElMessage.error(error.message || t('security.enrollment.rediscoveryLoadFailed'))
  } finally {
    if (request === rediscoveryReloadRequest) rediscoveryReloading.value = false
  }
}

async function submitRediscovery() {
  const row = rediscoveryEnrollment.value
  if (!row?.id || rediscoveryConflict.value) return
  rediscoverySaving.value = true
  try {
    const baselineMarker = discoveryRefreshMarker(row)
    const execution = await protectionEnrollmentAPI.rediscover(row.id, { version: Number(row.version) })
    if (Number(execution?.enrollment_version) > 0) {
      replaceEnrollment({ ...row, version: Number(execution.enrollment_version) })
    }
    discoveryRefreshWatches.set(row.id, baselineMarker)
    rediscoveryDialog.value = false
    await load({ background: true, syncFindings: true })
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.rediscoveryCreated'))
    if (execution?.execution_id) await openMonitorExecution(execution.execution_id)
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      rediscoveryConflict.value = true
      ElMessage.warning(t('security.enrollment.rediscoveryVersionConflict'))
      return
    }
    if (isDiscoveryExecutionInProgress(error)) {
      discoveryRefreshWatches.set(row.id, discoveryRefreshMarker(row))
      rediscoveryDialog.value = false
      await load({ background: true })
      scheduleAutoRefresh({ reset: true })
      ElMessage.info(t('security.enrollment.discoveryExecutionInProgress'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    rediscoverySaving.value = false
  }
}

function focusReEnrollmentCancel() {
  nextTick(() => (reEnrollmentCancelButton.value?.$el || reEnrollmentCancelButton.value)?.focus?.())
}

function openReEnrollment(row) {
  if (!row?.id || row.state !== 'released') return
  reEnrollmentReloadRequest += 1
  reEnrollmentSource.value = row
  reEnrollmentConflict.value = false
  reEnrollmentDialog.value = true
}

function closeReEnrollmentDialog() {
  reEnrollmentReloadRequest += 1
  reEnrollmentSource.value = null
  reEnrollmentConflict.value = false
  reEnrollmentReloading.value = false
}

async function reloadReEnrollmentBaseline() {
  if (!reEnrollmentSource.value?.id) return
  const request = ++reEnrollmentReloadRequest
  reEnrollmentReloading.value = true
  try {
    const latest = await protectionEnrollmentAPI.get(reEnrollmentSource.value.id)
    if (request !== reEnrollmentReloadRequest) return
    replaceEnrollment(latest)
    if (latest?.state !== 'released') {
      reEnrollmentDialog.value = false
      await load({ background: true })
      ElMessage.info(t('security.enrollment.reEnrollmentUnavailable'))
      return
    }
    reEnrollmentSource.value = latest
    reEnrollmentConflict.value = false
    ElMessage.success(t('security.enrollment.reEnrollmentReloadedLatest'))
    focusReEnrollmentCancel()
  } catch (error) {
    if (request !== reEnrollmentReloadRequest) return
    ElMessage.error(error.message || t('security.enrollment.reEnrollmentLoadFailed'))
  } finally {
    if (request === reEnrollmentReloadRequest) reEnrollmentReloading.value = false
  }
}

async function submitReEnrollment() {
  const source = reEnrollmentSource.value
  if (!source?.id || reEnrollmentConflict.value) return
  reEnrollmentSaving.value = true
  try {
    const result = await protectionEnrollmentAPI.reEnroll(source.id, { version: Number(source.version) })
    replaceEnrollment({ ...source, version: Number(result.source_enrollment_version) })
    reEnrollmentDialog.value = false
    detailDrawer.value = false
    listScope.value = 'current'
    currentPage.value = 1
    rows.value = [result.enrollment]
    total.value = 1
    await load()
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.reEnrolled'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      reEnrollmentConflict.value = true
      ElMessage.warning(t('security.enrollment.reEnrollmentVersionConflict'))
      return
    }
    if (isProtectionEnrollmentAlreadyActive(error)) {
      reEnrollmentDialog.value = false
      detailDrawer.value = false
      listScope.value = 'current'
      currentPage.value = 1
      await load({ background: true })
      scheduleAutoRefresh({ reset: true })
      ElMessage.info(t('security.enrollment.enrollmentAlreadyActive'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    reEnrollmentSaving.value = false
  }
}

function openRelease(row, basis = 'manual') {
  releaseReloadRequest += 1
  releasing.value = row
  releaseBasis.value = basis
  releaseForm.reason = ''
  releaseConflict.value = false
  releaseDialog.value = true
}

function focusReleaseCancel() {
  nextTick(() => {
    releaseFormRef.value?.clearValidate()
    const cancelButton = releaseCancelButton.value?.$el || releaseCancelButton.value
    cancelButton?.focus?.()
  })
}

function closeReleaseDialog() {
  releaseReloadRequest += 1
  releasing.value = null
  releaseForm.reason = ''
  releaseBasis.value = 'manual'
  releaseConflict.value = false
  releaseReloading.value = false
}

function replaceEnrollment(latest) {
  const index = rows.value.findIndex(item => item.id === latest?.id)
  if (index >= 0) rows.value.splice(index, 1, latest)
  if (detailRow.value?.id === latest?.id) detailRow.value = latest
}

async function reloadReleaseBaseline() {
  if (!releasing.value?.id) return
  const request = ++releaseReloadRequest
  releaseReloading.value = true
  try {
    const latest = await protectionEnrollmentAPI.get(releasing.value.id)
    if (request !== releaseReloadRequest) return
    replaceEnrollment(latest)
    if (['releasing', 'released'].includes(latest?.state)) {
      releaseDialog.value = false
      await load({ background: true })
      ElMessage.info(t('security.enrollment.alreadyReleasing'))
      return
    }
    if (releaseBasis.value === 'no_supported_findings' && !isZeroFindingDiscovery(latest)) {
      releaseDialog.value = false
      await load({ background: true })
      ElMessage.info(t('security.enrollment.noSupportedFindingsReleaseUnavailable'))
      return
    }
    releasing.value = latest
    releaseForm.reason = ''
    releaseConflict.value = false
    ElMessage.success(t('security.enrollment.releaseReloadedLatest'))
    focusReleaseCancel()
  } catch (error) {
    if (request !== releaseReloadRequest) return
    ElMessage.error(error.message || t('security.enrollment.releaseLoadFailed'))
  } finally {
    if (request === releaseReloadRequest) releaseReloading.value = false
  }
}

async function releaseEnrollment() {
  if (!releasing.value?.id || releaseConflict.value) return
  if (!await validateRequiredForm(releaseFormRef)) return
  releaseSaving.value = true
  try {
    const released = await protectionEnrollmentAPI.release(releasing.value.id, {
      version: Number(releasing.value.version),
      basis: releaseBasis.value,
      reason: releaseForm.reason.trim()
    })
    replaceEnrollment(released)
    releaseDialog.value = false
    await load()
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.releaseStarted'))
  } catch (error) {
    if (isResourceVersionConflict(error)) {
      releaseConflict.value = true
      ElMessage.warning(t('security.enrollment.releaseVersionConflict'))
      return
    }
    if (isNoSupportedFindingsReleaseUnavailable(error)) {
      releaseDialog.value = false
      await load({ background: true })
      ElMessage.info(t('security.enrollment.noSupportedFindingsReleaseUnavailable'))
      return
    }
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    releaseSaving.value = false
  }
}

async function handleCreateClosed() {
  selectedItemRequest += 1
  selectedResource.value = null
  selectedItem.value = null
  selectedItemLoading.value = false
  initialLocator.value = ''
  if (route.query.action === 'enroll' || route.query.locator) {
    await navigateConsoleModuleRoute(router, 'security', { path: '/protection-enrollments' }, { history: 'replace' })
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    refreshHiddenAt = Date.now()
    clearRefreshTimer()
    return
  }
  if (refreshHiddenAt && autoRefreshStartedAt) {
    autoRefreshStartedAt += Date.now() - refreshHiddenAt
  }
  refreshHiddenAt = 0
  if (autoRefreshActive.value) runAutoRefresh()
}

watch(() => route.query, async routeQuery => {
  const routeState = resolveWorkspaceRouteState(routeQuery)
  if (routeState.changed) {
    const location = { path: '/protection-enrollments', query: routeState.query }
    await navigateConsoleModuleRoute(router, 'security', location, { history: 'replace' })
    return
  }
  activeWorkspace.value = routeState.tab
  reviewQueueTypeID.value = routeState.reviewQueue.sensitiveDataTypeID
  reviewQueueDetectorVersion.value = routeState.reviewQueue.detectorVersion
  reviewQueuePage.value = routeState.reviewQueue.page
  reviewQueuePageSize.value = routeState.reviewQueue.pageSize
  if (!workspaceMounted) return
  if (routeState.tab === 'review-queue') {
    stopAutoRefresh()
    await Promise.all([loadReviewQueue(routeState.reviewQueue.page), loadFindingDefinitions(), loadDetectorCapabilities()])
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
  document.addEventListener('visibilitychange', handleVisibilityChange)
  workspaceMounted = true
  if (activeWorkspace.value === 'review-queue') {
    await Promise.all([loadReviewQueue(reviewQueuePage.value), loadFindingDefinitions(), loadDetectorCapabilities()])
  } else {
    await Promise.all([load(), loadAccessRequestQueue()])
    scheduleAutoRefresh({ reset: true })
  }
  await loadEngines()
})

onBeforeUnmount(() => {
  clearRefreshTimer()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
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
.access-review-card { margin-bottom: 12px; border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.access-review-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.access-review-card__header p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.access-review-card__scope { display: flex; align-items: center; gap: 10px; }
.access-review-filters { display: flex; flex-wrap: wrap; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--addp-border-color); }
.access-review-filters > .el-input { width: min(240px, 100%); }
.access-review-filters > .el-select { width: min(180px, 100%); }
.access-review-filters > :deep(.el-date-editor) { width: min(360px, 100%); }
.access-actor { display: flex; flex-direction: column; gap: 2px; }
.access-actor span { color: var(--addp-text-secondary); font-size: 12px; }
.access-authorization-state { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.access-authorization-state span { color: var(--addp-text-secondary); font-size: 12px; }
.access-authorization-link { height: auto; padding: 0; }
.list-scope-bar { display: flex; align-items: center; margin-bottom: 12px; }
.enrollment-card { border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.review-queue-intro { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 12px; padding: 13px 15px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.review-queue-intro p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.review-queue-filters { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.review-queue-filters .el-select { width: min(340px, 100%); }
.review-queue-card :deep(.el-card__body) { padding-top: 8px; }
.access-decision-actions { display: flex; align-items: center; gap: 4px; }
.access-decision-unavailable { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; color: var(--addp-text-secondary); font-size: 12px; }
.queue-candidate, .queue-recognition, .queue-evidence { display: flex; min-width: 0; flex-direction: column; align-items: flex-start; gap: 5px; }
.queue-candidate span, .queue-evidence small { color: var(--addp-text-secondary); font-size: 12px; }
.queue-recognition code { max-width: 100%; overflow: hidden; color: var(--addp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.queue-actions { display: flex; flex-wrap: wrap; gap: 2px; }
.resource-cell { display: flex; width: 100%; flex-direction: column; align-items: flex-start; gap: 5px; padding: 4px 0; color: inherit; text-align: left; border: 0; background: transparent; cursor: pointer; }
.resource-name { color: var(--el-color-primary); font-size: 15px; font-weight: 600; }
.resource-path { max-width: 100%; overflow: hidden; color: var(--addp-text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.resource-meta { display: flex; align-items: center; gap: 8px; color: var(--addp-text-tertiary); font-size: 12px; }
.state-cell { display: flex; flex-direction: column; align-items: flex-start; gap: 7px; }
.state-cell span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.45; }
.discovery-cell { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
.discovery-cell span { color: var(--addp-text-tertiary); font-size: 12px; }
.owner-grid { display: grid; grid-template-columns: repeat(2, minmax(170px, 1fr)); gap: 8px 16px; }
.owner-item { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.owner-name { color: var(--addp-text-secondary); font-size: 13px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 16px; }
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
.finding-list { display: flex; flex-direction: column; gap: 12px; }
.finding-card { container: finding-card / inline-size; padding: 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.finding-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-card__header > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.finding-card__header strong { overflow-wrap: anywhere; font-size: 15px; }
.finding-card__header span { color: var(--addp-text-secondary); font-size: 12px; }
.finding-explanation { display: grid; grid-template-columns: minmax(0, .9fr) minmax(0, 1fr) minmax(0, 1.25fr); overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.explanation-stage { min-width: 0; padding: 12px; }
.explanation-stage + .explanation-stage { border-left: 1px solid var(--addp-border-color); }
.explanation-stage__title { display: flex; align-items: center; gap: 7px; margin-bottom: 10px; color: var(--addp-text-primary); }
.explanation-stage__title > span { display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center; flex: 0 0 auto; color: var(--el-color-primary); font-size: 12px; font-weight: 700; border: 1px solid var(--el-color-primary); border-radius: 50%; }
.explanation-stage__title strong { font-size: 13px; }
.explanation-stage p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.explanation-stage .explanation-primary { color: var(--addp-text-primary); font-weight: 600; }
.explanation-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.detection-rule-audit { display: flex; flex-direction: column; gap: 7px; margin: 12px 0 0; padding-top: 10px; border-top: 1px dashed var(--addp-border-color); }
.detection-rule-audit > div { display: grid; grid-template-columns: 88px minmax(0, 1fr); gap: 8px; font-size: 12px; line-height: 1.5; }
.detection-rule-audit dt { color: var(--addp-text-tertiary); }
.detection-rule-audit dd { margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.detection-rule-audit__details { align-items: center; }
.detection-rule-audit__details dd { display: flex; align-items: center; }
.rule-help-button { min-height: 24px; padding: 0 2px; }
.recognition-rule-details { display: flex; flex-direction: column; gap: 10px; margin: 0; }
.recognition-rule-details > div { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 10px; font-size: 12px; line-height: 1.6; }
.recognition-rule-details dt { color: var(--addp-text-tertiary); }
.recognition-rule-details dd { margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
:global(.security-rule-popover) { max-width: calc(100vw - 32px); }
.finding-outlets { display: flex; flex-direction: column; gap: 7px; }
.finding-outlet { display: grid; grid-template-columns: minmax(78px, auto) minmax(0, 1fr) auto; align-items: center; gap: 7px; font-size: 12px; }
.finding-outlet > span { color: var(--addp-text-secondary); }
.finding-outlet > strong { min-width: 0; color: var(--addp-text-primary); font-weight: 500; line-height: 1.4; overflow-wrap: anywhere; }
.finding-observed-at { margin: 8px 0 0; color: var(--addp-text-tertiary); font-size: 12px; text-align: right; }
.review-result { margin-top: 12px; padding: 10px 12px; border-left: 3px solid var(--el-color-primary); background: var(--addp-bg-primary); }
.review-result span { color: var(--addp-text-tertiary); font-size: 12px; }
.review-result p { margin: 4px 0 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.finding-card__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.finding-pagination { justify-content: flex-end; margin-top: 14px; }
.manual-assessment-list { margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--addp-border-color); }
.manual-assessment-list h5 { margin: 0 0 10px; font-size: 14px; }
.manual-assessment-card { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.manual-assessment-card + .manual-assessment-card { margin-top: 8px; }
.manual-assessment-card > div:first-child { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.manual-assessment-card strong { overflow-wrap: anywhere; }
.manual-assessment-card span, .manual-assessment-card p { margin: 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.manual-assessment-card .resource-policy-summary, .resource-policy-summary { color: var(--addp-text-primary); font-weight: 600; }
.manual-assessment-card__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
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
.exemption-section { margin-top: 24px; }
.exemption-section__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.exemption-section__header h4 { margin: 0; }
.exemption-section__header p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.exemption-loading { margin-top: 14px; }
.exemption-list { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
.exemption-card { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.exemption-card.is-focused { border-color: var(--el-color-primary); box-shadow: 0 0 0 1px var(--el-color-primary); }
.exemption-card__main { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 4px; }
.exemption-card__title { display: flex; align-items: center; gap: 8px; }
.exemption-card__title strong { min-width: 0; overflow-wrap: anywhere; }
.exemption-card__main > span, .exemption-card__main > p { margin: 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.exemption-card__actions { display: flex; flex: 0 0 auto; gap: 4px; }
.form-help { display: block; margin-top: 6px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.review-target { display: flex; flex-direction: column; gap: 5px; margin-bottom: 18px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.review-target__header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: 12px; }
.review-target strong { overflow-wrap: anywhere; }
.review-target__header strong { min-width: 0; }
.review-target__header .el-tag { flex: 0 0 auto; }
.review-target span { color: var(--addp-text-secondary); font-size: 13px; }
.review-basis { margin: -6px 0 18px; border: 1px solid var(--addp-border-color); border-radius: 8px; }
.review-basis :deep(.el-collapse-item__header) { min-height: 44px; padding: 0 12px; border-bottom: 0; border-radius: 8px; background: var(--addp-bg-secondary); }
.review-basis :deep(.el-collapse-item__wrap) { border-bottom: 0; border-radius: 0 0 8px 8px; background: var(--addp-bg-primary); }
.review-basis :deep(.el-collapse-item__content) { padding: 0; }
.review-basis__title { display: flex; min-width: 0; align-items: center; gap: 7px; }
.review-basis__title svg { width: 16px; height: 16px; flex: 0 0 auto; color: var(--el-color-primary); }
.review-basis__title span { flex: 0 0 auto; color: var(--addp-text-primary); font-weight: 600; }
.review-basis__title small { overflow: hidden; color: var(--addp-text-tertiary); font-weight: 400; text-overflow: ellipsis; white-space: nowrap; }
.review-basis__facts { display: flex; flex-direction: column; gap: 0; margin: 0; padding: 4px 14px 12px; }
.review-basis__facts > div { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 12px; padding: 9px 0; border-top: 1px solid var(--addp-border-color-light); font-size: 12px; line-height: 1.6; }
.review-basis__facts dt { color: var(--addp-text-tertiary); }
.review-basis__facts dd { display: flex; min-width: 0; flex-wrap: wrap; gap: 6px 10px; margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.review-basis__outlets { flex-direction: column; }
.review-basis__outlets span { display: grid; grid-template-columns: minmax(82px, auto) minmax(0, 1fr); gap: 8px; }
.review-basis__outlets strong { color: var(--addp-text-primary); font-weight: 500; }
.decision-group { display: flex; width: 100%; }
.decision-group :deep(.el-radio-button) { flex: 1; }
.decision-group :deep(.el-radio-button__inner) { width: 100%; }
.manual-assessment-form { margin-top: 16px; }
.component-option { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.component-option small { color: var(--addp-text-tertiary); }
.wide { width: 100%; }
.detail-facts { margin-top: 22px; }
.technical-details { margin-top: 18px; }
.technical-value { overflow-wrap: anywhere; font-family: monospace; color: var(--addp-text-secondary); }
.detail-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
.release-form { margin-top: 16px; }
:deep(.el-card__body) { padding: 0; }
:deep(.el-table) { background: var(--addp-bg-primary); }
:deep(.el-drawer__body) { padding-top: 8px; }
@container finding-card (max-width: 920px) {
  .finding-explanation { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .explanation-stage:nth-child(3) { grid-column: 1 / -1; border-top: 1px solid var(--addp-border-color); border-left: 0; }
}
@container finding-card (max-width: 560px) {
  .finding-explanation { grid-template-columns: 1fr; }
  .explanation-stage:nth-child(3) { grid-column: auto; }
  .explanation-stage + .explanation-stage { border-top: 1px solid var(--addp-border-color); border-left: 0; }
}
@media (max-width: 1280px) {
  .owner-grid { grid-template-columns: 1fr; }
}
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
  .access-review-card__header { flex-direction: column; }
  .access-review-filters > .el-input, .access-review-filters > .el-select, .access-review-filters > :deep(.el-date-editor) { width: 100%; }
  .review-queue-intro { align-items: flex-start; flex-direction: column; }
  .review-queue-filters .el-select { width: 100%; }
  .finding-section__header, .manual-assessment-card, .exemption-section__header, .exemption-card { flex-direction: column; }
  .finding-section__actions { width: 100%; justify-content: space-between; }
  .review-basis__title small { display: none; }
  .review-basis__facts > div { grid-template-columns: 1fr; gap: 4px; }
}
</style>
