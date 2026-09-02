<template>
  <div
    class="page-container"
    data-testid="catalog-entry-list"
    :data-load-state="loading || facetsLoading ? 'loading' : 'loaded'"
  >
    <div class="page-header">
      <div>
        <h1>{{ t('catalog.entries.title') }}</h1>
        <p>{{ t('catalog.entries.description') }}</p>
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

    <section
      class="view-scope"
      data-testid="catalog-view-scope"
      :data-active-view="filters.view"
      :data-result-total="loading ? undefined : result.total"
      :aria-label="t('catalog.entries.view.label')"
    >
      <div class="view-scope-control">
        <span class="view-scope-label">{{ t('catalog.entries.view.label') }}</span>
        <el-radio-group v-if="canViewInventory" v-model="filters.view" @change="changeView">
          <el-radio-button value="governance">{{ t('catalog.entries.view.governance') }}</el-radio-button>
          <el-radio-button value="inventory">{{ t('catalog.entries.view.inventory') }}</el-radio-button>
        </el-radio-group>
        <strong v-else class="view-scope-current">{{ t('catalog.entries.view.governance') }}</strong>
        <span class="view-scope-total">
          {{ loading ? t('catalog.entries.view.counting') : t('catalog.entries.view.resultCount', { count: result.total }) }}
        </span>
      </div>
      <p class="view-scope-description">{{ pageDescription }}</p>
    </section>

    <el-alert
      v-if="coverageGapActive"
      class="coverage-gap-alert"
      data-testid="catalog-coverage-gap-alert"
      type="info"
      :closable="false"
      show-icon
    >
      <template #title>
        <div class="coverage-gap-title">
          <span>{{ coverageGapTitle }}</span>
          <el-button link type="primary" @click="clearCoverageGap">{{ t('catalog.entries.exitCoverageGap') }}</el-button>
        </div>
      </template>
    </el-alert>

    <el-alert
      v-if="unavailableFacetLabels.length > 0"
      class="facet-alert"
      type="warning"
      :closable="false"
      :title="t('catalog.entries.facetUnavailable', { facets: formattedUnavailableFacets })"
    />

    <el-card v-if="!coverageGapActive" v-loading="facetsLoading" shadow="never" class="navigation-card" data-testid="catalog-entry-navigation">
      <template #header>
        <div class="navigation-header">
          <strong>{{ t('catalog.entries.navigation.title') }}</strong>
          <span>{{ t('catalog.entries.navigation.description') }}</span>
        </div>
      </template>
      <nav class="navigation-grid" :aria-label="t('catalog.entries.navigation.title')">
        <el-form-item :label="t('catalog.entries.navigation.primaryDomain')">
          <el-select
            :model-value="filters.primary_domain_id"
            data-testid="catalog-domain-navigation"
            clearable
            filterable
            :placeholder="t('catalog.entries.navigation.allDomains')"
            @change="changeDomainNavigation"
          >
            <el-option
              v-if="filters.view === 'inventory'"
              value="__unclassified__"
              :label="`${t('catalog.entries.navigation.unclassifiedDomain')} · ${t('catalog.entries.navigation.governanceGap')}`"
            />
            <el-option v-for="option in domainOptions" :key="option.id" :value="String(option.id)" :label="facetOptionLabel(option)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.navigation.accountableDepartment')">
          <el-select
            :model-value="filters.accountable_department_id"
            data-testid="catalog-department-navigation"
            clearable
            filterable
            :placeholder="t('catalog.entries.navigation.allDepartments')"
            @change="changeDepartmentNavigation"
          >
            <el-option
              v-if="filters.view === 'inventory'"
              value="__unassigned__"
              :label="`${t('catalog.entries.navigation.unassignedDepartment')} · ${t('catalog.entries.navigation.governanceGap')}`"
            />
            <el-option v-for="option in departmentOptions" :key="option.id" :value="String(option.id)" :label="facetOptionLabel(option)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.entries.navigation.entryType')">
          <el-select
            :model-value="filters.entry_type"
            data-testid="catalog-entry-type-navigation"
            clearable
            filterable
            :placeholder="t('catalog.entries.navigation.allEntryTypes')"
            @change="value => selectNavigation('entry_type', value || '')"
          >
            <el-option
              v-for="option in entryTypeOptions"
              :key="option.entry_type"
              :value="option.entry_type"
              :label="`${entryTypeLabel(option.entry_type)} (${option.count || 0})`"
            />
          </el-select>
        </el-form-item>
      </nav>
      <div v-if="currentScope.length" class="current-scope">
        <span>{{ t('catalog.entries.navigation.currentScope') }}</span>
        <el-tag v-for="item in currentScope" :key="item.dimension" closable @close="selectNavigation(item.dimension, '')">
          {{ item.label }}
        </el-tag>
      </div>
    </el-card>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filters" @submit.prevent="applyFilters">
        <el-form-item :label="t('catalog.entries.search')">
          <el-input v-model="filters.search" clearable :disabled="coverageGapActive" :placeholder="t('catalog.entries.searchPlaceholder')" @keyup.enter="applyFilters" />
        </el-form-item>
        <el-form-item>
          <el-button data-testid="catalog-advanced-filters-toggle" text type="primary" @click="advancedFiltersOpen = !advancedFiltersOpen">
            {{ t(`catalog.entries.${advancedFiltersOpen ? 'hideAdvancedFilters' : 'advancedFilters'}`) }}
          </el-button>
        </el-form-item>
        <template v-if="advancedFiltersOpen">
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
        <el-form-item :label="t('catalog.entries.sourceEngine')">
          <el-select
            v-model="filters.source_engine_id"
            data-testid="catalog-engine-filter"
            clearable
            filterable
            :loading="facetsLoading"
            :disabled="facets.source_engines.status === 'unavailable'"
            :placeholder="t('catalog.entries.selectEngine')"
          >
            <el-option v-for="option in engineOptions" :key="option.id" :label="engineOptionLabel(option)" :value="option.id" />
          </el-select>
        </el-form-item>
        </template>
        <el-form-item>
          <el-button type="primary" :icon="Search" @click="applyFilters">{{ t('catalog.common.search') }}</el-button>
          <el-button @click="resetFilters">{{ t('catalog.common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <div v-if="canBatchGovernance" class="batch-toolbar" data-testid="catalog-batch-governance-toolbar">
        <span>{{ t('catalog.entries.batchGovernance.selected', { count: selectedEntries.length }) }}</span>
        <div class="batch-toolbar-actions">
          <el-button :disabled="selectedEntries.length === 0" type="primary" data-testid="catalog-batch-governance-open" @click="openBatchGovernance">
            {{ t('catalog.entries.batchGovernance.action') }}
          </el-button>
          <el-button :disabled="selectedEntries.length === 0" @click="clearBatchSelection">
            {{ t('catalog.entries.batchGovernance.clear') }}
          </el-button>
        </div>
      </div>
      <el-table ref="entryTable" v-loading="loading" :data="result.data" row-key="id" @selection-change="handleSelectionChange" @row-click="openEntry">
        <el-table-column v-if="canBatchGovernance" type="selection" width="48" />
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

    <el-dialog
      v-model="batchDialogVisible"
      class="addp-dialog"
      data-testid="catalog-batch-governance-dialog"
      :title="t('catalog.entries.batchGovernance.title')"
      width="min(560px, 92vw)"
      :close-on-click-modal="false"
    >
      <p class="batch-dialog-description">
        {{ t('catalog.entries.batchGovernance.description', { count: selectedEntries.length }) }}
      </p>
      <el-form label-position="top" :model="batchForm">
        <el-form-item :label="t('catalog.entries.batchGovernance.operation')">
          <el-select
            v-model="batchForm.operation"
            data-testid="catalog-batch-governance-operation"
            :placeholder="t('catalog.entries.batchGovernance.operationPlaceholder')"
            style="width: 100%"
            @change="changeBatchOperation"
          >
            <el-option :label="t('catalog.entries.batchGovernance.assignPrimaryDomain')" value="assign_primary_domain" />
            <el-option :label="t('catalog.entries.batchGovernance.assignAccountableDepartment')" value="assign_accountable_department" />
          </el-select>
        </el-form-item>
        <el-alert
          v-if="batchForm.operation === 'assign_primary_domain' && unsupportedBatchEntries.length > 0"
          type="warning"
          show-icon
          :closable="false"
          :title="t('catalog.entries.batchGovernance.unsupportedOwnerManaged', { count: unsupportedBatchEntries.length })"
        />
        <el-form-item :label="batchTargetLabel" class="batch-target-field">
          <el-select
            v-model="batchForm.reference_id"
            data-testid="catalog-batch-governance-target"
            filterable
            remote
            reserve-keyword
            :disabled="!batchForm.operation"
            :loading="batchCandidateLoading"
            :remote-method="searchBatchCandidates"
            :placeholder="t('catalog.entries.batchGovernance.targetPlaceholder')"
            style="width: 100%"
            @visible-change="visible => visible && searchBatchCandidates('')"
          >
            <el-option v-for="option in batchCandidateOptions" :key="option.id" :label="candidateLabel(option)" :value="option.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="batchDialogVisible = false">{{ t('catalog.entries.batchGovernance.cancel') }}</el-button>
        <el-button
          type="primary"
          data-testid="catalog-batch-governance-submit"
          :loading="batchSubmitting"
          :disabled="batchSubmitDisabled"
          @click="submitBatchGovernance"
        >
          {{ t('catalog.entries.batchGovernance.confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, Search, WarningFilled } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute } from '@common-ui'
import { batchGovernance, listEntries, listEntryFacets, listReferenceCandidates } from '../api/catalog'
import { useAuthStore } from '../store/auth'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import { coverageDimensionLabel } from '../utils/governanceCoverageView'
import {
  applyEntryNavigationSelection,
  applyUnassignedDepartmentSelection,
  applyUnclassifiedDomainSelection,
  buildEntryFacetQuery
} from '../utils/entryNavigation'
import { buildEntryListQuery, isCanonicalEntryListQuery, parseEntryListRoute } from '../utils/entryRouteState'
import {
  BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT,
  BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN,
  buildBatchGovernancePayload,
  unsupportedPrimaryDomainEntries
} from '../utils/batchGovernance'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const canManageGovernance = computed(() => authStore.hasPermission('catalog.entry.update'))
const canViewInventory = computed(() => authStore.hasPermission('catalog.inventory.read'))
const canBatchGovernance = computed(() => canManageGovernance.value && canViewInventory.value && filters.view === 'inventory')
const governanceStatuses = ['discovered', 'curated', 'certified', 'deprecated']
const availableGovernanceStatuses = computed(() => filters.view === 'inventory' ? governanceStatuses : governanceStatuses.filter(status => status !== 'discovered'))
const visibilities = ['inventory', 'department', 'tenant']
const filters = reactive(parseEntryListRoute(route.query))
const result = reactive({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
const facets = reactive(emptyFacets())
const loading = ref(false)
const facetsLoading = ref(false)
const entryTable = ref(null)
const selectedEntries = ref([])
const batchDialogVisible = ref(false)
const batchSubmitting = ref(false)
const batchCandidateLoading = ref(false)
const batchCandidateOptions = ref([])
const batchForm = reactive({ operation: '', reference_id: '' })
const advancedFiltersOpen = ref(Boolean(filters.source_status || filters.governance_status || filters.visibility || filters.source_engine_id))
let requestVersion = 0
let facetRequestVersion = 0
let batchCandidateRequestVersion = 0
let loadedFacetKey = ''

const pageDescription = computed(() => t(`catalog.entries.view.${filters.view}Description`))
const coverageGapActive = computed(() => Boolean(filters.coverage_dimension && filters.coverage_state === 'missing'))
const coverageGapTitle = computed(() => t('catalog.entries.coverageGapActive', {
  dimension: coverageDimensionLabel(t, filters.coverage_dimension, 'name')
}))
const emptyDescription = computed(() => coverageGapActive.value
  ? t('catalog.entries.coverageGapEmpty', { dimension: coverageDimensionLabel(t, filters.coverage_dimension, 'name') })
  : t(`catalog.entries.view.${filters.view}Empty`))
const domainOptions = computed(() => optionsWithSelected(facets.primary_domains, filters.primary_domain_id))
const departmentOptions = computed(() => optionsWithSelected(facets.accountable_departments, filters.accountable_department_id))
const entryTypeOptions = computed(() => optionsWithSelectedEntryType(facets.entry_types, filters.entry_type))
const engineOptions = computed(() => optionsWithSelected(facets.source_engines, filters.source_engine_id))
const engineOptionsByID = computed(() => new Map(engineOptions.value.map(option => [String(option.id), option])))
const currentScope = computed(() => {
  const scope = []
  const domain = domainOptions.value.find(option => String(option.id) === filters.primary_domain_id)
  if (domain) scope.push({ dimension: 'primary_domain', label: `${t('catalog.entries.navigation.primaryDomain')}：${domain.name}` })
  const department = departmentOptions.value.find(option => String(option.id) === filters.accountable_department_id)
  if (department) scope.push({ dimension: 'accountable_department', label: `${t('catalog.entries.navigation.accountableDepartment')}：${department.name}` })
  const entryType = entryTypeOptions.value.find(option => option.entry_type === filters.entry_type)
  if (entryType) scope.push({ dimension: 'entry_type', label: `${t('catalog.entries.navigation.entryType')}：${entryTypeLabel(entryType.entry_type)}` })
  return scope
})
const unsupportedBatchEntries = computed(() => unsupportedPrimaryDomainEntries(selectedEntries.value))
const batchTargetLabel = computed(() => batchForm.operation === BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN
  ? t('catalog.entries.primaryDomain')
  : batchForm.operation === BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT
    ? t('catalog.entries.accountableDepartment')
    : t('catalog.entries.batchGovernance.target'))
const batchSubmitDisabled = computed(() => batchSubmitting.value || selectedEntries.value.length === 0 || !batchForm.operation || !batchForm.reference_id ||
  (batchForm.operation === BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN && unsupportedBatchEntries.value.length > 0))
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
  return { view: 'governance', primary_domains: emptyFacet(), accountable_departments: emptyFacet(), entry_types: [], source_engines: emptyFacet() }
}

function optionsWithSelected(facet, selectedID) {
  const options = [...(facet.options || [])]
  const normalized = String(selectedID || '')
  if (normalized && !options.some(option => String(option.id) === normalized)) {
    options.unshift({ id: normalized, name: t('catalog.entries.unresolvedReference'), count: 0 })
  }
  return options
}

function optionsWithSelectedEntryType(options, selectedEntryType) {
  const result = [...(options || [])]
  if (selectedEntryType && !result.some(option => option.entry_type === selectedEntryType)) {
    result.unshift({ entry_type: selectedEntryType, count: 0 })
  }
  return result
}

function entryTypeLabel(entryType) {
  const key = { data_item: 'dataItem', business_entity: 'businessEntity', logical_model: 'logicalModel', metric: 'metric', data_service: 'dataService', development_artifact: 'developmentArtifact', data_application: 'dataApplication' }[entryType] || 'dataItem'
  return t(`catalog.entryType.${key}`)
}

function facetOptionLabel(option) {
  return `${option.name} (${option.count || 0})`
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
  if (filters.source_status || filters.governance_status || filters.visibility || filters.source_engine_id) {
    advancedFiltersOpen.value = true
  }
}

async function loadEntries() {
  const version = ++requestVersion
  loading.value = true
  try {
    const response = await listEntries(filters)
    if (version !== requestVersion) return
    clearBatchSelection()
    Object.assign(result, response, { data: response.data || [] })
  } catch (error) {
    if (version !== requestVersion) return
    clearBatchSelection()
    Object.assign(result, { data: [], total: 0, page: filters.page, page_size: filters.page_size, total_pages: 0 })
    ElMessage.error(error?.response?.data?.error || t('catalog.entries.loadFailed'))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function loadFacets(force = false) {
  const params = buildEntryFacetQuery(filters)
  const facetKey = JSON.stringify(params)
  if (!force && loadedFacetKey === facetKey) return
  const version = ++facetRequestVersion
  facetsLoading.value = true
  try {
    const response = await listEntryFacets(params)
    if (version !== facetRequestVersion) return
    Object.assign(facets, emptyFacets(), response)
    loadedFacetKey = facetKey
  } catch {
    if (version !== facetRequestVersion) return
    Object.assign(facets, {
      view: filters.view,
      primary_domains: { status: 'unavailable', options: [] },
      accountable_departments: { status: 'unavailable', options: [] },
      entry_types: [],
      source_engines: { status: 'unavailable', options: [] }
    })
    loadedFacetKey = facetKey
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
  if (coverageGapActive.value) filters.search = ''
  filters.page = 1
  await navigateList('push')
}

async function selectNavigation(dimension, value) {
  Object.assign(filters, applyEntryNavigationSelection(filters, dimension, value))
  loadedFacetKey = ''
  await navigateList('push')
}

async function changeDomainNavigation(value) {
  if (value === '__unclassified__') {
    await selectUnclassifiedDomain()
    return
  }
  await selectNavigation('primary_domain', value || '')
}

async function changeDepartmentNavigation(value) {
  if (value === '__unassigned__') {
    await selectUnassignedDepartment()
    return
  }
  await selectNavigation('accountable_department', value || '')
}

async function selectUnclassifiedDomain() {
  Object.assign(filters, applyUnclassifiedDomainSelection(filters))
  loadedFacetKey = ''
  await navigateList('push')
}

async function selectUnassignedDepartment() {
  Object.assign(filters, applyUnassignedDepartmentSelection(filters))
  loadedFacetKey = ''
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
  if (view === 'governance') {
    filters.coverage_dimension = ''
    filters.coverage_state = ''
  }
  filters.page = 1
  loadedFacetKey = ''
  await navigateList('replace')
}

async function clearCoverageGap() {
  filters.coverage_dimension = ''
  filters.coverage_state = ''
  filters.page = 1
  await navigateList('push')
}

async function refreshPage() {
  loadedFacetKey = ''
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

async function openEntry(row, column) {
  if (column?.type === 'selection') return
  await navigateConsoleModuleRoute(router, 'catalog', {
    path: `/entries/${row.id}`,
    query: buildEntryListQuery(filters)
  })
}

function handleSelectionChange(rows) {
  selectedEntries.value = canBatchGovernance.value ? rows.slice(0, 200) : []
}

function clearBatchSelection() {
  entryTable.value?.clearSelection()
  selectedEntries.value = []
}

function openBatchGovernance() {
  if (selectedEntries.value.length === 0) return
  batchForm.operation = filters.coverage_dimension === 'primary_domain'
    ? BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN
    : filters.coverage_dimension === 'accountable_department'
      ? BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT
      : ''
  batchForm.reference_id = ''
  batchCandidateOptions.value = []
  batchDialogVisible.value = true
  if (batchForm.operation) searchBatchCandidates('')
}

function changeBatchOperation() {
  batchForm.reference_id = ''
  batchCandidateOptions.value = []
  if (batchForm.operation) searchBatchCandidates('')
}

async function searchBatchCandidates(search = '') {
  const referenceType = batchForm.operation === BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN
    ? 'domain'
    : batchForm.operation === BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT
      ? 'department'
      : ''
  if (!referenceType) return
  const version = ++batchCandidateRequestVersion
  batchCandidateLoading.value = true
  try {
    const normalizedSearch = String(search || '').trim()
    const response = await listReferenceCandidates({
      reference_type: referenceType,
      ...(normalizedSearch ? { search: normalizedSearch } : {}),
      page: 1,
      page_size: 50
    })
    if (version !== batchCandidateRequestVersion) return
    const options = new Map()
    const selected = batchCandidateOptions.value.find(option => String(option.id) === String(batchForm.reference_id))
    if (selected) options.set(String(selected.id), selected)
    for (const option of response.data || []) options.set(String(option.id), { ...option, id: String(option.id) })
    batchCandidateOptions.value = [...options.values()]
  } catch (error) {
    if (version === batchCandidateRequestVersion) {
      ElMessage.error(error?.response?.data?.error || t('catalog.entries.batchGovernance.candidateSearchFailed'))
    }
  } finally {
    if (version === batchCandidateRequestVersion) batchCandidateLoading.value = false
  }
}

function candidateLabel(option) {
  return option.code ? `${option.name} · ${option.code}` : option.name
}

async function submitBatchGovernance() {
  if (batchSubmitDisabled.value) return
  batchSubmitting.value = true
  try {
    const payload = buildBatchGovernancePayload(selectedEntries.value, batchForm.operation, batchForm.reference_id)
    await batchGovernance(payload)
    ElMessage.success(t('catalog.entries.batchGovernance.success', { count: selectedEntries.value.length }))
    batchDialogVisible.value = false
    clearBatchSelection()
    loadedFacetKey = ''
    await Promise.all([loadEntries(), loadFacets(true)])
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('catalog.entries.batchGovernance.failed'))
  } finally {
    batchSubmitting.value = false
  }
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
.view-scope { padding: 16px; margin-bottom: 16px; border: 1px solid var(--addp-border-color); border-radius: 4px; background: var(--addp-bg-secondary); }
.view-scope-control { display: flex; align-items: center; gap: 12px; }
.view-scope-label { color: var(--addp-text-primary); font-weight: 600; }
.view-scope-current { color: var(--el-color-primary); }
.view-scope-total { margin-left: auto; color: var(--addp-text-secondary); font-size: 13px; }
.view-scope-description { margin: 10px 0 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.6; }
.facet-alert, .coverage-gap-alert { margin-bottom: 16px; }
.coverage-gap-title { display: flex; align-items: center; justify-content: space-between; gap: 16px; width: 100%; }
.navigation-card { margin-bottom: 16px; }
.navigation-header { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; }
.navigation-header span { color: var(--addp-text-secondary); font-size: 12px; }
.navigation-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; }
.navigation-grid :deep(.el-form-item) { display: block; margin: 0; min-width: 0; }
.navigation-grid :deep(.el-form-item__label) { color: var(--addp-text-primary); font-weight: 600; }
.navigation-grid :deep(.el-select) { width: 100%; }
.current-scope { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 14px; color: var(--addp-text-secondary); font-size: 13px; }
.filter-card { margin-bottom: 16px; }
.filter-card :deep(.el-form-item) { margin-bottom: 12px; }
.filter-card :deep(.el-select), .filter-card :deep(.el-input) { width: 210px; }
.table-card :deep(.el-table__row) { cursor: pointer; }
.batch-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; color: var(--addp-text-secondary); }
.batch-toolbar-actions { display: flex; gap: 8px; }
.batch-dialog-description { margin: 0 0 16px; color: var(--addp-text-secondary); }
.batch-target-field { margin-top: 16px; }
.entry-link { color: var(--el-color-primary); font-weight: 600; }
.pagination-row { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 900px) {
  .page-header { flex-direction: column; }
  .header-actions { flex-wrap: wrap; }
  .view-scope-control { align-items: flex-start; flex-direction: column; }
  .view-scope-total { margin-left: 0; }
  .batch-toolbar { align-items: flex-start; flex-direction: column; }
  .batch-toolbar-actions { flex-wrap: wrap; }
  .navigation-header { align-items: flex-start; flex-direction: column; gap: 4px; }
  .navigation-grid { grid-template-columns: 1fr; }
  .filter-card :deep(.el-form--inline .el-form-item) { display: flex; margin: 0 0 12px; width: 100%; }
  .filter-card :deep(.el-select), .filter-card :deep(.el-input) { width: 100%; }
}
</style>
