<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>{{ pageTitle }}</h1>
        <p>{{ pageDescription }}</p>
      </div>
      <div class="header-actions">
        <el-button v-if="canManageGovernance" :icon="WarningFilled" @click="openGovernanceTasks">
          {{ t('catalog.governanceTasks.action') }}
        </el-button>
        <el-button :icon="Refresh" :loading="loading || facetsLoading" @click="refreshPage">
          {{ t('catalog.common.refresh') }}
        </el-button>
      </div>
    </div>

    <div v-if="canViewInventory" class="view-switch" role="group" :aria-label="t('catalog.entries.view.label')">
      <el-radio-group v-model="filters.view" @change="changeView">
        <el-radio-button value="governance">{{ t('catalog.entries.view.governance') }}</el-radio-button>
        <el-radio-button value="inventory">{{ t('catalog.entries.view.inventory') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-alert
      v-if="unavailableFacetLabels.length > 0"
      class="facet-alert"
      type="warning"
      :closable="false"
      :title="t('catalog.entries.facetUnavailable', { facets: formattedUnavailableFacets })"
    />

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filters" @submit.prevent="applyFilters">
        <el-form-item :label="t('catalog.entries.search')">
          <el-input v-model="filters.search" clearable :placeholder="t('catalog.entries.searchPlaceholder')" @keyup.enter="applyFilters" />
        </el-form-item>
        <el-form-item :label="t('catalog.entries.type')">
          <el-select v-model="filters.entry_type" clearable :placeholder="t('catalog.common.all')">
            <el-option v-for="entryType in entryTypes" :key="entryType" :label="entryTypeLabel(entryType)" :value="entryType" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.sourceStatus')">
          <el-select v-model="filters.source_status" clearable :placeholder="t('catalog.common.all')">
            <el-option :label="t('catalog.status.source.active')" value="active" />
            <el-option :label="t('catalog.status.source.missing')" value="missing" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.governanceStatus')">
          <el-select v-model="filters.governance_status" clearable :placeholder="t('catalog.common.all')">
            <el-option v-for="status in availableGovernanceStatuses" :key="status" :label="t(`catalog.status.governance.${status}`)" :value="status" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.visibility')">
          <el-select v-model="filters.visibility" clearable :placeholder="t('catalog.common.all')">
            <el-option v-for="visibility in visibilities" :key="visibility" :label="t(`catalog.status.visibility.${visibility}`)" :value="visibility" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.primaryDomain')">
          <el-select
            v-model="filters.primary_domain_id"
            clearable
            filterable
            :loading="facetsLoading"
            :disabled="facets.primary_domains.status === 'unavailable'"
            :placeholder="t('catalog.entries.selectPrimaryDomain')"
          >
            <el-option v-for="option in domainOptions" :key="option.id" :label="facetOptionLabel(option)" :value="option.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.accountableDepartment')">
          <el-select
            v-model="filters.accountable_department_id"
            clearable
            filterable
            :loading="facetsLoading"
            :disabled="facets.accountable_departments.status === 'unavailable'"
            :placeholder="t('catalog.entries.selectDepartment')"
          >
            <el-option v-for="option in departmentOptions" :key="option.id" :label="facetOptionLabel(option)" :value="option.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.sourceEngine')">
          <el-select
            v-model="filters.source_engine_id"
            clearable
            filterable
            :loading="facetsLoading"
            :disabled="facets.source_engines.status === 'unavailable'"
            :placeholder="t('catalog.entries.selectEngine')"
          >
            <el-option v-for="option in engineOptions" :key="option.id" :label="engineOptionLabel(option)" :value="option.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="Search" @click="applyFilters">{{ t('catalog.common.search') }}</el-button>
          <el-button @click="resetFilters">{{ t('catalog.common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table v-loading="loading" :data="result.data" @row-click="openEntry">
        <el-table-column prop="display_name" :label="t('catalog.entries.name')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="entry-link">{{ row.display_name || t('catalog.entries.unnamed') }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="entry_type" :label="t('catalog.entries.type')" width="130">
          <template #default="{ row }">{{ entryTypeLabel(row.entry_type) }}</template>
        </el-table-column>
        <el-table-column prop="source_status" :label="t('catalog.entries.sourceStatus')" width="130">
          <template #default="{ row }"><el-tag :type="sourceTagType(row.source_status)">{{ catalogStatusLabel(t, 'catalog.status.source', row.source_status) }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="governance_status" :label="t('catalog.entries.governanceStatus')" width="150">
          <template #default="{ row }"><el-tag :type="governanceTagType(row.governance_status)">{{ catalogStatusLabel(t, 'catalog.status.governance', row.governance_status) }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="visibility" :label="t('catalog.entries.visibility')" width="130">
          <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.visibility', row.visibility) }}</template>
        </el-table-column>
        <el-table-column :label="t('catalog.entries.sourceEngine')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ sourceEngineLabel(row.source_engine_id) }}</template>
        </el-table-column>
        <el-table-column prop="updated_at" :label="t('catalog.entries.updatedAt')" min-width="180">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && result.data.length === 0" :description="emptyDescription" />
      <div class="pagination-row">
        <el-pagination
          background
          layout="total, sizes, prev, pager, next"
          :total="result.total"
          :current-page="filters.page"
          :page-size="filters.page_size"
          :page-sizes="[20, 50, 100, 200]"
          @current-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, Search, WarningFilled } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute } from '@common-ui'
import { listEntries, listEntryFacets } from '../api/catalog'
import { useAuthStore } from '../store/auth'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import { buildEntryListQuery, isCanonicalEntryListQuery, parseEntryListRoute } from '../utils/entryRouteState'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const canManageGovernance = computed(() => authStore.hasPermission('catalog.entry.update'))
const canViewInventory = computed(() => authStore.hasPermission('catalog.inventory.read'))
const governanceStatuses = ['discovered', 'curated', 'certified', 'deprecated']
const availableGovernanceStatuses = computed(() => filters.view === 'inventory' ? governanceStatuses : governanceStatuses.filter(status => status !== 'discovered'))
const visibilities = ['inventory', 'department', 'tenant']
const entryTypes = ['data_item', 'business_entity', 'logical_model', 'metric', 'data_service', 'development_artifact']
const filters = reactive(parseEntryListRoute(route.query))
const result = reactive({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
const facets = reactive(emptyFacets())
const loading = ref(false)
const facetsLoading = ref(false)
let requestVersion = 0
let facetRequestVersion = 0
let loadedFacetView = ''

const pageTitle = computed(() => t(`catalog.entries.view.${filters.view}Title`))
const pageDescription = computed(() => t(`catalog.entries.view.${filters.view}Description`))
const emptyDescription = computed(() => t(`catalog.entries.view.${filters.view}Empty`))
const domainOptions = computed(() => optionsWithSelected(facets.primary_domains, filters.primary_domain_id))
const departmentOptions = computed(() => optionsWithSelected(facets.accountable_departments, filters.accountable_department_id))
const engineOptions = computed(() => optionsWithSelected(facets.source_engines, filters.source_engine_id))
const engineOptionsByID = computed(() => new Map(engineOptions.value.map(option => [String(option.id), option])))
const unavailableFacetLabels = computed(() => {
  const labels = []
  if (facets.primary_domains.status === 'unavailable') labels.push(t('catalog.entries.primaryDomain'))
  if (facets.accountable_departments.status === 'unavailable') labels.push(t('catalog.entries.accountableDepartment'))
  if (facets.source_engines.status === 'unavailable') labels.push(t('catalog.entries.sourceEngine'))
  return labels
})
const formattedUnavailableFacets = computed(() => new Intl.ListFormat(locale.value === 'en' ? 'en' : 'zh-CN').format(unavailableFacetLabels.value))

function emptyFacet() {
  return { status: 'current', options: [] }
}

function emptyFacets() {
  return { view: 'governance', primary_domains: emptyFacet(), accountable_departments: emptyFacet(), source_engines: emptyFacet() }
}

function optionsWithSelected(facet, selectedID) {
  const options = [...(facet.options || [])]
  const normalized = String(selectedID || '')
  if (normalized && !options.some(option => String(option.id) === normalized)) {
    options.unshift({ id: normalized, name: t('catalog.entries.unresolvedReference'), count: 0 })
  }
  return options
}

function entryTypeLabel(entryType) {
  const key = { data_item: 'dataItem', business_entity: 'businessEntity', logical_model: 'logicalModel', metric: 'metric', data_service: 'dataService', development_artifact: 'developmentArtifact' }[entryType] || 'dataItem'
  return t(`catalog.entryType.${key}`)
}

function facetOptionLabel(option) {
  const identity = option.code ? `${option.name} · ${option.code}` : option.name
  return option.count > 0 ? `${identity} (${option.count})` : identity
}

function engineOptionLabel(option) {
  const identity = option.engine_type ? `${option.name} · ${option.engine_type}` : option.name
  return option.count > 0 ? `${identity} (${option.count})` : identity
}

function sourceEngineLabel(value) {
  const id = String(value || '')
  if (!id) return '-'
  const option = engineOptionsByID.value.get(id)
  if (!option || option.name === t('catalog.entries.unresolvedReference')) return t('catalog.entries.engineUnavailable')
  return option.engine_type ? `${option.name} · ${option.engine_type}` : option.name
}

function syncFilters(query) {
  Object.assign(filters, parseEntryListRoute(query))
}

async function loadEntries() {
  const version = ++requestVersion
  loading.value = true
  try {
    const response = await listEntries(filters)
    if (version !== requestVersion) return
    Object.assign(result, response, { data: response.data || [] })
  } catch (error) {
    if (version !== requestVersion) return
    Object.assign(result, { data: [], total: 0, page: filters.page, page_size: filters.page_size, total_pages: 0 })
    ElMessage.error(error?.response?.data?.error || t('catalog.entries.loadFailed'))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function loadFacets(force = false) {
  if (!force && loadedFacetView === filters.view) return
  const version = ++facetRequestVersion
  facetsLoading.value = true
  try {
    const response = await listEntryFacets({ view: filters.view })
    if (version !== facetRequestVersion) return
    Object.assign(facets, emptyFacets(), response)
    loadedFacetView = filters.view
  } catch {
    if (version !== facetRequestVersion) return
    Object.assign(facets, {
      view: filters.view,
      primary_domains: { status: 'unavailable', options: [] },
      accountable_departments: { status: 'unavailable', options: [] },
      source_engines: { status: 'unavailable', options: [] }
    })
    loadedFacetView = filters.view
  } finally {
    if (version === facetRequestVersion) facetsLoading.value = false
  }
}

async function navigateList(history = 'push') {
  await navigateConsoleModuleRoute(router, 'catalog', {
    path: '/entries',
    query: buildEntryListQuery(filters)
  }, { history })
}

async function applyFilters() {
  filters.page = 1
  await navigateList('push')
}

async function resetFilters() {
  const currentView = filters.view
  Object.assign(filters, parseEntryListRoute(currentView === 'inventory' ? { view: 'inventory' } : {}))
  await navigateList('push')
}

async function changeView(view) {
  filters.view = view
  if (view === 'governance' && filters.governance_status === 'discovered') filters.governance_status = ''
  filters.page = 1
  loadedFacetView = ''
  await navigateList('replace')
}

async function refreshPage() {
  loadedFacetView = ''
  await Promise.all([loadEntries(), loadFacets(true)])
}

async function changePage(page) {
  filters.page = page
  await navigateList('push')
}

async function changePageSize(pageSize) {
  filters.page = 1
  filters.page_size = pageSize
  await navigateList('push')
}

async function openEntry(row) {
  await navigateConsoleModuleRoute(router, 'catalog', {
    path: `/entries/${row.id}`,
    query: buildEntryListQuery(filters)
  })
}

async function openGovernanceTasks() {
  await navigateConsoleModuleRoute(router, 'catalog', { path: '/governance/tasks' })
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

watch(() => route.query, async query => {
  syncFilters(query)
  const canonical = buildEntryListQuery(filters)
  if (!isCanonicalEntryListQuery(query, canonical)) {
    await navigateList('replace')
    return
  }
  await Promise.all([loadEntries(), loadFacets()])
}, { immediate: true })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
.page-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.header-actions { display: flex; gap: 8px; }
.view-switch { margin-bottom: 16px; }
.facet-alert { margin-bottom: 16px; }
.filter-card { margin-bottom: 16px; }
.filter-card :deep(.el-form-item) { margin-bottom: 12px; }
.filter-card :deep(.el-select), .filter-card :deep(.el-input) { width: 210px; }
.table-card :deep(.el-table__row) { cursor: pointer; }
.entry-link { color: var(--el-color-primary); font-weight: 600; }
.pagination-row { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 900px) {
  .page-header { flex-direction: column; }
  .header-actions { flex-wrap: wrap; }
  .filter-card :deep(.el-form--inline .el-form-item) { display: flex; margin: 0 0 12px; width: 100%; }
  .filter-card :deep(.el-select), .filter-card :deep(.el-input) { width: 100%; }
}
</style>
