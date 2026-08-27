<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>{{ t('catalog.myCatalog.title') }}</h1>
        <p>{{ t('catalog.myCatalog.description') }}</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="loadEntries">{{ t('catalog.common.refresh') }}</el-button>
    </div>

    <el-card shadow="never" class="relation-card">
      <el-segmented v-model="filters.relation" :options="relationOptions" @change="changeRelation" />
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table v-loading="loading" :data="result.data" @row-click="openEntry">
        <el-table-column prop="display_name" :label="t('catalog.entries.name')" min-width="240" show-overflow-tooltip>
          <template #default="{ row }"><span class="entry-link">{{ row.display_name || t('catalog.entries.unnamed') }}</span></template>
        </el-table-column>
        <el-table-column prop="source_status" :label="t('catalog.entries.sourceStatus')" width="130">
          <template #default="{ row }"><el-tag :type="row.source_status === 'active' ? 'success' : 'danger'">{{ catalogStatusLabel(t, 'catalog.status.source', row.source_status) }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="governance_status" :label="t('catalog.entries.governanceStatus')" width="150">
          <template #default="{ row }"><el-tag :type="governanceTagType(row.governance_status)">{{ catalogStatusLabel(t, 'catalog.status.governance', row.governance_status) }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="visibility" :label="t('catalog.entries.visibility')" width="140">
          <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.visibility', row.visibility) }}</template>
        </el-table-column>
        <el-table-column prop="updated_at" :label="t('catalog.entries.updatedAt')" min-width="180">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && result.data.length === 0" :description="t(`catalog.myCatalog.empty.${filters.relation}`)" />
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
import { Refresh } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute } from '@common-ui'
import { listMyEntries } from '../api/catalog'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import { buildMyCatalogQuery, isCanonicalMyCatalogQuery, parseMyCatalogRoute } from '../utils/myCatalogRouteState'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const filters = reactive(parseMyCatalogRoute(route.query))
const result = reactive({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
const loading = ref(false)
let requestVersion = 0
const relationOptions = computed(() => ['responsible', 'favorite', 'following'].map(value => ({ value, label: t(`catalog.myCatalog.relation.${value}`) })))

async function loadEntries() {
  const version = ++requestVersion
  loading.value = true
  try {
    const response = await listMyEntries(filters)
    if (version !== requestVersion) return
    Object.assign(result, response, { data: response.data || [] })
  } catch (error) {
    if (version === requestVersion) ElMessage.error(error?.response?.data?.error || t('catalog.myCatalog.loadFailed'))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function navigateList(history = 'push') {
  await navigateConsoleModuleRoute(router, 'catalog', { path: '/me/entries', query: buildMyCatalogQuery(filters) }, { history })
}

async function changeRelation() {
  filters.page = 1
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
  await navigateConsoleModuleRoute(router, 'catalog', { path: `/entries/${row.id}` })
}

function governanceTagType(status) {
  return { discovered: 'info', curated: 'primary', certified: 'success', deprecated: 'warning' }[status] || 'info'
}

function formatDate(value) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

watch(() => route.query, async query => {
  Object.assign(filters, parseMyCatalogRoute(query))
  const canonical = buildMyCatalogQuery(filters)
  if (!isCanonicalMyCatalogQuery(query, canonical)) {
    await navigateList('replace')
    return
  }
  await loadEntries()
}, { immediate: true })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
.page-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.relation-card { margin-bottom: 16px; }
.table-card :deep(.el-table__row) { cursor: pointer; }
.entry-link { color: var(--el-color-primary); font-weight: 600; }
.pagination-row { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 760px) { .page-header { flex-direction: column; } }
</style>
