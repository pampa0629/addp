<template>
  <div class="document-list">
    <div class="page-header">
      <div><h2>{{ $t('standard.document.title') }}</h2><p>{{ $t('standard.document.subtitle') }}</p></div>
      <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.document.create') }}</el-button>
    </div>

    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="7"><el-input v-model="filters.keyword" :prefix-icon="Search" :placeholder="$t('standard.document.searchPlaceholder')" clearable @change="handleFilterChange" /></el-col>
        <el-col :span="5"><el-select v-model="filters.doc_type" :placeholder="$t('standard.document.filterTypePlaceholder')" clearable @change="handleFilterChange"><el-option v-for="type in documentTypes" :key="type" :label="docTypeLabel(type)" :value="type" /></el-select></el-col>
        <el-col :span="6"><el-select v-model="filters.owner_domain_id" :placeholder="$t('standard.common.selectDomain')" clearable @change="handleFilterChange"><el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" /></el-select></el-col>
        <el-col :span="6"><el-select v-model="filters.status" :placeholder="$t('standard.common.selectStatus')" clearable @change="handleFilterChange"><el-option v-for="status in revisionStatuses" :key="status" :label="statusLabel(status)" :value="status" /></el-select></el-col>
      </el-row>
    </el-card>

    <el-card>
      <el-table :data="documents" v-loading="loading" stripe>
        <el-table-column prop="code" :label="$t('standard.common.code')" min-width="110" />
        <el-table-column :label="$t('standard.document.nameLabel')" min-width="140"><template #default="{ row }">{{ displayRevision(row)?.name || '-' }}</template></el-table-column>
        <el-table-column v-if="!isNarrow" :label="$t('standard.common.scopeLabel')" width="96"><template #default="{ row }">{{ scopeLabel(row.scope_type) }}</template></el-table-column>
        <el-table-column v-if="!isNarrow" :label="$t('standard.document.domainLabel')" width="110"><template #default="{ row }">{{ domainName(row.owner_domain_id) || '-' }}</template></el-table-column>
        <el-table-column v-if="!isNarrow" :label="$t('standard.common.type')" width="84"><template #default="{ row }"><el-tag size="small" :type="docTypeTagType(row.doc_type)">{{ docTypeLabel(row.doc_type) }}</el-tag></template></el-table-column>
        <el-table-column v-if="!isNarrow" :label="$t('standard.document.versionLabel')" width="90"><template #default="{ row }">{{ displayRevision(row)?.version_label || '-' }}</template></el-table-column>
        <el-table-column :label="$t('standard.common.status')" width="80"><template #default="{ row }"><el-tag size="small" :type="statusType(displayRevision(row)?.status)">{{ statusLabel(displayRevision(row)?.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="100" fixed="right"><template #default="{ row }"><div class="table-actions"><el-button link type="primary" @click="goToDetail(row)">{{ $t('standard.common.detail') }}</el-button><el-button v-if="canDelete && !row.has_publication_history" link type="danger" @click="deleteDocument(row)">{{ $t('standard.common.delete') }}</el-button></div></template></el-table-column>
      </el-table>
      <el-pagination v-if="total" class="pagination" :total="total" :page-size="filters.page_size" :current-page="filters.page" layout="total, prev, pager, next" @current-change="handlePageChange" />
    </el-card>

    <el-dialog v-model="dialogVisible" :title="$t('standard.document.createTitle')" width="680px" @closed="resetForm">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
        <el-form-item :label="$t('standard.common.code')" prop="code"><el-input v-model="form.code" /></el-form-item>
        <el-form-item :label="$t('standard.common.scopeLabel')" prop="scope_type"><el-select v-model="form.scope_type" style="width:100%" @change="onScopeChange"><el-option :label="$t('standard.common.scopeValue.tenant_common')" value="tenant_common" /><el-option :label="$t('standard.common.scopeValue.domain')" value="domain" /></el-select></el-form-item>
        <el-form-item v-if="form.scope_type === 'domain'" :label="$t('standard.document.domainLabel')" prop="owner_domain_id"><el-select v-model="form.owner_domain_id" filterable style="width:100%"><el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" /></el-select></el-form-item>
        <el-form-item :label="$t('standard.document.nameLabel')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="$t('standard.document.typeLabel')" prop="doc_type"><el-select v-model="form.doc_type" style="width:100%"><el-option v-for="type in documentTypes" :key="type" :label="docTypeLabel(type)" :value="type" /></el-select></el-form-item>
        <el-form-item :label="$t('standard.document.sourceLabel')"><el-input v-model="form.source_org" /></el-form-item>
        <el-form-item :label="$t('standard.document.versionLabel')"><el-input v-model="form.version_label" /></el-form-item>
        <el-form-item :label="$t('standard.document.descriptionLabel')"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="$t('standard.revision.changeSummary')" prop="change_summary"><el-input v-model="form.change_summary" /></el-form-item>
        <el-form-item :label="$t('standard.revision.effectiveFrom')"><el-date-picker v-model="form.effective_from" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
        <el-form-item :label="$t('standard.document.fileLabel')"><el-upload ref="uploadRef" :auto-upload="false" :limit="1" :on-change="file => selectedFile = file.raw" :on-remove="() => selectedFile = null" accept=".md,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt"><el-button>{{ $t('standard.document.fileSelectBtn') }}</el-button><template #tip><div class="upload-tip">{{ $t('standard.document.extractionMarkdownTip') }}</div></template></el-upload></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="createDocument">{{ $t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { createLatestRequestCoordinator } from '@common-ui'
