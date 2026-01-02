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
      <el-form-item label="时间范围">
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          value-format="YYYY-MM-DD HH:mm:ss"
          @change="loadLogs"
        />
      </el-form-item>

      <el-form-item label="操作类型">
        <el-select v-model="actionFilter" placeholder="全部操作" clearable @change="loadLogs">
          <el-option label="全部" value="" />
          <el-option label="创建 (POST)" value="POST" />
          <el-option label="更新 (PUT)" value="PUT" />
          <el-option label="删除 (DELETE)" value="DELETE" />
          <el-option label="修改 (PATCH)" value="PATCH" />
        </el-select>
      </el-form-item>

      <el-form-item label="资源类型">
        <el-select v-model="entityFilter" placeholder="全部资源" clearable @change="loadLogs">
          <el-option label="全部" value="" />
          <el-option label="用户" value="user" />
          <el-option label="引擎" value="engine" />
          <el-option label="租户" value="tenant" />
        </el-select>
      </el-form-item>

      <el-form-item label="用户名">
        <el-input v-model="usernameFilter" placeholder="输入用户名" clearable @keyup.enter="loadLogs" style="width: 150px" />
      </el-form-item>

      <el-form-item label="IP地址">
        <el-input v-model="ipFilter" placeholder="输入IP" clearable @keyup.enter="loadLogs" style="width: 150px" />
      </el-form-item>

      <el-form-item>
        <el-button type="primary" @click="loadLogs">查询</el-button>
        <el-button @click="resetFilters">重置</el-button>
      </el-form-item>
    </el-form>

    <!-- 日志列表 -->
    <el-table :data="logs" v-loading="loading" stripe @row-click="showDetails">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="username" label="用户" width="120" />
      <el-table-column prop="action" label="操作" min-width="200" />
      <el-table-column prop="entity_type" label="资源类型" width="120" />
      <el-table-column prop="ip_address" label="IP地址" width="150" />
      <el-table-column label="时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <el-pagination
      v-model:current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      layout="total, prev, pager, next"
      style="margin-top: 20px; justify-content: flex-end"
      @current-change="loadLogs"
    />

    <!-- 日志详情对话框 -->
    <el-dialog v-model="detailsVisible" title="日志详情" width="800px">
      <el-descriptions :column="2" border v-if="currentLog">
        <el-descriptions-item label="日志ID">{{ currentLog.id }}</el-descriptions-item>
        <el-descriptions-item label="用户">{{ currentLog.username || '未知' }}</el-descriptions-item>
        <el-descriptions-item label="操作">{{ currentLog.action }}</el-descriptions-item>
        <el-descriptions-item label="资源类型">{{ currentLog.entity_type || '-' }}</el-descriptions-item>
        <el-descriptions-item label="资源ID">{{ currentLog.entity_id || '-' }}</el-descriptions-item>
        <el-descriptions-item label="IP地址">{{ currentLog.ip_address }}</el-descriptions-item>
        <el-descriptions-item label="操作时间" :span="2">{{ formatDate(currentLog.created_at) }}</el-descriptions-item>
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
const dateRange = ref(null)
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
  dateRange.value = null
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
</style>