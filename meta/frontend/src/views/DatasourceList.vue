<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <span>数据源列表</span>
          <el-button type="primary" @click="handleAutoSyncAll" :loading="syncing">
            <el-icon><Refresh /></el-icon> 全量同步
          </el-button>
        </div>
      </template>

      <el-table :data="datasources" border v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="datasource_name" label="数据源名称" width="200" />
        <el-table-column prop="datasource_type" label="类型" width="120">
          <template #default="{ row }">
            <el-tag>{{ row.datasource_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sync_status" label="同步状态" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.sync_status === 'success'" type="success">成功</el-tag>
            <el-tag v-else-if="row.sync_status === 'syncing'" type="warning">同步中</el-tag>
            <el-tag v-else-if="row.sync_status === 'failed'" type="danger">失败</el-tag>
            <el-tag v-else type="info">{{ row.sync_status || '未同步' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sync_level" label="同步级别" width="120" />
        <el-table-column prop="last_sync_at" label="最后同步时间" width="180">
          <template #default="{ row }">
            {{ row.last_sync_at || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180" />
        <el-table-column label="操作" fixed="right" width="200">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleSync(row)">
              同步
            </el-button>
            <el-button type="success" size="small" @click="handleViewMetadata(row)">
              查看元数据
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 同步日志 -->
    <el-card style="margin-top: 20px">
      <template #header>
        <span>同步日志</span>
      </template>

      <el-table :data="syncLogs" border v-loading="loadingLogs">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="sync_type" label="类型" width="100" />
        <el-table-column prop="sync_level" label="级别" width="100" />
        <el-table-column prop="target_database" label="目标数据库" width="150" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'success'" type="success">成功</el-tag>
            <el-tag v-else-if="row.status === 'running'" type="warning">运行中</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger">失败</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="duration_seconds" label="耗时(秒)" width="100" />
        <el-table-column prop="databases_scanned" label="数据库" width="80" />
        <el-table-column prop="tables_scanned" label="表" width="80" />
        <el-table-column prop="fields_scanned" label="字段" width="80" />
        <el-table-column prop="started_at" label="开始时间" width="180" />
        <el-table-column prop="error_message" label="错误信息" show-overflow-tooltip />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import metaApi from '../api/meta'

const router = useRouter()
const datasources = ref([])
const syncLogs = ref([])
const loading = ref(false)
const loadingLogs = ref(false)
const syncing = ref(false)

// 加载数据源列表
const loadDatasources = async () => {
  loading.value = true
  try {
    const res = await metaApi.getDatasources()
    datasources.value = res.data
  } catch (error) {
    ElMessage.error('加载数据源列表失败')
  } finally {
    loading.value = false
  }
}

// 加载同步日志
const loadSyncLogs = async () => {
  loadingLogs.value = true
  try {
    const res = await metaApi.getSyncLogs({ limit: 20 })
    syncLogs.value = res.data
  } catch (error) {
    ElMessage.error('加载同步日志失败')
  } finally {
    loadingLogs.value = false
  }
}

// 同步单个数据源
const handleSync = async (row) => {
  try {
    await metaApi.syncResource(row.resource_id)
    ElMessage.success('同步请求已发送')
    setTimeout(() => {
      loadDatasources()
      loadSyncLogs()
    }, 2000)
  } catch (error) {
    ElMessage.error('同步失败')
  }
}

// 全量同步
const handleAutoSyncAll = async () => {
  syncing.value = true
  try {
    await metaApi.autoSyncAll()
    ElMessage.success('全量同步请求已发送')
    setTimeout(() => {
      loadDatasources()
      loadSyncLogs()
    }, 2000)
  } catch (error) {
    ElMessage.error('同步失败')
  } finally {
    syncing.value = false
  }
}

// 查看元数据
const handleViewMetadata = (row) => {
  router.push('/metadata')
}

onMounted(() => {
  loadDatasources()
  loadSyncLogs()
})
</script>
