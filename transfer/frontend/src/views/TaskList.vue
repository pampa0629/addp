<template>
  <div class="task-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskList.title') }}</span>
          <el-button type="primary" @click="handleCreate">
            <el-icon><Plus /></el-icon>
            {{ t('transfer.taskList.createTask') }}
          </el-button>
        </div>
      </template>

      <!-- 统计卡片 -->
      <div class="stats-row">
        <el-row :gutter="16">
          <el-col :span="4">
            <el-statistic :title="t('transfer.taskList.totalTasks')" :value="stats.total_tasks || 0" />
          </el-col>
          <el-col :span="5">
            <el-statistic :title="t('transfer.taskList.notExecuted')" :value="stats.not_executed_tasks || 0" />
          </el-col>
          <el-col :span="5">
            <el-statistic :title="t('transfer.taskList.running')" :value="stats.last_running_tasks || 0">
              <template #prefix>
                <el-icon color="var(--el-color-primary)"><Loading /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="5">
            <el-statistic :title="t('transfer.taskList.success')" :value="stats.last_success_tasks || 0">
              <template #prefix>
                <el-icon color="var(--el-color-success)"><SuccessFilled /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="5">
            <el-statistic :title="t('transfer.taskList.failed')" :value="stats.last_failed_tasks || 0">
              <template #prefix>
                <el-icon color="var(--el-color-danger)"><CircleCloseFilled /></el-icon>
              </template>
            </el-statistic>
          </el-col>
        </el-row>
      </div>

      <!-- 搜索栏 -->
      <div class="search-bar">
        <el-form :inline="true" :model="searchForm">
          <el-form-item :label="t('transfer.taskList.taskName')">
            <el-input v-model="searchForm.name" :placeholder="t('transfer.taskList.taskNamePlaceholder')" clearable />
          </el-form-item>
          <el-form-item :label="t('transfer.taskList.status')">
            <el-select v-model="searchForm.status" :placeholder="t('transfer.taskList.statusPlaceholder')" clearable class="search-select">
              <el-option :label="t('transfer.taskList.idle')" value="idle" />
              <el-option :label="t('transfer.taskList.running')" value="running" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="handleSearch">{{ t('transfer.taskList.search') }}</el-button>
            <el-button @click="handleReset">{{ t('transfer.taskList.reset') }}</el-button>
          </el-form-item>
        </el-form>
      </div>

      <!-- 任务表格 -->
      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="t('transfer.taskList.name')" min-width="200" />
        <el-table-column prop="status" :label="t('transfer.taskList.status')" width="110">
          <template #default="{ row }">
            <el-tag :type="getTaskStatusTagType(row)">
              {{ getTaskStatusLabel(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="schedule" :label="t('transfer.taskList.schedule')" min-width="200">
          <template #default="{ row }">
            {{ formatSchedule(row.schedule) }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="t('transfer.taskList.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskList.actions')" width="360" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="handleDetail(row.id)">{{ t('transfer.taskList.detail') }}</el-button>
            <el-button size="small" @click="handleEdit(row)" :disabled="isRunning(row)">{{ t('transfer.taskList.edit') }}</el-button>
            <template v-if="isManualTask(row)">
              <el-button size="small" type="primary" @click="handleExecute(row)" :disabled="isRunning(row)">
                {{ t('transfer.taskList.execute') }}
              </el-button>
              <el-button size="small" type="warning" @click="handleStop(row)" :disabled="!isRunning(row)">
                {{ t('transfer.taskList.stop') }}
              </el-button>
              <el-button size="small" type="danger" @click="handleDelete(row)" :disabled="!canDeleteManual(row)">
                {{ t('transfer.taskList.delete') }}
              </el-button>
            </template>
            <template v-else>
              <el-button size="small" type="primary" @click="handleResume(row)" :disabled="!canStartSchedule(row)">
                {{ t('transfer.taskList.start') }}
              </el-button>
              <el-button size="small" type="warning" @click="handlePause(row)" :disabled="!canPauseSchedule(row)">
                {{ t('transfer.taskList.pause') }}
              </el-button>
              <el-button size="small" @click="handleExecute(row)" :disabled="isRunning(row)">
                {{ t('transfer.taskList.runOnce') }}
              </el-button>
              <el-button size="small" type="danger" @click="handleDelete(row)" :disabled="isRunning(row)">
                {{ t('transfer.taskList.delete') }}
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Loading, SuccessFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { taskAPI } from '@/api/tasks'
import { formatDate } from '@common-ui'
import { formatSchedule, getTaskStatusLabel, getTaskStatusTagType } from '@/utils/formatters'

const router = useRouter()
const { t } = useI18n()

const loading = ref(false)
const tasks = ref([])
const stats = ref({})
const searchForm = ref({
  name: '',
  status: ''
})
const pagination = ref({
  page: 1,
  page_size: 20,
  total: 0
})

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.value.page,
      page_size: pagination.value.page_size,
      ...searchForm.value
    }
    Object.keys(params).forEach((key) => {
      const value = params[key]
      if (value === '' || value === null || value === undefined) {
        delete params[key]
      }
    })
    const res = await taskAPI.list(params)
    tasks.value = res.items || []
    pagination.value.total = res.total || 0
  } catch (error) {
    console.error('加载任务列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载统计数据
const loadStatistics = async () => {
  try {
    stats.value = await taskAPI.statistics()
  } catch (error) {
    console.error('加载统计数据失败:', error)
  }
}

// 搜索
const handleSearch = () => {
  pagination.value.page = 1
  loadTasks()
}

// 重置
const handleReset = () => {
  searchForm.value = { name: '', status: '' }
  handleSearch()
}

// 分页
const handlePageChange = () => {
  loadTasks()
}

const handleSizeChange = () => {
  pagination.value.page = 1
  loadTasks()
}

// 创建任务
const handleCreate = () => {
  router.push('/tasks/create')
}

// 查看详情
const handleDetail = (id) => {
  router.push(`/tasks/${id}/detail`)
}

const handleEdit = (task) => {
  router.push(`/tasks/${task.id}/edit`)
}

// 任务执行（手动或单次执行）
const handleExecute = async (task) => {
  try {
    await taskAPI.start(task.id)
    const message = isManualTask(task) ? t('transfer.taskList.executeSubmitted') : t('transfer.taskList.runOnceSubmitted')
    ElMessage.success(message)
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    console.error('执行任务失败:', error)
  }
}

// 停止手动任务
const handleStop = async (task) => {
  try {
    await ElMessageBox.confirm(t('transfer.taskList.stopConfirm'), t('transfer.taskList.hint'), {
      confirmButtonText: t('transfer.taskList.confirm'),
      cancelButtonText: t('transfer.taskList.cancel'),
      type: 'warning'
    })
    await taskAPI.stop(task.id)
    ElMessage.success(t('transfer.taskList.stopped'))
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

// 暂停定时任务
const handlePause = async (task) => {
  try {
    await ElMessageBox.confirm(t('transfer.taskList.pauseConfirm'), t('transfer.taskList.hint'), {
      confirmButtonText: t('transfer.taskList.confirm'),
      cancelButtonText: t('transfer.taskList.cancel'),
      type: 'warning'
    })
    await taskAPI.pause(task.id)
    ElMessage.success(t('transfer.taskList.paused'))
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

// 启用定时任务
const handleResume = async (task) => {
  try {
    await taskAPI.resume(task.id)
    ElMessage.success(t('transfer.taskList.resumed'))
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    console.error('启用任务失败:', error)
  }
}

// 删除任务
const handleDelete = async (task) => {
  try {
    await ElMessageBox.confirm(t('transfer.taskList.deleteConfirm'), t('transfer.taskList.hint'), {
      confirmButtonText: t('transfer.taskList.confirm'),
      cancelButtonText: t('transfer.taskList.cancel'),
      type: 'warning'
    })
    await taskAPI.delete(task.id)
    ElMessage.success(t('transfer.taskList.deleted'))
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除任务失败:', error)
    }
  }
}

// 辅助函数
const isManualTask = (task) => !task.schedule
const isRunning = (task) => task.status === 'running'
const canStartSchedule = (task) => !task.enabled
const canPauseSchedule = (task) => task.enabled
const canDeleteManual = (task) => !isRunning(task)

// 初始化
onMounted(() => {
  loadTasks()
  loadStatistics()

  // 每5秒刷新一次
  setInterval(() => {
    loadTasks()
    loadStatistics()
  }, 5000)
})
</script>

<style scoped>
.task-list {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.stats-row {
  margin-bottom: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid #EBEEF5;
}

.search-bar {
  margin-bottom: 20px;
}

.search-select {
  min-width: 220px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
