<template>
  <div class="execution-list">
    <div class="header">
      <h2>执行记录</h2>
      <el-button @click="handleBack">返回</el-button>
    </div>

    <el-table :data="executions" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="执行ID" width="100"></el-table-column>
      <el-table-column label="状态" width="120">
        <template #default="scope">
          <el-tag :type="getStatusType(scope.row.status)">
            {{ getStatusText(scope.row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="current_step" label="当前步骤" width="150"></el-table-column>
      <el-table-column label="开始时间" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.started_at) }}
        </template>
      </el-table-column>
      <el-table-column label="完成时间" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.completed_at) }}
        </template>
      </el-table-column>
      <el-table-column prop="error_message" label="错误信息" show-overflow-tooltip></el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="scope">
          <el-button size="small" @click="handleViewDetail(scope.row)">详情</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        @current-change="handlePageChange"
        layout="total, prev, pager, next"
      />
    </div>

    <!-- 执行详情对话框 -->
    <el-dialog v-model="detailVisible" title="执行详情" width="800px">
      <div v-if="currentExecution" class="execution-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="执行ID">{{ currentExecution.id }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="getStatusType(currentExecution.status)">
              {{ getStatusText(currentExecution.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="开始时间">
            {{ formatTime(currentExecution.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="完成时间">
            {{ formatTime(currentExecution.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="当前步骤" :span="2">
            {{ currentExecution.current_step || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="错误信息" :span="2">
            {{ currentExecution.error_message || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <h4 style="margin-top: 20px">步骤结果</h4>
        <el-collapse v-if="currentExecution.step_results">
          <el-collapse-item
            v-for="(result, stepId) in currentExecution.step_results"
            :key="stepId"
            :title="`${stepId} - ${result.status}`"
          >
            <pre>{{ JSON.stringify(result, null, 2) }}</pre>
          </el-collapse-item>
        </el-collapse>
        <p v-else>暂无步骤结果</p>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import orchestrationAPI from '../api/orchestration'

const route = useRoute()
const router = useRouter()

const executions = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const detailVisible = ref(false)
const currentExecution = ref(null)

onMounted(() => {
  loadExecutions()
})

async function loadExecutions() {
  loading.value = true
  try {
    const orchId = route.params.id
    const result = await orchestrationAPI.listExecutions(orchId, {
      limit: pageSize.value,
      offset: (currentPage.value - 1) * pageSize.value
    })
    executions.value = result.items
    total.value = result.total
  } catch (error) {
    ElMessage.error('加载执行记录失败')
  } finally {
    loading.value = false
  }
}

async function handleViewDetail(row) {
  try {
    currentExecution.value = await orchestrationAPI.getExecution(row.id)
    detailVisible.value = true
  } catch (error) {
    ElMessage.error('加载执行详情失败')
  }
}

function handlePageChange() {
  loadExecutions()
}

function handleBack() {
  router.push('/orchestrations')
}

function getStatusType(status) {
  const types = {
    pending: 'info',
    running: 'warning',
    completed: 'success',
    failed: 'danger'
  }
  return types[status] || 'info'
}

function getStatusText(status) {
  const texts = {
    pending: '等待中',
    running: '运行中',
    completed: '已完成',
    failed: '失败'
  }
  return texts[status] || status
}

function formatTime(time) {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}
</script>

<style scoped>
.execution-list {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

h2 {
  margin: 0;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

.execution-detail pre {
  background: var(--addp-bg-secondary);
  padding: 10px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
}
</style>
