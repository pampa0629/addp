<template>
  <div class="page" data-testid="data-application-list">
    <div class="page-header">
      <div>
        <h2>{{ t('workbench.dataApplications') }}</h2>
        <p>{{ t('workbench.dataApplicationsSubtitle') }}</p>
      </div>
      <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon>{{ t('workbench.createDataApplication') }}</el-button>
    </div>
    <el-card>
      <el-table v-loading="loading" :data="items">
        <el-table-column prop="name" :label="t('workbench.name')" min-width="180" />
        <el-table-column :label="t('workbench.publicationStatus')" width="140">
          <template #default="scope"><el-tag :type="statusType(scope.row.publication_status)">{{ t(`workbench.statuses.${scope.row.publication_status}`) }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="current_revision_number" :label="t('workbench.revision')" width="100" />
        <el-table-column prop="updated_at" :label="t('workbench.updatedAt')" min-width="180" />
        <el-table-column width="240" fixed="right">
          <template #default="scope">
            <el-button link type="primary" @click="router.push(`/applications/${scope.row.id}`)">{{ t('workbench.edit') }}</el-button>
            <el-button v-if="scope.row.publication_status === 'published'" link type="primary" @click="openRuntime(scope.row)">{{ t('workbench.run') }}</el-button>
            <el-button v-if="!scope.row.current_revision_number" link type="danger" @click="remove(scope.row)">{{ t('workbench.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="prev, pager, next, total" @current-change="load" />
    </el-card>

    <el-dialog v-model="createVisible" :title="t('workbench.createDataApplication')" width="620px">
      <el-form label-position="top">
        <el-form-item :label="t('workbench.name')"><el-input v-model="createForm.name" maxlength="200" /></el-form-item>
        <el-form-item :label="t('workbench.description')"><el-input v-model="createForm.description" type="textarea" maxlength="2000" /></el-form-item>
        <el-form-item :label="t('workbench.sourceViews')">
          <el-select v-model="createForm.sourceViewIds" multiple filterable class="full" :loading="viewsLoading">
            <el-option v-for="view in availableViews" :key="view.id" :label="view.name" :value="view.id" />
          </el-select>
          <div class="hint">{{ t('workbench.sourceViewsHint') }}</div>
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="createVisible = false">{{ t('workbench.cancel') }}</el-button><el-button type="primary" :loading="creating" @click="create">{{ t('workbench.create') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { listViews } from '../api/views'
import { createDataApplication, deleteDataApplication, listDataApplications } from '../api/dataApplications'
import { navigateWorkbenchRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const rawRouter = useRouter()
const router = { push: (location) => navigateWorkbenchRoute(rawRouter, location) }
const items = ref([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const createVisible = ref(false)
const creating = ref(false)
const viewsLoading = ref(false)
const availableViews = ref([])
const createForm = reactive({ name: '', description: '', sourceViewIds: [] })

function statusType(status) {
  if (status === 'published') return 'success'
  if (status === 'offline') return 'info'
  return 'warning'
}

async function load() {
  loading.value = true
  try {
    const { data } = await listDataApplications({ page: page.value, page_size: 20 })
    items.value = data.data || []
    total.value = data.total || 0
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function openCreate() {
  createForm.name = ''
  createForm.description = ''
  createForm.sourceViewIds = []
  createVisible.value = true
  viewsLoading.value = true
  try {
    const { data } = await listViews({ page: 1, page_size: 100 })
    availableViews.value = data.data || []
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    viewsLoading.value = false
  }
}

async function create() {
  if (!createForm.name.trim() || createForm.sourceViewIds.length === 0) return ElMessage.warning(t('workbench.incompleteDataApplication'))
  creating.value = true
  try {
    const { data } = await createDataApplication({
      name: createForm.name.trim(), description: createForm.description.trim(), source_view_ids: [...createForm.sourceViewIds],
    })
    createVisible.value = false
    await router.push(`/applications/${data.id}`)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
  } finally {
    creating.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(t('workbench.deleteDataApplicationConfirm'))
  await deleteDataApplication(row.id, row.version)
  ElMessage.success(t('workbench.dataApplicationDeleted'))
  await load()
}

function openRuntime(row) {
  window.open(`/data-apps/${row.id}`, '_blank', 'noopener')
}

onMounted(load)
</script>

<style scoped>
.page{display:flex;flex-direction:column;gap:16px}.page-header{display:flex;justify-content:space-between;align-items:center}.page-header h2{margin:0;color:var(--addp-text-primary)}.page-header p,.hint{margin:6px 0 0;color:var(--addp-text-secondary)}.hint{font-size:12px}.full{width:100%}.el-pagination{margin-top:16px;justify-content:flex-end}
</style>
