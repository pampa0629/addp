<template>
  <div class="task-monitor">
    <el-card class="filter-card" shadow="never">
      <template #header>
        <div class="filter-header">
          <h2>{{ t('meta.monitor.title') }}</h2>
          <div class="actions">
            <el-button type="primary" @click="refresh" :loading="loading">{{ t('meta.monitor.refresh') }}</el-button>
          </div>
        </div>
      </template>

      <el-form :model="filters" label-width="100px" inline @submit.prevent>
        <el-form-item :label="t('meta.monitor.resource')">
          <el-select v-model="filters.engineId" :placeholder="t('meta.monitor.all')" clearable style="width: 220px">
            <el-option
              v-for="engine in engines"
              :key="engine.id"
              :label="engine.name"
              :value="engine.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('meta.monitor.status')">
          <el-select v-model="filters.status" :placeholder="t('meta.monitor.all')" clearable style="width: 180px">
            <el-option :label="t('meta.monitor.pending')" value="pending" />
            <el-option :label="t('meta.monitor.running')" value="running" />
            <el-option :label="t('meta.monitor.success')" value="success" />
            <el-option :label="t('meta.monitor.failed')" value="failed" />
            <el-option :label="t('meta.monitor.canceled')" value="canceled" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('meta.monitor.triggerType')">
          <el-select v-model="filters.triggerType" :placeholder="t('meta.monitor.all')" clearable style="width: 180px">
            <el-option :label="t('meta.monitor.manual')" value="manual" />
            <el-option :label="t('meta.monitor.scheduled')" value="scheduled" />
            <el-option :label="t('meta.monitor.system')" value="system" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('meta.monitor.storageEngine')">
          <el-select v-model="filters.storageType" :placeholder="t('meta.monitor.all')" clearable style="width: 180px">
            <el-option
              v-for="option in storageTypeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('meta.monitor.timeRange')">
          <el-date-picker
            v-model="filters.range"
            type="datetimerange"
            :start-placeholder="t('meta.monitor.startTime')"
            :end-placeholder="t('meta.monitor.endTime')"
            format="YYYY-MM-DD HH:mm"
            value-format="YYYY-MM-DD HH:mm:ss"
            :default-time="['00:00:00', '23:59:59']"
            style="width: 350px"
            clearable
          />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="applyFilters" :loading="loading">{{ t('meta.monitor.query') }}</el-button>
          <el-button @click="resetFilters">{{ t('meta.monitor.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="result-card" shadow="hover">
      <el-table :data="taskRuns" v-loading="loading" height="640">
        <el-table-column prop="id" :label="t('meta.monitor.runId')" width="90" />
        <el-table-column prop="task_name" :label="t('meta.monitor.taskName')" min-width="200">
          <template #default="{ row }">
            <div class="task-name-cell">
              <div class="task-name">{{ runDisplayName(row) }}</div>
              <div class="task-plan-name" v-if="runPlanName(row)">{{ t('meta.monitor.plan') }}{{ runPlanName(row) }}</div>
              <div class="resource-name" v-if="row.resource_name">{{ row.resource_name }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('meta.monitor.storageEngine')" width="140">
          <template #default="{ row }">{{ formatStorageType(row) }}</template>
        </el-table-column>
        <el-table-column :label="t('meta.monitor.triggerType')" width="120">
          <template #default="{ row }">{{ formatTriggerType(row.trigger_type) }}</template>
        </el-table-column>
        <el-table-column :label="t('meta.monitor.status')" width="120">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">{{ formatRunStatus(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('meta.monitor.preprocessCache')" width="160">
          <template #default="{ row }">
            <div v-if="row.preprocess_task_count > 0" style="font-size: 12px">
              <el-tag :type="preprocessStatusTag(row.preprocess_status)" size="small">
                {{ formatPreprocessStatus(row.preprocess_status) }}
              </el-tag>
              <div style="margin-top: 4px; color: var(--addp-text-tertiary)">
                {{ t('meta.monitor.preprocessSuccess', { success: row.preprocess_success_count, total: row.preprocess_task_count }) }}
              </div>
            </div>
            <span v-else style="color: #c0c4cc">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('meta.monitor.progress')" width="180">
          <template #default="{ row }">
            <el-progress :percentage="row.progress_percent || 0" :status="progressStatus(row.status)" />
          </template>
        </el-table-column>
        <el-table-column prop="started_at" :label="t('meta.monitor.startTime')" width="180">
          <template #default="{ row }">{{ formatDate(row.started_at) }}</template>
        </el-table-column>
        <el-table-column prop="completed_at" :label="t('meta.monitor.completedTime')" width="180">
          <template #default="{ row }">{{ formatDate(row.completed_at) }}</template>
        </el-table-column>
        <el-table-column prop="progress_message" :label="t('meta.monitor.latestStatus')" min-width="240" show-overflow-tooltip />
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
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import metaApi from '../api/meta'

const { t } = useI18n()

const engines = ref([])
const taskRuns = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)
const autoRefreshTimer = ref(null)

const ACTIVE_REFRESH_INTERVAL_MS = 3000
const IDLE_REFRESH_INTERVAL_MS = 15000

const filters = reactive({
  engineId: null,
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
  object_storage: t('meta.monitor.objectStorage'),
  'object-storage': t('meta.monitor.objectStorage')
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

  engines.value.forEach(res => addOption(res.resource_type))
  taskRuns.value.forEach(run => {
    if (run.storage_type) {
      addOption(run.storage_type)
    } else if (run.resource_type) {
      addOption(run.resource_type)
    }
  })

  return options.sort((a, b) => a.label.localeCompare(b.label, 'zh-CN'))
})

const hasActiveRuns = computed(() =>
  taskRuns.value.some(run => ['pending', 'running'].includes(run.status))
)

const clearAutoRefresh = () => {
  if (autoRefreshTimer.value) {
    window.clearTimeout(autoRefreshTimer.value)
    autoRefreshTimer.value = null
  }
}

const scheduleAutoRefresh = () => {
  clearAutoRefresh()
  const delay = hasActiveRuns.value ? ACTIVE_REFRESH_INTERVAL_MS : IDLE_REFRESH_INTERVAL_MS
  autoRefreshTimer.value = window.setTimeout(async () => {
    await loadTaskRuns({ silent: true })
    scheduleAutoRefresh()
  }, delay)
}

const loadEngines = async () => {
  try {
    const res = await metaApi.getResources()
    engines.value = Array.isArray(res) ? res : []
  } catch (error) {
    ElMessage.error(t('meta.monitor.loadResourcesFailed', { msg: error.response?.data?.error || error.message }))
  }
}

const loadTaskRuns = async ({ silent = false } = {}) => {
  if (!silent) {
    loading.value = true
  }
  try {
    const params = {
      page: page.value,
      page_size: pageSize,
      engine_id: filters.engineId || undefined,
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
    if (!silent) {
      ElMessage.error(t('meta.monitor.loadTaskRunsFailed', { msg: error.response?.data?.error || error.message }))
    }
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

const applyFilters = () => {
  page.value = 1
  loadTaskRuns()
}

const resetFilters = () => {
  filters.engineId = null
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
  clearAutoRefresh()
  loadEngines()
  loadTaskRuns().finally(scheduleAutoRefresh)
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
    case 'pending': return t('meta.monitor.pending')
    case 'running': return t('meta.monitor.running')
    case 'success': return t('meta.monitor.success')
    case 'failed': return t('meta.monitor.failed')
    case 'canceled': return t('meta.monitor.canceled')
    default: return status || '-'
  }
}

const formatTriggerType = type => {
  switch (type) {
    case 'manual': return t('meta.monitor.manual')
    case 'scheduled': return t('meta.monitor.scheduled')
    case 'system': return t('meta.monitor.system')
    default: return type || '-'
  }
}

const formatDate = value => {
  if (!value) return '-'
  return new Date(value).toLocaleString('zh-CN')
}

const formatPreprocessStatus = status => {
  switch (status) {
    case 'pending': return t('meta.monitor.preprocessPending')
    case 'running': return t('meta.monitor.preprocessRunning')
    case 'success': return t('meta.monitor.success')
    case 'failed': return t('meta.monitor.failed')
    case 'skipped': return t('meta.monitor.preprocessSkipped')
    default: return status || '-'
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
    return t('meta.monitor.taskId', { id: row.task_id })
  }
  return t('meta.monitor.runIdLabel', { id: row.id })
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

onUnmounted(() => {
  clearAutoRefresh()
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
  color: var(--addp-text-tertiary);
}

.resource-name {
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.pagination {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
</style>
