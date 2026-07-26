<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload"><el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" /></el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.memberships.member')" min-width="190"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.display_name }}</strong><span>{{ row.username || row.principal_id }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.principalType')" width="140"><template #default="{ row }">{{ t(`system.iam.principalType.${row.principal_type}`) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.source')" width="150"><template #default="{ row }">{{ t(`system.iam.source.${row.source_type}`) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.joinedAt')" width="180"><template #default="{ row }">{{ formatDate(row.joined_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.expiresAt')" width="180"><template #default="{ row }">{{ formatDate(row.expires_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="280" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.tenant_membership.update') && row.status !== 'ended'" link type="primary" :icon="Edit" @click="openExpiry(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="can('iam.tenant_membership.suspend') && row.status === 'active'" link type="warning" :icon="VideoPause" @click="changeStatus(row, 'suspend')">{{ t('system.iam.common.suspend') }}</el-button>
          <el-button v-if="can('iam.tenant_membership.restore') && row.status === 'suspended'" link type="success" :icon="RefreshLeft" @click="changeStatus(row, 'restore')">{{ t('system.iam.common.restore') }}</el-button>
          <el-button v-if="can('iam.tenant_membership.close') && row.status !== 'ended'" link type="danger" :icon="CircleClose" @click="changeStatus(row, 'close')">{{ t('system.iam.common.close') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="expiryVisible" :title="t('system.iam.memberships.editExpiry')" width="480px">
      <el-form label-position="top"><el-form-item :label="t('system.iam.memberships.expiresAt')"><el-date-picker v-model="expiryDate" type="datetime" clearable /></el-form-item></el-form>
      <template #footer><el-button @click="expiryVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="saveExpiry">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, Refresh, RefreshLeft, Search, VideoPause } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const statuses = ['active', 'suspended', 'ended']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const expiryVisible = ref(false)
const expiryRow = ref(null)
const expiryDate = ref(null)

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function statusType(status) { return ({ active: 'success', suspended: 'warning', ended: 'info' })[status] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.memberships.list({ page: page.value, page_size: pageSize.value, ...filters })
    rows.value = result.data || []; total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
function openExpiry(row) { expiryRow.value = row; expiryDate.value = row.expires_at ? new Date(row.expires_at) : null; expiryVisible.value = true }
async function saveExpiry() {
  submitting.value = true
  try {
    await iamAPI.memberships.update(expiryRow.value.id, expiryDate.value ? expiryDate.value.toISOString() : null)
    ElMessage.success(t('system.iam.common.saved')); expiryVisible.value = false; await load()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed')) }
  finally { submitting.value = false }
}
async function changeStatus(row, action) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t(`system.iam.common.${action}`), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'),
      type: action === 'close' ? 'warning' : 'info'
    })
    await iamAPI.memberships[action](row.id, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}
onMounted(load)
</script>
