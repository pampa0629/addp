<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>{{ t('catalog.collections.title') }}</h1>
        <p>{{ t('catalog.collections.description') }}</p>
      </div>
      <div class="header-actions">
        <el-button v-if="canUpdate" type="primary" :icon="Plus" :disabled="updateGroupOptions.length === 0" @click="openCreate">
          {{ t('catalog.collections.create') }}
        </el-button>
        <el-button :icon="Refresh" :loading="loading" @click="loadCollections">{{ t('catalog.common.refresh') }}</el-button>
      </div>
    </div>

    <el-alert v-if="readGroupOptions.length === 0" type="info" :closable="false" show-icon :title="t('catalog.collections.noMembership')" class="membership-hint" />

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true">
        <el-form-item :label="t('catalog.collections.projectGroup')">
          <el-select v-model="filters.project_group_id" clearable :placeholder="t('catalog.common.all')" @change="changeGroup">
            <el-option v-for="group in readGroupOptions" :key="group.project_group_id" :label="groupLabel(group)" :value="group.project_group_id" />
          </el-select>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table v-loading="loading" :data="result.data" @row-click="openCollection">
        <el-table-column prop="name" :label="t('catalog.collections.name')" min-width="220"><template #default="{ row }"><span class="collection-link">{{ row.name }}</span></template></el-table-column>
        <el-table-column prop="description" :label="t('catalog.collections.collectionDescription')" min-width="260" show-overflow-tooltip />
        <el-table-column prop="project_group_id" :label="t('catalog.collections.projectGroup')" min-width="160"><template #default="{ row }">{{ projectGroupLabel(row.project_group_id) }}</template></el-table-column>
        <el-table-column prop="updated_at" :label="t('catalog.entries.updatedAt')" min-width="180"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && result.data.length === 0" :description="t('catalog.collections.empty')" />
      <div class="pagination-row">
        <el-pagination background layout="total, sizes, prev, pager, next" :total="result.total" :current-page="filters.page" :page-size="filters.page_size" :page-sizes="[20, 50, 100, 200]" @current-change="changePage" @size-change="changePageSize" />
      </div>
    </el-card>

    <el-dialog v-model="createVisible" :title="t('catalog.collections.create')" width="560px" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item :label="t('catalog.collections.projectGroup')" required>
          <el-select v-model="createForm.project_group_id" style="width: 100%">
            <el-option v-for="group in updateGroupOptions" :key="group.project_group_id" :label="groupLabel(group)" :value="group.project_group_id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.collections.name')" required><el-input v-model="createForm.name" maxlength="200" show-word-limit /></el-form-item>
        <el-form-item :label="t('catalog.collections.collectionDescription')"><el-input v-model="createForm.description" type="textarea" :rows="4" maxlength="2000" show-word-limit /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">{{ t('catalog.edit.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" :disabled="!createForm.project_group_id || !createForm.name.trim()" @click="submitCreate">{{ t('catalog.collections.create') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute } from '@common-ui'
import { createCollection, listCollections } from '../api/catalog'
import { useAuthStore } from '../store/auth'
import { buildCollectionQuery, isCanonicalCollectionQuery, parseCollectionRoute } from '../utils/collectionRouteState'
import { projectGroupsForPermission } from '../utils/projectGroupScope'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const filters = reactive(parseCollectionRoute(route.query))
const result = reactive({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
const loading = ref(false)
const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({ project_group_id: '', name: '', description: '' })
let requestVersion = 0
const canUpdate = computed(() => authStore.hasPermission('catalog.collection.update'))
const allGroupOptions = computed(() => authStore.authContext?.organization?.project_groups || [])
const readGroupOptions = computed(() => projectGroupsForPermission(authStore.authContext, 'catalog.collection.read'))
const updateGroupOptions = computed(() => projectGroupsForPermission(authStore.authContext, 'catalog.collection.update'))

function groupLabel(group) {
  return `${t('catalog.collections.projectGroup')} #${group.project_group_id} · ${t(`catalog.collections.groupRole.${group.relation_role}`)}`
}

function projectGroupLabel(id) {
  const group = allGroupOptions.value.find(item => item.project_group_id === String(id))
  return group ? groupLabel(group) : `${t('catalog.collections.projectGroup')} #${id}`
}

async function loadCollections() {
  const version = ++requestVersion
  loading.value = true
  try {
    const response = await listCollections(filters)
    if (version !== requestVersion) return
    Object.assign(result, response, { data: response.data || [] })
  } catch (error) {
    if (version === requestVersion) ElMessage.error(error?.response?.data?.error || t('catalog.collections.loadFailed'))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function navigateList(history = 'push') {
  await navigateConsoleModuleRoute(router, 'catalog', { path: '/collections', query: buildCollectionQuery(filters) }, { history })
}

async function changeGroup() { filters.page = 1; await navigateList() }
async function changePage(page) { filters.page = page; await navigateList() }
async function changePageSize(pageSize) { filters.page = 1; filters.page_size = pageSize; await navigateList() }

async function openCollection(row) {
  await navigateConsoleModuleRoute(router, 'catalog', { path: `/collections/${row.id}` })
}

function openCreate() {
  const filteredGroup = updateGroupOptions.value.some(group => group.project_group_id === filters.project_group_id) ? filters.project_group_id : ''
  Object.assign(createForm, { project_group_id: filteredGroup || updateGroupOptions.value[0]?.project_group_id || '', name: '', description: '' })
  createVisible.value = true
}

async function submitCreate() {
  creating.value = true
  try {
    const created = await createCollection({ ...createForm, name: createForm.name.trim(), description: createForm.description.trim(), entry_ids: [] })
    createVisible.value = false
    ElMessage.success(t('catalog.collections.created'))
    await navigateConsoleModuleRoute(router, 'catalog', { path: `/collections/${created.id}` })
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('catalog.collections.createFailed'))
  } finally {
    creating.value = false
  }
}

function formatDate(value) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

watch(() => route.query, async query => {
  Object.assign(filters, parseCollectionRoute(query))
  const canonical = buildCollectionQuery(filters)
  if (!isCanonicalCollectionQuery(query, canonical)) { await navigateList('replace'); return }
  await loadCollections()
}, { immediate: true })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
.page-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.header-actions { display: flex; gap: 8px; }
.membership-hint, .filter-card { margin-bottom: 16px; }
.filter-card :deep(.el-form-item) { margin-bottom: 0; }
.filter-card :deep(.el-select) { width: 300px; }
.table-card :deep(.el-table__row) { cursor: pointer; }
.collection-link { color: var(--el-color-primary); font-weight: 600; }
.pagination-row { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 760px) { .page-header { flex-direction: column; } }
</style>
