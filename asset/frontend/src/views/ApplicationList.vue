<template>
  <div class="application-list">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- ===== 申请与授权 ===== -->
      <el-tab-pane label="申请与授权" name="applications">
        <div v-loading="loading">
          <div class="toolbar">
            <el-radio-group v-model="displayStatus" @change="handleFilterChange">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="pending">待审批</el-radio-button>
              <el-radio-button value="authorized">已授权</el-radio-button>
              <el-radio-button value="expired">已过期</el-radio-button>
              <el-radio-button value="revoked">已撤销</el-radio-button>
              <el-radio-button value="rejected">已驳回</el-radio-button>
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
            <el-table-column label="资产名称" min-width="160">
              <template #default="{ row }">
                <span class="asset-name">{{ row.asset_name || `资产 #${row.asset_id}` }}</span>
              </template>
            </el-table-column>
            <el-table-column label="申请人 ID" width="100" prop="applicant_id" />
            <el-table-column label="申请理由" min-width="180" show-overflow-tooltip prop="reason" />
            <el-table-column label="时长（天）" width="100" prop="duration_day" align="center" />
            <el-table-column label="提交时间" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="到期时间" width="160">
              <template #default="{ row }">
                <span v-if="row.auth_expires_at" :class="{ 'expired-text': isExpired(row.auth_expires_at) }">
                  {{ formatDate(row.auth_expires_at) }}
                </span>
                <span v-else style="color: var(--el-text-color-placeholder)">-</span>
              </template>
            </el-table-column>
            <el-table-column label="审批备注" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">
                <span>{{ row.review_note || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="DISPLAY_STATUS_CONFIG[deriveDisplayStatus(row)]?.type" size="small">
                  {{ DISPLAY_STATUS_CONFIG[deriveDisplayStatus(row)]?.label }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="140" align="center" fixed="right">
              <template #default="{ row }">
                <template v-if="deriveDisplayStatus(row) === 'pending'">
                  <el-button type="primary" size="small" text @click="openApproveDialog(row)">通过</el-button>
                  <el-button type="danger" size="small" text @click="openRejectDialog(row)">驳回</el-button>
                </template>
                <el-button
                  v-else-if="deriveDisplayStatus(row) === 'authorized'"
                  type="danger"
                  size="small"
                  text
                  @click="revokeByApplication(row)"
                >撤销</el-button>
                <span v-else style="font-size: 12px; color: var(--el-text-color-placeholder)">-</span>
              </template>
            </el-table-column>
          </el-table>

          <div class="batch-bar" v-if="selectedRows.length > 0">
            <span class="selected-count">已选 {{ selectedRows.length }} 条</span>
            <el-button type="primary" size="small" @click="batchApprove">批量通过</el-button>
            <el-button type="danger" size="small" plain @click="openBatchRejectDialog">批量驳回</el-button>
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
          问题反馈
          <el-badge v-if="unhandledCount > 0" :value="unhandledCount" type="danger" style="margin-left: 4px" />
        </template>
        <div v-loading="feedbackLoading">
          <div class="toolbar">
            <el-radio-group v-model="feedbackFilter" @change="fetchFeedbacks">
              <el-radio-button value="">全部反馈</el-radio-button>
              <el-radio-button value="unhandled">待处理</el-radio-button>
              <el-radio-button value="handled">已处理</el-radio-button>
            </el-radio-group>
          </div>

          <el-table :data="feedbacks" border style="width: 100%">
            <el-table-column label="资产名称" min-width="160">
              <template #default="{ row }">
                <span class="asset-name">{{ row.asset_name || `资产 #${row.asset_id}` }}</span>
              </template>
            </el-table-column>
            <el-table-column label="用户" width="120" prop="user_name" />
            <el-table-column label="评分" width="140" align="center">
              <template #default="{ row }">
                <el-rate :model-value="row.score" disabled allow-half size="small" />
              </template>
            </el-table-column>
            <el-table-column label="问题标签" min-width="200">
              <template #default="{ row }">
                <el-tag v-for="tag in row.tags" :key="tag" size="small" type="warning" style="margin-right: 4px">{{ tag }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="评价内容" min-width="200" show-overflow-tooltip prop="comment" />
            <el-table-column label="提交时间" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="row.is_handled ? 'success' : 'warning'" size="small">
                  {{ row.is_handled ? '已处理' : '待处理' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="110" align="center" fixed="right">
              <template #default="{ row }">
                <el-button
                  :type="row.is_handled ? 'info' : 'primary'"
                  size="small"
                  text
                  @click="toggleHandled(row)"
                >{{ row.is_handled ? '撤回' : '标记已处理' }}</el-button>
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
    <el-dialog v-model="approveDialogVisible" title="审批通过" width="420px">
      <el-form label-width="80px">
        <el-form-item label="备注">
          <el-input
            v-model="approveNote"
            type="textarea"
            :rows="3"
            placeholder="可填写备注（选填）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="approveDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="confirmApprove">确认通过</el-button>
      </template>
    </el-dialog>

    <!-- 审批驳回对话框 -->
    <el-dialog v-model="rejectDialogVisible" title="审批驳回" width="420px">
      <el-form ref="rejectFormRef" :model="rejectForm" :rules="rejectRules" label-width="80px">
        <el-form-item label="驳回原因" prop="reason">
          <el-input
            v-model="rejectForm.reason"
            type="textarea"
            :rows="3"
            placeholder="请填写驳回原因（必填）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectDialogVisible = false">取消</el-button>
        <el-button type="danger" :loading="submitting" @click="confirmReject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { formatDate } from '@common-ui'
import { applicationAPI, ratingAPI } from '../api/asset'

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
const rejectRules = {
  reason: [{ required: true, message: '驳回原因不能为空', trigger: 'blur' }]
}

const DISPLAY_STATUS_CONFIG = {
  pending:    { label: '待审批', type: 'warning' },
  authorized: { label: '已授权', type: 'success' },
  expired:    { label: '已过期', type: 'info' },
  revoked:    { label: '已撤销', type: 'info' },
  rejected:   { label: '已驳回', type: 'danger' },
}

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
    ElMessage.error('获取申请列表失败')
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
    ElMessage.success('审批通过')
    approveDialogVisible.value = false
    fetchApplications()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
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
      ElMessage.success('已驳回')
      rejectDialogVisible.value = false
      fetchApplications()
    } catch (err) {
      ElMessage.error(err.message || '操作失败')
    } finally {
      submitting.value = false
    }
  })
}

async function revokeByApplication(row) {
  try {
    await ElMessageBox.confirm(
      `确认撤销"${row.asset_name || `资产 #${row.asset_id}`}"的授权？`,
      '撤销授权',
      { type: 'warning', confirmButtonText: '确认撤销', confirmButtonClass: 'el-button--danger' }
    )
  } catch { return }
  try {
    await applicationAPI.revokeAuth(row.id)
    ElMessage.success('授权已撤销')
    fetchApplications()
  } catch (err) {
    ElMessage.error(err.message || '撤销失败')
  }
}

async function batchApprove() {
  const pendingRows = selectedRows.value.filter(r => r.status === 'pending')
  if (pendingRows.length === 0) { ElMessage.warning('所选记录中无待审批申请'); return }
  try {
    await ElMessageBox.confirm(`确认批量通过 ${pendingRows.length} 条申请？`, '批量通过', { type: 'warning' })
  } catch { return }
  submitting.value = true
  let successCount = 0
  for (const row of pendingRows) {
    try { await applicationAPI.approve(row.id, { note: '' }); successCount++ } catch { /* 忽略单条失败 */ }
  }
  submitting.value = false
  ElMessage.success(`已通过 ${successCount} 条`)
  fetchApplications()
}

function openBatchRejectDialog() {
  const pendingRows = selectedRows.value.filter(r => r.status === 'pending')
  if (pendingRows.length === 0) { ElMessage.warning('所选记录中无待审批申请'); return }
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
    ElMessage.error('获取问题反馈失败')
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
    ElMessage.success(row.is_handled ? '已标记为已处理' : '已撤回处理标记')
    fetchUnhandledCount()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
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
