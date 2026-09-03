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
                :loading="reenrollingID === row.id"
                @click="reEnroll(row)"
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
              <div v-else-if="activeAssessmentForFinding(finding) && canUpdateAssessments" class="finding-card__actions">
                <el-button type="danger" plain @click="revokeAssessment(activeAssessmentForFinding(finding))">
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
              </div>
              <div class="manual-assessment-card__actions">
                <el-tag :type="assessment.current?.conclusion === 'sensitive' ? 'success' : 'info'">
                  {{ assessmentConclusionLabel(assessment.current?.conclusion) }}
                </el-tag>
                <el-button
                  v-if="assessment.current?.conclusion === 'sensitive' && canUpdateAssessments"
                  link
                  type="danger"
                  @click="revokeAssessment(assessment)"
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
            :loading="reenrollingID === detailRow.id"
            @click="reEnroll(detailRow)"
          >
            {{ t('security.enrollment.reEnroll') }}
          </el-button>
          <el-button
            v-if="canRelease && ['enrolling', 'active'].includes(detailRow.state)"
            :loading="rediscovering"
            @click="rediscover(detailRow)"
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
        <el-form label-position="top">
          <el-form-item :label="t('security.finding.decision')" required>
            <el-radio-group v-model="reviewForm.decision" class="decision-group">
              <el-radio-button value="confirm">{{ t('security.finding.decisions.confirm') }}</el-radio-button>
              <el-radio-button value="adjust">{{ t('security.finding.decisions.adjust') }}</el-radio-button>
              <el-radio-button value="reject">{{ t('security.finding.decisions.reject') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <template v-if="reviewForm.decision === 'adjust'">
            <el-form-item :label="t('security.finding.sensitiveDataType')" required>
              <el-select v-model="reviewForm.sensitiveDataTypeID" class="wide" :placeholder="t('security.finding.selectSensitiveDataType')">
                <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('security.finding.securityGrade')" required>
              <el-select v-model="reviewForm.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
                <el-option v-for="item in securityGrades" :key="item.id" :label="item.name" :value="String(item.id)" />
              </el-select>
            </el-form-item>
          </template>
          <el-form-item :label="t('security.finding.rationale')" required>
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
      <el-form class="manual-assessment-form" label-position="top">
        <el-form-item :label="t('security.assessment.component')" required>
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
        <el-form-item :label="t('security.finding.sensitiveDataType')" required>
          <el-select v-model="manualAssessmentForm.sensitiveDataTypeID" class="wide" :placeholder="t('security.finding.selectSensitiveDataType')" @change="applyDefaultGrade">
            <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.finding.securityGrade')" required>
          <el-select v-model="manualAssessmentForm.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
            <el-option v-for="item in securityGrades" :key="item.id" :label="item.name" :value="String(item.id)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.assessment.rationale')" required>
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

    <el-dialog v-model="releaseDialog" class="addp-dialog" :title="releaseDialogTitle" width="min(560px, calc(100vw - 24px))">
      <el-alert type="warning" :closable="false" :title="releaseDialogWarning" />
      <el-input
        v-model="releaseReason"
        class="release-reason"
        type="textarea"
        :rows="3"
        :placeholder="releaseReasonPlaceholder"
      />
      <template #footer>
        <el-button @click="releaseDialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="danger" :loading="saving" @click="releaseEnrollment">{{ releaseConfirmLabel }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled, Refresh } from '@element-plus/icons-vue'
import {
  ResourceTreePicker,
  listResourceTreeEngines,
  navigateConsoleModuleRoute,
  openMonitorExecution,
  resolveCanonicalTabRouteState
} from '@common-ui'
import { assessmentAPI, classificationAPI, detectorCapabilityAPI, findingAPI, gradeAPI, metaAPI, protectionEnrollmentAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import {
  buildFindingReviewPayload,
  discoveryRefreshMarker,
  findingDecisionState,
  findingOutletRules,
  findingReviewState,
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
const rediscovering = ref(false)
const reenrollingID = ref('')
const createDrawer = ref(false)
const detailDrawer = ref(false)
const releaseDialog = ref(false)
const reviewDialog = ref(false)
const manualAssessmentDialog = ref(false)
const selectedResource = ref(null)
const selectedItem = ref(null)
const selectedItemLoading = ref(false)
const initialLocator = ref('')
const detailRow = ref(null)
const releasing = ref(null)
const releaseReason = ref('')
const releaseBasis = ref('manual')
const engineNames = ref(new Map())
const findings = ref([])
const findingsTotal = ref(0)
const findingsPage = ref(1)
const findingsPageSize = 20
const findingsLoading = ref(false)
const assessments = ref([])
const assessmentsLoading = ref(false)
const componentOptions = ref([])
const componentsLoading = ref(false)
const sensitiveTypes = ref([])
const securityClassifications = ref([])
const securityGrades = ref([])
const detectorCapabilities = ref([])
const reviewQueueRows = ref([])
const reviewQueueTotal = ref(0)
const reviewQueuePage = ref(initialWorkspaceRoute.reviewQueue.page)
const reviewQueuePageSize = ref(initialWorkspaceRoute.reviewQueue.pageSize)
const reviewQueueTypeID = ref(initialWorkspaceRoute.reviewQueue.sensitiveDataTypeID)
const reviewQueueDetectorVersion = ref(initialWorkspaceRoute.reviewQueue.detectorVersion)
const reviewQueueLoading = ref(false)
const reviewingFinding = ref(null)
const reviewSaving = ref(false)
const reviewBasisExpanded = ref([])
const reviewRationaleInput = ref(null)
const reviewForm = reactive({ decision: 'confirm', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
const manualRationaleInput = ref(null)
const manualAssessmentSaving = ref(false)
const manualAssessmentForm = reactive({ componentKey: '', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
let selectedItemRequest = 0
let findingsRequest = 0
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
const governanceLoading = computed(() => findingsLoading.value || assessmentsLoading.value)
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
  const id = String(finding?.explanation?.assessment_id || '')
  if (!id) return null
  const assessment = assessments.value.find(item => item.id === id)
  return assessment?.current?.conclusion === 'sensitive' ? assessment : null
}

function assessmentSummary(assessment) {
  return t('security.assessment.summary', {
    type: typeName(assessment.current?.sensitive_data_type_id),
    classification: classificationName(assessment.current?.security_classification_id),
    grade: gradeName(assessment.current?.security_grade_id)
  })
}

function assessmentConclusionLabel(conclusion) {
  const normalized = conclusion === 'sensitive' ? 'sensitive' : 'not_sensitive'
  return t(`security.assessment.conclusions.${normalized}`)
}

async function loadFindingDefinitions() {
  if (sensitiveTypes.value.length && securityClassifications.value.length && securityGrades.value.length) return
  const [types, classifications, grades] = await Promise.all([sensitiveDataTypeAPI.list(), classificationAPI.list(), gradeAPI.list()])
  sensitiveTypes.value = Array.isArray(types) ? types : []
  securityClassifications.value = Array.isArray(classifications) ? classifications : []
  securityGrades.value = Array.isArray(grades) ? grades : []
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

async function loadGovernance(page = findingsPage.value) {
  await Promise.all([
    loadFindings(page),
    loadAssessments(),
    loadFindingDefinitions().catch(error => ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed')))
  ])
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
  nextTick(() => manualRationaleInput.value?.focus?.())
}

async function submitManualAssessment() {
  if (!manualAssessmentForm.componentKey || !manualAssessmentForm.sensitiveDataTypeID || !manualAssessmentForm.securityGradeID || !manualAssessmentForm.rationale.trim()) {
    return ElMessage.warning(t('security.assessment.required'))
  }
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

async function revokeAssessment(assessment) {
  try {
    const result = await ElMessageBox.prompt(
      t('security.assessment.revokePrompt', { component: assessment.component_key }),
      t('security.assessment.revokeTitle'),
      {
        confirmButtonText: t('security.assessment.confirmRevoke'),
        cancelButtonText: t('security.common.cancel'),
        inputType: 'textarea',
        inputPlaceholder: t('security.assessment.revokeRationalePlaceholder'),
        inputValidator: value => String(value || '').trim() ? true : t('security.assessment.revokeRationaleRequired')
      }
    )
    await assessmentAPI.revoke(assessment.id, {
      version: Number(assessment.version),
      rationale: String(result.value || '').trim()
    })
    await load({ background: true })
    await loadGovernance(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.assessment.revoked'))
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error.message || t('security.common.failed'))
  }
}

function focusReviewRationale() {
  nextTick(() => reviewRationaleInput.value?.focus?.())
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
  if (!reviewForm.rationale.trim()) return ElMessage.warning(t('security.finding.rationaleRequired'))
  if (reviewForm.decision === 'adjust' && (!reviewForm.sensitiveDataTypeID || !reviewForm.securityGradeID)) {
    return ElMessage.warning(t('security.finding.adjustmentRequired'))
  }
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
      await load({ background: true, syncFindings: true })
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

function openDetail(row) {
  detailRow.value = row
  detailDrawer.value = true
  findingsPage.value = 1
  findings.value = []
  findingsTotal.value = 0
  assessments.value = []
  loadGovernance(1)
}

function handleDetailClosed() {
  findingsRequest += 1
  findings.value = []
  findingsTotal.value = 0
  assessments.value = []
  findingsLoading.value = false
  detailRow.value = null
}

async function rediscover(row) {
  rediscovering.value = true
  try {
    const baselineMarker = discoveryRefreshMarker(row)
    const execution = await protectionEnrollmentAPI.rediscover(row.id, { version: Number(row.version) })
    discoveryRefreshWatches.set(row.id, baselineMarker)
    await load({ background: true, syncFindings: true })
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.rediscoveryCreated'))
    if (execution?.execution_id) await openMonitorExecution(execution.execution_id)
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    rediscovering.value = false
  }
}

async function reEnroll(row) {
  try {
    await ElMessageBox.confirm(
      t('security.enrollment.reEnrollWarning', { resource: resourceName(row) }),
      t('security.enrollment.reEnroll'),
      {
        confirmButtonText: t('security.enrollment.confirmReEnroll'),
        cancelButtonText: t('security.common.cancel'),
        type: 'warning'
      }
    )
    reenrollingID.value = row.id
    await protectionEnrollmentAPI.reEnroll(row.id, { version: Number(row.version) })
    detailDrawer.value = false
    listScope.value = 'current'
    currentPage.value = 1
    await load()
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.reEnrolled'))
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    reenrollingID.value = ''
  }
}

function openRelease(row, basis = 'manual') {
  releasing.value = row
  releaseBasis.value = basis
  releaseReason.value = ''
  releaseDialog.value = true
}

async function releaseEnrollment() {
  if (!releaseReason.value.trim()) return ElMessage.warning(t('security.enrollment.releaseReasonRequired'))
  saving.value = true
  try {
    await protectionEnrollmentAPI.release(releasing.value.id, {
      version: Number(releasing.value.version),
      basis: releaseBasis.value,
      reason: releaseReason.value.trim()
    })
    releaseDialog.value = false
    await load()
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.enrollment.releaseStarted'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    saving.value = false
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
    await load()
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
    await load()
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
.list-scope-bar { display: flex; align-items: center; margin-bottom: 12px; }
.enrollment-card { border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.review-queue-intro { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 12px; padding: 13px 15px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.review-queue-intro p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.review-queue-filters { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.review-queue-filters .el-select { width: min(340px, 100%); }
.review-queue-card :deep(.el-card__body) { padding-top: 8px; }
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
.manual-assessment-card__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
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
.release-reason { margin-top: 16px; }
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
  .review-queue-intro { align-items: flex-start; flex-direction: column; }
  .review-queue-filters .el-select { width: 100%; }
  .finding-section__header, .manual-assessment-card { flex-direction: column; }
  .finding-section__actions { width: 100%; justify-content: space-between; }
  .review-basis__title small { display: none; }
  .review-basis__facts > div { grid-template-columns: 1fr; gap: 4px; }
}
</style>
