<template>
  <div class="task-monitor">
    <el-card class="filter-card" shadow="never">
      <template #header>
        <div class="filter-header">
          <h2>任务监控</h2>
          <div class="actions">
            <el-button type="primary" @click="refresh" :loading="loading">刷新</el-button>
          </div>
        </div>
      </template>

      <el-form :model="filters" label-width="100px" inline @submit.prevent>
        <el-form-item label="资源">
          <el-select v-model="filters.resourceId" placeholder="全部" clearable style="width: 220px">
            <el-option
              v-for="resource in resources"
              :key="resource.id"
              :label="resource.name"
              :value="resource.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="状态">
          <el-select v-model="filters.status" placeholder="全部" clearable style="width: 180px">
            <el-option label="待执行" value="pending" />
            <el-option label="执行中" value="running" />
            <el-option label="成功" value="success" />
            <el-option label="失败" value="failed" />
            <el-option label="已取消" value="canceled" />
          </el-select>
        </el-form-item>

        <el-form-item label="触发类型">
          <el-select v-model="filters.triggerType" placeholder="全部" clearable style="width: 180px">
            <el-option label="手动触发" value="manual" />
            <el-option label="调度触发" value="scheduled" />
            <el-option label="系统触发" value="system" />
          </el-select>
        </el-form-item>

        <el-form-item label="存储引擎">
          <el-select v-model="filters.storageType" placeholder="全部" clearable style="width: 180px">
            <el-option
              v-for="option in storageTypeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="时间范围">
          <el-date-picker
            v-model="filters.range"
            type="datetimerange"
            start-placeholder="开始时间"
            end-placeholder="结束时间"
            format="YYYY-MM-DD HH:mm"
            value-format="YYYY-MM-DD HH:mm:ss"
            :default-time="['00:00:00', '23:59:59']"
            style="width: 350px"
            clearable
          />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="applyFilters" :loading="loading">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="result-card" shadow="hover">
      <el-table :data="taskRuns" v-loading="loading" height="640">
        <el-table-column prop="id" label="运行ID" width="90" />
        <el-table-column prop="task_name" label="任务名称" min-width="200">
          <template #default="{ row }">
            <div class="task-name-cell">
              <div class="task-name">{{ runDisplayName(row) }}</div>
              <div class="task-plan-name" v-if="runPlanName(row)">计划：{{ runPlanName(row) }}</div>
              <div class="resource-name" v-if="row.resource_name">{{ row.resource_name }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="存储引擎" width="140">
          <template #default="{ row }">{{ formatStorageType(row) }}</template>
        </el-table-column>
        <el-table-column label="触发类型" width="120">
          <template #default="{ row }">{{ formatTriggerType(row.trigger_type) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">{{ formatRunStatus(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="缓存预处理" width="160">
          <template #default="{ row }">
            <div v-if="row.preprocess_task_count > 0" style="font-size: 12px">
              <el-tag :type="preprocessStatusTag(row.preprocess_status)" size="small">
                {{ formatPreprocessStatus(row.preprocess_status) }}
              </el-tag>
              <div style="margin-top: 4px; color: #909399">
                {{ row.preprocess_success_count }}/{{ row.preprocess_task_count }} 成功
              </div>
            </div>
            <span v-else style="color: #c0c4cc">-</span>
          </template>
        </el-table-column>
        <el-table-column label="进度" width="180">
          <template #default="{ row }">
            <el-progress :percentage="row.progress_percent || 0" :status="progressStatus(row.status)" />
          </template>
        </el-table-column>
        <el-table-column prop="started_at" label="开始时间" width="180">
          <template #default="{ row }">{{ formatDate(row.started_at) }}</template>
        </el-table-column>
        <el-table-column prop="completed_at" label="完成时间" width="180">
          <template #default="{ row }">{{ formatDate(row.completed_at) }}</template>
        </el-table-column>
        <el-table-column prop="progress_message" label="最新状态" min-width="240" show-overflow-tooltip />
      </el-table>

      <div class="pagination">
        <el-pagination
          background
          layout="prev, pager, next, jumper"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import metaApi from '../api/meta'

const resources = ref([])
const taskRuns = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)

const filters = reactive({
  resourceId: null,
  status: '',
  triggerType: '',
  storageType: '',
  range: []
})

const normalizeEngineKey = value =>
  (value || '')
    .toString()
    .trim()
    .toLowerCase()
    .replace(/\s+/g, '_')

const ENGINE_LABELS = {
  postgresql: 'PostgreSQL',
  postgres: 'PostgreSQL',
  mysql: 'MySQL',
  minio: 'MinIO',
  s3: 'S3',
  oss: 'OSS',
  object_storage: '对象存储',
  'object-storage': '对象存储'
}

