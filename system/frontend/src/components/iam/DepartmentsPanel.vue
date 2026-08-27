<template>
  <div>
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" clearable :placeholder="t('system.iam.common.status')" @change="reload">
          <el-option v-for="item in ['active', 'disabled']" :key="item" :label="statusLabel(item)" :value="item" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.department.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.organization.departments.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.common.name')" min-width="220"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.name }}</strong><span>{{ row.code }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.organization.parent')" min-width="170"><template #default="{ row }">{{ departmentName(row.parent_id) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.common.updatedAt')" width="180"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="300" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.department_membership.read')" link type="primary" :icon="User" @click="openMembers(row)">{{ t('system.iam.organization.members') }}</el-button>
          <el-button v-if="row.status === 'active' && can('iam.department.update')" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="row.status === 'active' && can('iam.department.update')" link type="danger" :icon="CircleClose" @click="changeStatus(row, 'disable')">{{ t('system.iam.organization.departments.disable') }}</el-button>
          <el-button v-if="row.status === 'disabled' && can('iam.department.restore')" link type="success" :icon="RefreshLeft" @click="changeStatus(row, 'restore')">{{ t('system.iam.common.restore') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="formVisible" :title="formMode === 'create' ? t('system.iam.organization.departments.create') : t('system.iam.organization.departments.edit')" width="540px" :close-on-click-modal="false">
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('system.iam.organization.versionConflict')" />
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.common.code')"><el-input v-model="form.code" :disabled="formMode === 'edit'" /></el-form-item>
        <el-form-item :label="t('system.iam.common.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('system.iam.organization.parent')">
          <el-select v-model="form.parentId" clearable style="width: 100%" :placeholder="t('system.iam.organization.root')">
            <el-option v-for="item in parentOptions" :key="item.id" :label="`${item.name} · ${item.code}`" :value="item.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!formValid" @click="save">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>

    <OrganizationMembershipsDialog v-model="membersVisible" kind="department" :organization="selectedDepartment" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, Plus, Refresh, RefreshLeft, Search, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import OrganizationMembershipsDialog from './OrganizationMembershipsDialog.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const rows = ref([])
const allDepartments = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const formVisible = ref(false)
const formMode = ref('create')
const editing = ref(null)
const versionConflict = ref(false)
const form = reactive({ code: '', name: '', parentId: null })
const membersVisible = ref(false)
const selectedDepartment = ref(null)
const formValid = computed(() => Boolean(form.code.trim()) && Boolean(form.name.trim()))
const parentOptions = computed(() => allDepartments.value.filter(item => item.status === 'active' && item.id !== editing.value?.id))

function statusLabel(value) { return t(`system.iam.status.${value}`) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
function departmentName(id) { return id ? allDepartments.value.find(item => item.id === id)?.name || `#${id}` : t('system.iam.organization.root') }
async function loadOptions() {
  const result = await iamAPI.departments.list({ page: 1, page_size: 100 })
  allDepartments.value = result.data || []
}
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.departments.list({ page: page.value, page_size: pageSize.value, search: filters.search || undefined, status: filters.status || undefined })
    rows.value = result.data || []; total.value = result.total || 0
    await loadOptions()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
async function openCreate() {
  await loadOptions(); formMode.value = 'create'; editing.value = null; versionConflict.value = false
  Object.assign(form, { code: '', name: '', parentId: null }); formVisible.value = true
}
async function openEdit(row) {
  await loadOptions(); formMode.value = 'edit'; editing.value = row; versionConflict.value = false
  Object.assign(form, { code: row.code, name: row.name, parentId: row.parent_id }); formVisible.value = true
}
async function save() {
  submitting.value = true; versionConflict.value = false
  try {
    if (formMode.value === 'create') await iamAPI.departments.create({ code: form.code.trim(), name: form.name.trim(), parent_id: form.parentId || null })
    else await iamAPI.departments.update(editing.value.id, { name: form.name.trim(), parent_id: form.parentId || null, version: editing.value.version })
    ElMessage.success(t('system.iam.common.saved')); formVisible.value = false; await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'resource_version_conflict') versionConflict.value = true
    ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally { submitting.value = false }
}
async function changeStatus(row, action) {
  try {
    const message = action === 'disable' ? t('system.iam.organization.departments.disableHint') : t('system.iam.common.reasonPrompt')
    const { value } = await ElMessageBox.prompt(message, action === 'disable' ? t('system.iam.organization.departments.disable') : t('system.iam.common.restore'), {
      inputValidator: text => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.departments[action](row.id, row.version, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}
function openMembers(row) { selectedDepartment.value = row; membersVisible.value = true }
onMounted(load)
</script>
