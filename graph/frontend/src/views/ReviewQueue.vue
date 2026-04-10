<template>
  <div class="review-queue">
    <div class="page-header">
      <el-button text @click="$router.back()">← {{ t('graph.common.back') }}</el-button>
      <h2>{{ t('graph.review.title') }}</h2>
      <div class="header-stats">
        <el-tag type="warning">{{ t('graph.review.pending') }} {{ total }}</el-tag>
      </div>
    </div>

    <!-- 过滤栏 -->
    <div class="filter-bar">
      <el-tabs v-model="activeTab" @tab-change="loadItems">
        <el-tab-pane :label="t('graph.review.pendingEntities')" name="entity" />
        <el-tab-pane :label="t('graph.review.pendingRelations')" name="relation" />
        <el-tab-pane :label="t('graph.review.all')" name="" />
      </el-tabs>
      <div class="filter-actions">
        <el-select v-model="filterStatus" @change="loadItems" placeholder="状态" style="width:120px" clearable>
          <el-option :label="t('graph.review.statusPending')" value="pending" />
          <el-option :label="t('graph.review.statusApproved')" value="approved" />
          <el-option :label="t('graph.review.statusRejected')" value="rejected" />
          <el-option :label="t('graph.review.statusModified')" value="modified" />
        </el-select>
        <el-button
          v-if="selectedIds.length > 0"
          type="success"
          @click="handleBatchApprove"
        >{{ t('graph.review.batchApprove') }} ({{ selectedIds.length }})</el-button>
        <el-button
          v-if="selectedIds.length > 0"
          type="danger"
          @click="handleBatchReject"
        >{{ t('graph.review.batchReject') }} ({{ selectedIds.length }})</el-button>
      </div>
    </div>

    <el-table
      :data="items"
      v-loading="loading"
      @selection-change="handleSelectionChange"
      row-key="id"
    >
      <el-table-column type="selection" width="40" :selectable="row => row.status === 'pending'" />
      <el-table-column :label="t('graph.review.itemType')" width="80">
        <template #default="{ row }">
          <el-tag :type="row.item_type === 'entity' ? 'primary' : 'success'" size="small">
            {{ row.item_type === 'entity' ? t('graph.review.entity') : t('graph.review.relation') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.review.contentSummary')" min-width="200">
        <template #default="{ row }">
          <div class="content-summary">
            <strong>{{ getContentSummary(row) }}</strong>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.review.confidence')" width="120">
        <template #default="{ row }">
          <el-progress
            :percentage="Math.round(row.confidence * 100)"
            :status="row.confidence >= 0.6 ? '' : 'exception'"
            :stroke-width="8"
          />
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.review.sourceText')" min-width="200">
        <template #default="{ row }">
          <el-text size="small" class="source-text" :title="row.source_text">{{ truncate(row.source_text, 80) }}</el-text>
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.common.status')" width="80">
        <template #default="{ row }">
          <el-tag :type="reviewStatusType(row.status)" size="small">{{ reviewStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.common.actions')" width="200" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status === 'pending'">
            <el-button size="small" type="success" @click="handleApprove(row.id)">{{ t('graph.review.approve') }}</el-button>
            <el-button size="small" type="danger" @click="handleReject(row.id)">{{ t('graph.review.reject') }}</el-button>
            <el-button size="small" @click="openModifyDialog(row)">{{ t('graph.review.modify') }}</el-button>
          </template>
          <el-text v-else size="small" type="info">{{ t('graph.review.processed') }}</el-text>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-wrap">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="loadItems"
      />
    </div>

    <!-- 修改弹窗 -->
    <el-dialog v-model="showModifyDialog" :title="t('graph.review.modifyDialogTitle')" width="520px">
      <el-form v-if="modifyItem" label-width="130px">
        <template v-if="modifyItem.item_type === 'entity'">
          <el-form-item :label="t('graph.review.entityType')">
            <el-input v-model="modifyContent.type" />
          </el-form-item>
          <el-form-item :label="t('graph.review.uniqueKeyField')">
            <el-input v-model="modifyContent.unique_key_field" />
          </el-form-item>
          <el-form-item :label="t('graph.review.uniqueKeyValue')">
            <el-input v-model="modifyContent.unique_key_value" />
          </el-form-item>
          <el-form-item v-for="(val, key) in modifyContent.properties" :key="key" :label="key">
            <el-input v-model="modifyContent.properties[key]" />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item :label="t('graph.review.relationType')"><el-input v-model="modifyContent.type" /></el-form-item>
          <el-form-item :label="t('graph.review.sourceType')"><el-input v-model="modifyContent.source_type" /></el-form-item>
          <el-form-item :label="t('graph.review.sourceUniqueValue')"><el-input v-model="modifyContent.source_unique_value" /></el-form-item>
          <el-form-item :label="t('graph.review.targetType')"><el-input v-model="modifyContent.target_type" /></el-form-item>
          <el-form-item :label="t('graph.review.targetUniqueValue')"><el-input v-model="modifyContent.target_unique_value" /></el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="showModifyDialog = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleModify">{{ t('graph.review.confirmWrite') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { buildAPI } from '../api/graphBuild'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const route = useRoute()
const graphId = route.params.id

const items = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = 20
const activeTab = ref('entity')
const filterStatus = ref('pending')
const selectedIds = ref([])

const showModifyDialog = ref(false)
const modifyItem = ref(null)
const modifyContent = ref({})
const saving = ref(false)

async function loadItems() {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize,
      item_type: activeTab.value || undefined,
      status: filterStatus.value || undefined
    }
    const res = await buildAPI.listReviewItems(graphId, params)
    items.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    ElMessage.error(t('graph.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleSelectionChange(rows) {
  selectedIds.value = rows.map(r => r.id)
}

async function handleApprove(itemId) {
  try {
    await buildAPI.approveItem(graphId, itemId)
    ElMessage.success(t('graph.review.writtenToNeo4j'))
    await loadItems()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.review.operationFailed'))
  }
}

async function handleReject(itemId) {
  try {
    await buildAPI.rejectItem(graphId, itemId)
    ElMessage.success(t('graph.review.rejected'))
    await loadItems()
  } catch (e) {
    ElMessage.error(t('graph.review.operationFailed'))
  }
}

async function handleBatchApprove() {
  try {
    await buildAPI.batchReview(graphId, selectedIds.value, 'approve')
    ElMessage.success(t('graph.review.batchApproved', { count: selectedIds.value.length }))
    selectedIds.value = []
    await loadItems()
  } catch (e) {
    ElMessage.error(t('graph.review.batchFailed'))
  }
}

async function handleBatchReject() {
  try {
    await buildAPI.batchReview(graphId, selectedIds.value, 'reject')
    ElMessage.success(t('graph.review.batchRejected', { count: selectedIds.value.length }))
    selectedIds.value = []
    await loadItems()
  } catch (e) {
    ElMessage.error(t('graph.review.batchFailed'))
  }
}

function openModifyDialog(row) {
  modifyItem.value = row
  modifyContent.value = JSON.parse(JSON.stringify(row.content || {}))
  showModifyDialog.value = true
}

async function handleModify() {
  saving.value = true
  try {
    await buildAPI.modifyItem(graphId, modifyItem.value.id, modifyContent.value)
    ElMessage.success(t('graph.review.modifiedAndWritten'))
    showModifyDialog.value = false
    await loadItems()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.review.operationFailed'))
  } finally {
    saving.value = false
  }
}

function getContentSummary(row) {
  const c = row.content || {}
  if (row.item_type === 'entity') {
    const name = c.properties?.name || c.unique_key_value || '-'
    return `[${c.type}] ${name}`
  }
  return `${c.source_unique_value || '?'} --[${c.type}]--> ${c.target_unique_value || '?'}`
}

function truncate(str, len) {
  if (!str) return '-'
  return str.length > len ? str.slice(0, len) + '...' : str
}

function reviewStatusType(s) {
  return { pending: 'warning', approved: 'success', rejected: 'danger', modified: 'primary' }[s] || 'info'
}
function reviewStatusLabel(s) {
  return {
    pending: t('graph.review.statusPending'),
    approved: t('graph.review.statusApproved'),
    rejected: t('graph.review.statusRejected'),
    modified: t('graph.review.statusModified')
  }[s] || s
}

onMounted(loadItems)
</script>

<style scoped>
.review-queue { padding: 20px; }
.page-header { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.page-header h2 { margin: 0; flex: 1; }
.filter-bar { display: flex; justify-content: space-between; align-items: flex-end; margin-bottom: 12px; }
.filter-actions { display: flex; gap: 8px; align-items: center; padding-bottom: 8px; }
.content-summary { font-size: 13px; }
.source-text { color: #888; display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.pagination-wrap { margin-top: 16px; display: flex; justify-content: flex-end; }
</style>
