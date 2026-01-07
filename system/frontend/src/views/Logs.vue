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

      <el-form-item label="操作类型">
        <el-select v-model="actionFilter" placeholder="全部操作" clearable @change="loadLogs" style="width: 130px">
          <el-option label="全部" value="" />
          <el-option label="创建 (POST)" value="POST" />
          <el-option label="更新 (PUT)" value="PUT" />
          <el-option label="删除 (DELETE)" value="DELETE" />
          <el-option label="修改 (PATCH)" value="PATCH" />
        </el-select>
      </el-form-item>

      <el-form-item label="资源类型">
        <el-select v-model="entityFilter" placeholder="全部资源" clearable @change="loadLogs" style="width: 120px">
          <el-option label="全部" value="" />
          <el-option label="用户" value="user" />
          <el-option label="引擎" value="engine" />
          <el-option label="租户" value="tenant" />
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
      <el-table-column prop="module_name" label="模块" width="100" />
      <el-table-column prop="username" label="用户" width="110" />
      <el-table-column prop="action" label="操作" min-width="180" />
      <el-table-column prop="entity_type" label="资源类型" width="110" />
      <el-table-column label="状态码" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.http_status" :type="getStatusType(row.http_status)" size="small">
            {{ row.http_status }}
          </el-tag>
          <span v-else style="color: #909399">-</span>
        </template>
      </el-table-column>
      <el-table-column label="耗时" width="80">
        <template #default="{ row }">
          <span v-if="row.duration_ms !== null && row.duration_ms !== undefined">
            {{ row.duration_ms }}ms
          </span>
          <span v-else style="color: #909399">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="ip_address" label="IP地址" width="135" />
      <el-table-column label="时间" width="165">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
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
    <el-dialog v-model="detailsVisible" title="日志详情" width="900px">
      <el-descriptions :column="2" border v-if="currentLog">
        <el-descriptions-item label="日志ID">{{ currentLog.id }}</el-descriptions-item>
        <el-descriptions-item label="请求ID">{{ currentLog.request_id || '-' }}</el-descriptions-item>
        <el-descriptions-item label="模块名称">{{ currentLog.module_name || '-' }}</el-descriptions-item>
        <el-descriptions-item label="用户">{{ currentLog.username || '未知' }}</el-descriptions-item>
        <el-descriptions-item label="操作" :span="2">{{ currentLog.action }}</el-descriptions-item>
        <el-descriptions-item label="资源类型">{{ currentLog.entity_type || '-' }}</el-descriptions-item>
        <el-descriptions-item label="资源ID">{{ currentLog.entity_id || '-' }}</el-descriptions-item>
        <el-descriptions-item label="HTTP状态码">
          <el-tag v-if="currentLog.http_status" :type="getStatusType(currentLog.http_status)">
            {{ currentLog.http_status }}
          </el-tag>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="请求耗时">
          {{ currentLog.duration_ms !== null && currentLog.duration_ms !== undefined ? currentLog.duration_ms + 'ms' : '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="日志级别">
          <el-tag v-if="currentLog.log_level" :type="getLogLevelType(currentLog.log_level)">
            {{ currentLog.log_level }}
          </el-tag>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="IP地址">{{ currentLog.ip_address }}</el-descriptions-item>
        <el-descriptions-item label="操作时间" :span="2">{{ formatDate(currentLog.created_at) }}</el-descriptions-item>
        <el-descriptions-item v-if="currentLog.error_message" label="错误信息" :span="2">
          <div class="error-message">{{ currentLog.error_message }}</div>
        </el-descriptions-item>
      </el-descriptions>

      <!-- 请求详情 -->
      <el-divider>请求详情</el-divider>
      <div v-if="currentLog?.details" class="code-block">
        <pre>{{ formatJSON(currentLog.details) }}</pre>
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
const actionFilter = ref('')
const entityFilter = ref('')
const usernameFilter = ref('')
const ipFilter = ref('')

// 日志详情
const detailsVisible = ref(false)
const currentLog = ref(null)

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
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
  if (status >= 300 && status < 400) return 'info'
  if (status >= 400 && status < 500) return 'warning'
  if (status >= 500) return 'danger'
  return 'info'
}

// 根据日志级别返回标签类型
const getLogLevelType = (level) => {
  switch (level) {
    case 'INFO': return 'info'
    case 'WARN': return 'warning'
    case 'ERROR': return 'danger'
    default: return 'info'
  }
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
  // 如果用户手动选择了日期范围，清空快速时间选择
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
    // 构建查询参数
    const params = {
      page: currentPage.value,
      page_size: pageSize.value,
    }

    // 添加过滤条件
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
    if (actionFilter.value) {
      params.action = actionFilter.value
    }
    if (entityFilter.value) {
      params.entity_type = entityFilter.value
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
  actionFilter.value = ''
  entityFilter.value = ''
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
  // 构建查询参数（与查询接口相同）
  const params = new URLSearchParams({
    format: format
  })

  // 添加过滤条件
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
  if (actionFilter.value) {
    params.append('action', actionFilter.value)
  }
  if (entityFilter.value) {
    params.append('entity_type', entityFilter.value)
  }
  if (usernameFilter.value) {
    params.append('username', usernameFilter.value)
  }
  if (ipFilter.value) {
    params.append('ip', ipFilter.value)
  }

  // 获取 token
  const token = localStorage.getItem('token')

  // 创建下载链接
  const url = `/api/logs/export?${params.toString()}`
  const a = document.createElement('a')
  a.href = url
  a.download = `audit_logs_${new Date().toISOString().replace(/[:.]/g, '-')}.${format}`

  // 使用 fetch 下载（支持 Authorization header）
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

.error-message {
  color: #f56c6c;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}
</style>