import { documentAPI, domainAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getDocumentTypeTagType } from '../utils/documentType'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const route = useRoute(), router = useRouter()
const { canCreate, canDelete } = useStandardPermissions('document')
const documents = ref([]), domains = ref([]), total = ref(0), loading = ref(false), saving = ref(false)
const dialogVisible = ref(false), formRef = ref(null), uploadRef = ref(null), selectedFile = ref(null)
const isNarrow = ref(false)
let narrowMediaQuery
const syncNarrowViewport = event => { isNarrow.value = event.matches }
const listRequests = createLatestRequestCoordinator()
const documentTypes = ['national', 'industry', 'internal', 'reference']
const revisionStatuses = ['draft', 'in_review', 'published', 'withdrawn']
const filters = reactive({ keyword: '', doc_type: '', owner_domain_id: null, status: '', page: 1, page_size: 20 })
const emptyForm = () => ({ code: '', scope_type: 'tenant_common', owner_domain_id: null, name: '', doc_type: 'reference', source_org: '', version_label: '', description: '', change_summary: '', effective_from: null, tags: [] })
const form = ref(emptyForm())
const rules = computed(() => ({
  code: [{ required: true, message: t('standard.document.codeRequired'), trigger: 'blur' }],
  name: [{ required: true, message: t('standard.document.nameRequired'), trigger: 'blur' }],
  doc_type: [{ required: true, message: t('standard.document.typeRequired'), trigger: 'change' }],
  change_summary: [{ required: true, message: t('standard.revision.changeSummaryRequired'), trigger: 'blur' }],
  owner_domain_id: [{ required: form.value.scope_type === 'domain', message: t('standard.common.selectDomain'), trigger: 'change' }]
}))
const flattenDomains = nodes => nodes.flatMap(node => [node, ...flattenDomains(node.children || [])])
const displayRevision = row => row.draft_revision || row.current_revision
const docTypeLabel = type => t(`standard.document.${type}`)
const docTypeTagType = getDocumentTypeTagType
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const scopeLabel = scope => scope ? t(`standard.common.scopeValue.${scope}`) : '-'
const domainName = id => domains.value.find(item => item.id === id)?.name
const filterQuery = () => Object.fromEntries(Object.entries(filters).filter(([, value]) => value !== '' && value !== null && value !== 1 && value !== 20).map(([key, value]) => [key, String(value)]))

