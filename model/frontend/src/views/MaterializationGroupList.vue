<template>
  <div class="materialization-group-list">
    <div class="page-header">
      <div>
        <h2>{{ t('model.materialization_group.title') }}</h2>
        <p>{{ t('model.materialization_group.subtitle') }}</p>
      </div>
      <el-button v-if="can('model.materialization_group.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('model.materialization_group.create') }}
      </el-button>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="load-error">
      <el-button link type="danger" @click="loadGroups">{{ t('model.common.retry') }}</el-button>
    </el-alert>

    <el-card v-else shadow="never">
      <el-table :data="groups" v-loading="loading" stripe :empty-text="t('model.materialization_group.empty')">
        <el-table-column prop="name" :label="t('model.materialization_group.name')" min-width="180" />
        <el-table-column prop="code" :label="t('model.materialization_group.code')" min-width="180" />
        <el-table-column :label="t('model.materialization_group.members')" min-width="280">
          <template #default="{ row }">
            <el-tag v-for="member in orderedMembers(row)" :key="member.logical_table_id" class="member-tag" size="small">
              {{ logicalTableLabel(member.logical_table_id) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="version" :label="t('model.materialization_group.version')" width="90" />
        <el-table-column prop="updated_at" :label="t('model.materialization_group.updatedAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('model.materialization_group.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('model.materialization_group.update')" link type="primary" @click="openEdit(row)">{{ t('model.common.edit') }}</el-button>
            <el-popconfirm
              v-if="can('model.materialization_group.delete')"
              :title="t('model.materialization_group.deleteConfirm')"
              @confirm="deleteGroup(row)"
            >
              <template #reference><el-button link type="danger">{{ t('model.common.delete') }}</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        :total="pagination.total"
        class="pagination"
        @current-change="changePage"
        @size-change="changePageSize"
      />
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      class="addp-dialog"
      :title="editingID ? t('model.materialization_group.edit') : t('model.materialization_group.create')"
      width="min(640px, calc(100vw - 32px))"
      :close-on-click-modal="!submitting"
      :close-on-press-escape="!submitting"
      @closed="clearDialogRoute"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="t('model.materialization_group.code')" prop="code">
          <el-input v-model="form.code" maxlength="100" :disabled="Boolean(editingID)" />
        </el-form-item>
        <el-form-item :label="t('model.materialization_group.name')" prop="name">
          <el-input v-model="form.name" maxlength="200" />
        </el-form-item>
        <el-form-item :label="t('model.materialization_group.description')">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item :label="t('model.materialization_group.members')" prop="logical_table_ids">
          <el-select v-model="form.logical_table_ids" multiple filterable style="width:100%" :placeholder="t('model.materialization_group.membersPlaceholder')">
            <el-option v-for="table in logicalTables" :key="table.id" :label="`${table.name} (${table.code})`" :value="table.id" />
          </el-select>
          <div class="form-help">{{ t('model.materialization_group.membersHelp') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="dialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('model.common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { logicalTableAPI, materializationGroupAPI } from '../api/model'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { navigateModelRoute } from '../utils/moduleNavigation'
import {
  buildMaterializationGroupRouteQuery,
  resolveMaterializationGroupRouteState
} from '../utils/materializationGroupRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const groups = ref([])
const logicalTables = ref([])
const loading = ref(false)
const loadError = ref('')
const dialogVisible = ref(false)
const submitting = ref(false)
const editingID = ref(null)
const formRef = ref(null)
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const form = reactive({ code: '', name: '', description: '', logical_table_ids: [], version: 0 })
let routeReady = false
let listSequence = 0

const rules = {
  code: [
    { required: true, message: t('model.materialization_group.codeRequired'), trigger: 'blur' },
    { pattern: /^[a-z][a-z0-9_]*$/, message: t('model.materialization_group.codeFormat'), trigger: 'blur' }
  ],
  name: [{ required: true, message: t('model.materialization_group.nameRequired'), trigger: 'blur' }],
  logical_table_ids: [{ type: 'array', required: true, min: 1, message: t('model.materialization_group.membersRequired'), trigger: 'change' }]
}

const tableByID = () => new Map(logicalTables.value.map(table => [table.id, table]))
const logicalTableLabel = id => {
  const table = tableByID().get(id)
  return table ? `${table.name} (${table.code})` : `#${id}`
}
const orderedMembers = group => [...(group.members || [])].sort((left, right) => left.position - right.position)
const formatTime = value => value ? new Date(value).toLocaleString() : '-'
const resetForm = () => Object.assign(form, { code: '', name: '', description: '', logical_table_ids: [], version: 0 })

const syncRoute = (mode = 'list', groupID = null, history = 'replace') => navigateModelRoute(router, {
  path: route.path,
  query: buildMaterializationGroupRouteQuery({ mode, groupID, page: pagination.page, pageSize: pagination.pageSize })
}, { history })

const loadGroups = async () => {
  const sequence = ++listSequence
  loading.value = true
  loadError.value = ''
  try {
    const response = await materializationGroupAPI.list({ page: pagination.page, page_size: pagination.pageSize })
    if (sequence !== listSequence) return
    groups.value = response?.data || []
    pagination.total = response?.total || 0
    const lastPage = Math.max(1, Math.ceil(pagination.total / pagination.pageSize))
    if (pagination.page > lastPage) {
      pagination.page = lastPage
      await syncRoute()
    }
  } catch (error) {
    if (sequence !== listSequence) return
    groups.value = []
    pagination.total = 0
    loadError.value = getModelErrorMessage(error, t, 'model.common.load_failed')
  } finally {
    if (sequence === listSequence) loading.value = false
  }
}

const loadReferences = async () => {
  try {
    logicalTables.value = await logicalTableAPI.listAll({ status: 'approved' })
  } catch (error) {
    logicalTables.value = []
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.load_failed'))
  }
}

const openCreate = () => syncRoute('create', null, 'push')
const openEdit = group => syncRoute('edit', group.id, 'push')
const clearDialogRoute = () => {
  editingID.value = null
  resetForm()
  if (routeReady && resolveMaterializationGroupRouteState(route.query).mode !== 'list') syncRoute()
}

const restoreDialog = async state => {
  if (state.mode === 'list') {
    dialogVisible.value = false
    editingID.value = null
    resetForm()
    return
  }
  if (state.mode === 'create') {
    if (!can('model.materialization_group.create')) {
      ElMessage.error(t('model.common.permission_denied'))
      await syncRoute()
      return
    }
    editingID.value = null
    resetForm()
    dialogVisible.value = true
    return
  }
  if (!can('model.materialization_group.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    await syncRoute()
    return
  }
  try {
    const group = await materializationGroupAPI.get(state.groupID)
    editingID.value = group.id
    Object.assign(form, {
      code: group.code,
      name: group.name,
      description: group.description || '',
      logical_table_ids: orderedMembers(group).map(member => member.logical_table_id),
      version: group.version
    })
    dialogVisible.value = true
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.load_failed'))
    await syncRoute()
  }
}

const submit = async () => {
  const permission = editingID.value ? 'model.materialization_group.update' : 'model.materialization_group.create'
  if (!can(permission)) return ElMessage.error(t('model.common.permission_denied'))
  try { await formRef.value.validate() } catch { return }
  submitting.value = true
  try {
    const payload = {
      code: form.code.trim(), name: form.name.trim(), description: form.description.trim(),
      logical_table_ids: [...form.logical_table_ids], version: editingID.value ? form.version : 0
    }
    if (editingID.value) await materializationGroupAPI.update(editingID.value, payload)
    else await materializationGroupAPI.create(payload)
    ElMessage.success(editingID.value ? t('model.common.update_success') : t('model.common.create_success'))
    dialogVisible.value = false
    await loadGroups()
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.op_failed'))
  } finally {
    submitting.value = false
  }
}

const deleteGroup = async group => {
  if (!can('model.materialization_group.delete')) return ElMessage.error(t('model.common.permission_denied'))
  try {
    await materializationGroupAPI.delete(group.id, group.version)
    ElMessage.success(t('model.common.delete_success'))
    await loadGroups()
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.delete_failed'))
  }
}

const changePage = async page => { pagination.page = page; await syncRoute() }
const changePageSize = async size => { pagination.pageSize = size; pagination.page = 1; await syncRoute() }

watch(() => route.query, async query => {
  const state = resolveMaterializationGroupRouteState(query)
  if (state.changed) {
    await navigateModelRoute(router, { path: route.path, query: state.query }, { history: 'replace' })
    return
  }
  const listChanged = pagination.page !== state.page || pagination.pageSize !== state.pageSize
  pagination.page = state.page
  pagination.pageSize = state.pageSize
  if (listChanged || !routeReady) await loadGroups()
  await restoreDialog(state)
  routeReady = true
}, { immediate: true })

onMounted(loadReferences)
</script>

<style scoped>
.materialization-group-list { padding: 20px; }
.page-header { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:16px; }
.page-header h2 { margin:0; font-size:18px; }
.page-header p { margin:6px 0 0; color:var(--el-text-color-secondary); }
.load-error { margin-bottom:16px; }
.member-tag { margin:2px 6px 2px 0; }
.pagination { margin-top:16px; justify-content:flex-end; }
.form-help { margin-top:6px; color:var(--el-text-color-secondary); font-size:12px; }
</style>
