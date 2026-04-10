<template>
  <div class="vectorization-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('manager.vectorization.title') }}</span>
          <el-button type="primary" @click="refreshTasks" :icon="Refresh" circle />
        </div>
      </template>

      <!-- 筛选条件 -->
      <el-form :inline="true" class="filter-form">
        <el-form-item :label="t('manager.vectorization.filterEngine')">
          <el-select v-model="filterEngineId" :placeholder="t('manager.vectorization.allEngines')" clearable style="width: 200px" @change="handleFilterChange">
            <el-option v-for="engine in engines" :key="engine.id" :label="engine.name" :value="engine.id" />
          </el-select>
        </el-form-item>
      </el-form>

      <!-- 任务列表 -->
      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="task_id" :label="t('manager.vectorization.taskId')" width="250" show-overflow-tooltip />
        <el-table-column prop="task_type" :label="t('manager.vectorization.type')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.task_type === 'object'" type="info">{{ t('manager.vectorization.typeSingle') }}</el-tag>
            <el-tag v-else type="primary">{{ t('manager.vectorization.typeDirectory') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="engine_id" :label="t('manager.vectorization.engine')" width="100" />
        <el-table-column prop="bucket" :label="t('manager.vectorization.bucket')" width="150" show-overflow-tooltip />
        <el-table-column :label="t('manager.vectorization.path')" width="200" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.object_key || row.prefix || '/' }}
          </template>
        </el-table-column>
        <el-table-column prop="status" :label="t('manager.vectorization.status')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'completed'" type="success">{{ t('manager.vectorization.statusCompleted') }}</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger">{{ t('manager.vectorization.statusFailed') }}</el-tag>
            <el-tag v-else-if="row.status === 'running'" type="warning">{{ t('manager.vectorization.statusRunning') }}</el-tag>
            <el-tag v-else type="info">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.stats')" width="200">
          <template #default="{ row }">
            <div style="font-size: 12px">
              {{ t('manager.vectorization.statsFormat', { total: row.total, vectorized: row.vectorized, skipped: row.skipped, failed: row.failed }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.duration')" width="100">
          <template #default="{ row }">
            {{ formatDuration(row.duration) }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="t('manager.vectorization.createdAt')" width="180" />
        <el-table-column :label="t('manager.vectorization.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="showTaskDetail(row)">{{ t('manager.vectorization.detail') }}</el-button>
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
    <el-dialog v-model="detailDialogVisible" :title="t('manager.vectorization.dialogTitle')" width="800px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.vectorization.taskId')">{{ selectedTask.task_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.type')">{{ selectedTask.task_type === 'object' ? t('manager.vectorization.typeSingle') : t('manager.vectorization.typeDirectory') }}</el-descriptions-item>
        <el-descriptions-item label="Engine ID">{{ selectedTask.engine_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.bucket')">{{ selectedTask.bucket }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.path')" :span="2">
          {{ selectedTask.object_key || selectedTask.prefix || '/' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.recursive')">{{ selectedTask.recursive ? t('common.yes') : t('common.no') }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.status')">
          <el-tag v-if="selectedTask.status === 'completed'" type="success">{{ t('manager.vectorization.statusCompleted') }}</el-tag>
          <el-tag v-else-if="selectedTask.status === 'failed'" type="danger">{{ t('manager.vectorization.statusFailed') }}</el-tag>
          <el-tag v-else-if="selectedTask.status === 'running'" type="warning">{{ t('manager.vectorization.statusRunning') }}</el-tag>
          <el-tag v-else type="info">{{ selectedTask.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.totalFiles')">{{ selectedTask.total }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.succeeded')">{{ selectedTask.vectorized }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.skipped')">{{ selectedTask.skipped }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.failed')">{{ selectedTask.failed }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.duration')">{{ formatDuration(selectedTask.duration) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.startTime')">{{ selectedTask.started_at || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.endTime')">{{ selectedTask.completed_at || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.createdAt')">{{ selectedTask.created_at }}</el-descriptions-item>
      </el-descriptions>

      <!-- 错误详情 -->
      <div v-if="selectedTask && selectedTask.errors && selectedTask.errors.length > 0" style="margin-top: 20px">
        <el-divider content-position="left">{{ t('manager.vectorization.errorDetail') }}</el-divider>
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
import { useI18n } from 'vue-i18n'
import client from '../api/client'

const { t } = useI18n()

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
    const response = await client.get('/manager/engines')
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

    const response = await client.get('/manager/embedding_tasks', { params })
    tasks.value = response.data || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载任务列表失败:', error)
    ElMessage.error(t('manager.vectorization.loadFailed'))
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
  if (seconds < 60) return t('manager.vectorization.durationSeconds', { n: seconds })
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  return t('manager.vectorization.durationMinutes', { m: minutes, s: remainingSeconds })
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
