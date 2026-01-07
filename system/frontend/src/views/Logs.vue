<template>
  <el-card>
    <template #header>
      <div class="card-header">
        <span>审计日志</span>
        <div>
          <el-button-group style="margin-right: 10px">
            <el-button @click="exportLogs('csv')">导出 CSV</el-button>
            <el-button @click="exportLogs('json')">导出 JSON</el-button>
          </el-button-group>
          <el-button type="primary" :icon="Refresh" @click="loadLogs">刷新</el-button>
        </div>
      </div>
    </template>

    <!-- 高级过滤 -->
    <el-form :inline="true" class="filter-form">
      <el-form-item label="快速时间">
        <el-radio-group v-model="quickTimeRange" @change="handleQuickTimeChange">
          <el-radio-button label="">全部</el-radio-button>
          <el-radio-button label="today">今天</el-radio-button>
          <el-radio-button label="yesterday">昨天</el-radio-button>
          <el-radio-button label="week">最近7天</el-radio-button>
          <el-radio-button label="month">最近30天</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item label="时间范围">
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          value-format="YYYY-MM-DD HH:mm:ss"
          @change="handleDateRangeChange"
        />
      </el-form-item>

      <el-form-item label="模块">
        <el-select v-model="moduleFilter" placeholder="全部模块" clearable @change="loadLogs" style="width: 120px">
          <el-option label="全部" value="" />
          <el-option label="System" value="system" />
          <el-option label="Manager" value="manager" />
          <el-option label="Meta" value="meta" />
          <el-option label="Transfer" value="transfer" />
          <el-option label="Orchestrator" value="orchestrator" />
          <el-option label="Develop" value="develop" />
          <el-option label="Service" value="service" />
        </el-select>
      </el-form-item>

      <el-form-item label="状态码">
        <el-select v-model="statusFilter" placeholder="全部状态" clearable @change="loadLogs" style="width: 140px">
          <el-option label="全部" value="" />
          <el-option label="成功 (2xx)" value="2xx" />
          <el-option label="客户端错误 (4xx)" value="4xx" />
          <el-option label="服务器错误 (5xx)" value="5xx" />
        </el-select>
      </el-form-item>

      <el-form-item label="HTTP方法">
        <el-select v-model="methodFilter" placeholder="全部" clearable @change="loadLogs" style="width: 110px">
          <el-option label="全部" value="" />
          <el-option label="POST" value="POST" />
          <el-option label="PUT" value="PUT" />
          <el-option label="DELETE" value="DELETE" />
          <el-option label="PATCH" value="PATCH" />
        </el-select>
      </el-form-item>

      <el-form-item label="用户名">
        <el-input v-model="usernameFilter" placeholder="输入用户名" clearable @keyup.enter="loadLogs" style="width: 130px" />
      </el-form-item>

      <el-form-item label="IP地址">
        <el-input v-model="ipFilter" placeholder="输入IP" clearable @keyup.enter="loadLogs" style="width: 130px" />
      </el-form-item>

      <el-form-item>
        <el-button type="primary" @click="loadLogs">查询</el-button>
        <el-button @click="resetFilters">重置</el-button>
      </el-form-item>
    </el-form>

    <!-- 日志列表 -->
    <el-table :data="logs" v-loading="loading" stripe @row-click="showDetails">
      <el-table-column prop="id" label="ID" width="70" />

      <!-- 操作: HTTP方法 + 路径 -->
      <el-table-column label="操作" min-width="220">
        <template #default="{ row }">
          <el-tag :type="getMethodType(row.http_method)" size="small" style="margin-right: 8px">
            {{ row.http_method }}
          </el-tag>
          <span>{{ row.resource_path }}</span>
        </template>
      </el-table-column>

      <el-table-column prop="username" label="用户" width="110" />

      <!-- 状态码 (100%有数据,颜色标识) -->
      <el-table-column label="状态" width="70">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.http_status)" size="small">
            {{ row.http_status }}
          </el-tag>
        </template>
      </el-table-column>

      <!-- 耗时 (慢请求高亮) -->
      <el-table-column label="耗时" width="80" sortable>
        <template #default="{ row }">
          <span :class="{ 'slow': row.duration_ms > 1000, 'warning': row.duration_ms > 500 }">
            {{ row.duration_ms }}ms
          </span>
        </template>
      </el-table-column>

      <el-table-column prop="ip_address" label="IP" width="130" />
      <el-table-column prop="module_name" label="模块" width="90" />
      <el-table-column label="时间" width="160" sortable>
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
    <el-dialog v-model="detailsVisible" title="操作详情" width="800px">
      <el-descriptions :column="2" border v-if="currentLog">
        <!-- 始终显示的字段 -->
        <el-descriptions-item label="日志ID">{{ currentLog.id }}</el-descriptions-item>
        <el-descriptions-item label="请求ID">{{ currentLog.request_id }}</el-descriptions-item>

        <!-- SuperAdmin标识 -->
        <el-descriptions-item label="操作用户">
          {{ currentLog.username || '未认证' }}
          <el-tag v-if="currentLog.username === 'SuperAdmin'" type="danger" size="small" style="margin-left: 8px">
            超管
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="所属租户">
          {{ currentLog.tenant_id || '系统级操作' }}
        </el-descriptions-item>

        <!-- 操作和结果 -->
        <el-descriptions-item label="操作">
          <el-tag :type="getMethodType(currentLog.http_method)">
            {{ currentLog.http_method }}
          </el-tag>
          <span style="margin-left: 8px">{{ currentLog.resource_path }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="结果">
          <el-tag :type="getStatusType(currentLog.http_status)">
            {{ currentLog.http_status }}
          </el-tag>
          <span style="margin-left: 8px" :class="{ 'slow': currentLog.duration_ms > 1000, 'warning': currentLog.duration_ms > 500 }">
            {{ currentLog.duration_ms }}ms
          </span>
        </el-descriptions-item>

        <!-- 条件显示: 仅在有值时显示 -->
        <el-descriptions-item v-if="currentLog.entity_type" label="操作资源" :span="2">
          {{ currentLog.entity_type }} #{{ currentLog.entity_id }}
        </el-descriptions-item>

        <el-descriptions-item label="来源">
          {{ currentLog.ip_address }} / {{ currentLog.module_name }}
        </el-descriptions-item>
        <el-descriptions-item label="时间">
          {{ formatDateTime(currentLog.created_at) }}
        </el-descriptions-item>

        <!-- 错误信息: 仅失败时显示 -->
        <el-descriptions-item v-if="currentLog.http_status >= 400" label="错误信息" :span="2">
          <div class="error-box">{{ currentLog.error_message || '无详细信息' }}</div>
        </el-descriptions-item>
      </el-descriptions>

      <!-- 请求详情: JSON展示 -->
      <el-divider>请求详情</el-divider>
      <div v-if="currentLog?.request_body" class="code-block">
        <pre>{{ formatJSON(currentLog.request_body) }}</pre>
      </div>
      <div v-else class="empty-text">无请求详情</div>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { logsAPI } from '../api/logs'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

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
    ElMessage.error('加载日志列表失败')
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
      ElMessage.success(`导出 ${format.toUpperCase()} 成功`)
    })
    .catch(() => {
      ElMessage.error('导出失败')
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
  background-color: #f5f7fa;
  border-radius: 4px;
}

.code-block {
  background-color: #f5f5f5;
  padding: 15px;
  border-radius: 4px;
  max-height: 400px;
  overflow-y: auto;
}

.code-block pre {
  margin: 0;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.empty-text {
  text-align: center;
  color: #999;
  padding: 20px;
}

.error-box {
  color: #f56c6c;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
  background-color: #fef0f0;
  padding: 10px;
  border-radius: 4px;
}

/* ✅ 慢请求高亮样式 */
.slow {
  color: #F56C6C;
  font-weight: bold;
}

.warning {
  color: #E6A23C;
  font-weight: 600;
}
</style>
