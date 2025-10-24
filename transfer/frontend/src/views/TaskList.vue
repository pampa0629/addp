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
          <el-col :span="6">
            <el-statistic title="总任务数" :value="stats.total_tasks || 0" />
          </el-col>
          <el-col :span="6">
            <el-statistic title="运行中" :value="stats.running_tasks || 0">
              <template #prefix>
                <el-icon color="#409EFF"><Loading /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="6">
            <el-statistic title="成功" :value="stats.success_tasks || 0">
              <template #prefix>
                <el-icon color="#67C23A"><SuccessFilled /></el-icon>
              </template>
            </el-statistic>
          </el-col>
          <el-col :span="6">
            <el-statistic title="失败" :value="stats.failed_tasks || 0">
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
          <el-form-item label="任务类型">
            <el-select v-model="searchForm.type" placeholder="请选择" clearable>
              <el-option label="导入" value="import" />
              <el-option label="导出" value="export" />
              <el-option label="同步" value="sync" />
            </el-select>
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="searchForm.status" placeholder="请选择" clearable>
              <el-option label="待执行" value="pending" />
              <el-option label="运行中" value="running" />
              <el-option label="成功" value="success" />
              <el-option label="失败" value="failed" />
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
        <el-table-column prop="type" label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="getTypeTagType(row.type)">
              {{ getTypeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="mode" label="模式" width="120">
          <template #default="{ row }">
            {{ getModeLabel(row.mode) }}
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusTagType(row.status)">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="progress" label="进度" width="120">
          <template #default="{ row }">
            <el-progress :percentage="row.progress || 0" :status="getProgressStatus(row.status)" />
          </template>
        </el-table-column>
        <el-table-column prop="schedule" label="调度" width="150">
          <template #default="{ row }">
            {{ row.schedule || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="handleDetail(row.id)">详情</el-button>
            <el-button size="small" type="primary" @click="handleStart(row.id)"
              :disabled="row.status === 'running'">
              启动
            </el-button>
            <el-button size="small" type="warning" @click="handleStop(row.id)"
              :disabled="row.status !== 'running'">
              停止
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(row.id)">删除</el-button>
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
import { taskAPI } from '@/api/tasks'

const router = useRouter()

const loading = ref(false)
const tasks = ref([])
const stats = ref({})
const searchForm = ref({
  name: '',
  type: '',
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
  searchForm.value = { name: '', type: '', status: '' }
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

// 启动任务
const handleStart = async (id) => {
  try {
    await taskAPI.start(id)
    ElMessage.success('任务已启动')
    loadTasks()
    loadStatistics()
  } catch (error) {
    console.error('启动任务失败:', error)
  }
}

// 停止任务
const handleStop = async (id) => {
  try {
    await ElMessageBox.confirm('确定要停止该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.stop(id)
    ElMessage.success('任务已停止')
    loadTasks()
    loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

// 删除任务
const handleDelete = async (id) => {
  try {
    await ElMessageBox.confirm('确定要删除该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.delete(id)
    ElMessage.success('任务已删除')
    loadTasks()
    loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除任务失败:', error)
    }
  }
}

// 辅助函数
const getTypeLabel = (type) => {
  const labels = {
    import: '导入',
    export: '导出',
    sync: '同步'
  }
  return labels[type] || type
}

const getTypeTagType = (type) => {
  const types = {
    import: 'success',
    export: 'warning',
    sync: 'primary'
  }
  return types[type] || ''
}

const getModeLabel = (mode) => {
  const labels = {
    batch: '批处理',
    stream: '流式',
    'micro-batch': '微批处理'
  }
  return labels[mode] || mode
}

const getStatusLabel = (status) => {
  const labels = {
    pending: '待执行',
    running: '运行中',
    success: '成功',
    failed: '失败',
    stopped: '已停止',
    paused: '已暂停'
  }
  return labels[status] || status
}

const getStatusTagType = (status) => {
  const types = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger',
    stopped: 'warning',
    paused: 'info'
  }
  return types[status] || ''
}

const getProgressStatus = (status) => {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'exception'
  return undefined
}

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

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

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
