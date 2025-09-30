<template>
  <div class="data-management">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>数据源管理</span>
          <el-button type="primary" :icon="Refresh" @click="syncDataSources" :loading="syncing">
            从存储引擎同步
          </el-button>
        </div>
      </template>

      <el-table :data="dataSources" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column prop="resource_type" label="类型" width="150">
          <template #default="{ row }">
            <el-tag :type="getTypeColor(row.resource_type)">
              {{ getTypeLabel(row.resource_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusColor(row.status)">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后检查" width="180">
          <template #default="{ row }">
            {{ row.last_checked ? formatDate(row.last_checked) : '未检查' }}
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary">打开</el-button>
            <el-button size="small" type="danger" @click="deleteDataSource(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        style="margin-top: 20px; justify-content: flex-end"
        @current-change="loadDataSources"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { managerAPI } from '../api/manager'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const dataSources = ref([])
const loading = ref(false)
const syncing = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const typeMap = {
  'postgresql': 'PostgreSQL',
  'minio': 'MinIO',
  'mysql': 'MySQL'
}

const getTypeLabel = (type) => {
  return typeMap[type] || type
}

const getTypeColor = (type) => {
  const colorMap = {
    'postgresql': 'primary',
    'minio': 'warning',
    'mysql': 'success'
  }
  return colorMap[type] || ''
}

const getStatusLabel = (status) => {
  const statusMap = {
    'active': '活跃',
    'inactive': '未激活',
    'error': '错误'
  }
  return statusMap[status] || status
}

const getStatusColor = (status) => {
  const colorMap = {
    'active': 'success',
    'inactive': 'info',
    'error': 'danger'
  }
  return colorMap[status] || ''
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

const loadDataSources = async () => {
  loading.value = true
  try {
    const response = await managerAPI.getDataSources(currentPage.value, pageSize.value)
    dataSources.value = response.data.data || []
    total.value = response.data.total || 0
  } catch (error) {
    ElMessage.error('加载数据源失败: ' + (error.response?.data?.error || error.message))
    console.error(error)
  } finally {
    loading.value = false
  }
}

const syncDataSources = async () => {
  syncing.value = true
  try {
    await managerAPI.syncDataSources()
    ElMessage.success('同步成功')
    loadDataSources()
  } catch (error) {
    ElMessage.error('同步失败: ' + (error.response?.data?.error || error.message))
    console.error(error)
  } finally {
    syncing.value = false
  }
}

const deleteDataSource = (row) => {
  ElMessageBox.confirm(`确定要删除数据源 "${row.name}" 吗？`, '确认删除', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    try {
      await managerAPI.deleteDataSource(row.id)
      ElMessage.success('删除成功')
      loadDataSources()
    } catch (error) {
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
    }
  }).catch(() => {})
}

onMounted(() => {
  loadDataSources()
})
</script>

<style scoped>
.data-management {
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}
</style>