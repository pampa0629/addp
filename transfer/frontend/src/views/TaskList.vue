<template>
  <div class="task-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>数据传输任务</span>
          <el-button type="primary" @click="handleCreate">
            <el-icon><Plus /></el-icon>
            创建任务
          </el-button>
        </div>
      </template>

      <!-- 统计卡片 -->
      <div class="stats-row">
        <el-row :gutter="16">
          <el-col :span="4">
            <el-statistic title="总任务数" :value="stats.total_tasks || 0" />
          </el-col>
          <el-col :span="5">
            <el-statistic title="未执行" :value="stats.not_executed_tasks || 0" />
          </el-col>
          <el-col :span="5">
            <el-statistic title="执行中" :value="stats.last_running_tasks || 0">
              <template #prefix>
                <el-icon color="#409EFF"><Loading /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="5">
            <el-statistic title="成功" :value="stats.last_success_tasks || 0">
              <template #prefix>
                <el-icon color="#67C23A"><SuccessFilled /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="5">
            <el-statistic title="失败" :value="stats.last_failed_tasks || 0">
              <template #prefix>
                <el-icon color="#F56C6C"><CircleCloseFilled /></el-icon>
              </template>
            </el-statistic>
          </el-col>
        </el-row>
      </div>

      <!-- 搜索栏 -->
      <div class="search-bar">
        <el-form :inline="true" :model="searchForm">
          <el-form-item label="任务名称">
            <el-input v-model="searchForm.name" placeholder="请输入任务名称" clearable />
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="searchForm.status" placeholder="请选择" clearable class="search-select">
              <el-option label="未执行" value="pending" />
              <el-option label="执行中" value="running" />
              <el-option label="已启动" value="scheduled" />
              <el-option label="已暂停" value="paused" />
              <el-option label="已完成" value="completed" />
              <el-option label="已停止" value="stopped" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="handleSearch">查询</el-button>
            <el-button @click="handleReset">重置</el-button>
          </el-form-item>
        </el-form>
      </div>

      <!-- 任务表格 -->
      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="任务名称" min-width="200" />
        <el-table-column prop="mode" label="模式" width="120">
          <template #default="{ row }">
            {{ getModeLabel(row.mode) }}
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="getTaskStatusTagType(row)">
              {{ getTaskStatusLabel(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_execution_status" label="最后执行状态" width="140">
          <template #default="{ row }">
            <el-tag :type="getExecutionTagType(row.last_execution_status)">
              {{ getLastExecutionLabel(row.last_execution_status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_execution_finished_at" label="最后执行时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.last_execution_finished_at || row.last_execution_started_at) }}
          </template>
        </el-table-column>
        <el-table-column prop="schedule" label="调度" min-width="200">
          <template #default="{ row }">
            {{ formatSchedule(row.schedule) }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="360" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="handleDetail(row.id)">详情</el-button>
            <template v-if="isManualTask(row)">
              <el-button size="small" type="primary" @click="handleExecute(row)" :disabled="isRunning(row)">
                执行
              </el-button>
              <el-button size="small" type="warning" @click="handleStop(row)" :disabled="!isRunning(row)">
                停止
              </el-button>
              <el-button size="small" type="danger" @click="handleDelete(row)" :disabled="!canDeleteManual(row)">
                删除
              </el-button>
            </template>
            <template v-else>
              <el-button size="small" type="primary" @click="handleResume(row)" :disabled="!canStartSchedule(row)">
                启动
              </el-button>
              <el-button size="small" type="warning" @click="handlePause(row)" :disabled="!canPauseSchedule(row)">
                暂停
              </el-button>
              <el-button size="small" @click="handleExecute(row)" :disabled="isRunning(row)">
                单次执行
              </el-button>
              <el-button size="small" type="danger" @click="handleDelete(row)" :disabled="isRunning(row)">
                删除
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Loading, SuccessFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { taskAPI } from '@/api/tasks'
import { formatDate, formatSchedule, getModeLabel, getTaskStatusLabel, getTaskStatusTagType, getLastExecutionLabel, getExecutionTagType } from '@/utils/formatters'

const router = useRouter()

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
    tasks.value = res.data || []
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

// 任务执行（手动或单次执行）
const handleExecute = async (task) => {
  try {
    await taskAPI.start(task.id)
    const message = isManualTask(task) ? '任务执行已提交' : '单次执行已提交'
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
    await ElMessageBox.confirm('确定要停止该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.stop(task.id)
    ElMessage.success('任务已停止')
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
    await ElMessageBox.confirm('确定要暂停该定时任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.pause(task.id)
    ElMessage.success('任务已暂停')
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
    ElMessage.success('任务已启用')
    await loadTasks()
    await loadStatistics()
  } catch (error) {
    console.error('启用任务失败:', error)
  }
}

// 删除任务
const handleDelete = async (task) => {
  try {
    await ElMessageBox.confirm('确定要删除该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.delete(task.id)
    ElMessage.success('任务已删除')
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
const canStartSchedule = (task) => ['pending', 'paused'].includes(task.status)
const canPauseSchedule = (task) => ['scheduled', 'running'].includes(task.status)
const canDeleteManual = (task) => isRunning(task)

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
