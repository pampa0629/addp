<template>
  <div class="page-container">
    <div class="page-header">
      <el-button text :icon="ArrowLeft" @click="goBack">{{ t('catalog.collections.back') }}</el-button>
      <div class="header-actions">
        <el-button v-if="canUpdate && collection" type="primary" :icon="Edit" @click="openEdit">{{ t('catalog.collections.edit') }}</el-button>
        <el-button v-if="canUpdate && collection" type="danger" :icon="Delete" @click="removeCollection">{{ t('catalog.collections.delete') }}</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="loadCollection">{{ t('catalog.common.refresh') }}</el-button>
      </div>
    </div>

    <el-skeleton v-if="loading && !collection" :rows="6" animated />
    <el-result v-else-if="error" icon="error" :title="t('catalog.collections.loadFailed')" :sub-title="error">
      <template #extra><el-button type="primary" @click="loadCollection">{{ t('catalog.common.retry') }}</el-button></template>
    </el-result>
    <template v-else-if="collection">
      <el-card shadow="never" class="summary-card">
        <h1>{{ collection.name }}</h1>
        <p v-if="collection.description">{{ collection.description }}</p>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('catalog.collections.projectGroup')">#{{ collection.project_group_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.entry.version')">{{ collection.version }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.collections.createdBy')">#{{ collection.created_by }}</el-descriptions-item>
          <el-descriptions-item :label="t('catalog.entries.updatedAt')">{{ formatDate(collection.updated_at) }}</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <el-card shadow="never">
        <template #header><strong>{{ t('catalog.collections.entries') }} ({{ collection.entries?.length || 0 }})</strong></template>
        <el-alert type="info" :closable="false" show-icon :title="t('catalog.collections.visibilityHint')" class="visibility-hint" />
        <el-table :data="collection.entries || []" @row-click="openEntry">
          <el-table-column prop="display_name" :label="t('catalog.entries.name')" min-width="240"><template #default="{ row }"><span class="entry-link">{{ row.display_name || t('catalog.entries.unnamed') }}</span></template></el-table-column>
          <el-table-column prop="source_status" :label="t('catalog.entries.sourceStatus')" width="130"><template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.source', row.source_status) }}</template></el-table-column>
          <el-table-column prop="governance_status" :label="t('catalog.entries.governanceStatus')" width="150"><template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.governance', row.governance_status) }}</template></el-table-column>
          <el-table-column prop="updated_at" :label="t('catalog.entries.updatedAt')" min-width="180"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
        </el-table>
        <el-empty v-if="!collection.entries?.length" :description="t('catalog.collections.noEntries')" />
      </el-card>
    </template>

    <el-dialog v-model="editVisible" :title="t('catalog.collections.edit')" width="680px" :close-on-click-modal="false">
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('catalog.collections.versionConflict')" class="edit-alert" />
      <el-form label-position="top">
        <el-form-item :label="t('catalog.collections.name')" required><el-input v-model="editForm.name" maxlength="200" show-word-limit /></el-form-item>
        <el-form-item :label="t('catalog.collections.collectionDescription')"><el-input v-model="editForm.description" type="textarea" :rows="3" maxlength="2000" show-word-limit /></el-form-item>
        <el-form-item :label="t('catalog.collections.entries')">
          <el-select v-model="editForm.entry_ids" multiple filterable remote reserve-keyword :remote-method="searchEntries" :loading="entrySearching" style="width: 100%" :placeholder="t('catalog.collections.searchEntries')">
            <el-option v-for="entry in entryOptions" :key="entry.id" :label="entry.display_name || entry.id" :value="entry.id" />
          </el-select>
        </el-form-item>
        <p class="form-hint">{{ t('catalog.collections.replaceHint') }}</p>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">{{ t('catalog.edit.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="!editForm.name.trim()" @click="saveCollection">{{ t('catalog.edit.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Delete, Edit, Refresh } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute, useConsolePageDescriptor } from '@common-ui'
import { deleteCollection, getCollection, listEntries, updateCollection } from '../api/catalog'
import { useAuthStore } from '../store/auth'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import { canAccessProjectGroup } from '../utils/projectGroupScope'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const collection = ref(null)
const loading = ref(false)
const error = ref('')
const editVisible = ref(false)
const saving = ref(false)
const versionConflict = ref(false)
const entrySearching = ref(false)
const entryOptions = ref([])
const editForm = reactive({ name: '', description: '', entry_ids: [] })
let requestVersion = 0
let searchVersion = 0
const canUpdate = computed(() => Boolean(collection.value) && canAccessProjectGroup(authStore.authContext, 'catalog.collection.update', collection.value.project_group_id))

useConsolePageDescriptor(router, 'catalog', {
  title: computed(() => t('catalog.collections.recentVisitTitle')),
  subject: computed(() => collection.value?.name || ''),
  ready: computed(() => Boolean(collection.value))
})

async function loadCollection() {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const response = await getCollection(String(route.params.id || ''))
    if (version === requestVersion) collection.value = response
  } catch (requestError) {
    if (version === requestVersion) {
      collection.value = null
      error.value = requestError?.response?.data?.error || t('catalog.collections.loadFailed')
    }
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

function openEdit() {
  versionConflict.value = false
  Object.assign(editForm, {
    name: collection.value.name,
    description: collection.value.description || '',
    entry_ids: (collection.value.entries || []).map(entry => entry.id)
  })
  entryOptions.value = [...(collection.value.entries || [])]
  editVisible.value = true
}

async function searchEntries(search) {
  const version = ++searchVersion
  entrySearching.value = true
  try {
    const response = await listEntries({ search: String(search || '').trim(), page: 1, page_size: 50 })
    if (version !== searchVersion) return
    const selected = new Map((collection.value?.entries || []).map(entry => [entry.id, entry]))
    for (const entry of response.data || []) selected.set(entry.id, entry)
    entryOptions.value = [...selected.values()]
  } catch (requestError) {
    if (version === searchVersion) ElMessage.error(requestError?.response?.data?.error || t('catalog.entries.loadFailed'))
  } finally {
    if (version === searchVersion) entrySearching.value = false
  }
}

async function saveCollection() {
  saving.value = true
  versionConflict.value = false
  try {
    collection.value = await updateCollection(collection.value.id, {
      version: collection.value.version,
      name: editForm.name.trim(),
      description: editForm.description.trim(),
      entry_ids: editForm.entry_ids
    })
    editVisible.value = false
    ElMessage.success(t('catalog.collections.saved'))
  } catch (requestError) {
    if (requestError?.response?.status === 409 && requestError?.response?.data?.error_code === 'catalog_collection_version_conflict') {
      versionConflict.value = true
    }
    ElMessage.error(requestError?.response?.data?.error || t('catalog.collections.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function removeCollection() {
  try {
    await ElMessageBox.confirm(t('catalog.collections.deleteHint'), t('catalog.collections.delete'), { type: 'warning' })
    await deleteCollection(collection.value.id, collection.value.version)
    ElMessage.success(t('catalog.collections.deleted'))
    await goBack()
  } catch (requestError) {
    if (requestError === 'cancel' || requestError === 'close') return
    ElMessage.error(requestError?.response?.data?.error || t('catalog.collections.deleteFailed'))
  }
}

async function openEntry(row) {
  await navigateConsoleModuleRoute(router, 'catalog', { path: `/entries/${row.id}` })
}

async function goBack() {
  await navigateConsoleModuleRoute(router, 'catalog', { path: '/collections' }, { history: 'replace' })
}

function formatDate(value) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

watch(() => route.params.id, loadCollection, { immediate: true })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header, .header-actions { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
.header-actions { margin-bottom: 0; }
.summary-card { margin-bottom: 16px; }
.summary-card h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.summary-card p, .form-hint { color: var(--addp-text-secondary); }
.visibility-hint, .edit-alert { margin-bottom: 16px; }
.entry-link { color: var(--el-color-primary); font-weight: 600; }
.form-hint { margin: 0; font-size: 13px; }
@media (max-width: 760px) { .page-header { flex-direction: column; } .header-actions { flex-wrap: wrap; } }
</style>
