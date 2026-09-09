<template>
  <div class="document-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left"><el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button><h2>{{ title }}</h2><el-tag :type="statusType(revision.status)">{{ statusLabel(revision.status) }}</el-tag></div>
      <div class="header-right">
        <el-button v-if="!document.draft_revision && canUpdate" @click="newDraft">{{ $t('standard.document.createRevision') }}</el-button>
        <el-button v-if="editable" type="primary" :loading="saving" @click="saveAll">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="editable" type="warning" @click="revisionAction('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="revisionAction('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="revisionAction('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="revision.status === 'published' && canPublish" type="danger" @click="revisionAction('withdraw')">{{ $t('standard.revision.withdraw') }}</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.document.identityInfo') }}</h3></template>
          <el-form label-width="120px" :disabled="!canUpdate">
            <el-form-item :label="$t('standard.common.code')"><el-input :model-value="document.code" disabled /></el-form-item>
            <el-form-item :label="$t('standard.common.scopeLabel')"><el-select v-model="identity.scope_type" style="width:100%" @change="onScopeChange"><el-option :label="$t('standard.common.scopeValue.tenant_common')" value="tenant_common" /><el-option :label="$t('standard.common.scopeValue.domain')" value="domain" /></el-select></el-form-item>
            <el-form-item v-if="identity.scope_type === 'domain'" :label="$t('standard.document.domainLabel')"><el-select v-model="identity.owner_domain_id" filterable style="width:100%"><el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" /></el-select></el-form-item>
            <el-form-item :label="$t('standard.document.typeLabel')"><el-select v-model="identity.doc_type" style="width:100%"><el-option v-for="type in documentTypes" :key="type" :label="$t(`standard.document.${type}`)" :value="type" /></el-select></el-form-item>
            <el-form-item :label="$t('standard.document.sourceLabel')"><el-input v-model="identity.source_org" /></el-form-item>
            <el-form-item :label="$t('standard.common.tags')"><el-select v-model="identity.tags" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
          </el-form>
        </el-card>

        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.document.revisionInfo') }}</h3><span v-if="revision.revision_no">R{{ revision.revision_no }}</span></div></template>
          <el-form label-width="120px" :disabled="!editable">
            <el-form-item :label="$t('standard.document.nameLabel')"><el-input v-model="revision.name" /></el-form-item>
            <el-form-item :label="$t('standard.document.versionLabel')"><el-input v-model="revision.version_label" /></el-form-item>
            <el-form-item :label="$t('standard.document.descriptionLabel')"><el-input v-model="revision.description" type="textarea" :rows="4" /></el-form-item>
            <el-form-item :label="$t('standard.revision.changeSummary')"><el-input v-model="revision.change_summary" /></el-form-item>
            <el-form-item :label="$t('standard.revision.effectiveFrom')"><el-date-picker v-model="revision.effective_from" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
            <el-form-item :label="$t('standard.revision.effectiveTo')"><el-date-picker v-model="revision.effective_to" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" /></el-form-item>
          </el-form>
          <el-divider>{{ $t('standard.document.attachment') }}</el-divider>
          <div class="file-row">
            <span>{{ revision.file_name || $t('standard.document.noAttachment') }}<template v-if="revision.file_name"> · {{ formatFileSize(revision.file_size) }} · SHA256 {{ shortHash(revision.content_sha256) }}</template></span>
            <el-button v-if="revision.file_name" link type="primary" @click="downloadRevision">{{ $t('standard.document.download') }}</el-button>
            <el-upload v-if="editable" :auto-upload="false" :show-file-list="false" :on-change="uploadRevision"><el-button size="small" :loading="uploading">{{ revision.file_name ? $t('standard.document.reupload') : $t('standard.document.uploadFile') }}</el-button></el-upload>
            <el-button v-if="canExtract" type="primary" plain size="small" :loading="extracting" @click="extractCandidates">{{ $t('standard.document.extractCandidates') }}</el-button>
          </div>
          <el-alert :title="$t('standard.document.candidateBoundaryHint')" type="info" :closable="false" show-icon class="hint" />
        </el-card>

        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.document.extractionResults') }}</h3><el-tag type="info">{{ pendingCandidateCount }} {{ $t('standard.document.pendingCandidateVariants') }}</el-tag></div></template>
          <div class="candidate-toolbar">
            <el-input v-model="candidateQuery.keyword" class="candidate-search" :prefix-icon="Search" clearable :placeholder="$t('standard.document.candidateSearchPlaceholder')" :aria-label="$t('standard.document.candidateSearchPlaceholder')" @change="applyCandidateFilters" />
            <el-select v-model="candidateQuery.state" clearable :placeholder="$t('standard.document.allCandidateStates')" @change="applyCandidateFilters"><el-option v-for="state in ['pending','retained','formalized','rejected']" :key="state" :value="state" :label="candidateGroupStateLabel(state)" /></el-select>
            <el-select v-model="candidateQuery.candidate_type" clearable :placeholder="$t('standard.document.allCandidateTypes')" @change="applyCandidateFilters"><el-option v-for="type in ['glossary','element','code_set','metric']" :key="type" :value="type" :label="candidateTypeLabel(type)" /></el-select>
            <span>{{ $t('standard.document.candidateFamilyTotal', { families: candidateFamilyResponse.total, variants: candidateFamilyResponse.variant_total }) }}</span>
          </div>
          <div class="candidate-comparison-facets">
            <span class="candidate-comparison-label">
              <span id="candidate-comparison-filter-label">{{ $t('standard.document.comparisonFacetLabel') }}</span>
              <el-tooltip :content="$t('standard.document.comparisonFacetHint')" placement="top">
                <el-button class="candidate-comparison-help" link circle :aria-label="$t('standard.document.comparisonFacetHelpLabel')">
                  <el-icon><InfoFilled /></el-icon>
                </el-button>
              </el-tooltip>
            </span>
            <el-radio-group v-model="candidateQuery.comparison_result" size="small" aria-labelledby="candidate-comparison-filter-label" @change="applyCandidateFilters">
              <el-radio-button value="">{{ $t('standard.document.allComparisonResults') }} · {{ comparisonFacetTotal }}</el-radio-button>
              <el-radio-button v-for="result in comparisonResults" :key="result" :value="result">{{ comparisonLabel(result) }} · {{ comparisonCount(result) }}</el-radio-button>
            </el-radio-group>
          </div>
          <el-empty v-if="!candidateFamilies.length && !candidateLoading" :description="$t('standard.document.noCandidateFamilies')" />
          <div v-loading="candidateLoading">
            <el-collapse v-model="expandedCandidateFamilies" class="candidate-families">
              <el-collapse-item v-for="family in candidateFamilies" :key="family.family_key" :name="family.family_key" class="candidate-family">
                <template #title>
                  <div class="candidate-family-header">
                    <div><el-tag size="small">{{ candidateTypeLabel(family.candidate_type) }}</el-tag><strong>{{ family.representative_name }}</strong><code>{{ family.code }}</code></div>
                    <div class="candidate-family-meta"><span>{{ $t('standard.document.candidateFamilyVariantCount', { count: family.variant_count }) }}</span><span>{{ $t('standard.document.candidateFamilyOccurrenceCount', { count: family.occurrence_count }) }}</span></div>
                  </div>
                </template>
                <section v-if="family.variant_differences.length" class="candidate-variant-differences" :aria-label="$t('standard.document.candidateVariantDifferencesTitle')">
                  <div class="candidate-variant-differences-header">
                    <strong>{{ $t('standard.document.candidateVariantDifferencesTitle') }}</strong>
                    <span>{{ $t('standard.document.candidateVariantDifferencesHint', { count: family.variant_differences.length }) }}</span>
                  </div>
                  <div v-for="difference in family.variant_differences" :key="difference.field" class="candidate-variant-difference-row">
                    <strong>{{ candidateVariantFieldLabel(difference.field) }}</strong>
                    <div class="candidate-variant-values">
                      <div v-for="item in difference.values" :key="item.semantic_fingerprint" class="candidate-variant-value">
                        <button type="button" class="candidate-variant-jump" :aria-label="$t('standard.document.candidateVariantJumpLabel', { index: item.variant_index })" @click="focusCandidateVariant(family.family_key, item.semantic_fingerprint)">
                          <el-tag size="small" type="info">{{ $t('standard.document.candidateVariantIndex', { index: item.variant_index }) }}</el-tag>
                        </button>
                        <div v-if="difference.field === 'items'" class="candidate-variant-code-items">
                          <span v-for="codeItem in item.value" :key="`${codeItem.code}:${codeItem.name}:${codeItem.definition}`"><code>{{ codeItem.code }}</code> · {{ codeItem.name }}<template v-if="codeItem.definition"> — {{ codeItem.definition }}</template></span>
                          <span v-if="!item.value.length">{{ $t('standard.document.emptyValue') }}</span>
                        </div>
                        <span v-else-if="difference.field === 'dimensions'">{{ item.value.length ? item.value.join('、') : $t('standard.document.emptyValue') }}</span>
                        <span v-else>{{ item.value || $t('standard.document.emptyValue') }}</span>
                      </div>
                    </div>
                  </div>
                </section>
                <el-card v-for="(group, variantIndex) in family.variants" :id="candidateVariantCardId(family.family_key, group.semantic_fingerprint)" :key="group.semantic_fingerprint" shadow="never" class="candidate-card" tabindex="-1">
                  <div class="candidate-header"><div><el-tag size="small" type="info">{{ $t('standard.document.candidateVariantIndex', { index: variantIndex + 1 }) }}</el-tag><strong>{{ group.candidate.name }}</strong><code>{{ group.candidate.code }}</code></div><el-tag :type="candidateGroupStateTagType(group.state)">{{ candidateGroupStateLabel(group.state) }}</el-tag></div>
                  <div class="candidate-group-meta"><span>{{ $t('standard.document.candidateOccurrenceCount', { count: group.occurrence_count }) }}</span><span>{{ $t('standard.document.candidateFirstSeen') }} {{ formatTime(group.first_seen_at) }}</span><span>{{ $t('standard.document.candidateLastSeen') }} {{ formatTime(group.last_seen_at) }}</span></div>
                  <p>{{ group.candidate.definition }}</p>
                  <div v-if="group.candidate.payload?.code_set_code" class="candidate-reference"><span>{{ $t('standard.document.codeSetReference') }}</span><code>{{ group.candidate.payload.code_set_code }}</code></div>
                  <div v-if="group.candidate.comparison" class="candidate-comparison">
                    <div class="comparison-summary">
                      <el-tag size="small" :type="comparisonTagType(group.candidate.comparison.result)">{{ comparisonLabel(group.candidate.comparison.result) }}</el-tag>
                      <span>{{ comparisonSummary(group.candidate.comparison.result) }}</span>
                    </div>
                    <div v-if="group.candidate.comparison.standard_id" class="comparison-target">
                      <span>{{ $t('standard.document.comparisonTarget') }}: <strong>{{ group.candidate.comparison.name }}</strong> <code>{{ group.candidate.comparison.code }}</code> · {{ scopeLabel(group.candidate.comparison.scope_type) }}<template v-if="group.candidate.comparison.owner_domain_id"> / {{ domainName(group.candidate.comparison.owner_domain_id) }}</template> · R{{ group.candidate.comparison.revision_no }} · {{ statusLabel(group.candidate.comparison.revision_status) }}</span>
                      <el-button link type="primary" size="small" @click="openComparedStandard(group.candidate)">{{ $t('standard.document.openExistingStandard') }}</el-button>
                    </div>
                    <el-table v-if="group.candidate.comparison.differences?.length" :data="group.candidate.comparison.differences" size="small" border class="comparison-differences">
                      <el-table-column :label="$t('standard.document.differenceField')" width="140"><template #default="{ row }">{{ comparisonFieldLabel(row.field) }}</template></el-table-column>
                      <el-table-column :label="$t('standard.document.candidateValue')" min-width="220"><template #default="{ row }"><div class="comparison-value"><template v-if="row.candidate_value.kind === 'code_items'"><div v-for="item in row.candidate_value.items || []" :key="item.code" class="comparison-item"><code>{{ item.code }}</code> · {{ item.name }}<span v-if="item.definition"> — {{ item.definition }}</span></div></template><template v-else>{{ comparisonValueText(row.candidate_value, row.field) }}</template></div></template></el-table-column>
                      <el-table-column :label="$t('standard.document.standardValue')" min-width="220"><template #default="{ row }"><div class="comparison-value"><template v-if="row.standard_value.kind === 'code_items'"><div v-for="item in row.standard_value.items || []" :key="item.code" class="comparison-item"><code>{{ item.code }}</code> · {{ item.name }}<span v-if="item.definition"> — {{ item.definition }}</span></div></template><template v-else>{{ comparisonValueText(row.standard_value, row.field) }}</template></div></template></el-table-column>
                    </el-table>
                  </div>
                  <pre v-if="Object.keys(group.candidate.payload || {}).length">{{ JSON.stringify(group.candidate.payload, null, 2) }}</pre>
                  <el-collapse class="candidate-occurrences"><el-collapse-item :name="group.semantic_fingerprint"><template #title>{{ $t('standard.document.candidateEvidenceHistory', { count: group.occurrence_count }) }}</template><div v-for="occurrence in group.occurrences" :key="occurrence.candidate_id" class="candidate-occurrence"><div class="candidate-occurrence-header"><span>#{{ occurrence.extraction_id }} · R{{ revisionNo(occurrence.document_revision_id) }} · {{ formatTime(occurrence.extracted_at) }}</span><el-tag size="small" :type="occurrence.formalization ? 'success' : occurrence.status === 'pending' ? 'warning' : occurrence.status === 'retained' ? 'success' : 'info'">{{ occurrence.formalization ? candidateGroupStateLabel('formalized') : candidateStatusLabel(occurrence.status) }}</el-tag></div><blockquote v-for="evidence in occurrence.evidences || []" :key="evidence.id"><small>{{ evidence.section_path }} · L{{ evidence.start_line }}-{{ evidence.end_line }} · {{ shortHash(evidence.excerpt_hash) }}</small><div>{{ evidence.excerpt }}</div></blockquote></div></el-collapse-item></el-collapse>
                  <div v-if="group.state === 'pending' && canUpdate" class="candidate-actions"><el-button size="small" type="success" @click="decideCandidate(group.candidate, 'retained')">{{ $t('standard.document.retainCandidate') }}</el-button><el-button size="small" @click="decideCandidate(group.candidate, 'rejected')">{{ $t('standard.document.rejectCandidate') }}</el-button></div>
                  <div v-else-if="group.state === 'formalized' && group.candidate.formalization" class="formalization-result">
                    <span>{{ formalizationActionLabel(group.candidate.formalization.action) }} · R{{ group.candidate.formalization.revision_no }} · {{ statusLabel(group.candidate.formalization.target_revision_status) }}</span>
                    <el-button link type="primary" size="small" @click="openFormalizedStandard(group.candidate)">{{ $t('standard.document.openFormalizedStandard') }}</el-button>
                  </div>
                  <div v-else-if="group.state === 'retained' && canFormalizeCandidate(group.candidate)" class="candidate-actions"><el-button size="small" type="primary" @click="openFormalization(group.candidate)">{{ formalizationButtonLabel(group.candidate) }}</el-button></div>
                </el-card>
              </el-collapse-item>
            </el-collapse>
          </div>
          <el-pagination v-if="candidateFamilyResponse.total > candidateQuery.page_size" class="candidate-pagination" background layout="prev, pager, next" :current-page="candidateQuery.page" :page-size="candidateQuery.page_size" :total="candidateFamilyResponse.total" @current-change="changeCandidatePage" />
        </el-card>

        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.document.relatedItems') }}</h3></template>
          <el-tabs><el-tab-pane :label="$t('standard.document.relatedElements')"><el-tag v-for="item in mappings.elements || []" :key="item.element_id" class="mapping-tag">{{ item.name || item.element_id }}</el-tag><el-empty v-if="!mappings.elements?.length" :description="$t('standard.document.noRelated')" /></el-tab-pane><el-tab-pane :label="$t('standard.document.relatedGlossaries')"><el-tag v-for="item in mappings.glossaries || []" :key="item.glossary_id" class="mapping-tag">{{ item.name || item.glossary_id }}</el-tag><el-empty v-if="!mappings.glossaries?.length" :description="$t('standard.document.noRelated')" /></el-tab-pane><el-tab-pane :label="$t('standard.document.relatedMetrics')"><el-tag v-for="item in mappings.metrics || []" :key="item.metric_id" class="mapping-tag">{{ item.name || item.metric_id }}</el-tag><el-empty v-if="!mappings.metrics?.length" :description="$t('standard.document.noRelated')" /></el-tab-pane></el-tabs>
        </el-card>
      </el-col>

      <el-col :span="8">
        <el-card class="section-card"><template #header><h3>{{ $t('standard.document.revisionHistory') }}</h3></template><el-table :data="history" size="small" @row-click="selectRevision"><el-table-column prop="revision_no" label="R" width="55" /><el-table-column prop="version_label" :label="$t('standard.document.versionLabel')" /><el-table-column :label="$t('standard.common.status')"><template #default="{ row }"><el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column></el-table></el-card>
        <el-card class="section-card"><template #header><h3>{{ $t('standard.common.metadata') }}</h3></template><el-descriptions :column="1" size="small"><el-descriptions-item :label="$t('standard.common.id')">{{ document.id }}</el-descriptions-item><el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(document.created_at) }}</el-descriptions-item><el-descriptions-item :label="$t('standard.common.updatedAt')">{{ formatTime(document.updated_at) }}</el-descriptions-item></el-descriptions></el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="formalizationDialog" :title="$t('standard.document.formalizationTitle')" width="560px" class="addp-dialog" destroy-on-close>
      <el-alert :title="formalizationDialogHint" type="info" :closable="false" show-icon />
      <el-form label-width="120px" class="formalization-form">
        <el-form-item :label="$t('standard.document.formalizationTarget')"><strong>{{ formalizationCandidate?.name }}</strong><code>{{ formalizationCandidate?.code }}</code></el-form-item>
        <el-form-item v-if="requiresMetricType" :label="$t('standard.metric.typeLabel')" required><el-select v-model="formalizationForm.metric_type" style="width:100%"><el-option v-for="type in ['atomic','derived','composite']" :key="type" :value="type" :label="$t(`standard.metric.${type}`)" /></el-select></el-form-item>
        <el-form-item :label="$t('standard.revision.changeSummary')" required><el-input v-model="formalizationForm.change_summary" type="textarea" :rows="3" maxlength="1000" show-word-limit /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formalizationDialog = false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="formalizing" @click="submitFormalization">{{ $t('standard.document.confirmFormalization') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ArrowLeft, InfoFilled, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { createLatestRequestCoordinator, useConsolePageDescriptor } from '@common-ui'
import { documentAPI, domainAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useAuthStore } from '../store/auth'
import { buildStandardPermission } from '../utils/standardPermissions'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import { buildCandidateVariantDifferences } from '../utils/documentCandidateVariants'
import { saveBlob } from '../utils/download'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter(), authStore = useAuthStore()
const { canUpdate, canPublish, canCreateExtraction } = useStandardPermissions('document')
const loading = ref(false), candidateLoading = ref(false), saving = ref(false), uploading = ref(false), extracting = ref(false), formalizing = ref(false), formalizationDialog = ref(false), formalizationCandidate = ref(null)
const document = ref({}), history = ref([]), domains = ref([]), mappings = ref({ elements: [], glossaries: [], metrics: [] })
const revision = reactive({}), identity = reactive({ scope_type: 'tenant_common', owner_domain_id: null, doc_type: 'reference', source_org: '', steward_id: null, tags: [] })
const formalizationForm = reactive({ change_summary: '', metric_type: '' })
const candidateQuery = reactive({ keyword: '', state: '', candidate_type: '', comparison_result: '', page: 1, page_size: 20 })
const candidateFamilyResponse = reactive({ data: [], total: 0, variant_total: 0, page: 1, page_size: 20, total_pages: 1, variant_status_counts: { pending: 0, retained: 0, rejected: 0, formalized: 0 }, family_comparison_counts: { all: 0, new: 0, exact: 0, content_conflict: 0, scope_conflict: 0 } })
const expandedCandidateFamilies = ref([])
const candidateRequests = createLatestRequestCoordinator()
const documentTypes = ['national', 'industry', 'internal', 'reference']
const comparisonResults = ['new', 'exact', 'content_conflict', 'scope_conflict']
const editable = computed(() => canUpdate.value && revision.status === 'draft' && document.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && document.value.draft_revision_id === revision.id)
const canExtract = computed(() => canCreateExtraction.value && Boolean(revision.file_name) && (revision.media_type === 'text/markdown' || revision.file_name?.toLowerCase().endsWith('.md')))
const title = computed(() => revision.name || document.value.code || t('standard.document.detailTitle'))
const candidateFamilies = computed(() => (candidateFamilyResponse.data || []).map(family => ({ ...family, variant_differences: buildCandidateVariantDifferences(family) })))
const pendingCandidateCount = computed(() => candidateFamilyResponse.variant_status_counts?.pending || 0)
const comparisonCount = result => candidateFamilyResponse.family_comparison_counts?.[result] || 0
const comparisonFacetTotal = computed(() => candidateFamilyResponse.family_comparison_counts?.all || 0)
const requiresMetricType = computed(() => formalizationCandidate.value?.candidate_type === 'metric' && formalizationCandidate.value?.comparison?.result === 'new')
const formalizationDialogHint = computed(() => formalizationCandidate.value ? t(`standard.document.formalizationHint.${formalizationCandidate.value.comparison?.result || 'new'}`) : '')
useConsolePageDescriptor(router, 'standard', { title: computed(() => t('standard.document.recentVisitTitle')), subject: title, ready: computed(() => Boolean(document.value.id)) })
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const candidateTypeLabel = type => t(`standard.document.candidateType.${type}`)
const candidateStatusLabel = status => t(`standard.document.candidateStatus.${status}`)
const candidateGroupStateLabel = state => t(`standard.document.candidateGroupState.${state}`)
const candidateGroupStateTagType = state => ({ pending: 'warning', retained: 'success', formalized: 'success', rejected: 'info' }[state] || 'info')
const comparisonTagType = result => ({ new: 'info', exact: 'success', content_conflict: 'warning', scope_conflict: 'danger' }[result] || 'info')
const comparisonLabel = result => t(`standard.document.comparisonResult.${result}`)
const comparisonSummary = result => t(`standard.document.comparisonSummary.${result}`)
const formalizationActionLabel = action => t(`standard.document.formalizationAction.${action}`)
const formalizationButtonLabel = candidate => t(`standard.document.formalizationButton.${candidate.comparison?.result === 'exact' ? 'exact' : candidate.comparison?.result === 'content_conflict' ? 'content_conflict' : 'new'}`)
const canFormalizeCandidate = candidate => {
  if (!canUpdate.value || !candidate?.comparison || candidate.comparison.result === 'scope_conflict') return false
  const action = candidate.comparison.result === 'new' ? 'create' : 'update'
  return authStore.hasPermission(buildStandardPermission(candidate.candidate_type, action))
}
const comparisonFieldLabel = field => field ? t(`standard.document.comparisonField.${field}`) : ''
const candidateVariantFieldLabel = field => t(`standard.document.candidateVariantField.${field}`)
const candidateVariantCardId = (familyKey, semanticFingerprint) => `candidate-variant-${encodeURIComponent(familyKey)}-${encodeURIComponent(semanticFingerprint)}`
const focusCandidateVariant = (familyKey, semanticFingerprint) => {
  const card = window.document.getElementById(candidateVariantCardId(familyKey, semanticFingerprint))
  if (!card) return
  card.scrollIntoView({ behavior: 'smooth', block: 'center' })
  card.focus({ preventScroll: true })
}
const scopeLabel = scope => scope ? t(`standard.common.scopeValue.${scope}`) : '-'
const domainName = id => domains.value.find(item => item.id === id)?.name || `#${id}`
const comparisonValueText = (value, field) => {
  if (!value || value.kind === 'empty') return t('standard.document.emptyValue')
  if (value.kind === 'integer') return field === 'owner_domain_id' ? domainName(value.integer) : String(value.integer)
  if (field === 'scope_type') return scopeLabel(value.text)
  return value.text || t('standard.document.emptyValue')
}
const formatTime = value => formatStandardDateTime(value, locale.value)
const formatFileSize = bytes => !bytes ? '0 B' : bytes < 1024 ? `${bytes} B` : bytes < 1048576 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1048576).toFixed(1)} MB`
const shortHash = value => value ? `${value.slice(0, 10)}…` : '-'
const flattenDomains = nodes => nodes.flatMap(node => [node, ...flattenDomains(node.children || [])])
const setRevision = value => { Object.keys(revision).forEach(key => delete revision[key]); Object.assign(revision, JSON.parse(JSON.stringify(value || {}))) }
const revisionNo = id => history.value.find(item => item.id === id)?.revision_no || '-'
const onScopeChange = scope => { if (scope !== 'domain') identity.owner_domain_id = null }
const goBack = () => navigateStandardRoute(router, { path: '/documents', query: route.query }, { history: 'replace' })

async function load() {
  loading.value = true
  try {
    const [aggregate, revisions, candidateRows, mappingRows] = await Promise.all([documentAPI.get(route.params.id), documentAPI.listRevisions(route.params.id), documentAPI.listCandidateFamilies(route.params.id, candidateQuery), documentAPI.getMappings(route.params.id)])
    document.value = aggregate; history.value = revisions || []; applyCandidateFamilyResponse(candidateRows); mappings.value = mappingRows || { elements: [], glossaries: [], metrics: [] }
    Object.assign(identity, { scope_type: aggregate.scope_type, owner_domain_id: aggregate.owner_domain_id || null, doc_type: aggregate.doc_type, source_org: aggregate.source_org || '', steward_id: aggregate.steward_id || null, tags: aggregate.tags || [] })
    setRevision(aggregate.draft_revision || aggregate.current_revision || history.value[0])
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')); goBack() }
  finally { loading.value = false }
}
const candidateRequestKey = params => JSON.stringify({ document_id: String(route.params.id), ...params })
function applyCandidateFamilyResponse(result) {
  Object.assign(candidateFamilyResponse, result)
  const availableKeys = new Set((result.data || []).map(family => family.family_key))
  const retainedKeys = expandedCandidateFamilies.value.filter(key => availableKeys.has(key))
  expandedCandidateFamilies.value = result.data?.length === 1 ? [result.data[0].family_key] : retainedKeys
}
async function loadCandidateFamilies() {
  const params = { ...candidateQuery }
  const key = candidateRequestKey(params)
  const request = candidateRequests.begin(key)
  candidateLoading.value = true
  try {
    const result = await documentAPI.listCandidateFamilies(route.params.id, params)
    if (!candidateRequests.isCurrent(request, candidateRequestKey({ ...candidateQuery }))) return
    if (candidateQuery.page > result.total_pages) {
      candidateQuery.page = result.total_pages
      await loadCandidateFamilies()
      return
    }
    applyCandidateFamilyResponse(result)
  } catch (error) {
    if (candidateRequests.isCurrent(request, candidateRequestKey({ ...candidateQuery }))) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed'))
  } finally {
    if (candidateRequests.isCurrent(request, candidateRequestKey({ ...candidateQuery }))) candidateLoading.value = false
  }
}
function applyCandidateFilters() { candidateQuery.page = 1; loadCandidateFamilies() }
function changeCandidatePage(page) { candidateQuery.page = page; loadCandidateFamilies() }
async function saveAll() {
  if (!revision.name?.trim() || !revision.change_summary?.trim()) { ElMessage.warning(t('standard.document.revisionRequired')); return }
  if (identity.scope_type === 'domain' && !identity.owner_domain_id) { ElMessage.warning(t('standard.common.selectDomain')); return }
  saving.value = true
  try { let aggregate = await documentAPI.update(document.value.id, { ...identity, version: document.value.version }); aggregate = await documentAPI.updateRevision(document.value.id, revision.id, { ...revision, version: aggregate.version }); document.value = aggregate; ElMessage.success(t('standard.common.saveSuccess')); await load() }
  catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) }
  finally { saving.value = false }
}
async function newDraft() { try { const { value } = await ElMessageBox.prompt(t('standard.revision.changeSummary'), t('standard.document.createRevision'), { inputValidator: input => Boolean(input?.trim()) }); await documentAPI.createRevision(document.value.id, { version: document.value.version, change_summary: value.trim() }); await load() } catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) } }
async function revisionAction(action) { try { await ElMessageBox.confirm(t(`standard.revision.confirm.${action}`), t('standard.common.hint'), { type: 'warning' }); await documentAPI[`${action}Revision`](document.value.id, revision.id, document.value.version); await load() } catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) } }
function selectRevision(row) { setRevision(row) }
async function uploadRevision(file) { if (!editable.value || uploading.value) return; uploading.value = true; try { const data = new FormData(); data.append('file', file.raw); await documentAPI.uploadFile(document.value.id, revision.id, data, document.value.version); ElMessage.success(t('standard.document.uploadSuccess')); await load() } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } finally { uploading.value = false } }
async function downloadRevision() { try { const blob = await documentAPI.download(document.value.id, revision.id); saveBlob(blob, revision.file_name || revision.name) } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.document.downloadFailed')) } }
async function extractCandidates() { if (extracting.value) return; extracting.value = true; try { await documentAPI.extractCandidates(document.value.id, revision.id, document.value.version); ElMessage.success(t('standard.document.extractionSuccess')); await load() } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } finally { extracting.value = false } }
async function decideCandidate(candidate, status) { try { await documentAPI.updateCandidate(candidate.id, { version: candidate.version, status }); await loadCandidateFamilies(); ElMessage.success(t('standard.common.updateSuccess')) } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } }
function openFormalization(candidate) {
  formalizationCandidate.value = candidate
  formalizationForm.change_summary = t('standard.document.formalizationDefaultSummary', { name: candidate.name })
  formalizationForm.metric_type = ''
  formalizationDialog.value = true
}
async function submitFormalization() {
  if (!formalizationForm.change_summary.trim() || (requiresMetricType.value && !formalizationForm.metric_type)) { ElMessage.warning(t('standard.document.formalizationRequired')); return }
  formalizing.value = true
  try {
    const candidate = formalizationCandidate.value
    await documentAPI.formalizeCandidate(candidate.id, { version: candidate.version, change_summary: formalizationForm.change_summary.trim(), ...(requiresMetricType.value ? { metric_type: formalizationForm.metric_type } : {}) })
    formalizationDialog.value = false
    await loadCandidateFamilies()
    ElMessage.success(t('standard.document.formalizationSuccess'))
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) }
  finally { formalizing.value = false }
}
function openComparedStandard(candidate) {
  const basePath = { glossary: '/glossaries', element: '/elements', code_set: '/code-sets', metric: '/metrics' }[candidate.candidate_type]
  if (basePath && candidate.comparison?.standard_id) navigateStandardRoute(router, { path: `${basePath}/${candidate.comparison.standard_id}` })
}
function openFormalizedStandard(candidate) {
  const basePath = { glossary: '/glossaries', element: '/elements', code_set: '/code-sets', metric: '/metrics' }[candidate.candidate_type]
  if (basePath && candidate.formalization?.standard_id) navigateStandardRoute(router, { path: `${basePath}/${candidate.formalization.standard_id}` })
}
watch(() => route.params.id, load, { immediate: true })
onMounted(async () => { try { domains.value = flattenDomains(await domainAPI.list() || []) } catch { domains.value = [] } })
</script>

<style scoped>
.document-detail { padding:20px; }.page-header,.header-left,.header-right,.card-header,.file-row,.candidate-header,.candidate-family-header,.candidate-family-header > div,.candidate-family-meta,.candidate-reference,.comparison-summary,.comparison-target,.formalization-result,.candidate-toolbar,.candidate-comparison-facets,.candidate-comparison-label,.candidate-group-meta,.candidate-occurrence-header,.candidate-variant-differences-header,.candidate-variant-value { display:flex; align-items:center; gap:12px; }.page-header,.card-header,.candidate-header,.candidate-family-header,.comparison-target,.formalization-result,.candidate-occurrence-header { justify-content:space-between; }.page-header { margin-bottom:20px; }.header-left h2,.card-header h3 { margin:0; }.section-card { margin-bottom:20px; }.file-row,.candidate-toolbar,.candidate-comparison-facets,.candidate-comparison-facets .el-radio-group,.candidate-group-meta,.candidate-family-header { flex-wrap:wrap; }.hint { margin-top:14px; }.candidate-toolbar,.candidate-comparison-facets { margin-bottom:14px; }.candidate-toolbar .candidate-search { width:280px; }.candidate-toolbar .el-select { width:180px; }.candidate-toolbar > span,.candidate-comparison-label,.candidate-group-meta,.candidate-family-meta,.candidate-variant-differences-header span { color:var(--addp-text-secondary); }.candidate-comparison-label { gap:2px; }.candidate-comparison-help { color:var(--addp-text-secondary); }.candidate-group-meta { margin-top:8px; font-size:13px; }.candidate-families { border-top:0; }.candidate-family { margin-bottom:12px; border:1px solid var(--el-border-color-light); border-radius:6px; overflow:hidden; }.candidate-family :deep(.el-collapse-item__header) { height:auto; min-height:48px; padding:10px 14px; border-bottom:0; background:var(--addp-bg-secondary); }.candidate-family :deep(.el-collapse-item__wrap) { border-bottom:0; }.candidate-family :deep(.el-collapse-item__content) { padding:12px 14px 2px; }.candidate-family-header { width:100%; padding-right:12px; }.candidate-family-meta { font-size:13px; }.candidate-variant-differences { margin-bottom:12px; padding:12px; border:1px solid var(--el-color-warning-light-5); border-radius:6px; background:var(--el-color-warning-light-9); }.candidate-variant-differences-header { margin-bottom:10px; flex-wrap:wrap; }.candidate-variant-difference-row { display:grid; grid-template-columns:120px minmax(0,1fr); gap:10px; padding:8px 0; border-top:1px solid var(--el-border-color-light); }.candidate-variant-values,.candidate-variant-code-items { display:flex; flex-direction:column; gap:6px; min-width:0; }.candidate-variant-value { align-items:flex-start; min-width:0; overflow-wrap:anywhere; }.candidate-variant-jump { flex:none; padding:0; border:0; background:transparent; cursor:pointer; }.candidate-variant-jump:focus-visible { outline:2px solid var(--el-color-primary); outline-offset:2px; border-radius:4px; }.candidate-card { margin-bottom:10px; }.candidate-card:focus { box-shadow:0 0 0 2px var(--el-color-primary-light-5); }.candidate-header > div { display:flex; align-items:center; gap:8px; }.candidate-reference { margin:8px 0; color:var(--addp-text-secondary); }.candidate-reference code { color:var(--addp-text-primary); overflow-wrap:anywhere; }.candidate-comparison { margin:10px 0; padding:10px 12px; border:1px solid var(--el-border-color-light); border-radius:6px; background:var(--addp-bg-secondary); }.comparison-target { margin-top:8px; }.comparison-target span { min-width:0; overflow-wrap:anywhere; }.comparison-differences { margin-top:10px; }.comparison-value { white-space:pre-wrap; overflow-wrap:anywhere; color:var(--addp-text-primary); }.comparison-item + .comparison-item { margin-top:4px; }.candidate-card pre { white-space:pre-wrap; background:var(--addp-bg-secondary); padding:10px; border-radius:4px; }.candidate-card blockquote { margin:8px 0; padding:8px 12px; border-left:3px solid var(--el-color-primary); background:var(--addp-bg-secondary); }.candidate-card blockquote small { color:var(--addp-text-secondary); }.candidate-occurrences { margin:10px 0; }.candidate-occurrence + .candidate-occurrence { margin-top:12px; padding-top:12px; border-top:1px solid var(--addp-border-color-light); }.candidate-actions { text-align:right; }.candidate-pagination { justify-content:flex-end; margin-top:16px; }.formalization-result { margin-top:12px; padding:8px 10px; border-radius:6px; background:var(--el-color-success-light-9); color:var(--el-color-success-dark-2); }.formalization-form { margin-top:18px; }.formalization-form code { margin-left:8px; }.mapping-tag { margin:4px; }
@media (max-width:768px) { .document-detail { padding:12px; }.page-header { align-items:flex-start; flex-wrap:wrap; }.document-detail :deep(.el-col) { max-width:100%; flex:0 0 100%; }.candidate-variant-difference-row { grid-template-columns:1fr; } }
</style>
