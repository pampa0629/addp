<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>{{ t('catalog.governanceTasks.title') }}</h1>
        <p>{{ t('catalog.governanceTasks.description') }}</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="loadTasks">
        {{ t('catalog.common.refresh') }}
      </el-button>
    </div>

    <el-alert
      type="warning"
      :closable="false"
      show-icon
      :title="t('catalog.governanceTasks.repairHint')"
      class="repair-hint"
    />

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filters" @submit.prevent="applyFilters">
        <el-form-item :label="t('catalog.governanceTasks.status')">
          <el-select v-model="filters.status">
            <el-option :label="t('catalog.governanceTasks.statusOpen')" value="open" />
            <el-option :label="t('catalog.governanceTasks.statusResolved')" value="resolved" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.governanceTasks.entry')">
          <el-select
            v-model="filters.entry_id"
            clearable filterable remote reserve-keyword
            :loading="entryOptionsLoading"
            :remote-method="searchEntryOptions"
            :placeholder="t('catalog.governanceTasks.entryPlaceholder')"
            @visible-change="visible => visible && searchEntryOptions('')"
          >
            <el-option v-for="option in entryOptions" :key="option.id" :label="option.display_name || t('catalog.entries.unnamed')" :value="option.id" />
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
        <el-table-column prop="entry_display_name" :label="t('catalog.governanceTasks.entry')" min-width="210" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="entry-link">{{ row.entry_display_name || t('catalog.entries.unnamed') }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="responsibility_role" :label="t('catalog.governanceTasks.role')" min-width="150">
          <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.edit.role', row.responsibility_role) }}</template>
        </el-table-column>
        <el-table-column :label="t('catalog.governanceTasks.subject')" min-width="190" show-overflow-tooltip>
          <template #default="{ row }">{{ subjectLabel(row) }}</template>
        </el-table-column>
        <el-table-column prop="reason" :label="t('catalog.governanceTasks.reason')" min-width="170">
          <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.governanceTasks.reasonValue', row.reason) }}</template>
        </el-table-column>
        <el-table-column prop="status" :label="t('catalog.governanceTasks.status')" width="120">
          <template #default="{ row }">
            <el-tag :type="row.status === 'open' ? 'warning' : 'success'">
              {{ row.status === 'open' ? t('catalog.governanceTasks.statusOpen') : t('catalog.governanceTasks.statusResolved') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('catalog.governanceTasks.time')" min-width="180">
          <template #default="{ row }">{{ formatDate(row.resolved_at || row.opened_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && result.data.length === 0" :description="t('catalog.governanceTasks.empty')" />
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
import { reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute } from '@common-ui'
import { listEntries, listGovernanceTasks } from '../api/catalog'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import {
  buildGovernanceEntryCandidateQuery,
  buildGovernanceTaskQuery,
  isCanonicalGovernanceTaskQuery,
  parseGovernanceTaskRoute
} from '../utils/governanceTaskRouteState'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const filters = reactive(parseGovernanceTaskRoute(route.query))
const result = reactive({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
const loading = ref(false)
const entryOptions = ref([])
const entryOptionsLoading = ref(false)
let requestVersion = 0
let entryOptionsRequestVersion = 0

if (filters.entry_id) {
  entryOptions.value = [{ id: filters.entry_id, display_name: t('catalog.edit.referenceUnavailable') }]
}

async function loadTasks() {
  const version = ++requestVersion
  loading.value = true
  try {
    const response = await listGovernanceTasks(filters)
    if (version !== requestVersion) return
    Object.assign(result, response, { data: response.data || [] })
    const selected = result.data.find(item => item.catalog_entry_id === filters.entry_id)
    if (selected) mergeEntryOption({ id: selected.catalog_entry_id, display_name: selected.entry_display_name })
  } catch (error) {
    if (version === requestVersion) ElMessage.error(error?.response?.data?.error || t('catalog.governanceTasks.loadFailed'))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function searchEntryOptions(search = '') {
  const version = ++entryOptionsRequestVersion
  entryOptionsLoading.value = true
  try {
    const response = await listEntries(buildGovernanceEntryCandidateQuery(search))
    if (version !== entryOptionsRequestVersion) return
    const selected = entryOptions.value.filter(item => item.id === filters.entry_id)
    const options = new Map(selected.map(item => [item.id, item]))
    for (const item of response.data || []) options.set(item.id, item)
    entryOptions.value = [...options.values()]
  } catch (error) {
    if (version === entryOptionsRequestVersion) ElMessage.error(error?.response?.data?.error || t('catalog.governanceTasks.entrySearchFailed'))
  } finally {
    if (version === entryOptionsRequestVersion) entryOptionsLoading.value = false
  }
}

function mergeEntryOption(option) {
  const options = new Map(entryOptions.value.map(item => [item.id, item]))
  options.set(option.id, option)
  entryOptions.value = [...options.values()]
}

async function navigateList(history = 'push') {
  await navigateConsoleModuleRoute(router, 'catalog', {
    path: '/governance/tasks',
    query: buildGovernanceTaskQuery(filters)
  }, { history })
}

async function applyFilters() {
  filters.page = 1
  await navigateList()
}

async function resetFilters() {
  Object.assign(filters, parseGovernanceTaskRoute({}))
  await navigateList()
}

async function changePage(page) {
  filters.page = page
  await navigateList()
}

async function changePageSize(pageSize) {
  filters.page = 1
  filters.page_size = pageSize
  await navigateList()
}

async function openEntry(row) {
  await navigateConsoleModuleRoute(router, 'catalog', { path: `/entries/${row.catalog_entry_id}` })
}

function subjectLabel(row) {
  const snapshot = row.observed_snapshot || {}
  const name = snapshot.name || snapshot.code
  return name || t('catalog.edit.referenceUnavailable')
}

function formatDate(value) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

watch(() => route.query, async query => {
  Object.assign(filters, parseGovernanceTaskRoute(query))
  if (filters.entry_id && !entryOptions.value.some(item => item.id === filters.entry_id)) {
    mergeEntryOption({ id: filters.entry_id, display_name: t('catalog.edit.referenceUnavailable') })
  }
  const canonical = buildGovernanceTaskQuery(filters)
  if (!isCanonicalGovernanceTaskQuery(query, canonical)) {
    await navigateList('replace')
    return
  }
  await loadTasks()
}, { immediate: true })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
.page-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.repair-hint, .filter-card { margin-bottom: 16px; }
.filter-card :deep(.el-form-item) { margin-bottom: 0; }
.filter-card :deep(.el-select) { width: 160px; }
.filter-card :deep(.el-input) { width: 330px; }
.table-card :deep(.el-table__row) { cursor: pointer; }
.entry-link { color: var(--el-color-primary); font-weight: 600; }
.pagination-row { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 900px) {
  .page-header { flex-direction: column; }
  .filter-card :deep(.el-form--inline .el-form-item) { display: flex; margin: 0 0 12px; width: 100%; }
  .filter-card :deep(.el-select), .filter-card :deep(.el-input) { width: 100%; }
}
</style>
