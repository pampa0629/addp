<template>
  <div>
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" clearable :placeholder="t('system.iam.common.status')" @change="reload">
          <el-option v-for="item in ['planned', 'active', 'closed']" :key="item" :label="statusLabel(item)" :value="item" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.project_group.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.organization.projectGroups.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.common.name')" min-width="220"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.name }}</strong><span>{{ row.code }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.common.description')" min-width="220" show-overflow-tooltip prop="description" />
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.organization.projectGroups.period')" min-width="210"><template #default="{ row }">{{ period(row) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="280" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.project_group_membership.read')" link type="primary" :icon="User" @click="openMembers(row)">{{ t('system.iam.organization.members') }}</el-button>
          <el-button v-if="row.status !== 'closed' && can('iam.project_group.update')" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="row.status !== 'closed' && can('iam.project_group.close')" link type="danger" :icon="CircleClose" @click="closeGroup(row)">{{ t('system.iam.organization.projectGroups.close') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="formVisible" :title="formMode === 'create' ? t('system.iam.organization.projectGroups.create') : t('system.iam.organization.projectGroups.edit')" width="600px" :close-on-click-modal="false">
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('system.iam.organization.versionConflict')" />
      <el-form label-position="top" class="iam-form-grid">
        <el-form-item :label="t('system.iam.common.code')"><el-input v-model="form.code" :disabled="formMode === 'edit'" /></el-form-item>
        <el-form-item :label="t('system.iam.common.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item class="iam-form-span" :label="t('system.iam.common.description')"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="t('system.iam.common.status')">
          <el-select v-model="form.status" style="width: 100%"><el-option v-for="item in writableStatuses" :key="item" :label="statusLabel(item)" :value="item" /></el-select>
        </el-form-item>
        <div />
        <el-form-item :label="t('system.iam.organization.projectGroups.startsAt')"><el-date-picker v-model="form.startsAt" type="datetime" clearable style="width: 100%" /></el-form-item>
        <el-form-item :label="t('system.iam.organization.projectGroups.endsAt')"><el-date-picker v-model="form.endsAt" type="datetime" clearable style="width: 100%" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!formValid" @click="save">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>

    <OrganizationMembershipsDialog v-model="membersVisible" kind="project_group" :organization="selectedGroup" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, Plus, Refresh, Search, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import OrganizationMembershipsDialog from './OrganizationMembershipsDialog.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const rows = ref([])
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
const form = reactive({ code: '', name: '', description: '', status: 'planned', startsAt: null, endsAt: null })
const membersVisible = ref(false)
const selectedGroup = ref(null)
const writableStatuses = computed(() => formMode.value === 'edit' && editing.value?.status === 'active' ? ['active'] : ['planned', 'active'])
const formValid = computed(() => Boolean(form.code.trim()) && Boolean(form.name.trim()) && (!form.startsAt || !form.endsAt || form.endsAt > form.startsAt))

function statusLabel(value) { return t(`system.iam.status.${value}`) }
function statusType(value) { return ({ planned: 'info', active: 'success', closed: 'info' })[value] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
function period(row) { return `${formatDate(row.starts_at)} — ${formatDate(row.ends_at)}` }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.projectGroups.list({ page: page.value, page_size: pageSize.value, search: filters.search || undefined, status: filters.status || undefined })
    rows.value = result.data || []; total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
function openCreate() {
  formMode.value = 'create'; editing.value = null; versionConflict.value = false
  Object.assign(form, { code: '', name: '', description: '', status: 'planned', startsAt: null, endsAt: null }); formVisible.value = true
}
function openEdit(row) {
  formMode.value = 'edit'; editing.value = row; versionConflict.value = false
  Object.assign(form, { code: row.code, name: row.name, description: row.description || '', status: row.status, startsAt: row.starts_at ? new Date(row.starts_at) : null, endsAt: row.ends_at ? new Date(row.ends_at) : null }); formVisible.value = true
}
function requestPayload() {
  return { code: form.code.trim(), name: form.name.trim(), description: form.description.trim(), status: form.status, starts_at: form.startsAt?.toISOString() || null, ends_at: form.endsAt?.toISOString() || null }
}
async function save() {
  submitting.value = true; versionConflict.value = false
  try {
    const payload = requestPayload()
    if (formMode.value === 'create') await iamAPI.projectGroups.create(payload)
    else { delete payload.code; payload.version = editing.value.version; await iamAPI.projectGroups.update(editing.value.id, payload) }
    ElMessage.success(t('system.iam.common.saved')); formVisible.value = false; await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'resource_version_conflict') versionConflict.value = true
    ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally { submitting.value = false }
}
async function closeGroup(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.organization.projectGroups.closeHint'), t('system.iam.organization.projectGroups.close'), {
      inputValidator: text => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.projectGroups.close(row.id, row.version, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}
function openMembers(row) { selectedGroup.value = row; membersVisible.value = true }
onMounted(load)
</script>