async function loadDocuments() {
  const params = { ...filters, keyword: filters.keyword || undefined, doc_type: filters.doc_type || undefined, owner_domain_id: filters.owner_domain_id || undefined, status: filters.status || undefined }
  const key = JSON.stringify(params), request = listRequests.begin(key); loading.value = true
  try { const result = await documentAPI.list(params); if (listRequests.isCurrent(request, key)) { documents.value = result.data || []; total.value = result.total || 0 } }
  catch (error) { if (listRequests.isCurrent(request, key)) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')) }
  finally { if (listRequests.isCurrent(request, key)) loading.value = false }
}
function handleFilterChange() { filters.page = 1; navigateStandardRoute(router, { path: '/documents', query: filterQuery() }, { history: 'replace' }); loadDocuments() }
function handlePageChange(page) { filters.page = page; navigateStandardRoute(router, { path: '/documents', query: filterQuery() }, { history: 'replace' }); loadDocuments() }
function onScopeChange(scope) { if (scope !== 'domain') form.value.owner_domain_id = null }
function openCreateDialog() { form.value = emptyForm(); selectedFile.value = null; dialogVisible.value = true }
function resetForm() { form.value = emptyForm(); selectedFile.value = null; uploadRef.value?.clearFiles() }
function goToDetail(row) { navigateStandardRoute(router, { path: `/documents/${row.id}`, query: route.query }) }
async function createDocument() {
  if (saving.value || !formRef.value) return
  if (!(await formRef.value.validate().catch(() => false))) return
  saving.value = true
  try {
    let aggregate = await documentAPI.create(form.value)
    if (selectedFile.value) { const data = new FormData(); data.append('file', selectedFile.value); aggregate = await documentAPI.uploadFile(aggregate.id, aggregate.draft_revision.id, data, aggregate.version) }
    ElMessage.success(t('standard.common.createSuccess')); dialogVisible.value = false; goToDetail(aggregate)
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) }
  finally { saving.value = false }
}
async function deleteDocument(row) {
  try { await ElMessageBox.confirm(t('standard.document.confirmDelete', { name: displayRevision(row)?.name || row.code }), t('standard.common.hint'), { type: 'warning' }); await documentAPI.delete(row.id); ElMessage.success(t('standard.common.deleteSuccess')); await loadDocuments() }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
watch(() => route.query, query => { filters.keyword = typeof query.keyword === 'string' ? query.keyword : ''; filters.doc_type = typeof query.doc_type === 'string' ? query.doc_type : ''; filters.owner_domain_id = Number(query.owner_domain_id) || null; filters.status = typeof query.status === 'string' ? query.status : ''; filters.page = Number(query.page) || 1; filters.page_size = Number(query.page_size) || 20; loadDocuments() }, { immediate: true })
onMounted(async () => {
  narrowMediaQuery = window.matchMedia('(max-width: 768px)')
  syncNarrowViewport(narrowMediaQuery)
  narrowMediaQuery.addEventListener('change', syncNarrowViewport)
  try { domains.value = flattenDomains(await domainAPI.list() || []) } catch { domains.value = [] }
})
onBeforeUnmount(() => narrowMediaQuery?.removeEventListener('change', syncNarrowViewport))
</script>

<style scoped>
.document-list { min-height:100%; padding:20px; color:var(--addp-text-primary); background:var(--addp-bg-secondary); }
.page-header { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:20px; }.page-header h2 { margin:0 0 4px; font-size:18px; }.page-header p { margin:0; color:var(--addp-text-secondary); }
.filter-card { margin-bottom:16px; }.pagination { margin-top:16px; justify-content:flex-end; }.table-actions { display:flex; white-space:nowrap; }.upload-tip { color:var(--addp-text-secondary); font-size:12px; }
@media (max-width:768px) { .document-list { padding:12px; }.page-header { flex-wrap:wrap; gap:10px; }.document-list :deep(.el-col) { max-width:100%; flex:0 0 100%; margin-bottom:8px; } }
</style>
