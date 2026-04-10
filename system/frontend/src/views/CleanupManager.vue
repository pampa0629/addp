<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.cleanup.title') }}</span>
          <div class="header-actions">
            <el-button
              type="primary"
              :icon="Search"
              :loading="scanLoading"
              @click="startScan"
            >
              {{ t('system.cleanup.scan') }}
            </el-button>
            <el-button
              :icon="Refresh"
              @click="loadHistory"
            >
              {{ t('system.cleanup.refreshHistory') }}
            </el-button>
          </div>
        </div>
      </template>

      <!-- 当前扫描/执行任务 -->
      <el-alert
        v-if="currentTask"
        :title="t('system.cleanup.taskInProgress', { action: currentTask.action === 'scan' ? t('system.cleanup.history.actionScan') : t('system.cleanup.history.actionCleanup') })"
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <div>{{ t('system.cleanup.taskId', { id: currentTask.task_id }) }}</div>
        <div>{{ t('system.cleanup.taskStatus', { status: getStatusText(currentTask.status) }) }}</div>
        <el-progress
          v-if="currentTask.progress"
          :percentage="Math.round((currentTask.progress.completed / currentTask.progress.total) * 100)"
          :status="currentTask.status === 'completed' ? 'success' : undefined"
        />
      </el-alert>

      <!-- 扫描结果 -->
      <div v-if="scanResult" class="scan-result">
        <el-descriptions :title="t('system.cleanup.scanResult.title')" :column="2" border>
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanTime')">
            {{ formatTime(scanResult.task.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanScope')">
            {{ scanResult.task.expected_modules?.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.riskLevel')">
            <el-tag :type="getRiskLevelType(scanResult.summary.risk_level)">
              {{ getRiskLevelText(scanResult.summary.risk_level) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.itemsToClean')">
            {{ t('system.cleanup.scanResult.items', { count: scanResult.summary.total_items_to_clean }) }}
          </el-descriptions-item>
        </el-descriptions>

        <!-- Meta 模块扫描详情 -->
        <div v-if="scanResult.results?.meta" class="module-result">
          <h3>{{ t('system.cleanup.meta.title') }}</h3>
          <el-row :gutter="20">
            <el-col :span="8">
              <el-statistic :title="t('system.cleanup.meta.softDeleted')" :value="scanResult.results.meta.statistics.soft_deleted?.items || 0">
                <template #suffix>{{ t('system.cleanup.meta.count') }}</template>
              </el-statistic>
              <div class="stat-detail">{{ t('system.cleanup.meta.nodes', { count: scanResult.results.meta.statistics.soft_deleted?.nodes || 0 }) }}</div>
            </el-col>
            <el-col :span="8">
              <el-statistic :title="t('system.cleanup.meta.invalidEngines')" :value="scanResult.results.meta.statistics.invalid_engines?.count || 0">
                <template #suffix>{{ t('system.cleanup.meta.engines') }}</template>
              </el-statistic>
            </el-col>
            <el-col :span="8">
              <el-statistic :title="t('system.cleanup.meta.duplicateFingerprints')" :value="scanResult.results.meta.statistics.duplicate_fingerprints?.count || 0">
                <template #suffix>{{ t('system.cleanup.meta.count') }}</template>
              </el-statistic>
            </el-col>
          </el-row>

          <!-- 无效引擎详情 -->
          <div v-if="scanResult.results.meta.statistics.invalid_engines?.details?.length > 0" class="invalid-engines">
            <h4>{{ t('system.cleanup.meta.invalidEngineDetail') }}</h4>
            <el-table :data="scanResult.results.meta.statistics.invalid_engines.details" border>
              <el-table-column prop="engine_id" :label="t('system.cleanup.meta.engineId')" width="100" />
              <el-table-column prop="engine_name" :label="t('system.cleanup.meta.engineName')" />
              <el-table-column prop="reason" :label="t('system.cleanup.meta.reason')" />
              <el-table-column prop="affected_nodes" :label="t('system.cleanup.meta.affectedNodes')" width="100" />
              <el-table-column prop="affected_items" :label="t('system.cleanup.meta.affectedItems')" width="100" />
            </el-table>
          </div>
        </div>

        <!-- 清理操作 -->
        <div class="cleanup-actions">
          <el-alert
            :title="t('system.cleanup.cleanupNote.title')"
            type="warning"
            :closable="false"
            style="margin-bottom: 15px"
          >
            <ul style="margin: 0; padding-left: 20px">
              <li><strong>{{ t('system.cleanup.cleanupNote.softDelete') }}</strong>: {{ t('system.cleanup.cleanupNote.softDeleteDesc') }}</li>
              <li><strong>{{ t('system.cleanup.cleanupNote.hardDelete') }}</strong>: {{ t('system.cleanup.cleanupNote.hardDeleteDesc') }}</li>
            </ul>
          </el-alert>

          <el-space>
            <el-button
              type="warning"
              :icon="WarningFilled"
              :loading="executeLoading"
              @click="executeCleanup('soft_delete')"
            >
              {{ t('system.cleanup.actions.softDelete') }}
            </el-button>
            <el-popconfirm
              :title="t('system.cleanup.actions.hardDeleteConfirm')"
              :confirm-button-text="t('system.cleanup.actions.confirm')"
              :cancel-button-text="t('system.cleanup.actions.cancel')"
              @confirm="executeCleanup('hard_delete')"
            >
              <template #reference>
                <el-button
                  type="danger"
                  :icon="Delete"
                  :loading="executeLoading"
                >
                  {{ t('system.cleanup.actions.hardDelete') }}
                </el-button>
              </template>
            </el-popconfirm>
          </el-space>
        </div>
      </div>

      <!-- 任务历史 -->
      <div class="task-history">
        <h3>{{ t('system.cleanup.history.title') }}</h3>
        <el-table
          :data="taskHistory"
          v-loading="historyLoading"
          border
          style="width: 100%"
        >
          <el-table-column prop="task_id" :label="t('system.cleanup.history.columns.taskId')" width="250" />
          <el-table-column :label="t('system.cleanup.history.columns.actionType')" width="100">
            <template #default="{ row }">
              <el-tag :type="row.action === 'scan' ? 'info' : 'warning'">
                {{ row.action === 'scan' ? t('system.cleanup.history.actionScan') : t('system.cleanup.history.actionCleanup') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.deleteType')" width="120">
            <template #default="{ row }">
              <el-tag v-if="row.delete_type === 'soft_delete'" type="warning">{{ t('system.cleanup.history.softDelete') }}</el-tag>
              <el-tag v-else-if="row.delete_type === 'hard_delete'" type="danger">{{ t('system.cleanup.history.hardDelete') }}</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.status')" width="100">
            <template #default="{ row }">
              {{ getStatusText(row.status) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.scope')" width="100">
            <template #default="{ row }">
              {{ row.expected_modules?.join(', ') || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.startTime')" width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.actions')" fixed="right" width="100">
            <template #default="{ row }">
              <el-button
                type="primary"
                link
                @click="viewTaskDetail(row.task_id)"
              >
                {{ t('system.cleanup.history.viewDetail') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <!-- 任务详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      :title="t('system.cleanup.detail.title')"
      width="70%"
    >
      <pre v-if="taskDetail" style="max-height: 500px; overflow: auto">{{ JSON.stringify(taskDetail, null, 2) }}</pre>
      <el-skeleton v-else :rows="10" animated />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Delete, WarningFilled } from '@element-plus/icons-vue'
import { cleanupApi } from '../api/cleanup'

const { t } = useI18n()

const scanLoading = ref(false)
const executeLoading = ref(false)
const historyLoading = ref(false)
const currentTask = ref(null)
const scanResult = ref(null)
const taskHistory = ref([])
const detailDialogVisible = ref(false)
const taskDetail = ref(null)

// 开始扫描
const startScan = async () => {
  try {
    scanLoading.value = true
    const response = await cleanupApi.createScanTask({ scope: ['meta'] })
    const taskId = response.task_id

    ElMessage.success(t('system.cleanup.msg.scanCreated'))

    // 轮询任务状态
    await pollTaskStatus(taskId)
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.scanFailed', { error: error.message || '' }))
  } finally {
    scanLoading.value = false
  }
}

// 执行清理
const executeCleanup = async (deleteType) => {
  if (!scanResult.value) {
    ElMessage.warning(t('system.cleanup.msg.noScanFirst'))
    return
  }

  try {
    executeLoading.value = true
    const response = await cleanupApi.createExecuteTask({
      based_on_scan: scanResult.value.task_id,
      delete_type: deleteType
    })

    ElMessage.success(t('system.cleanup.msg.cleanupCreated'))

    // 轮询任务状态
    await pollTaskStatus(response.task_id)
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.cleanupFailed', { error: error.message || '' }))
  } finally {
    executeLoading.value = false
  }
}

// 轮询任务状态
const pollTaskStatus = async (taskId) => {
  const maxAttempts = 20
  let attempts = 0

  const poll = async () => {
    try {
      const status = await cleanupApi.getTaskStatus(taskId)
      currentTask.value = status

      if (status.status === 'completed' || status.status === 'completed_with_errors') {
        // 扫描完成，显示结果
        if (status.action === 'scan') {
          scanResult.value = status
        }
        // 刷新历史
        await loadHistory()
        return
      }

      if (status.status === 'failed') {
        ElMessage.error(t('system.cleanup.msg.taskFailed'))
        return
      }

      // 继续轮询
      if (attempts < maxAttempts) {
        attempts++
        setTimeout(poll, 2000)
      }
    } catch (error) {
      console.error('查询任务状态失败:', error)
    }
  }

  poll()
}

// 加载任务历史
const loadHistory = async () => {
  try {
    historyLoading.value = true
    const response = await cleanupApi.getTaskHistory({ page: 1, page_size: 10 })
    taskHistory.value = response.tasks || []
  } catch (error) {
    console.error('加载任务历史失败:', error)
  } finally {
    historyLoading.value = false
  }
}

// 查看任务详情
const viewTaskDetail = async (taskId) => {
  try {
    detailDialogVisible.value = true
    taskDetail.value = null
    const detail = await cleanupApi.getTaskStatus(taskId)
    taskDetail.value = detail
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.detailFailed'))
  }
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

// 获取状态文本
const getStatusText = (status) => {
  const statusMap = {
    pending: t('system.cleanup.status.pending'),
    running: t('system.cleanup.status.running'),
    completed: t('system.cleanup.status.completed'),
    completed_with_errors: t('system.cleanup.status.completedWithErrors'),
    failed: t('system.cleanup.status.failed')
  }
  return statusMap[status] || status
}

const getRiskLevelText = (level) => {
  const levelMap = {
    low: t('system.cleanup.risk.low'),
    medium: t('system.cleanup.risk.medium'),
    high: t('system.cleanup.risk.high')
  }
  return levelMap[level] || level
}

// 获取风险等级标签类型
const getRiskLevelType = (level) => {
  const typeMap = {
    low: 'success',
    medium: 'warning',
    high: 'danger'
  }
  return typeMap[level] || 'info'
}

onMounted(() => {
  loadHistory()
})
</script>

<style scoped>
.page-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 10px;
}

.scan-result {
  margin-top: 20px;
}

.module-result {
  margin-top: 30px;
  padding: 20px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.module-result h3 {
  margin-top: 0;
  margin-bottom: 20px;
}

.stat-detail {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 5px;
}

.invalid-engines {
  margin-top: 20px;
}

.invalid-engines h4 {
  margin-bottom: 10px;
}

.cleanup-actions {
  margin-top: 30px;
  padding: 20px;
  background: #fff;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
}

.task-history {
  margin-top: 40px;
}

.task-history h3 {
  margin-bottom: 15px;
}
</style>
