<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.target_principal_id" :placeholder="t('system.iam.identityChanges.targetId')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload">
          <el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.platform_identity_change.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.identityChanges.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column prop="id" label="ID" width="90" />
      <el-table-column :label="t('system.iam.identityChanges.changeType')" min-width="190"><template #default="{ row }">{{ typeLabel(row.change_type) }}</template></el-table-column>
      <el-table-column prop="target_principal_id" :label="t('system.iam.identityChanges.targetId')" width="150" />
      <el-table-column prop="reason" :label="t('system.iam.common.reason')" min-width="220" show-overflow-tooltip />
      <el-table-column :label="t('system.iam.identityChanges.requester')" width="140"><template #default="{ row }">{{ row.requested_by_principal_id }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.identityChanges.requestedAt')" width="180"><template #default="{ row }">{{ formatDate(row.requested_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="190" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status === 'pending' && row.requested_by_principal_id !== principalId">
            <el-button v-if="can('iam.platform_identity_change.approve')" link type="success" :icon="CircleCheck" @click="openReview(row, 'approve')">{{ t('system.iam.common.approve') }}</el-button>
            <el-button v-if="can('iam.platform_identity_change.reject')" link type="danger" :icon="CircleClose" @click="openReview(row, 'reject')">{{ t('system.iam.common.reject') }}</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="createVisible" :title="t('system.iam.identityChanges.create')" width="520px">
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.identityChanges.changeType')" required>
          <el-select v-model="createForm.change_type"><el-option v-for="type in changeTypes" :key="type" :label="typeLabel(type)" :value="type" /></el-select>
        </el-form-item>
        <el-form-item :label="t('system.iam.identityChanges.targetId')" required><el-input v-model="createForm.target_principal_id" /></el-form-item>
        <el-form-item :label="t('system.iam.common.reason')" required><el-input v-model="createForm.reason" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="createVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!canCreate" @click="createRequest">{{ t('system.iam.common.submit') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="reviewVisible" :title="t(`system.iam.common.${review.action}`)" width="500px">
      <el-form label-position="top"><el-form-item :label="t('system.iam.common.reviewReason')" required><el-input v-model="review.reason" type="textarea" :rows="3" /></el-form-item></el-form>
      <template #footer><el-button @click="reviewVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button :type="review.action === 'approve' ? 'success' : 'danger'" :loading="submitting" :disabled="!review.reason.trim()" @click="submitReview">{{ t('system.iam.common.confirm') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, CircleClose, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const principalId = computed(() => authStore.authContext?.principal?.id || '')
const statuses = ['pending', 'approved', 'rejected', 'cancelled', 'applied']
const changeTypes = ['platform_identity_suspend', 'platform_identity_reactivate']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ status: '', target_principal_id: '' })
const createVisible = ref(false)
const createForm = reactive({ change_type: 'platform_identity_suspend', target_principal_id: '', reason: '' })
const reviewVisible = ref(false)
const review = reactive({ row: null, action: 'approve', reason: '' })
const canCreate = computed(() => createForm.target_principal_id.trim() && createForm.reason.trim())

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function typeLabel(type) { return t(`system.iam.identityChanges.types.${type}`) }
function statusType(status) { return ({ pending: 'warning', approved: 'success', applied: 'success', rejected: 'danger', cancelled: 'info' })[status] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value, status: filters.status || undefined, target_principal_id: filters.target_principal_id || undefined }
    const result = await iamAPI.identityChanges.list(params)
    rows.value = result.data || []; total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
function openCreate() { Object.assign(createForm, { change_type: 'platform_identity_suspend', target_principal_id: '', reason: '' }); createVisible.value = true }
async function createRequest() {
  submitting.value = true
  try {
    await iamAPI.identityChanges.create({ ...createForm, target_principal_id: createForm.target_principal_id.trim(), reason: createForm.reason.trim() })
    ElMessage.success(t('system.iam.common.submitted')); createVisible.value = false; await load()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.submitFailed')) }
  finally { submitting.value = false }
}
function openReview(row, action) { Object.assign(review, { row, action, reason: '' }); reviewVisible.value = true }
async function submitReview() {
  submitting.value = true
  try {
    await iamAPI.identityChanges[review.action](review.row.id, review.reason.trim())
    ElMessage.success(t('system.iam.common.updated')); reviewVisible.value = false; await load()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
  finally { submitting.value = false }
}
onMounted(load)
</script>
