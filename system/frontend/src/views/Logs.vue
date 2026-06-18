<template>
  <el-card>
    <template #header>
      <div class="card-header">
        <span>{{ t('system.log.title') }}</span>
        <div>
          <el-button-group style="margin-right: 10px">
            <el-button @click="exportLogs('csv')">{{ t('system.log.exportCsv') }}</el-button>
            <el-button @click="exportLogs('json')">{{ t('system.log.exportJson') }}</el-button>
          </el-button-group>
          <el-button type="primary" :icon="Refresh" @click="loadLogs">{{ t('system.log.refresh') }}</el-button>
        </div>
      </div>
    </template>

    <!-- 高级过滤 -->
    <el-form :inline="true" class="filter-form">
      <el-form-item :label="t('system.log.quickTime')">
        <el-radio-group v-model="quickTimeRange" @change="handleQuickTimeChange">
          <el-radio-button value="">{{ t('system.log.quickAll') }}</el-radio-button>
          <el-radio-button value="today">{{ t('system.log.quickToday') }}</el-radio-button>
          <el-radio-button value="yesterday">{{ t('system.log.quickYesterday') }}</el-radio-button>
          <el-radio-button value="week">{{ t('system.log.quickWeek') }}</el-radio-button>
          <el-radio-button value="month">{{ t('system.log.quickMonth') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('system.log.timeRange')">
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          :range-separator="t('system.log.rangeSeparator')"
          :start-placeholder="t('system.log.startTime')"
          :end-placeholder="t('system.log.endTime')"
          value-format="YYYY-MM-DD HH:mm:ss"
          @change="handleDateRangeChange"
        />
      </el-form-item>

      <el-form-item :label="t('system.log.module')">
        <el-select v-model="moduleFilter" :placeholder="t('system.log.allModules')" clearable @change="loadLogs" style="width: 120px">
          <el-option :label="t('system.log.allOption')" value="" />
          <el-option label="System" value="system" />
          <el-option label="Manager" value="manager" />
          <el-option label="Meta" value="meta" />
          <el-option label="Transfer" value="transfer" />
          <el-option label="Orchestrator" value="orchestrator" />
          <el-option label="Develop" value="develop" />
          <el-option label="Service" value="service" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('system.log.statusCode')">
        <el-select v-model="statusFilter" :placeholder="t('system.log.allStatus')" clearable @change="loadLogs" style="width: 140px">
          <el-option :label="t('system.log.allOption')" value="" />
          <el-option :label="t('system.log.statusSuccess')" value="2xx" />
          <el-option :label="t('system.log.statusClientError')" value="4xx" />
          <el-option :label="t('system.log.statusServerError')" value="5xx" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('system.log.httpMethod')">
        <el-select v-model="methodFilter" :placeholder="t('system.log.allOption')" clearable @change="loadLogs" style="width: 110px">
          <el-option :label="t('system.log.allOption')" value="" />
          <el-option label="POST" value="POST" />
          <el-option label="PUT" value="PUT" />
          <el-option label="DELETE" value="DELETE" />
          <el-option label="PATCH" value="PATCH" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('system.log.username')">
        <el-input v-model="usernameFilter" :placeholder="t('system.log.usernamePlaceholder')" clearable @keyup.enter="loadLogs" style="width: 130px" />
      </el-form-item>

      <el-form-item :label="t('system.log.ipAddress')">
        <el-input v-model="ipFilter" :placeholder="t('system.log.ipPlaceholder')" clearable @keyup.enter="loadLogs" style="width: 130px" />
      </el-form-item>

      <el-form-item>
        <el-button type="primary" @click="loadLogs">{{ t('system.log.query') }}</el-button>
        <el-button @click="resetFilters">{{ t('system.log.reset') }}</el-button>
      </el-form-item>
    </el-form>

    <!-- 日志列表 -->
    <el-table :data="logs" v-loading="loading" stripe @row-click="showDetails">
      <el-table-column prop="id" :label="t('system.log.columns.id')" width="70" />

      <el-table-column :label="t('system.log.columns.action')" min-width="220">
        <template #default="{ row }">
          <el-tag :type="getMethodType(row.http_method)" size="small" style="margin-right: 8px">
            {{ row.http_method }}
          </el-tag>
          <span>{{ formatAction(row.resource_path) }}</span>
        </template>
      </el-table-column>

      <el-table-column prop="username" :label="t('system.log.columns.user')" width="110" />

      <el-table-column :label="t('system.log.columns.status')" width="70">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.http_status)" size="small">
            {{ row.http_status }}
          </el-tag>
        </template>
      </el-table-column>

      <el-table-column :label="t('system.log.columns.duration')" width="80" sortable>
        <template #default="{ row }">
          <span :class="{ 'slow': row.duration_ms > 1000, 'warning': row.duration_ms > 500 }">
            {{ row.duration_ms }}ms
          </span>
        </template>
      </el-table-column>

      <el-table-column prop="ip_address" :label="t('system.log.columns.ip')" width="130" />
      <el-table-column prop="module_name" :label="t('system.log.columns.module')" width="90" />
      <el-table-column :label="t('system.log.columns.time')" width="160" sortable>
        <template #default="{ row }">
          {{ formatDateTime(row.created_at) }}
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :page-sizes="[10, 20, 50, 100]"
      :total="total"
      layout="total, sizes, prev, pager, next, jumper"
      style="margin-top: 20px; justify-content: flex-end"
      @current-change="loadLogs"
      @size-change="loadLogs"
    />

    <!-- 日志详情对话框 -->
    <el-dialog v-model="detailsVisible" :title="t('system.log.detail.title')" width="800px">
      <el-descriptions :column="2" border v-if="currentLog">
        <el-descriptions-item :label="t('system.log.detail.logId')">{{ currentLog.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.log.detail.requestId')">{{ currentLog.request_id }}</el-descriptions-item>

        <el-descriptions-item :label="t('system.log.detail.user')">
          {{ currentLog.username || t('system.log.detail.unauthenticated') }}
          <el-tag v-if="currentLog.username === 'SuperAdmin'" type="danger" size="small" style="margin-left: 8px">
            {{ t('system.log.detail.superAdmin') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.log.detail.tenant')">
          {{ currentLog.tenant_id || t('system.log.detail.systemOp') }}
        </el-descriptions-item>

        <el-descriptions-item :label="t('system.log.detail.action')">
          <el-tag :type="getMethodType(currentLog.http_method)">
            {{ currentLog.http_method }}
          </el-tag>
          <span style="margin-left: 8px">{{ formatAction(currentLog.resource_path) }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.log.detail.result')">
          <el-tag :type="getStatusType(currentLog.http_status)">
            {{ currentLog.http_status }}
          </el-tag>
          <span style="margin-left: 8px" :class="{ 'slow': currentLog.duration_ms > 1000, 'warning': currentLog.duration_ms > 500 }">
            {{ currentLog.duration_ms }}ms
          </span>
        </el-descriptions-item>

        <el-descriptions-item v-if="currentLog.entity_type" :label="t('system.log.detail.resource')" :span="2">
          {{ currentLog.entity_type }} #{{ currentLog.entity_id }}
        </el-descriptions-item>

        <el-descriptions-item :label="t('system.log.detail.source')">
          {{ currentLog.ip_address }} / {{ currentLog.module_name }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.log.detail.time')">
          {{ formatDateTime(currentLog.created_at) }}
        </el-descriptions-item>

        <el-descriptions-item v-if="currentLog.http_status >= 400" :label="t('system.log.detail.error')" :span="2">
          <div class="error-box">{{ currentLog.error_message || t('system.log.detail.noError') }}</div>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider>{{ t('system.log.detail.requestDetail') }}</el-divider>
      <div v-if="currentLog?.request_body" class="code-block">
        <pre>{{ formatJSON(currentLog.request_body) }}</pre>
      </div>
      <div v-else class="empty-text">{{ t('system.log.detail.noRequest') }}</div>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { logsAPI } from '../api/logs'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const { t } = useI18n()

const logs = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

// 过滤条件
const quickTimeRange = ref('')
const dateRange = ref(null)
const moduleFilter = ref('')
const statusFilter = ref('')
const methodFilter = ref('')
const usernameFilter = ref('')
const ipFilter = ref('')

// 日志详情
const detailsVisible = ref(false)
const currentLog = ref(null)

const formatDateTime = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

const formatJSON = (jsonStr) => {
  try {
    const obj = JSON.parse(jsonStr)
    return JSON.stringify(obj, null, 2)
  } catch (e) {
    return jsonStr
  }
}

const formatAction = (action) => {
  if (!action) return '-'
  const key = `system.log.actions.${action.replaceAll('.', '_')}`
  const translated = t(key)
  return translated === key ? action : translated
}

// 根据HTTP状态码返回标签类型
const getStatusType = (status) => {
  if (status >= 200 && status < 300) return 'success'
  if (status >= 400 && status < 500) return 'warning'
  if (status >= 500) return 'danger'
  return 'info'
}

// 根据HTTP方法返回标签类型
const getMethodType = (method) => {
  const map = {
    'POST': 'success',
    'PUT': 'warning',
    'DELETE': 'danger',
    'PATCH': 'info',
  }
  return map[method] || ''
}

// 快速时间范围处理
const handleQuickTimeChange = () => {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())

  switch (quickTimeRange.value) {
    case 'today':
      dateRange.value = [
        formatDateToString(today),
        formatDateToString(now)
      ]
      break
    case 'yesterday':
      const yesterday = new Date(today)
      yesterday.setDate(yesterday.getDate() - 1)
      const yesterdayEnd = new Date(today)
      yesterdayEnd.setSeconds(yesterdayEnd.getSeconds() - 1)
      dateRange.value = [
        formatDateToString(yesterday),
        formatDateToString(yesterdayEnd)
      ]
      break
    case 'week':
      const weekAgo = new Date(today)
      weekAgo.setDate(weekAgo.getDate() - 7)
      dateRange.value = [
        formatDateToString(weekAgo),
        formatDateToString(now)
      ]
      break
    case 'month':
      const monthAgo = new Date(today)
      monthAgo.setDate(monthAgo.getDate() - 30)
      dateRange.value = [
        formatDateToString(monthAgo),
        formatDateToString(now)
      ]
      break
    default:
      dateRange.value = null
  }
  loadLogs()
}

// 日期范围变化处理
const handleDateRangeChange = () => {
  quickTimeRange.value = ''
  loadLogs()
}

// 格式化日期为字符串
const formatDateToString = (date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

const loadLogs = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      page_size: pageSize.value,
    }

    if (dateRange.value && dateRange.value.length === 2) {
      params.start_time = dateRange.value[0]
      params.end_time = dateRange.value[1]
    }
    if (moduleFilter.value) {
      params.module_name = moduleFilter.value
    }
    if (statusFilter.value) {
      params.status_code = statusFilter.value
    }
    if (methodFilter.value) {
      params.http_method = methodFilter.value
    }
    if (usernameFilter.value) {
      params.username = usernameFilter.value
    }
    if (ipFilter.value) {
      params.ip = ipFilter.value
    }

    const response = await logsAPI.list(params)
    logs.value = response.data || []
    total.value = response.total || 0
  } catch (error) {
    ElMessage.error(t('system.log.msg.loadFailed'))
    console.error(error)
  } finally {
    loading.value = false
  }
}

const resetFilters = () => {
  quickTimeRange.value = ''
  dateRange.value = null
  moduleFilter.value = ''
  statusFilter.value = ''
  methodFilter.value = ''
  usernameFilter.value = ''
  ipFilter.value = ''
  currentPage.value = 1
  loadLogs()
}

const showDetails = (row) => {
  currentLog.value = row
  detailsVisible.value = true
}

const exportLogs = (format) => {
  const params = new URLSearchParams({
    format: format
  })

  if (dateRange.value && dateRange.value.length === 2) {
    params.append('start_time', dateRange.value[0])
    params.append('end_time', dateRange.value[1])
  }
  if (moduleFilter.value) {
    params.append('module_name', moduleFilter.value)
  }
  if (statusFilter.value) {
    params.append('status_code', statusFilter.value)
  }
  if (methodFilter.value) {
    params.append('http_method', methodFilter.value)
  }
  if (usernameFilter.value) {
    params.append('username', usernameFilter.value)
  }
  if (ipFilter.value) {
    params.append('ip', ipFilter.value)
  }

  const token = localStorage.getItem('token')
  const url = `/api/logs/export?${params.toString()}`
  const a = document.createElement('a')
  a.href = url
  a.download = `audit_logs_${new Date().toISOString().replace(/[:.]/g, '-')}.${format}`

  fetch(url, {
    headers: {
      'Authorization': `Bearer ${token}`
    }
  })
    .then(response => response.blob())
    .then(blob => {
      const blobUrl = window.URL.createObjectURL(blob)
      a.href = blobUrl
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      window.URL.revokeObjectURL(blobUrl)
      ElMessage.success(t('system.log.msg.exportSuccess', { format: format.toUpperCase() }))
    })
    .catch(() => {
      ElMessage.error(t('system.log.msg.exportFailed'))
    })
}

onMounted(() => {
  loadLogs()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.filter-form {
  margin-bottom: 20px;
  padding: 15px;
  background: var(--addp-bg-primary) !important;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
}

.code-block {
  background: var(--addp-bg-secondary) !important;
  padding: 15px;
  border-radius: 4px;
  max-height: 400px;
  overflow-y: auto;
  border: 1px solid var(--addp-border-color);
}

.code-block pre {
  margin: 0;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-wrap: break-word;
  color: var(--addp-text-primary);
}

.empty-text {
  text-align: center;
  color: var(--addp-text-tertiary);
  padding: 20px;
}

.error-box {
  color: var(--el-color-danger);
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
  background: var(--addp-bg-secondary) !important;
  padding: 10px;
  border-radius: 4px;
  border: 1px solid var(--el-color-danger);
}

/* ✅ 慢请求高亮样式 */
.slow {
  color: var(--el-color-danger);
  font-weight: bold;
}

.warning {
  color: var(--el-color-warning);
  font-weight: 600;
}
</style>
