<template>
  <div
    class="page-container"
    data-testid="catalog-entry-detail"
    :data-load-state="loading ? 'loading' : error ? 'error' : entry ? 'loaded' : 'idle'"
    :data-entry-id="entry?.id || ''"
  >
    <div class="page-header">
      <el-button text :icon="ArrowLeft" @click="goBack">{{ t('catalog.common.back') }}</el-button>
      <div class="header-actions">
		<el-button v-if="entry" :type="marks.favorite ? 'primary' : ''" :icon="Star" :loading="markSaving" @click="toggleMark('favorite')">
		  {{ marks.favorite ? t('catalog.marks.favorited') : t('catalog.marks.favorite') }}
		</el-button>
		<el-button v-if="entry" :type="marks.following ? 'primary' : ''" :icon="Bell" :loading="markSaving" @click="toggleMark('following')">
		  {{ marks.following ? t('catalog.marks.following') : t('catalog.marks.follow') }}
		</el-button>
		<el-button v-if="canRebind && entry?.entry_status === 'active' && entry?.source?.source_status === 'missing'" type="warning" @click="openRebind">
		  {{ t('catalog.rebind.action') }}
		</el-button>
        <el-button v-if="canEdit && entry?.entry_status === 'active' && !editing" type="primary" :icon="Edit" @click="editing = true">
          {{ t('catalog.edit.action') }}
        </el-button>
        <el-button :icon="Refresh" :loading="loading" @click="reloadLatest">{{ t('catalog.common.refresh') }}</el-button>
      </div>
    </div>

    <el-skeleton v-if="loading && !entry" :rows="8" animated />
    <el-result v-else-if="error" icon="error" :title="t('catalog.entry.loadFailed')" :sub-title="error">
      <template #extra><el-button type="primary" @click="loadEntry">{{ t('catalog.common.retry') }}</el-button></template>
    </el-result>
    <template v-else-if="entry">
      <el-card shadow="never" class="summary-card">
        <div class="summary-header">
          <div class="summary-title">
            <h1>{{ entry.display_name || t('catalog.entries.unnamed') }}</h1>
            <span class="entry-id">{{ entry.id }}</span>
          </div>
          <div class="summary-tags">
			<el-tag v-if="entry.source" :type="sourceTagType(entry.source.source_status)">{{ t(`catalog.status.source.${entry.source.source_status}`) }}</el-tag>
            <el-tag :type="governanceTagType(entry.governance_status)">{{ t(`catalog.status.governance.${entry.governance_status}`) }}</el-tag>
          </div>
        </div>
        <p v-if="entry.business_description" class="description">{{ entry.business_description }}</p>
      </el-card>

      <el-card v-if="entry.recommended_successor_entry_id" shadow="never" class="successor-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.impact.governanceTitle') }}</strong>
            <el-button v-if="entry.recommended_successor" text type="primary" @click="goRecommendedSuccessor">
              {{ t('catalog.successor.open') }}
            </el-button>
          </div>
        </template>
        <el-alert type="warning" :closable="false" show-icon :title="t('catalog.successor.description')" class="owner-alert" />
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="t('catalog.successor.entry')">
            {{ entry.recommended_successor?.display_name || t('catalog.edit.referenceUnavailable') }}
          </el-descriptions-item>
          <el-descriptions-item v-if="entry.recommended_successor" :label="t('catalog.entries.governanceStatus')">
            {{ t(`catalog.status.governance.${entry.recommended_successor.governance_status}`) }}
          </el-descriptions-item>
          <el-descriptions-item v-if="entry.recommended_successor" :label="t('catalog.entries.sourceStatus')">
            {{ t(`catalog.status.source.${entry.recommended_successor.source_status}`) }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

	  <el-result v-if="entry.entry_status === 'merged'" icon="info" :title="t('catalog.rebind.mergedTitle')" :sub-title="t('catalog.rebind.mergedDescription')">
		<template #extra>
		  <el-button type="primary" @click="goCanonicalEntry">{{ t('catalog.rebind.openCanonical') }}</el-button>
		</template>
	  </el-result>

	  <template v-else>

      <EntryEditor
        v-if="editing"
        :entry="entry"
        :saving="saving"
        :conflict="conflict"
        :can-certify="canCertify"
        :can-deprecate="canDeprecate"
        @submit="saveEntry"
        @cancel="cancelEdit"
        @reload="reloadLatest"
      />

      <el-row :gutter="16" class="detail-grid">
        <el-col :xs="24" :lg="12">
          <el-card shadow="never" class="detail-card">
            <template #header><strong>{{ t('catalog.entry.catalogFacts') }}</strong></template>
            <el-descriptions :column="1" border>
              <el-descriptions-item :label="t('catalog.entry.businessName')">{{ entry.business_name || '-' }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entries.type')">{{ entryTypeLabel(entry.entry_type) }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entries.visibility')">{{ t(`catalog.status.visibility.${entry.visibility}`) }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entry.entryStatus')">{{ t(`catalog.status.entry.${entry.entry_status}`) }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entry.version')">{{ entry.version }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entries.updatedAt')">{{ formatDate(entry.updated_at) }}</el-descriptions-item>
            </el-descriptions>
          </el-card>
        </el-col>
        <el-col :xs="24" :lg="12">
          <el-card shadow="never" class="detail-card">
            <template #header><strong>{{ t('catalog.entry.sourceFacts') }}</strong></template>
            <el-descriptions :column="1" border>
              <el-descriptions-item :label="t('catalog.entry.sourceModule')">{{ entry.source.source_module }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entry.sourceIdentity')"><span class="break-all">{{ entry.source.source_identity }}</span></el-descriptions-item>
              <el-descriptions-item v-if="entry.source.source_module === 'meta'" :label="t('catalog.entries.engineId')">{{ entry.source.observed_snapshot?.engine_id ?? '-' }}</el-descriptions-item>
              <el-descriptions-item v-if="entry.source.source_module === 'meta'" :label="t('catalog.entry.metaItemId')">{{ entry.source.observed_snapshot?.item_id ?? '-' }}</el-descriptions-item>
              <el-descriptions-item v-if="entry.source.source_module === 'meta'" :label="t('catalog.entry.scannedDepth')">{{ entry.source.observed_snapshot?.scanned_depth || '-' }}</el-descriptions-item>
              <el-descriptions-item :label="t('catalog.entry.observedAt')">{{ formatDate(entry.source.observed_at) }}</el-descriptions-item>
            </el-descriptions>
          </el-card>
        </el-col>
      </el-row>

      <el-card v-if="isProfessionalEntry" shadow="never" class="owner-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.entry.ownerFacts', { module: ownerModuleName }) }}</strong>
            <el-button v-if="entry.source_resolution?.detail_path" text type="primary" @click="openOwnerDetail">
              {{ t('catalog.entry.ownerDetail', { module: ownerModuleName }) }}
            </el-button>
          </div>
        </template>
        <el-alert
          :type="sourceResolutionAlertType"
          :closable="false"
          show-icon
          :title="sourceResolutionText"
          class="owner-alert"
        />
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('catalog.entry.ownerResolution')">
            {{ sourceResolutionText }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('catalog.entry.lastObservedProjection')">
            {{ formatDate(entry.source_resolution?.last_observed_at) }}
          </el-descriptions-item>
          <el-descriptions-item v-if="entry.source_resolution?.owner_status" :label="t('catalog.entry.ownerStatus')">
            {{ entry.source_resolution.owner_status }}
          </el-descriptions-item>
          <el-descriptions-item v-if="entry.source_resolution?.owner_version" :label="t('catalog.entry.ownerVersion')">
            {{ entry.source_resolution.owner_version }}
          </el-descriptions-item>
          <el-descriptions-item v-for="item in ownerSummaryItems" :key="item.key" :label="item.label">
            <span class="break-all">{{ item.value }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <el-card v-if="professionalRelationSubject" shadow="never" class="professional-relation-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.impact.professionalTitle') }}</strong>
            <el-button v-if="canReadProfessionalRelations" text type="primary" :loading="professionalRelationLoading" @click="loadProfessionalRelations">
              {{ t('catalog.professionalRelations.refresh') }}
            </el-button>
          </div>
        </template>
        <el-alert type="info" :closable="false" show-icon :title="t('catalog.impact.federatedHint')" class="owner-alert" />
        <el-alert
          v-if="professionalCatalogEntryState === 'unavailable'"
          type="warning"
          :closable="false"
          show-icon
          :title="t('catalog.impact.catalogMappingUnavailable')"
          class="owner-alert"
        />
        <div v-if="professionalRelationLoading" v-loading="true" class="professional-relation-loading" />
        <template v-else-if="professionalRelationState === 'ready'">
          <el-alert
            v-if="professionalRelationGraph.truncated"
            type="warning"
            :closable="false"
            show-icon
            :title="t('catalog.professionalRelations.truncated')"
            class="owner-alert"
          />
          <el-table v-if="professionalRelationGraph.edges.length" :data="professionalRelationGraph.edges">
            <el-table-column :label="t('catalog.professionalRelations.relation')" min-width="190">
              <template #default="{ row }">{{ professionalRelationKindLabel(row.relation_kind) }}</template>
            </el-table-column>
            <el-table-column :label="t('catalog.professionalRelations.source')" min-width="220">
              <template #default="{ row }">
                <div class="relation-node">
                  <span>{{ professionalRelationNodeLabel(row.source) }}</span>
                  <el-button v-if="professionalCatalogEntry(row.source)" text type="primary" @click="openProfessionalCatalogEntry(row.source)">
                    {{ t('catalog.impact.openCatalogEntry') }}
                  </el-button>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('catalog.professionalRelations.target')" min-width="220">
              <template #default="{ row }">
                <div class="relation-node">
                  <span>{{ professionalRelationNodeLabel(row.target) }}</span>
                  <el-button v-if="professionalCatalogEntry(row.target)" text type="primary" @click="openProfessionalCatalogEntry(row.target)">
                    {{ t('catalog.impact.openCatalogEntry') }}
                  </el-button>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('catalog.professionalRelations.details')" min-width="240">
              <template #default="{ row }"><span class="break-all">{{ professionalRelationDetails(row) }}</span></template>
            </el-table-column>
          </el-table>
          <el-empty v-else :image-size="60" :description="t('catalog.professionalRelations.empty')" />
        </template>
        <el-alert
          v-else
          :type="professionalRelationState === 'subject_missing' ? 'warning' : 'info'"
          :closable="false"
          show-icon
          :title="professionalRelationStatusText"
        />
      </el-card>

      <el-card v-if="entry.quality_summary" shadow="never" class="quality-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.quality.title') }}</strong>
            <el-button v-if="entry.quality_summary.detail_path" text type="primary" @click="openQualityDetail">
              {{ t('catalog.quality.openDetail') }}
            </el-button>
          </div>
        </template>
        <el-alert :type="qualityAlertType" :closable="false" show-icon :title="qualityStatusText" class="owner-alert" />
        <el-descriptions v-if="entry.quality_summary.status === 'current'" :column="2" border>
          <el-descriptions-item :label="t('catalog.quality.executionStatus')">{{ entry.quality_summary.last_execution_status || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.quality.score')">
            {{ entry.quality_summary.quality_score == null ? '-' : `${Number(entry.quality_summary.quality_score).toFixed(1)}%` }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('catalog.quality.openIssues')">{{ entry.quality_summary.open_issue_count }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.quality.observedAt')">{{ formatDate(entry.quality_summary.last_observed_at) }}</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <el-card v-if="lineageSubject" shadow="never" class="lineage-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.impact.lineageTitle') }}</strong>
            <el-button v-if="canReadLineage" text type="primary" :loading="lineageLoading" @click="loadLineage">
              {{ t('catalog.lineage.refresh') }}
            </el-button>
          </div>
        </template>
        <div v-if="lineageLoading" v-loading="true" class="lineage-loading" />
        <template v-else-if="lineageState === 'ready'">
          <LineageViewer :graph="lineageGraph" :height="420" />
          <el-alert
            v-if="lineageCatalogEntryState === 'unavailable'"
            type="warning"
            :closable="false"
            show-icon
            :title="t('catalog.impact.catalogMappingUnavailable')"
            class="owner-alert lineage-mapping-alert"
          />
          <div v-if="lineageCatalogEntries.length" class="lineage-catalog-entries">
            <h3>{{ t('catalog.impact.lineageCatalogEntries') }}</h3>
            <el-table :data="lineageCatalogEntries">
              <el-table-column prop="display_name" :label="t('catalog.entries.name')" min-width="240" />
              <el-table-column :label="t('catalog.entries.type')" min-width="150">
                <template #default="{ row }">{{ entryTypeLabel(row.entry_type) }}</template>
              </el-table-column>
              <el-table-column :label="t('catalog.entries.sourceStatus')" width="130">
                <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.source', row.source_status) }}</template>
              </el-table-column>
              <el-table-column width="150">
                <template #default="{ row }">
                  <el-button text type="primary" @click="openCatalogEntry(row)">{{ t('catalog.impact.openCatalogEntry') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </template>
        <el-alert
          v-else
          :type="lineageState === 'subject_missing' ? 'warning' : 'info'"
          :closable="false"
          show-icon
          :title="lineageStatusText"
        />
      </el-card>

      <el-card v-if="!isProfessionalEntry" shadow="never" class="component-card">
        <template #header>
          <div class="card-title-row">
            <strong>{{ t('catalog.entry.components') }}</strong>
            <span>{{ t('catalog.entry.componentCount', { count: entry.components?.length || 0 }) }}</span>
          </div>
        </template>
        <el-table :data="entry.components || []">
          <el-table-column prop="display_name" :label="t('catalog.entry.componentName')" min-width="220" />
          <el-table-column prop="component_key" :label="t('catalog.entry.componentKey')" min-width="220" show-overflow-tooltip />
          <el-table-column prop="data_type" :label="t('catalog.entry.dataType')" width="180" />
          <el-table-column prop="component_status" :label="t('catalog.entry.componentStatus')" width="140">
            <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.source', row.component_status) }}</template>
          </el-table-column>
          <el-table-column :label="t('catalog.edit.element')" min-width="180">
            <template #default="{ row }">{{ componentElementByID.get(row.id)?.observed_snapshot?.name || (componentElementByID.has(row.id) ? t('catalog.edit.referenceUnavailable') : '-') }}</template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!entry.components?.length" :description="t('catalog.entry.noComponents')" />
      </el-card>

      <el-row :gutter="16" class="detail-grid">
        <el-col :xs="24" :lg="12">
          <el-card shadow="never" class="detail-card">
            <template #header><strong>{{ t('catalog.edit.semanticLinks') }}</strong></template>
            <el-table :data="entry.semantic_links || []">
              <el-table-column prop="semantic_type" :label="t('catalog.edit.semanticType')" width="120" />
              <el-table-column prop="relation_role" :label="t('catalog.edit.relationRole')" width="120" />
              <el-table-column :label="t('catalog.edit.referenceName')" min-width="180">
                <template #default="{ row }">{{ row.observed_snapshot?.name || t('catalog.edit.referenceUnavailable') }}</template>
              </el-table-column>
            </el-table>
            <el-empty v-if="!entry.semantic_links?.length" :image-size="60" :description="t('catalog.edit.noSemanticLinks')" />
          </el-card>
        </el-col>
        <el-col :xs="24" :lg="12">
          <el-card shadow="never" class="detail-card">
            <template #header><strong>{{ t('catalog.edit.responsibilities') }}</strong></template>
            <el-table :data="entry.responsibilities || []">
              <el-table-column :label="t('catalog.edit.responsibilityRole')" min-width="180">
                <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.edit.role', row.role) }}</template>
              </el-table-column>
              <el-table-column :label="t('catalog.edit.referenceName')" min-width="180">
                <template #default="{ row }">{{ row.observed_snapshot?.name || t('catalog.edit.referenceUnavailable') }}</template>
              </el-table-column>
              <el-table-column prop="status" :label="t('catalog.edit.responsibilityStatus')" width="120" />
            </el-table>
            <el-empty v-if="!entry.responsibilities?.length" :image-size="60" :description="t('catalog.edit.noResponsibilities')" />
          </el-card>
        </el-col>
      </el-row>

	  <el-card v-if="canReadAudit" shadow="never" class="history-card">
		<template #header>
		  <div class="card-title-row">
			<strong>{{ t('catalog.history.title') }}</strong>
			<el-button text :loading="historyLoading" @click="loadHistory">{{ t('catalog.common.refresh') }}</el-button>
		  </div>
		</template>
		<el-alert v-if="historyError" type="error" :closable="false" :title="historyError" show-icon />
		<template v-else>
		  <h3>{{ t('catalog.history.sourceBindings') }}</h3>
		  <el-table :data="history?.source_bindings || []" v-loading="historyLoading">
			<el-table-column prop="source_identity" :label="t('catalog.entry.sourceIdentity')" min-width="260" show-overflow-tooltip />
			<el-table-column prop="source_status" :label="t('catalog.entries.sourceStatus')" width="130" />
			<el-table-column :label="t('catalog.history.current')" width="100">
			  <template #default="{ row }">{{ row.is_current ? t('catalog.history.yes') : t('catalog.history.no') }}</template>
			</el-table-column>
			<el-table-column :label="t('catalog.history.boundAt')" min-width="180">
			  <template #default="{ row }">{{ formatDate(row.bound_at) }}</template>
			</el-table-column>
		  </el-table>
		  <h3>{{ t('catalog.history.auditEvents') }}</h3>
		  <el-timeline>
			<el-timeline-item v-for="event in history?.audit_events || []" :key="event.id" :timestamp="formatDate(event.created_at)">
			  <strong>{{ event.event_type }}</strong>
			  <div class="audit-details">{{ formatAuditDetails(event.details) }}</div>
			</el-timeline-item>
		  </el-timeline>
		  <el-empty v-if="!historyLoading && !history?.audit_events?.length" :image-size="60" :description="t('catalog.history.empty')" />
		</template>
	  </el-card>
	  </template>

	  <el-dialog v-model="rebindVisible" :title="t('catalog.rebind.title')" width="min(620px, 92vw)" :close-on-click-modal="false">
		<el-alert type="warning" :closable="false" :title="t('catalog.rebind.description')" show-icon />
		<el-form label-position="top" class="rebind-form">
		  <el-form-item :label="t('catalog.rebind.temporaryEntryId')" required>
			<el-input v-model.trim="rebindForm.temporary_entry_id" />
		  </el-form-item>
		  <el-form-item :label="t('catalog.rebind.temporaryEntryVersion')" required>
			<el-input-number v-model="rebindForm.temporary_entry_version" :min="1" :precision="0" controls-position="right" />
		  </el-form-item>
		  <el-form-item :label="t('catalog.rebind.newSourceIdentity')" required>
			<el-input v-model.trim="rebindForm.new_source_identity" />
		  </el-form-item>
		  <el-form-item :label="t('catalog.rebind.reason')" required>
			<el-input v-model.trim="rebindForm.reason" />
		  </el-form-item>
		  <el-form-item :label="t('catalog.rebind.evidence')" required>
			<el-input v-model.trim="rebindForm.evidence" type="textarea" :rows="3" />
		  </el-form-item>
		</el-form>
		<template #footer>
		  <el-button @click="rebindVisible = false">{{ t('catalog.edit.cancel') }}</el-button>
		  <el-button type="warning" :loading="rebinding" :disabled="!rebindFormComplete" @click="submitRebind">{{ t('catalog.rebind.confirm') }}</el-button>
		</template>
	  </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Bell, Edit, Refresh, Star } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute, openConsoleRoute, useConsolePageDescriptor } from '@common-ui'
import { createLineageApi, normalizeLineageGraph } from '@addp/common-frontend/graph/lineageApi.js'
import EntryEditor from '../components/EntryEditor.vue'
import { getEntry, getEntryHistory, getMyEntryMarks, rebindSource, replaceMyEntryMarks, resolveSourceEntries, updateEntry } from '../api/catalog'
import client from '../api/client'
import { useAuthStore } from '../store/auth'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import { buildEntryListQuery, parseEntryListRoute } from '../utils/entryRouteState'
import { lineageFailureState, lineageNodesToSourceReferences, resolveLineageSubject } from '../utils/lineageView'
import {
  normalizeProfessionalRelations,
  professionalRelationFailureState,
  professionalNodesToSourceReferences,
  professionalResourceKey,
  resolveProfessionalRelationSubject,
  sourceEntryResolutionMap
} from '../utils/professionalRelationView'

const route = useRoute()
const router = useRouter()
const LineageViewer = defineAsyncComponent(() => import('@addp/common-frontend/graph/LineageViewer.vue'))
const { t, locale } = useI18n()
const authStore = useAuthStore()
const entry = ref(null)
const loading = ref(false)
const saving = ref(false)
const editing = ref(false)
const conflict = ref(false)
const error = ref('')
const marks = ref({ favorite: false, following: false })
const markSaving = ref(false)
const history = ref(null)
const historyLoading = ref(false)
const historyError = ref('')
const rebindVisible = ref(false)
const rebinding = ref(false)
const rebindForm = ref(emptyRebindForm())
let requestVersion = 0
const canEdit = computed(() => authStore.hasPermission('catalog.entry.update'))
const canCertify = computed(() => authStore.hasPermission('catalog.entry.certify'))
const canDeprecate = computed(() => authStore.hasPermission('catalog.entry.deprecate'))
const canRebind = computed(() => authStore.hasPermission('catalog.source.rebind'))
const canReadAudit = computed(() => authStore.hasPermission('catalog.audit.read'))
const canReadLineage = computed(() => authStore.hasPermission('meta.lineage.read'))
const rebindFormComplete = computed(() => Boolean(
	rebindForm.value.temporary_entry_id && rebindForm.value.temporary_entry_version > 0 &&
	rebindForm.value.new_source_identity && rebindForm.value.reason && rebindForm.value.evidence
))
const componentElementByID = computed(() => new Map((entry.value?.component_elements || []).map(item => [item.component_id, item])))
const isProfessionalEntry = computed(() => ['model', 'standard', 'service', 'develop'].includes(entry.value?.source?.source_module))
const ownerModuleName = computed(() => ({ model: 'Model', standard: 'Standard', service: 'Service', develop: 'Develop' }[entry.value?.source?.source_module] || ''))
const sourceResolutionText = computed(() => {
  const status = entry.value?.source_resolution?.status || 'unavailable'
  return t(`catalog.entry.owner${status.charAt(0).toUpperCase()}${status.slice(1)}`, { module: ownerModuleName.value })
})
const sourceResolutionAlertType = computed(() => ({ current: 'success', unavailable: 'warning', missing: 'error' }[entry.value?.source_resolution?.status] || 'warning'))
const ownerSummaryItems = computed(() => {
  const summary = entry.value?.source_resolution?.summary || entry.value?.source?.observed_snapshot || {}
  const fields = [
    ['name', 'ownerName'],
    ['code', 'ownerCode'],
    ['object_kind', 'ownerKind'],
    ['model_status', 'ownerModelStatus'],
    ['table_type', 'ownerTableType'],
    ['layer', 'ownerLayer'],
    ['metric_type', 'ownerMetricType'],
    ['metric_status', 'ownerMetricStatus'],
    ['lifecycle_state', 'ownerLifecycleState'],
    ['domain_id', 'ownerDomain'],
    ['category_id', 'ownerMetricCategory'],
    ['unit_id', 'ownerMetricUnit'],
    ['service_status', 'ownerServiceStatus'],
    ['config_type', 'ownerServiceConfigType'],
    ['access_mode', 'ownerServiceAccessMode'],
    ['engine_id', 'ownerEngine'],
    ['runtime_engine_id', 'ownerRuntimeEngine'],
    ['artifact_type', 'ownerArtifactType'],
    ['task_status', 'ownerTaskStatus'],
    ['query_type', 'ownerQueryType']
  ]
  return fields
    .filter(([key]) => summary[key] !== null && summary[key] !== undefined && String(summary[key]).trim() !== '')
    .map(([key, label]) => ({ key, label: t(`catalog.entry.${label}`, { module: ownerModuleName.value }), value: summary[key] }))
})
const qualityStatusText = computed(() => t(`catalog.quality.${entry.value?.quality_summary?.status || 'unavailable'}`))
const qualityAlertType = computed(() => {
  const summary = entry.value?.quality_summary
  if (summary?.status === 'not_configured') return 'info'
  if (summary?.status !== 'current') return 'warning'
  if (['failed', 'timeout'].includes(summary.last_execution_status)) return 'error'
  if (['pending', 'running'].includes(summary.last_execution_status)) return 'warning'
  return summary.last_execution_status === 'success' ? 'success' : 'info'
})
const lineageSubject = computed(() => resolveLineageSubject(entry.value))
const lineageGraph = ref(normalizeLineageGraph())
const lineageState = ref('idle')
const lineageLoading = ref(false)
const lineageStatusText = computed(() => t(`catalog.lineage.${lineageState.value === 'loading' ? 'unavailable' : lineageState.value}`))
const lineageApi = createLineageApi({ request: client, baseUrl: '/meta' })
let lineageRequestVersion = 0
const lineageCatalogEntries = ref([])
const lineageCatalogEntryState = ref('idle')
let lineageCatalogEntryRequestVersion = 0
const professionalRelationSubject = computed(() => resolveProfessionalRelationSubject(entry.value))
const canReadProfessionalRelations = computed(() => (
  professionalRelationSubject.value?.permissions.every(permission => authStore.hasPermission(permission)) || false
))
const professionalRelationGraph = ref(normalizeProfessionalRelations())
const professionalRelationState = ref('idle')
const professionalRelationLoading = ref(false)
const professionalRelationStatusText = computed(() => t(`catalog.professionalRelations.${professionalRelationState.value === 'loading' ? 'unavailable' : professionalRelationState.value}`))
const professionalRelationNodeByKey = computed(() => new Map(
  professionalRelationGraph.value.nodes.map(node => [professionalResourceKey(node), node])
))
const canResolveCatalogEntries = computed(() => authStore.hasPermission('catalog.entry.read'))
const professionalCatalogEntryByKey = ref(new Map())
const professionalCatalogEntryState = ref('idle')
let professionalRelationRequestVersion = 0
let professionalCatalogEntryRequestVersion = 0

useConsolePageDescriptor(router, 'catalog', {
  title: computed(() => t('catalog.entry.recentVisitTitle')),
  subject: computed(() => entry.value?.display_name || ''),
  ready: computed(() => Boolean(entry.value))
})

async function loadEntry() {
  const id = String(route.params.id || '').trim()
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const response = await getEntry(id)
    if (version === requestVersion) {
	  entry.value = response
	  await loadMarks(id)
	  history.value = null
	  if (canReadAudit.value) await loadHistory()
	}
  } catch (requestError) {
    if (version === requestVersion) {
      entry.value = null
      error.value = requestError?.response?.data?.error || t('catalog.entry.loadFailed')
    }
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function loadLineage() {
  const subject = lineageSubject.value
  const version = ++lineageRequestVersion
  lineageGraph.value = normalizeLineageGraph()
  lineageLoading.value = false
  if (!subject) {
    lineageState.value = 'idle'
    return
  }
  if (!canReadLineage.value) {
    lineageState.value = 'forbidden'
    return
  }
  lineageState.value = 'loading'
  lineageLoading.value = true
  try {
    const response = await lineageApi.getGraph(subject)
    if (version !== lineageRequestVersion) return
    lineageGraph.value = normalizeLineageGraph(response)
    lineageState.value = 'ready'
    await loadLineageCatalogEntries(lineageGraph.value.nodes)
  } catch (requestError) {
    if (version !== lineageRequestVersion) return
    lineageState.value = lineageFailureState(requestError)
  } finally {
    if (version === lineageRequestVersion) lineageLoading.value = false
  }
}

async function loadLineageCatalogEntries(nodes) {
  const version = ++lineageCatalogEntryRequestVersion
  lineageCatalogEntries.value = []
  if (!canResolveCatalogEntries.value) {
    lineageCatalogEntryState.value = 'forbidden'
    return
  }
  const references = lineageNodesToSourceReferences(nodes)
  if (!references.length) {
    lineageCatalogEntryState.value = 'ready'
    return
  }
  lineageCatalogEntryState.value = 'loading'
  try {
    const response = await resolveSourceEntries(references)
    if (version !== lineageCatalogEntryRequestVersion) return
    const seen = new Set()
    const relatedEntries = []
    for (const result of response?.results || []) {
      const relatedEntry = result?.entry
      if (!result?.found || !relatedEntry?.id || relatedEntry.id === entry.value?.id || seen.has(relatedEntry.id)) continue
      seen.add(relatedEntry.id)
      relatedEntries.push(relatedEntry)
    }
    lineageCatalogEntries.value = relatedEntries
    lineageCatalogEntryState.value = 'ready'
  } catch {
    if (version === lineageCatalogEntryRequestVersion) lineageCatalogEntryState.value = 'unavailable'
  }
}

async function openCatalogEntry(catalogEntry) {
  if (!catalogEntry?.id) return
  await router.push({ name: 'EntryDetail', params: { id: catalogEntry.id }, query: route.query })
}

async function loadProfessionalRelations() {
  const subject = professionalRelationSubject.value
  const version = ++professionalRelationRequestVersion
  professionalRelationGraph.value = normalizeProfessionalRelations()
  professionalRelationLoading.value = false
  if (!subject) {
    professionalRelationState.value = 'idle'
    return
  }
  if (!canReadProfessionalRelations.value) {
    professionalRelationState.value = 'forbidden'
    return
  }
  professionalRelationState.value = 'loading'
  professionalRelationLoading.value = true
  try {
    const response = await client.get(subject.path, { params: { limit: 100 } })
    if (version !== professionalRelationRequestVersion) return
    professionalRelationGraph.value = normalizeProfessionalRelations(response)
    professionalRelationState.value = 'ready'
    await loadProfessionalCatalogEntries(professionalRelationGraph.value.nodes)
  } catch (requestError) {
    if (version !== professionalRelationRequestVersion) return
    professionalRelationState.value = professionalRelationFailureState(requestError)
  } finally {
    if (version === professionalRelationRequestVersion) professionalRelationLoading.value = false
  }
}

async function loadProfessionalCatalogEntries(nodes) {
  const version = ++professionalCatalogEntryRequestVersion
  professionalCatalogEntryByKey.value = new Map()
  if (!canResolveCatalogEntries.value) {
    professionalCatalogEntryState.value = 'forbidden'
    return
  }
  const references = professionalNodesToSourceReferences(nodes)
  if (!references.length) {
    professionalCatalogEntryState.value = 'ready'
    return
  }
  professionalCatalogEntryState.value = 'loading'
  try {
    const response = await resolveSourceEntries(references)
    if (version !== professionalCatalogEntryRequestVersion) return
    professionalCatalogEntryByKey.value = sourceEntryResolutionMap(response?.results)
    professionalCatalogEntryState.value = 'ready'
  } catch {
    if (version === professionalCatalogEntryRequestVersion) professionalCatalogEntryState.value = 'unavailable'
  }
}

function professionalCatalogEntry(resource) {
  return professionalCatalogEntryByKey.value.get(professionalResourceKey(resource)) || null
}

async function openProfessionalCatalogEntry(resource) {
  const catalogEntry = professionalCatalogEntry(resource)
  await openCatalogEntry(catalogEntry)
}

function professionalRelationNodeLabel(resource) {
  const node = professionalRelationNodeByKey.value.get(professionalResourceKey(resource))
  if (node?.name) return node.code ? `${node.name} (${node.code})` : node.name
  const type = t(`catalog.professionalRelations.resourceType.${resource?.resource_type || 'unknown'}`)
  return t('catalog.professionalRelations.resourceUnavailable', { type })
}

function professionalRelationKindLabel(kind) {
  const key = {
    'model.entity.one_to_one': 'entityOneToOne',
    'model.entity.one_to_many': 'entityOneToMany',
    'model.entity.many_to_many': 'entityManyToMany',
    'model.logical_table.entity': 'logicalTableEntity',
    'model.logical_table.fk': 'logicalTableForeignKey',
    'model.logical_table.join': 'logicalTableJoin',
    'model.logical_table.supports_metric': 'logicalTableSupportsMetric',
    'standard.metric.base_metric': 'metricBase',
    'standard.metric.dependency': 'metricDependency'
  }[kind]
  return key ? t(`catalog.professionalRelations.kind.${key}`) : kind
}

function professionalRelationDetails(edge) {
  const details = []
  if (edge.name) details.push(edge.name)
  if (edge.source_component) {
    details.push(t('catalog.professionalRelations.sourceComponent', {
      value: edge.source_component.name || edge.source_component.resource_id
    }))
  }
  if (edge.target_component) {
    details.push(t('catalog.professionalRelations.targetComponent', {
      value: edge.target_component.name || edge.target_component.resource_id
    }))
  }
  if (edge.coefficient != null) {
    details.push(t('catalog.professionalRelations.coefficient', { value: edge.coefficient }))
  }
  if (edge.note) details.push(edge.note)
  if (edge.description) details.push(edge.description)
  return details.join(' · ') || '-'
}

async function loadMarks(id) {
  try {
    marks.value = await getMyEntryMarks(id)
  } catch (requestError) {
    marks.value = { favorite: false, following: false }
    ElMessage.error(requestError?.response?.data?.error || t('catalog.marks.loadFailed'))
  }
}

async function toggleMark(type) {
  if (!entry.value || markSaving.value) return
  const next = { ...marks.value, [type]: !marks.value[type] }
  markSaving.value = true
  try {
    marks.value = await replaceMyEntryMarks(entry.value.id, next)
    ElMessage.success(t('catalog.marks.saved'))
  } catch (requestError) {
    ElMessage.error(requestError?.response?.data?.error || t('catalog.marks.saveFailed'))
  } finally {
    markSaving.value = false
  }
}

function emptyRebindForm() {
	return { temporary_entry_id: '', temporary_entry_version: 1, new_source_identity: '', reason: '', evidence: '' }
}

function openRebind() {
	rebindForm.value = emptyRebindForm()
	rebindVisible.value = true
}

async function submitRebind() {
	if (!entry.value || !rebindFormComplete.value) return
	rebinding.value = true
	try {
	  entry.value = await rebindSource(entry.value.id, { target_version: entry.value.version, ...rebindForm.value })
	  rebindVisible.value = false
	  ElMessage.success(t('catalog.rebind.success'))
	  if (canReadAudit.value) await loadHistory()
	} catch (requestError) {
	  ElMessage.error(requestError?.response?.data?.error || t('catalog.rebind.failed'))
	} finally {
	  rebinding.value = false
	}
}

async function loadHistory() {
	if (!entry.value || !canReadAudit.value) return
	historyLoading.value = true
	historyError.value = ''
	try {
	  history.value = await getEntryHistory(entry.value.id)
	} catch (requestError) {
	  historyError.value = requestError?.response?.data?.error || t('catalog.history.loadFailed')
	} finally {
	  historyLoading.value = false
	}
}

async function goCanonicalEntry() {
	if (!entry.value?.merged_into_entry_id) return
	await router.replace({ name: 'EntryDetail', params: { id: entry.value.merged_into_entry_id }, query: route.query })
}

async function goRecommendedSuccessor() {
	if (!entry.value?.recommended_successor?.id) return
	await router.push({ name: 'EntryDetail', params: { id: entry.value.recommended_successor.id }, query: route.query })
}

function formatAuditDetails(details) {
	if (!details || typeof details !== 'object') return ''
	return Object.entries(details).map(([key, value]) => `${key}: ${String(value)}`).join(' · ')
}

async function reloadLatest() {
  editing.value = false
  conflict.value = false
  await loadEntry()
}

function cancelEdit() {
  editing.value = false
  conflict.value = false
}

async function saveEntry(payload) {
  saving.value = true
  conflict.value = false
  try {
    entry.value = await updateEntry(entry.value.id, payload)
    editing.value = false
    ElMessage.success(t('catalog.edit.saved'))
  } catch (requestError) {
    const code = requestError?.response?.data?.error_code
    if (requestError?.response?.status === 409 && code === 'catalog_entry_version_conflict') {
      conflict.value = true
      ElMessage.warning(t('catalog.edit.conflict'))
      return
    }
    ElMessage.error(requestError?.response?.data?.error || t('catalog.edit.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function goBack() {
  await navigateConsoleModuleRoute(router, 'catalog', {
    path: '/entries',
    query: buildEntryListQuery(parseEntryListRoute(route.query))
  }, { history: 'replace' })
}

async function openOwnerDetail() {
  const path = entry.value?.source_resolution?.detail_path
  if (!path) return
  await openConsoleRoute(path, { source: 'addp-catalog' })
}

async function openQualityDetail() {
  const path = entry.value?.quality_summary?.detail_path
  if (!path) return
  await openConsoleRoute(path, { source: 'addp-catalog' })
}

function entryTypeLabel(entryType) {
  const key = { data_item: 'dataItem', business_entity: 'businessEntity', logical_model: 'logicalModel', metric: 'metric', data_service: 'dataService', development_artifact: 'developmentArtifact', data_application: 'dataApplication' }[entryType] || 'dataItem'
  return t(`catalog.entryType.${key}`)
}

function sourceTagType(status) {
  return status === 'active' ? 'success' : 'danger'
}

function governanceTagType(status) {
  return { discovered: 'info', curated: 'primary', certified: 'success', deprecated: 'warning' }[status] || 'info'
}

function formatDate(value) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

watch(() => route.params.id, loadEntry, { immediate: true })
watch(() => [lineageSubject.value?.item_id || '', canReadLineage.value], loadLineage, { immediate: true })
watch(
  () => [professionalRelationSubject.value?.key || '', canReadProfessionalRelations.value],
  loadProfessionalRelations,
  { immediate: true }
)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header, .header-actions { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
.header-actions { margin-bottom: 0; }
.summary-card { margin-bottom: 16px; }
.successor-card { margin-bottom: 16px; }
.summary-header, .summary-tags, .card-title-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.summary-title { min-width: 0; }
.summary-title h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; overflow-wrap: anywhere; }
.entry-id, .card-title-row span { color: var(--addp-text-secondary); font-size: 13px; }
.description { color: var(--addp-text-secondary); margin: 16px 0 0; white-space: pre-wrap; }
.detail-grid { row-gap: 16px; margin-bottom: 16px; }
.detail-card { height: 100%; }
.break-all { overflow-wrap: anywhere; }
.component-card { margin-bottom: 16px; }
.owner-card { margin-bottom: 16px; }
.professional-relation-card { margin-bottom: 16px; }
.professional-relation-loading { min-height: 160px; }
.relation-node { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.quality-card { margin-bottom: 16px; }
.lineage-card { margin-bottom: 16px; }
.lineage-loading { min-height: 180px; }
.lineage-mapping-alert { margin-top: 16px; }
.lineage-catalog-entries h3 { color: var(--addp-text-primary); font-size: 15px; margin: 18px 0 10px; }
.owner-alert { margin-bottom: 16px; }
.history-card { margin-bottom: 16px; }
.history-card h3 { color: var(--addp-text-primary); font-size: 15px; margin: 18px 0 10px; }
.audit-details { color: var(--addp-text-secondary); font-size: 13px; overflow-wrap: anywhere; margin-top: 4px; }
.rebind-form { margin-top: 16px; }
@media (max-width: 760px) {
  .summary-header { align-items: flex-start; flex-direction: column; }
  .summary-tags { flex-wrap: wrap; }
}
</style>
