<template>
  <div class="application-list">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- ===== 申请与授权 ===== -->
      <el-tab-pane :label="t('asset.application.tab')" name="applications">
        <div v-loading="loading">
          <div class="toolbar">
            <el-radio-group v-model="displayStatus" @change="handleFilterChange">
              <el-radio-button value="">{{ t('asset.application.all') }}</el-radio-button>
              <el-radio-button value="pending">{{ t('asset.application.pending') }}</el-radio-button>
              <el-radio-button value="authorized">{{ t('asset.application.authorized') }}</el-radio-button>
              <el-radio-button value="expired">{{ t('asset.application.expired') }}</el-radio-button>
              <el-radio-button value="revoked">{{ t('asset.application.revoked') }}</el-radio-button>
              <el-radio-button value="rejected">{{ t('asset.application.rejected') }}</el-radio-button>
            </el-radio-group>
          </div>

          <el-table
            :data="applications"
            @selection-change="handleSelectionChange"
            row-key="id"
            border
            style="width: 100%"
          >
            <el-table-column type="selection" width="48" />
            <el-table-column :label="t('asset.application.assetName')" min-width="160">
              <template #default="{ row }">
                <span class="asset-name">{{ row.asset_name || `Asset #${row.asset_id}` }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.applicantId')" width="100" prop="applicant_id" />
            <el-table-column :label="t('asset.application.reason')" min-width="180" show-overflow-tooltip prop="reason" />
            <el-table-column :label="t('asset.application.duration')" width="100" prop="duration_day" align="center" />
            <el-table-column :label="t('asset.application.submittedAt')" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('asset.application.expiresAt')" width="160">
              <template #default="{ row }">
                <span v-if="row.auth_expires_at" :class="{ 'expired-text': isExpired(row.auth_expires_at) }">
                  {{ formatDate(row.auth_expires_at) }}
                </span>
                <span v-else style="color: var(--el-text-color-placeholder)">-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.reviewNote')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">
                <span>{{ row.review_note || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.status')" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="DISPLAY_STATUS_CONFIG[deriveDisplayStatus(row)]?.type" size="small">
                  {{ DISPLAY_STATUS_CONFIG[deriveDisplayStatus(row)]?.label }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.actions')" width="140" align="center" fixed="right">
              <template #default="{ row }">
                <template v-if="deriveDisplayStatus(row) === 'pending'">
                  <el-button type="primary" size="small" text @click="openApproveDialog(row)">{{ t('asset.application.approve') }}</el-button>
                  <el-button type="danger" size="small" text @click="openRejectDialog(row)">{{ t('asset.application.reject') }}</el-button>
                </template>
                <el-button
                  v-else-if="deriveDisplayStatus(row) === 'authorized'"
                  type="danger"
                  size="small"
                  text
                  @click="revokeByApplication(row)"
                >{{ t('asset.application.revoke') }}</el-button>
                <span v-else style="font-size: 12px; color: var(--el-text-color-placeholder)">-</span>
              </template>
            </el-table-column>
          </el-table>

          <div class="batch-bar" v-if="selectedRows.length > 0">
            <span class="selected-count">{{ t('asset.application.selectedCount', { count: selectedRows.length }) }}</span>
            <el-button type="primary" size="small" @click="batchApprove">{{ t('asset.application.batchApprove') }}</el-button>
            <el-button type="danger" size="small" plain @click="openBatchRejectDialog">{{ t('asset.application.batchReject') }}</el-button>
          </div>

          <div class="pagination-bar">
            <el-pagination
              v-model:current-page="page"
              v-model:page-size="pageSize"
              :total="total"
              layout="total, prev, pager, next"
              @current-change="fetchApplications"
            />
          </div>
        </div>
      </el-tab-pane>

      <!-- ===== 问题反馈 ===== -->
      <el-tab-pane name="feedbacks">
        <template #label>
          {{ t('asset.application.feedbackTab') }}
          <el-badge v-if="unhandledCount > 0" :value="unhandledCount" type="danger" style="margin-left: 4px" />
        </template>
        <div v-loading="feedbackLoading">
          <div class="toolbar">
            <el-radio-group v-model="feedbackFilter" @change="fetchFeedbacks">
              <el-radio-button value="">{{ t('asset.application.allFeedback') }}</el-radio-button>
              <el-radio-button value="unhandled">{{ t('asset.application.unhandled') }}</el-radio-button>
              <el-radio-button value="handled">{{ t('asset.application.handled') }}</el-radio-button>
            </el-radio-group>
          </div>

          <el-table :data="feedbacks" border style="width: 100%">
            <el-table-column :label="t('asset.application.assetName')" min-width="160">
              <template #default="{ row }">
                <span class="asset-name">{{ row.asset_name || `Asset #${row.asset_id}` }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.user')" width="120" prop="user_name" />
            <el-table-column :label="t('asset.application.score')" width="140" align="center">
              <template #default="{ row }">
                <el-rate :model-value="row.score" disabled allow-half size="small" />
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.tags')" min-width="200">
              <template #default="{ row }">
                <el-tag v-for="tag in row.tags" :key="tag" size="small" type="warning" style="margin-right: 4px">{{ tag }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.comment')" min-width="200" show-overflow-tooltip prop="comment" />
            <el-table-column :label="t('asset.application.submittedAt')" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('asset.application.status')" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="row.is_handled ? 'success' : 'warning'" size="small">
                  {{ row.is_handled ? t('asset.application.handled') : t('asset.application.unhandled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('asset.application.actions')" width="110" align="center" fixed="right">
              <template #default="{ row }">
                <el-button
                  :type="row.is_handled ? 'info' : 'primary'"
                  size="small"
                  text
                  @click="toggleHandled(row)"
                >{{ row.is_handled ? t('asset.application.withdraw') : t('asset.application.markHandled') }}</el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="pagination-bar">
            <el-pagination
              v-model:current-page="feedbackPage"
              v-model:page-size="feedbackPageSize"
              :total="feedbackTotal"
              layout="total, prev, pager, next"
              @current-change="fetchFeedbacks"
            />
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 审批通过对话框 -->
    <el-dialog v-model="approveDialogVisible" :title="t('asset.application.approveDialogTitle')" width="420px">
      <el-form label-width="80px">
        <el-form-item :label="t('asset.application.note')">
          <el-input
            v-model="approveNote"
            type="textarea"
            :rows="3"
            :placeholder="t('asset.application.notePlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="approveDialogVisible = false">{{ t('asset.application.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="confirmApprove">{{ t('asset.application.confirmApprove') }}</el-button>
      </template>
    </el-dialog>

    <!-- 审批驳回对话框 -->
    <el-dialog v-model="rejectDialogVisible" :title="t('asset.application.rejectDialogTitle')" width="420px">
      <el-form ref="rejectFormRef" :model="rejectForm" :rules="rejectRules" label-width="80px">
        <el-form-item :label="t('asset.application.rejectReason')" prop="reason">
          <el-input
            v-model="rejectForm.reason"
            type="textarea"
            :rows="3"
            :placeholder="t('asset.application.rejectReasonPlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectDialogVisible = false">{{ t('asset.application.cancel') }}</el-button>
        <el-button type="danger" :loading="submitting" @click="confirmReject">{{ t('asset.application.confirmReject') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { formatDate } from '@common-ui'
import { applicationAPI, ratingAPI } from '../api/asset'

const { t } = useI18n()

// ===== Tab =====
const activeTab = ref('applications')

function handleTabChange(tab) {
  if (tab === 'feedbacks') fetchFeedbacks()
}

// ===== 申请与授权 =====
const loading = ref(false)
const submitting = ref(false)
const applications = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const displayStatus = ref('')
const selectedRows = ref([])

const approveDialogVisible = ref(false)
const approveNote = ref('')
const currentRow = ref(null)

const rejectDialogVisible = ref(false)
const rejectFormRef = ref(null)
const rejectForm = ref({ reason: '' })
const rejectRules = computed(() => ({
  reason: [{ required: true, message: t('asset.application.rejectReasonRequired'), trigger: 'blur' }]
}))

const DISPLAY_STATUS_CONFIG = computed(() => ({
  pending:    { label: t('asset.application.pending'), type: 'warning' },
  authorized: { label: t('asset.application.authorized'), type: 'success' },
  expired:    { label: t('asset.application.expired'), type: 'info' },
  revoked:    { label: t('asset.application.revoked'), type: 'info' },
  rejected:   { label: t('asset.application.rejected'), type: 'danger' },
}))

function deriveDisplayStatus(row) {
  if (row.status === 'pending') return 'pending'
  if (row.status === 'rejected') return 'rejected'
  if (row.status === 'approved') {
    if (row.auth_is_active === false) return 'revoked'
    if (row.auth_expires_at && new Date(row.auth_expires_at) <= new Date()) return 'expired'
    return 'authorized'
  }
  return 'pending'
}

function isExpired(dateStr) {
  return dateStr && new Date(dateStr) <= new Date()
}

function handleSelectionChange(rows) { selectedRows.value = rows }

function handleFilterChange() {
  page.value = 1
  fetchApplications()
}

async function fetchApplications() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (displayStatus.value) params.display_status = displayStatus.value
    const data = await applicationAPI.list(params)
    applications.value = data.data || []
    total.value = data.total || 0
  } catch {
    ElMessage.error(t('asset.application.fetchFailed'))
  } finally {
    loading.value = false
  }
}

function openApproveDialog(row) {
  currentRow.value = row
  approveNote.value = ''
  approveDialogVisible.value = true
}

async function confirmApprove() {
  submitting.value = true
  try {
    await applicationAPI.approve(currentRow.value.id, { note: approveNote.value })
    ElMessage.success(t('asset.application.approveSuccess'))
    approveDialogVisible.value = false
    fetchApplications()
  } catch (err) {
    ElMessage.error(err.message || t('asset.application.operationFailed'))
  } finally {
    submitting.value = false
  }
}

function openRejectDialog(row) {
  currentRow.value = row
  rejectForm.value.reason = ''
  rejectDialogVisible.value = true
}

async function confirmReject() {
  if (!rejectFormRef.value) return
  await rejectFormRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      await applicationAPI.reject(currentRow.value.id, { reason: rejectForm.value.reason })
      ElMessage.success(t('asset.application.rejectSuccess'))
      rejectDialogVisible.value = false
      fetchApplications()
    } catch (err) {
      ElMessage.error(err.message || t('asset.application.operationFailed'))
    } finally {
      submitting.value = false
    }
  })
}

async function revokeByApplication(row) {
  try {
    await ElMessageBox.confirm(
      t('asset.application.revokeConfirmMsg', { name: row.asset_name || `#${row.asset_id}` }),
      t('asset.application.revokeConfirmTitle'),
      { type: 'warning', confirmButtonText: t('asset.application.confirmRevoke'), confirmButtonClass: 'el-button--danger' }
    )
  } catch { return }
  try {
    await applicationAPI.revokeAuth(row.id)
    ElMessage.success(t('asset.application.revokeSuccess'))
    fetchApplications()
  } catch (err) {
    ElMessage.error(err.message || t('asset.application.revokeFailed'))
  }
}

async function batchApprove() {
  const pendingRows = selectedRows.value.filter(r => r.status === 'pending')
  if (pendingRows.length === 0) { ElMessage.warning(t('asset.application.noPendingSelected')); return }
  try {
    await ElMessageBox.confirm(t('asset.application.batchApproveConfirm', { count: pendingRows.length }), t('asset.application.batchApprove'), { type: 'warning' })
  } catch { return }
  submitting.value = true
  let successCount = 0
  for (const row of pendingRows) {
    try { await applicationAPI.approve(row.id, { note: '' }); successCount++ } catch { /* 忽略单条失败 */ }
  }
  submitting.value = false
  ElMessage.success(t('asset.application.batchApproveSuccess', { count: successCount }))
  fetchApplications()
}

function openBatchRejectDialog() {
  const pendingRows = selectedRows.value.filter(r => r.status === 'pending')
  if (pendingRows.length === 0) { ElMessage.warning(t('asset.application.noPendingSelected')); return }
  currentRow.value = null
  rejectForm.value.reason = ''
  rejectDialogVisible.value = true
}

// ===== 问题反馈 =====
const feedbackLoading = ref(false)
const feedbacks = ref([])
const feedbackTotal = ref(0)
const feedbackPage = ref(1)
const feedbackPageSize = ref(20)
const feedbackFilter = ref('')
const unhandledCount = ref(0)

async function fetchFeedbacks() {
  feedbackLoading.value = true
  try {
    const params = { has_feedback: true, page: feedbackPage.value, page_size: feedbackPageSize.value }
    if (feedbackFilter.value === 'unhandled') params.is_handled = false
    else if (feedbackFilter.value === 'handled') params.is_handled = true
    const data = await ratingAPI.list(params)
    feedbacks.value = data.data || []
    feedbackTotal.value = data.total || 0
  } catch {
    ElMessage.error(t('asset.application.fetchFeedbackFailed'))
  } finally {
    feedbackLoading.value = false
  }
}

async function fetchUnhandledCount() {
  try {
    const data = await ratingAPI.list({ has_feedback: true, is_handled: false, page_size: 1 })
    unhandledCount.value = data.total || 0
  } catch { /* 忽略 */ }
}

async function toggleHandled(row) {
  try {
    await ratingAPI.markHandled(row.id, !row.is_handled)
    row.is_handled = !row.is_handled
    ElMessage.success(row.is_handled ? t('asset.application.markedHandled') : t('asset.application.markedUnhandled'))
    fetchUnhandledCount()
  } catch (err) {
    ElMessage.error(err.message || t('asset.application.operationFailed'))
  }
}

onMounted(() => {
  fetchApplications()
  fetchUnhandledCount()
})
</script>

<style scoped>
.application-list {
  padding: 20px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.asset-name {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.expired-text {
  color: var(--el-color-danger);
}

.batch-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--el-color-primary-light-9);
  border-radius: 6px;
  border: 1px solid var(--el-color-primary-light-7);
  margin-top: 12px;
}

.selected-count {
  font-size: 13px;
  color: var(--el-color-primary);
  font-weight: 500;
  margin-right: 4px;
}

.pagination-bar {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>