const prettifyEngine = value => {
  const key = normalizeEngineKey(value)
  if (!key) return value || '-'
  return ENGINE_LABELS[key] || value || key
}

const storageTypeOptions = computed(() => {
  const seen = new Set()
  const options = []

  const addOption = raw => {
    const key = normalizeEngineKey(raw)
    if (!key || seen.has(key)) {
      return
    }
    seen.add(key)
    options.push({
      value: key,
      label: prettifyEngine(raw)
    })
  }

  resources.value.forEach(res => addOption(res.resource_type))
  taskRuns.value.forEach(run => {
    if (run.storage_type) {
      addOption(run.storage_type)
    } else if (run.resource_type) {
      addOption(run.resource_type)
    }
  })

  return options.sort((a, b) => a.label.localeCompare(b.label, 'zh-CN'))
})

const loadResources = async () => {
  try {
    const res = await metaApi.getResources()
    resources.value = res.data || []
  } catch (error) {
    ElMessage.error('加载资源列表失败: ' + (error.response?.data?.error || error.message))
  }
}

const loadTaskRuns = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize,
      resource_id: filters.resourceId || undefined,
      status: filters.status || undefined,
      trigger_type: filters.triggerType || undefined,
      storage_type: filters.storageType || undefined,
      started_after: filters.range?.[0] || undefined,
      started_before: filters.range?.[1] || undefined
    }
    const res = await metaApi.getScanRuns(null, params)
    taskRuns.value = res.items || []
    total.value = res.total || 0
  } catch (error) {
    ElMessage.error('加载任务运行失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {
  page.value = 1
  loadTaskRuns()
}

const resetFilters = () => {
  filters.resourceId = null
  filters.status = ''
  filters.triggerType = ''
  filters.storageType = ''
  filters.range = []
  applyFilters()
}

const handlePageChange = newPage => {
  page.value = newPage
  loadTaskRuns()
}

const refresh = () => {
  loadResources()
  loadTaskRuns()
}

const statusTag = status => {
  switch (status) {
    case 'success':
      return 'success'
    case 'running':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'pending':
      return 'info'
    default:
      return ''
  }
}

const progressStatus = status => {
  switch (status) {
    case 'running':
      return 'active'
    case 'failed':
      return 'exception'
    case 'success':
      return 'success'
    default:
      return undefined
  }
}

const formatRunStatus = status => {
  switch (status) {
    case 'pending':
      return '待执行'
    case 'running':
      return '执行中'
    case 'success':
      return '成功'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
    default:
      return status || '-'
  }
}

const formatTriggerType = type => {
  switch (type) {
    case 'manual':
      return '手动触发'
    case 'scheduled':
      return '调度触发'
    case 'system':
      return '系统触发'
    default:
      return type || '-'
  }
}

const formatDate = value => {
  if (!value) return '-'
  return new Date(value).toLocaleString('zh-CN')
}

const formatPreprocessStatus = status => {
  switch (status) {
    case 'pending':
      return '未开始'
    case 'running':
      return '进行中'
    case 'success':
      return '成功'
    case 'failed':
      return '失败'
    case 'skipped':
      return '已跳过'
    default:
      return status || '-'
  }
}

const preprocessStatusTag = status => {
  switch (status) {
    case 'success':
      return 'success'
    case 'running':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'pending':
      return 'info'
    case 'skipped':
      return ''
    default:
      return ''
  }
}

const runDisplayName = row => {
  if (!row) return ''
  const name = (row.task_name || row.name || '').trim()
  if (name) {
    return name
  }
  if (row.task_plan_name) {
    return row.task_plan_name
  }
  if (row.task_id) {
    return `任务 #${row.task_id}`
  }
  return `运行 #${row.id}`
}

const runPlanName = row => {
  if (!row?.task_plan_name) {
    return ''
  }
  const displayName = runDisplayName(row)
  return displayName === row.task_plan_name ? '' : row.task_plan_name
}

const formatStorageType = row => {
  const raw = row?.storage_type || row?.resource_type
  return prettifyEngine(raw)
}

onMounted(() => {
  refresh()
})
</script>

<style scoped>
.task-monitor {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.filter-card {
  border-radius: 12px;
}

.filter-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.actions {
  display: flex;
  gap: 12px;
}

.result-card {
  flex: 1;
  border-radius: 12px;
}

.task-name-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.task-name {
  font-weight: 600;
}

.task-plan-name {
  font-size: 12px;
  color: #909399;
}

.resource-name {
  font-size: 12px;
  color: #909399;
}

.pagination {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
</style>
