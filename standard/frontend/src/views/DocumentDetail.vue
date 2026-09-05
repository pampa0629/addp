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
          <template #header><div class="card-header"><h3>{{ $t('standard.document.extractionResults') }}</h3><el-tag type="info">{{ pendingCandidateCount }} {{ $t('standard.document.pendingCandidates') }}</el-tag></div></template>
          <el-empty v-if="!extractions.length" :description="$t('standard.document.noExtractions')" />
          <div v-for="batch in extractions" :key="batch.id" class="extraction-batch">
            <div class="batch-title">#{{ batch.id }} · {{ formatTime(batch.created_at) }} · Revision {{ revisionNo(batch.document_revision_id) }}</div>
            <el-card v-for="candidate in batch.candidates || []" :key="candidate.id" shadow="never" class="candidate-card">
              <div class="candidate-header"><div><el-tag size="small">{{ candidateTypeLabel(candidate.candidate_type) }}</el-tag><strong>{{ candidate.name }}</strong><code>{{ candidate.code }}</code></div><el-tag :type="candidate.status === 'pending' ? 'warning' : candidate.status === 'retained' ? 'success' : 'info'">{{ candidateStatusLabel(candidate.status) }}</el-tag></div>
              <p>{{ candidate.definition }}</p>
              <pre v-if="Object.keys(candidate.payload || {}).length">{{ JSON.stringify(candidate.payload, null, 2) }}</pre>
              <blockquote v-for="evidence in candidate.evidences || []" :key="evidence.id"><small>{{ evidence.section_path }} · L{{ evidence.start_line }}-{{ evidence.end_line }} · {{ shortHash(evidence.excerpt_hash) }}</small><div>{{ evidence.excerpt }}</div></blockquote>
              <div v-if="candidate.status === 'pending' && canUpdate" class="candidate-actions"><el-button size="small" type="success" @click="decideCandidate(candidate, 'retained')">{{ $t('standard.document.retainCandidate') }}</el-button><el-button size="small" @click="decideCandidate(candidate, 'rejected')">{{ $t('standard.document.rejectCandidate') }}</el-button></div>
            </el-card>
          </div>
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
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { documentAPI, domainAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import { saveBlob } from '../utils/download'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const { canUpdate, canPublish, canCreateExtraction } = useStandardPermissions('document')
const loading = ref(false), saving = ref(false), uploading = ref(false), extracting = ref(false)
const document = ref({}), history = ref([]), extractions = ref([]), domains = ref([]), mappings = ref({ elements: [], glossaries: [], metrics: [] })
const revision = reactive({}), identity = reactive({ scope_type: 'tenant_common', owner_domain_id: null, doc_type: 'reference', source_org: '', steward_id: null, tags: [] })
const documentTypes = ['national', 'industry', 'internal', 'reference']
const editable = computed(() => canUpdate.value && revision.status === 'draft' && document.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && document.value.draft_revision_id === revision.id)
const canExtract = computed(() => canCreateExtraction.value && Boolean(revision.file_name) && (revision.media_type === 'text/markdown' || revision.file_name?.toLowerCase().endsWith('.md')))
const title = computed(() => revision.name || document.value.code || t('standard.document.detailTitle'))
const pendingCandidateCount = computed(() => extractions.value.flatMap(item => item.candidates || []).filter(item => item.status === 'pending').length)
useConsolePageDescriptor(router, 'standard', { title: computed(() => t('standard.document.recentVisitTitle')), subject: title, ready: computed(() => Boolean(document.value.id)) })
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const candidateTypeLabel = type => t(`standard.document.candidateType.${type}`)
const candidateStatusLabel = status => t(`standard.document.candidateStatus.${status}`)
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
    const [aggregate, revisions, extractionRows, mappingRows] = await Promise.all([documentAPI.get(route.params.id), documentAPI.listRevisions(route.params.id), documentAPI.listExtractions(route.params.id), documentAPI.getMappings(route.params.id)])
    document.value = aggregate; history.value = revisions || []; extractions.value = extractionRows || []; mappings.value = mappingRows || { elements: [], glossaries: [], metrics: [] }
    Object.assign(identity, { scope_type: aggregate.scope_type, owner_domain_id: aggregate.owner_domain_id || null, doc_type: aggregate.doc_type, source_org: aggregate.source_org || '', steward_id: aggregate.steward_id || null, tags: aggregate.tags || [] })
    setRevision(aggregate.draft_revision || aggregate.current_revision || history.value[0])
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')); goBack() }
  finally { loading.value = false }
}
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
async function decideCandidate(candidate, status) { try { await documentAPI.updateCandidate(candidate.id, { version: candidate.version, status }); await load(); ElMessage.success(t('standard.common.updateSuccess')) } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } }
watch(() => route.params.id, load, { immediate: true })
onMounted(async () => { try { domains.value = flattenDomains(await domainAPI.list() || []) } catch { domains.value = [] } })
</script>

<style scoped>
.document-detail { padding:20px; }.page-header,.header-left,.header-right,.card-header,.file-row,.candidate-header { display:flex; align-items:center; gap:12px; }.page-header,.card-header,.candidate-header { justify-content:space-between; }.page-header { margin-bottom:20px; }.header-left h2,.card-header h3 { margin:0; }.section-card { margin-bottom:20px; }.file-row { flex-wrap:wrap; }.hint { margin-top:14px; }.extraction-batch { margin-bottom:18px; }.batch-title { margin-bottom:8px; color:var(--addp-text-secondary); }.candidate-card { margin-bottom:10px; }.candidate-header > div { display:flex; align-items:center; gap:8px; }.candidate-card pre { white-space:pre-wrap; background:var(--addp-bg-secondary); padding:10px; border-radius:4px; }.candidate-card blockquote { margin:8px 0; padding:8px 12px; border-left:3px solid var(--el-color-primary); background:var(--addp-bg-secondary); }.candidate-card blockquote small { color:var(--addp-text-secondary); }.candidate-actions { text-align:right; }.mapping-tag { margin:4px; }
@media (max-width:768px) { .document-detail { padding:12px; }.page-header { align-items:flex-start; flex-wrap:wrap; }.document-detail :deep(.el-col) { max-width:100%; flex:0 0 100%; } }
</style>
