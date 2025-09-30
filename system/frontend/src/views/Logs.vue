<template>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>日志管理</span>
          <el-button type="primary" :icon="Refresh" @click="loadLogs">刷新</el-button>
        </div>
      </template>

      <el-table :data="logs" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="username" label="用户" width="120" />
        <el-table-column prop="action" label="操作" min-width="200" />
        <el-table-column prop="resource_type" label="资源类型" width="120" />
        <el-table-column prop="ip_address" label="IP地址" width="150" />
        <el-table-column label="时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        style="margin-top: 20px; justify-content: flex-end"
        @current-change="loadLogs"
      />
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

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

const loadLogs = async () => {
  loading.value = true
  try {
    const response = await logsAPI.list(currentPage.value, pageSize.value)
    logs.value = response.data
    total.value = response.data.length
  } catch (error) {
    ElMessage.error('加载日志列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
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
</style>