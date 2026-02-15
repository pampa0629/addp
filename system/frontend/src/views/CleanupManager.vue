<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>垃圾数据清理</span>
          <div class="header-actions">
            <el-button
              type="primary"
              :icon="Search"
              :loading="scanLoading"
              @click="startScan"
            >
              扫描垃圾数据
            </el-button>
            <el-button
              :icon="Refresh"
              @click="loadHistory"
            >
              刷新历史
            </el-button>
          </div>
        </div>
      </template>

      <!-- 当前扫描/执行任务 -->
      <el-alert
        v-if="currentTask"
        :title="`${currentTask.action === 'scan' ? '扫描' : '清理'}任务进行中`"
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <div>任务ID: {{ currentTask.task_id }}</div>
        <div>状态: {{ getStatusText(currentTask.status) }}</div>
        <el-progress
          v-if="currentTask.progress"
          :percentage="Math.round((currentTask.progress.completed / currentTask.progress.total) * 100)"
          :status="currentTask.status === 'completed' ? 'success' : undefined"
        />
      </el-alert>

      <!-- 扫描结果 -->
      <div v-if="scanResult" class="scan-result">
        <el-descriptions title="扫描结果" :column="2" border>
          <el-descriptions-item label="扫描时间">
            {{ formatTime(scanResult.task.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="扫描范围">
            {{ scanResult.task.expected_modules?.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item label="风险等级">
            <el-tag :type="getRiskLevelType(scanResult.summary.risk_level)">
              {{ getRiskLevelText(scanResult.summary.risk_level) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="待清理项">
            {{ scanResult.summary.total_items_to_clean }} 项
          </el-descriptions-item>
        </el-descriptions>

        <!-- Meta 模块扫描详情 -->
        <div v-if="scanResult.results?.meta" class="module-result">
          <h3>Meta 模块扫描详情</h3>
          <el-row :gutter="20">
            <el-col :span="8">
              <el-statistic title="软删除数据" :value="scanResult.results.meta.statistics.soft_deleted?.items || 0">
                <template #suffix>项</template>
              </el-statistic>
              <div class="stat-detail">节点: {{ scanResult.results.meta.statistics.soft_deleted?.nodes || 0 }}</div>
            </el-col>
            <el-col :span="8">
              <el-statistic title="无效引擎数据" :value="scanResult.results.meta.statistics.invalid_engines?.count || 0">
                <template #suffix>个引擎</template>
              </el-statistic>
            </el-col>
            <el-col :span="8">
              <el-statistic title="重复指纹" :value="scanResult.results.meta.statistics.duplicate_fingerprints?.count || 0">
                <template #suffix>个</template>
              </el-statistic>
            </el-col>
          </el-row>

          <!-- 无效引擎详情 -->
          <div v-if="scanResult.results.meta.statistics.invalid_engines?.details?.length > 0" class="invalid-engines">
            <h4>无效引擎详情</h4>
            <el-table :data="scanResult.results.meta.statistics.invalid_engines.details" border>
              <el-table-column prop="engine_id" label="引擎ID" width="100" />
              <el-table-column prop="engine_name" label="引擎名称" />
              <el-table-column prop="reason" label="原因" />
              <el-table-column prop="affected_nodes" label="影响节点" width="100" />
              <el-table-column prop="affected_items" label="影响数据项" width="100" />
            </el-table>
          </div>
        </div>

        <!-- 清理操作 -->
        <div class="cleanup-actions">
          <el-alert
            title="清理说明"
            type="warning"
            :closable="false"
            style="margin-bottom: 15px"
          >
            <ul style="margin: 0; padding-left: 20px">
              <li><strong>标记删除</strong>: 将无效数据标记为软删除状态，可以恢复</li>
              <li><strong>物理删除</strong>: 永久删除已标记的软删除数据，<strong style="color: red">不可恢复</strong></li>
            </ul>
          </el-alert>

          <el-space>
            <el-button
              type="warning"
              :icon="WarningFilled"
              :loading="executeLoading"
              @click="executeCleanup('soft_delete')"
            >
              标记删除无效数据
            </el-button>
            <el-popconfirm
              title="物理删除后数据将永久删除，确定继续吗？"
              confirm-button-text="确定"
              cancel-button-text="取消"
              @confirm="executeCleanup('hard_delete')"
            >
              <template #reference>
                <el-button
                  type="danger"
                  :icon="Delete"
                  :loading="executeLoading"
                >
                  物理删除软删除数据
                </el-button>
              </template>
            </el-popconfirm>
          </el-space>
        </div>
      </div>

      <!-- 任务历史 -->
      <div class="task-history">
        <h3>任务历史</h3>
        <el-table
          :data="taskHistory"
          v-loading="historyLoading"
          border
          style="width: 100%"
        >
          <el-table-column prop="task_id" label="任务ID" width="250" />
          <el-table-column label="操作类型" width="100">
            <template #default="{ row }">
              <el-tag :type="row.action === 'scan' ? 'info' : 'warning'">
                {{ row.action === 'scan' ? '扫描' : '清理' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="删除类型" width="120">
            <template #default="{ row }">
              <el-tag v-if="row.delete_type === 'soft_delete'" type="warning">标记删除</el-tag>
              <el-tag v-else-if="row.delete_type === 'hard_delete'" type="danger">物理删除</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column prop="status" label="状态" width="100">
            <template #default="{ row }">
              {{ getStatusText(row.status) }}
            </template>
          </el-table-column>
          <el-table-column label="范围" width="100">
            <template #default="{ row }">
              {{ row.expected_modules?.join(', ') || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="开始时间" width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column label="操作" fixed="right" width="100">
            <template #default="{ row }">
              <el-button
                type="primary"
                link
                @click="viewTaskDetail(row.task_id)"
              >
                查看详情
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <!-- 任务详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      title="任务详情"
      width="70%"
    >
      <pre v-if="taskDetail" style="max-height: 500px; overflow: auto">{{ JSON.stringify(taskDetail, null, 2) }}</pre>
      <el-skeleton v-else :rows="10" animated />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Delete, WarningFilled } from '@element-plus/icons-vue'
import { cleanupApi } from '../api/cleanup'

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

    ElMessage.success('扫描任务已创建')

    // 轮询任务状态
    await pollTaskStatus(taskId)
  } catch (error) {
    ElMessage.error('创建扫描任务失败: ' + (error.message || '未知错误'))
  } finally {
    scanLoading.value = false
  }
}

// 执行清理
const executeCleanup = async (deleteType) => {
  if (!scanResult.value) {
    ElMessage.warning('请先执行扫描')
    return
  }

  try {
    executeLoading.value = true
    const response = await cleanupApi.createExecuteTask({
      based_on_scan: scanResult.value.task_id,
      delete_type: deleteType
    })

    ElMessage.success('清理任务已创建')

    // 轮询任务状态
    await pollTaskStatus(response.task_id)
  } catch (error) {
    ElMessage.error('创建清理任务失败: ' + (error.message || '未知错误'))
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
        ElMessage.error('任务执行失败')
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
    ElMessage.error('加载任务详情失败')
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
    pending: '等待中',
    running: '运行中',
    completed: '已完成',
    completed_with_errors: '完成(有错误)',
    failed: '失败'
  }
  return statusMap[status] || status
}

// 获取风险等级文本
const getRiskLevelText = (level) => {
  const levelMap = {
    low: '低',
    medium: '中',
    high: '高'
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
