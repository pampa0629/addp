<template>
  <div class="vectorization-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>向量化任务历史</span>
          <el-button type="primary" @click="refreshTasks" :icon="Refresh" circle />
        </div>
      </template>

      <!-- 筛选条件 -->
      <el-form :inline="true" class="filter-form">
        <el-form-item label="引擎">
          <el-select v-model="filterEngineId" placeholder="全部引擎" clearable style="width: 200px" @change="handleFilterChange">
            <el-option v-for="engine in engines" :key="engine.id" :label="engine.name" :value="engine.id" />
          </el-select>
        </el-form-item>
      </el-form>

      <!-- 任务列表 -->
      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="task_id" label="任务ID" width="250" show-overflow-tooltip />
        <el-table-column prop="task_type" label="类型" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.task_type === 'object'" type="info">单文件</el-tag>
            <el-tag v-else type="primary">目录</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="engine_id" label="引擎" width="100" />
        <el-table-column prop="bucket" label="存储桶" width="150" show-overflow-tooltip />
        <el-table-column label="路径" width="200" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.object_key || row.prefix || '/' }}
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'completed'" type="success">完成</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger">失败</el-tag>
            <el-tag v-else-if="row.status === 'running'" type="warning">运行中</el-tag>
            <el-tag v-else type="info">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="统计" width="200">
          <template #default="{ row }">
            <div style="font-size: 12px">
              总计: {{ row.total }} | 成功: {{ row.vectorized }} | 跳过: {{ row.skipped }} | 失败: {{ row.failed }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="执行时长" width="100">
          <template #default="{ row }">
            {{ formatDuration(row.duration) }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180" />
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="showTaskDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleSizeChange"
        @current-change="handlePageChange"
        style="margin-top: 20px; justify-content: center"
      />
    </el-card>

    <!-- 任务详情对话框 -->
    <el-dialog v-model="detailDialogVisible" title="任务详情" width="800px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item label="任务ID">{{ selectedTask.task_id }}</el-descriptions-item>
        <el-descriptions-item label="类型">{{ selectedTask.task_type === 'object' ? '单文件' : '目录' }}</el-descriptions-item>
        <el-descriptions-item label="引擎ID">{{ selectedTask.engine_id }}</el-descriptions-item>
        <el-descriptions-item label="存储桶">{{ selectedTask.bucket }}</el-descriptions-item>
        <el-descriptions-item label="路径" :span="2">
          {{ selectedTask.object_key || selectedTask.prefix || '/' }}
        </el-descriptions-item>
        <el-descriptions-item label="递归">{{ selectedTask.recursive ? '是' : '否' }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag v-if="selectedTask.status === 'completed'" type="success">完成</el-tag>
          <el-tag v-else-if="selectedTask.status === 'failed'" type="danger">失败</el-tag>
          <el-tag v-else-if="selectedTask.status === 'running'" type="warning">运行中</el-tag>
          <el-tag v-else type="info">{{ selectedTask.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="总文件数">{{ selectedTask.total }}</el-descriptions-item>
        <el-descriptions-item label="成功">{{ selectedTask.vectorized }}</el-descriptions-item>
        <el-descriptions-item label="跳过">{{ selectedTask.skipped }}</el-descriptions-item>
        <el-descriptions-item label="失败">{{ selectedTask.failed }}</el-descriptions-item>
        <el-descriptions-item label="执行时长">{{ formatDuration(selectedTask.duration) }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ selectedTask.started_at || '-' }}</el-descriptions-item>
        <el-descriptions-item label="完成时间">{{ selectedTask.completed_at || '-' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ selectedTask.created_at }}</el-descriptions-item>
      </el-descriptions>

      <!-- 错误详情 -->
      <div v-if="selectedTask && selectedTask.errors && selectedTask.errors.length > 0" style="margin-top: 20px">
        <el-divider content-position="left">错误详情</el-divider>
        <el-alert
          v-for="(error, index) in selectedTask.errors"
          :key="index"
          :title="error"
          type="error"
          :closable="false"
          style="margin-bottom: 10px"
        />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import client from '../api/client'

// 数据
const tasks = ref([])
const engines = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filterEngineId = ref(null)

// 详情对话框
const detailDialogVisible = ref(false)
const selectedTask = ref(null)

// 加载引擎列表
const loadEngines = async () => {
  try {
    const response = await client.get('/engines')
    engines.value = response.data?.engines || []
  } catch (error) {
    console.error('加载引擎列表失败:', error)
  }
}

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      page_size: pageSize.value
    }
    if (filterEngineId.value) {
      params.engine_id = filterEngineId.value
    }

    const response = await client.get('/embedding/tasks', { params })
    // response 已经是后端返回的完整 JSON 对象（因为 extractData = true）
    tasks.value = response.data || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载任务列表失败:', error)
    ElMessage.error('加载任务列表失败')
  } finally {
    loading.value = false
  }
}

// 刷新任务
const refreshTasks = () => {
  loadTasks()
}

// 筛选条件变化
const handleFilterChange = () => {
  currentPage.value = 1
  loadTasks()
}

// 分页变化
const handlePageChange = () => {
  loadTasks()
}

const handleSizeChange = () => {
  currentPage.value = 1
  loadTasks()
}

// 显示任务详情
const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
}

// 格式化执行时长
const formatDuration = (milliseconds) => {
  if (!milliseconds) return '-'
  const seconds = Math.floor(milliseconds / 1000)
  if (seconds < 60) return `${seconds}秒`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  return `${minutes}分${remainingSeconds}秒`
}

// 初始化
onMounted(() => {
  loadEngines()
  loadTasks()
})
</script>

<style scoped>
.vectorization-tasks {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-form {
  margin-bottom: 20px;
}
</style>
