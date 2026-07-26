<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload">
          <el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.user.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.users.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.users.account')" min-width="180">
        <template #default="{ row }">
          <div class="iam-primary-cell"><strong>{{ row.display_name }}</strong><span>{{ row.local_account?.username || '-' }}</span></div>
        </template>
      </el-table-column>
      <el-table-column prop="primary_email" :label="t('system.iam.users.email')" min-width="210" />
      <el-table-column :label="t('system.iam.common.status')" width="120">
        <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template>
      </el-table-column>
      <el-table-column prop="authorization_version" :label="t('system.iam.users.authVersion')" width="130" />
      <el-table-column :label="t('system.iam.common.updatedAt')" width="180"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="250" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.user.update') && row.status !== 'deactivated'" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="can('iam.user.suspend') && row.status === 'active'" link type="warning" :icon="VideoPause" @click="openLifecycle(row, 'suspend')">{{ t('system.iam.common.suspend') }}</el-button>
          <el-button v-if="can('iam.user.reactivate') && row.status === 'suspended'" link type="success" :icon="RefreshLeft" @click="openLifecycle(row, 'reactivate')">{{ t('system.iam.common.reactivate') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="dialogVisible" :title="editing ? t('system.iam.users.edit') : t('system.iam.users.create')" width="580px">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="iam-form-grid">
          <el-form-item :label="t('system.iam.users.displayName')" prop="display_name"><el-input v-model="form.display_name" /></el-form-item>
          <el-form-item :label="t('system.iam.users.email')"><el-input v-model="form.primary_email" /></el-form-item>
          <el-form-item :label="t('system.iam.users.locale')"><el-select v-model="form.locale"><el-option label="简体中文" value="zh-cn" /><el-option label="English" value="en" /></el-select></el-form-item>
          <template v-if="!editing">
            <el-form-item :label="t('system.iam.users.username')" prop="username"><el-input v-model="form.username" autocomplete="off" /></el-form-item>
            <el-form-item class="iam-form-span" :label="t('system.iam.users.password')" prop="password"><el-input v-model="form.password" type="password" show-password autocomplete="new-password" /></el-form-item>
          </template>
        </div>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="submit">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="lifecycleVisible" :title="t(`system.iam.common.${lifecycle.action}`)" width="520px">
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.common.reason')" required><el-input v-model="lifecycle.reason" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="t('system.iam.users.changeRequestId')"><el-input v-model="lifecycle.change_request_id" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="lifecycleVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!lifecycle.reason.trim()" @click="submitLifecycle">{{ t('system.iam.common.confirm') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Edit, Plus, Refresh, RefreshLeft, Search, VideoPause } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const statuses = ['active', 'suspended', 'deactivated']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const dialogVisible = ref(false)
const editing = ref(null)
const formRef = ref()
const form = reactive({ display_name: '', primary_email: '', locale: 'zh-cn', username: '', password: '' })
const lifecycleVisible = ref(false)
const lifecycle = reactive({ row: null, action: 'suspend', reason: '', change_request_id: '' })
const rules = computed(() => ({
  display_name: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  username: [{ required: !editing.value, message: t('system.iam.validation.required'), trigger: 'blur' }],
  password: [{ required: !editing.value, message: t('system.iam.validation.required'), trigger: 'blur' }]
}))

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function statusType(status) { return ({ active: 'success', suspended: 'warning', deactivated: 'info' })[status] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.platformUsers.list({ page: page.value, page_size: pageSize.value, ...filters })
    rows.value = result.data || []
    total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
function resetForm() { Object.assign(form, { display_name: '', primary_email: '', locale: 'zh-cn', username: '', password: '' }) }
function openCreate() { editing.value = null; resetForm(); dialogVisible.value = true }
function openEdit(row) {
  editing.value = row
  Object.assign(form, { display_name: row.display_name, primary_email: row.primary_email || '', locale: row.locale || 'zh-cn', username: '', password: '' })
  dialogVisible.value = true
}
async function submit() {
  await formRef.value?.validate()
  submitting.value = true
  const profile = { display_name: form.display_name, primary_email: form.primary_email || null, locale: form.locale || null }
  try {
    if (editing.value) await iamAPI.platformUsers.update(editing.value.id, profile)
    else await iamAPI.platformUsers.create({ ...profile, local_account: { username: form.username, password: form.password } })
    ElMessage.success(t('system.iam.common.saved')); dialogVisible.value = false; await load()
  } catch (error) { if (error !== false) ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed')) }
  finally { submitting.value = false }
}
function openLifecycle(row, action) { Object.assign(lifecycle, { row, action, reason: '', change_request_id: '' }); lifecycleVisible.value = true }
async function submitLifecycle() {
  submitting.value = true
  try {
    await iamAPI.platformUsers[lifecycle.action](lifecycle.row.id, {
      reason: lifecycle.reason.trim(),
      change_request_id: lifecycle.change_request_id.trim() || null
    })
    ElMessage.success(t('system.iam.common.updated')); lifecycleVisible.value = false; await load()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
  finally { submitting.value = false }
}
onMounted(load)
</script>
