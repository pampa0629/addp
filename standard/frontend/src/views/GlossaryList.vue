<template>
  <div class="glossary-list">
    <div class="page-header">
      <h2>{{ $t('standard.glossary.title') }}</h2>
      <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.glossary.create') }}</el-button>
    </div>

    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="8"><el-input v-model="filters.keyword" :placeholder="$t('standard.glossary.searchPlaceholder')" clearable :prefix-icon="Search" @change="handleFilterChange" /></el-col>
        <el-col :span="6">
          <el-select v-model="filters.owner_domain_id" :placeholder="$t('standard.common.selectDomain')" clearable @change="handleFilterChange">
            <el-option v-for="domain in domainList" :key="domain.id" :label="domain.name" :value="domain.id" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.status" :placeholder="$t('standard.common.selectStatus')" clearable @change="handleFilterChange">
            <el-option v-for="status in revisionStatuses" :key="status" :label="statusLabel(status)" :value="status" />
          </el-select>
        </el-col>
      </el-row>
    </el-card>

    <el-card class="table-card">
      <el-table :data="glossaries" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.common.code')" prop="code" min-width="130" />
        <el-table-column :label="$t('standard.glossary.nameLabel')" min-width="170">
          <template #default="{ row }"><span class="term-name">{{ displayRevision(row)?.name || '-' }}</span></template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.scopeLabel')" width="130">
          <template #default="{ row }">{{ scopeLabel(row.scope_type) }}</template>
        </el-table-column>
        <el-table-column :label="$t('standard.glossary.domainLabel')" width="130">
          <template #default="{ row }">{{ getDomainName(row.owner_domain_id) || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('standard.glossary.definitionLabel')" show-overflow-tooltip>
          <template #default="{ row }">{{ displayRevision(row)?.definition || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.status')" width="110">
          <template #default="{ row }"><el-tag :type="statusType(displayRevision(row)?.status)" size="small">{{ statusLabel(displayRevision(row)?.status) }}</el-tag></template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <div class="table-actions"><el-button link type="primary" @click="goToDetail(row)">{{ $t('standard.common.detail') }}</el-button><el-button v-if="canDelete && isGlossaryDeletable(row)" link type="danger" @click="handleDelete(row)">{{ $t('standard.common.delete') }}</el-button></div>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-if="total > 0" class="pagination" :total="total" :page-size="filters.page_size" :current-page="filters.page" layout="total, prev, pager, next" @current-change="handlePageChange" />
    </el-card>

    <el-dialog v-model="dialogVisible" :title="$t('standard.glossary.createTitle')" width="680px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item :label="$t('standard.common.code')" prop="code"><el-input v-model="form.code" :placeholder="$t('standard.glossary.codePlaceholder')" /></el-form-item>
        <el-form-item :label="$t('standard.common.scopeLabel')" prop="scope_type">
          <el-select v-model="form.scope_type" style="width:100%" @change="onScopeChange">
            <el-option :label="$t('standard.common.scopeValue.tenant_common')" value="tenant_common" />
            <el-option :label="$t('standard.common.scopeValue.domain')" value="domain" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.scope_type === 'domain'" :label="$t('standard.glossary.domainLabel')" prop="owner_domain_id">
          <el-select v-model="form.owner_domain_id" filterable style="width:100%"><el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" /></el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.nameLabel')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="$t('standard.glossary.aliasLabel')"><el-select v-model="form.alias" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
        <el-form-item :label="$t('standard.glossary.definitionLabel')" prop="definition"><el-input v-model="form.definition" type="textarea" :rows="4" /></el-form-item>
        <el-form-item :label="$t('standard.glossary.exampleLabel')"><el-input v-model="form.example" type="textarea" :rows="2" /></el-form-item>
        <el-form-item :label="$t('standard.glossary.noteLabel')"><el-input v-model="form.note" type="textarea" :rows="2" /></el-form-item>
        <el-form-item :label="$t('standard.common.tags')"><el-select v-model="form.tags" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
        <el-form-item :label="$t('standard.revision.changeSummary')" prop="change_summary"><el-input v-model="form.change_summary" /></el-form-item>
        <el-form-item :label="$t('standard.revision.effectiveFrom')"><el-date-picker v-model="form.effective_from" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { domainAPI, glossaryAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { createLatestRequestCoordinator } from '@common-ui'
import { buildGlossaryFilterQuery, createGlossaryForm, isGlossaryDeletable, resolveGlossaryFilters } from '../utils/glossaryRouteState'

const { t } = useI18n()
const { canCreate, canDelete } = useStandardPermissions('glossary')
const router = useRouter(), route = useRoute()
const loading = ref(false), submitting = ref(false), dialogVisible = ref(false), formRef = ref(null)
const glossaries = ref([]), domainList = ref([]), total = ref(0), form = ref(createGlossaryForm(null))
const listRequests = createLatestRequestCoordinator()
const revisionStatuses = ['draft', 'in_review', 'published', 'withdrawn']
const filters = reactive({ keyword: '', owner_domain_id: null, status: '', page: 1, page_size: 20 })
const rules = computed(() => ({
  code: [{ required: true, message: t('standard.glossary.codeRequired'), trigger: 'blur' }],
  name: [{ required: true, message: t('standard.glossary.nameRequired'), trigger: 'blur' }],
  definition: [{ required: true, message: t('standard.glossary.definitionRequired'), trigger: 'blur' }],
  change_summary: [{ required: true, message: t('standard.revision.changeSummaryRequired'), trigger: 'blur' }],
  owner_domain_id: [{ required: form.value.scope_type === 'domain', message: t('standard.common.selectDomain'), trigger: 'change' }]
}))
const displayRevision = row => row.draft_revision || row.current_revision
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const scopeLabel = scope => scope ? t(`standard.common.scopeValue.${scope}`) : '-'
const flattenDomains = nodes => nodes.flatMap(node => [node, ...flattenDomains(node.children || [])])
const getDomainName = id => domainList.value.find(item => item.id === id)?.name
const buildFilterQuery = () => buildGlossaryFilterQuery(filters)
const syncFilterRoute = () => navigateStandardRoute(router, { path: '/glossaries', query: buildFilterQuery() }, { history: 'replace' })
const onScopeChange = value => { if (value !== 'domain') form.value.owner_domain_id = null }
const goToDetail = row => navigateStandardRoute(router, { path: `/glossaries/${row.id}`, query: buildFilterQuery() })

async function loadDomains() { try { domainList.value = flattenDomains(await domainAPI.list() || []) } catch { domainList.value = [] } }
async function loadGlossaries() {
  const params = { page: filters.page, page_size: filters.page_size, keyword: filters.keyword || undefined, owner_domain_id: filters.owner_domain_id || undefined, status: filters.status || undefined }
  const key = JSON.stringify(params), request = listRequests.begin(key); loading.value = true
  try { const res = await glossaryAPI.list(params); if (listRequests.isCurrent(request, key)) { glossaries.value = res.data || []; total.value = res.total || 0 } }
  catch (error) { if (listRequests.isCurrent(request, key)) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')) }
  finally { if (listRequests.isCurrent(request, key)) loading.value = false }
}
function handleFilterChange() { filters.page = 1; syncFilterRoute(); loadGlossaries() }
function handlePageChange(page) { filters.page = page; syncFilterRoute(); loadGlossaries() }
function openCreateDialog() { form.value = createGlossaryForm(filters.owner_domain_id); dialogVisible.value = true }
async function handleSubmit() {
  if (submitting.value || !formRef.value) return
  submitting.value = true
  try {
    if (!(await formRef.value.validate().catch(() => false))) return
    await glossaryAPI.create(form.value); ElMessage.success(t('standard.common.createSuccess')); dialogVisible.value = false; await loadGlossaries()
  }
  catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) }
  finally { submitting.value = false }
}
async function handleDelete(row) {
  try { await ElMessageBox.confirm(t('standard.glossary.confirmDelete', { name: displayRevision(row)?.name || row.code }), t('standard.common.hint'), { type: 'warning' }); await glossaryAPI.delete(row.id); ElMessage.success(t('standard.common.deleteSuccess')); await loadGlossaries() }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
watch(() => route.query, query => { Object.assign(filters, resolveGlossaryFilters(query)); loadGlossaries() }, { immediate: true })
onMounted(loadDomains)
</script>

<style scoped>
.glossary-list { padding: 20px; }
.page-header { display:flex; justify-content:space-between; align-items:center; margin-bottom:20px; }
.filter-card, .table-card { margin-bottom:16px; }
.pagination { margin-top:16px; justify-content:flex-end; }
.term-name { font-weight:500; color:var(--addp-text-primary); }
.table-actions { display:flex; flex-wrap:nowrap; white-space:nowrap; }
@media (max-width:768px) { .glossary-list { padding:12px; } }
</style>
