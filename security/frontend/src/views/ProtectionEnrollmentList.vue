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
        <el-button v-if="canCreate" type="primary" @click="openCreate()">
          {{ t('security.enrollment.create') }}
        </el-button>
      </div>
    </div>

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

        <el-table-column :label="t('security.enrollment.discovery')" width="210">
          <template #default="{ row }">
            <div class="discovery-cell">
              <el-tag size="small" :type="discoveryPresentation(row).type">{{ discoveryPresentation(row).label }}</el-tag>
              <span>{{ formatDateTime(row.last_discovered_at) }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ t('security.enrollment.viewDetails') }}</el-button>
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

        <h4>{{ t('security.enrollment.discovery') }}</h4>
        <el-alert
          :type="discoveryPresentation(detailRow).alertType"
          :closable="false"
          :title="discoveryPresentation(detailRow).detailTitle"
          :description="discoveryPresentation(detailRow).detailDescription"
          show-icon
        />

        <section v-if="canReadFindings && normalizeDiscoverySummary(detailRow).findingCount > 0" class="finding-section">
          <div class="finding-section__header">
            <div>
              <h4>{{ t('security.finding.currentSnapshot') }}</h4>
              <p>{{ t('security.finding.currentSnapshotHint') }}</p>
            </div>
            <el-tag v-if="normalizeDiscoverySummary(detailRow).pendingReviewCount > 0" type="warning">
              {{ t('security.finding.pendingCount', { count: normalizeDiscoverySummary(detailRow).pendingReviewCount }) }}
            </el-tag>
            <el-tag v-else type="success">{{ t('security.finding.reviewCompleted') }}</el-tag>
          </div>

          <el-skeleton v-if="findingsLoading" :rows="3" animated />
          <el-empty v-else-if="findings.length === 0" :description="t('security.finding.currentSnapshotEmpty')" />
          <div v-else class="finding-list">
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
              <el-descriptions :column="2" size="small" border>
                <el-descriptions-item :label="t('security.finding.confidence')">{{ confidenceLabel(finding.confidence) }}</el-descriptions-item>
                <el-descriptions-item :label="t('security.finding.detector')">{{ finding.detector_version }}</el-descriptions-item>
                <el-descriptions-item :label="t('security.finding.evidence')">{{ evidenceDescription(finding) }}</el-descriptions-item>
                <el-descriptions-item :label="t('security.finding.observedAt')">{{ formatDateTime(finding.observed_at) }}</el-descriptions-item>
              </el-descriptions>
              <div v-if="finding.review" class="review-result">
                <span>{{ t('security.finding.reviewRationale') }}</span>
                <p>{{ finding.review.rationale }}</p>
              </div>
              <div v-else-if="canReviewFindings" class="finding-card__actions">
                <el-button type="primary" plain @click="openFindingReview(finding)">{{ t('security.finding.review') }}</el-button>
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
        </section>

        <h4>{{ t('security.enrollment.ownerProtection') }}</h4>
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
            </el-descriptions>
          </el-collapse-item>
        </el-collapse>

        <div class="detail-actions">
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
          <strong>{{ reviewingFinding.component_key }}</strong>
          <span>{{ typeName(reviewingFinding.sensitive_data_type_id) }} · {{ confidenceLabel(reviewingFinding.confidence) }}</span>
        </div>
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
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  ResourceTreePicker,
  listResourceTreeEngines,
  navigateConsoleModuleRoute,
  openMonitorExecution
} from '@common-ui'
import { findingAPI, gradeAPI, metaAPI, protectionEnrollmentAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import {
  buildFindingReviewPayload,
  discoveryRefreshMarker,
  findingReviewState,
  isZeroFindingDiscovery,
  needsEnrollmentRefresh,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'

const AUTO_REFRESH_FAST_INTERVAL_MS = 2000
const AUTO_REFRESH_SLOW_INTERVAL_MS = 5000
const AUTO_REFRESH_FAST_WINDOW_MS = 30000
const AUTO_REFRESH_TIMEOUT_MS = 120000

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

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
const createDrawer = ref(false)
const detailDrawer = ref(false)
const releaseDialog = ref(false)
const reviewDialog = ref(false)
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
const sensitiveTypes = ref([])
const securityGrades = ref([])
const reviewingFinding = ref(null)
const reviewSaving = ref(false)
const reviewRationaleInput = ref(null)
const reviewForm = reactive({ decision: 'confirm', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })
let selectedItemRequest = 0
let findingsRequest = 0
let refreshTimer = null
let autoRefreshStartedAt = 0
let autoRefreshTimedOut = false
let refreshHiddenAt = 0
const discoveryRefreshWatches = new Map()

const canCreate = computed(() => auth.hasPermission('security.enrollment.create'))
const canRelease = computed(() => auth.hasPermission('security.enrollment.update'))
const canReadFindings = computed(() => auth.hasPermission('security.finding.read'))
const canReviewFindings = computed(() => auth.hasPermission('security.finding.update'))
const refreshFeedback = computed(() => {
  if (autoRefreshActive.value) return t('security.enrollment.autoRefreshing')
  if (!lastRefreshedAt.value) return ''
  const language = locale.value === 'en' ? 'en-US' : 'zh-CN'
  return t('security.enrollment.lastRefreshed', { time: lastRefreshedAt.value.toLocaleTimeString(language) })
})
const emptyDescription = computed(() => t(`security.enrollment.emptyStates.${listScope.value}`))
const reviewRationalePlaceholder = computed(() => t(`security.finding.rationalePlaceholders.${reviewForm.decision}`))
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
  if (row.state === 'active') return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
  const activeOwners = (row.owner_progress || []).filter(owner => owner.acknowledged && owner.projection_state === 'active').length
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
  const effects = Array.isArray(owner.effects) ? owner.effects : []
  if (!owner.acknowledged) return t('security.enrollment.ownerEffectWaiting')
  if (owner.projection_state === 'enrolling') {
    return owner.consumer_owner === 'manager'
      ? t('security.enrollment.ownerEffectDeniedPendingRule')
      : t('security.enrollment.ownerEffectDeniedUnsupported')
  }
  return effects.length ? effects.map(effectLabel).join('、') : t('security.enrollment.ownerEffectActive')
}

function typeName(id) {
  const match = sensitiveTypes.value.find(item => String(item.id) === String(id))
  return match?.name || t('security.finding.typeId', { id })
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

function findingStatePresentation(finding) {
  const state = findingReviewState(finding)
  const types = { pending: 'warning', confirm: 'success', adjust: 'primary', reject: 'info' }
  return { type: types[state], label: t(`security.finding.states.${state}`) }
}

async function loadFindingDefinitions() {
  if (sensitiveTypes.value.length && securityGrades.value.length) return
  const [types, grades] = await Promise.all([sensitiveDataTypeAPI.list(), gradeAPI.list()])
  sensitiveTypes.value = Array.isArray(types) ? types : []
  securityGrades.value = Array.isArray(grades) ? grades : []
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
    const [response] = await Promise.all([
      findingAPI.list({ enrollment_id: row.id, source_snapshot_hash: row.latest_source_snapshot_hash, page: findingsPage.value, page_size: findingsPageSize }),
      loadFindingDefinitions()
    ])
    if (request !== findingsRequest) return
    findings.value = Array.isArray(response?.data) ? response.data : []
    findingsTotal.value = Number(response?.total || 0)
  } catch (error) {
    if (request === findingsRequest) ElMessage.error(error.message || t('security.finding.loadFailed'))
  } finally {
    if (request === findingsRequest) findingsLoading.value = false
  }
}

async function openFindingReview(finding) {
  try {
    await loadFindingDefinitions()
  } catch (error) {
    ElMessage.error(error.message || t('security.finding.loadDefinitionsFailed'))
    return
  }
  const sourceType = sensitiveTypes.value.find(item => String(item.id) === String(finding.sensitive_data_type_id))
  reviewingFinding.value = finding
  reviewForm.decision = 'confirm'
  reviewForm.sensitiveDataTypeID = String(finding.sensitive_data_type_id)
  reviewForm.securityGradeID = String(sourceType?.default_security_grade_id || '')
  reviewForm.rationale = ''
  reviewDialog.value = true
}

function focusReviewRationale() {
  nextTick(() => reviewRationaleInput.value?.focus?.())
}

async function submitFindingReview() {
  if (!reviewForm.rationale.trim()) return ElMessage.warning(t('security.finding.rationaleRequired'))
  if (reviewForm.decision === 'adjust' && (!reviewForm.sensitiveDataTypeID || !reviewForm.securityGradeID)) {
    return ElMessage.warning(t('security.finding.adjustmentRequired'))
  }
  reviewSaving.value = true
  try {
    const payload = buildFindingReviewPayload(reviewForm)
    await findingAPI.review(reviewingFinding.value.id, payload)
    reviewDialog.value = false
    await load()
    await loadFindings(findingsPage.value)
    scheduleAutoRefresh({ reset: true })
    ElMessage.success(t('security.finding.reviewSaved'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
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
      await loadFindings(1)
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
    await load({ background: true, syncFindings: true })
    scheduleAutoRefresh({ reset: true })
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
  loadFindings(1)
}

function handleDetailClosed() {
  findingsRequest += 1
  findings.value = []
  findingsTotal.value = 0
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

watch(
  () => [route.query.action, route.query.locator],
  ([action, locator]) => {
    if (action === 'enroll' && canCreate.value) openCreate(locator)
  },
  { immediate: true }
)

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await load()
  scheduleAutoRefresh({ reset: true })
  loadEngines()
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
.list-scope-bar { display: flex; align-items: center; margin-bottom: 12px; }
.enrollment-card { border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
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
h4 { margin: 24px 0 12px; }
.owner-detail-list { display: flex; flex-direction: column; gap: 10px; }
.owner-detail { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.owner-detail div { display: flex; flex-direction: column; gap: 5px; }
.owner-detail span { color: var(--addp-text-secondary); font-size: 12px; }
.finding-section { margin-top: 24px; }
.finding-section__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-section__header h4 { margin: 0; }
.finding-section__header p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.finding-list { display: flex; flex-direction: column; gap: 12px; }
.finding-card { padding: 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.finding-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-card__header > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.finding-card__header strong { overflow-wrap: anywhere; font-size: 15px; }
.finding-card__header span { color: var(--addp-text-secondary); font-size: 12px; }
.review-result { margin-top: 12px; padding: 10px 12px; border-left: 3px solid var(--el-color-primary); background: var(--addp-bg-primary); }
.review-result span { color: var(--addp-text-tertiary); font-size: 12px; }
.review-result p { margin: 4px 0 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.finding-card__actions { display: flex; justify-content: flex-end; margin-top: 12px; }
.finding-pagination { justify-content: flex-end; margin-top: 14px; }
.review-target { display: flex; flex-direction: column; gap: 5px; margin-bottom: 18px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.review-target strong { overflow-wrap: anywhere; }
.review-target span { color: var(--addp-text-secondary); font-size: 13px; }
.decision-group { display: flex; width: 100%; }
.decision-group :deep(.el-radio-button) { flex: 1; }
.decision-group :deep(.el-radio-button__inner) { width: 100%; }
.wide { width: 100%; }
.detail-facts { margin-top: 22px; }
.technical-details { margin-top: 18px; }
.technical-value { overflow-wrap: anywhere; font-family: monospace; color: var(--addp-text-secondary); }
.detail-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
.release-reason { margin-top: 16px; }
:deep(.el-card__body) { padding: 0; }
:deep(.el-table) { background: var(--addp-bg-primary); }
:deep(.el-drawer__body) { padding-top: 8px; }
@media (max-width: 1280px) {
  .owner-grid { grid-template-columns: 1fr; }
}
</style>
