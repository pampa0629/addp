<template>
  <div class="metadata-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <h2>元数据管理</h2>
          <p class="subtitle">浏览和管理数据库表及对象存储文件</p>
        </div>
      </template>

      <!-- 数据源选择器 -->
      <div class="datasource-selector">
        <el-select
          v-model="selectedDataSourceId"
          placeholder="选择数据源"
          @change="handleDataSourceChange"
          size="large"
          style="width: 300px"
        >
          <el-option
            v-for="ds in dataSources"
            :key="ds.id"
            :label="ds.name"
            :value="ds.id"
          >
            <span>{{ ds.name }}</span>
            <el-tag size="small" type="info" style="margin-left: 8px">{{ ds.resource_type }}</el-tag>
          </el-option>
        </el-select>

        <el-button
          type="primary"
          @click="handleScanMetadata"
          :loading="scanning"
          :disabled="!selectedDataSourceId"
        >
          <el-icon><Refresh /></el-icon>
          扫描元数据
        </el-button>
      </div>

      <!-- 数据库表管理 -->
      <div v-if="selectedDataSource && selectedDataSource.resource_type === 'postgresql'" class="table-section">
        <div class="section-header">
          <h3>数据库表</h3>
          <div class="filter-group">
            <el-radio-group v-model="tableFilter" @change="loadTables">
              <el-radio-button label="all">全部 ({{ scanResult?.total_items || 0 }})</el-radio-button>
              <el-radio-button label="managed">已纳管 ({{ scanResult?.managed_items || 0 }})</el-radio-button>
              <el-radio-button label="unmanaged">未纳管 ({{ scanResult?.unmanaged_items || 0 }})</el-radio-button>
            </el-radio-group>
          </div>
        </div>

        <el-table
          :data="tables"
          v-loading="loadingTables"
          stripe
          style="width: 100%; margin-top: 16px"
        >
          <el-table-column prop="full_name" label="表名" min-width="200">
            <template #default="{ row }">
              <el-tooltip :content="`${row.schema_name}.${row.table_name}`" placement="top">
                <span>{{ row.full_name }}</span>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column prop="table_type" label="类型" width="120">
            <template #default="{ row }">
              <el-tag size="small" :type="row.table_type === 'BASE TABLE' ? '' : 'info'">
                {{ row.table_type }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="table_size" label="大小" width="120">
            <template #default="{ row }">
              {{ formatBytes(row.table_size) }}
            </template>
          </el-table-column>
          <el-table-column prop="row_count" label="行数" width="120">
            <template #default="{ row }">
              {{ row.row_count !== null && row.row_count !== undefined ? row.row_count.toLocaleString() : '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="is_managed" label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.is_managed ? 'success' : 'info'" size="small">
                {{ row.is_managed ? '已纳管' : '未纳管' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="last_scanned" label="最后扫描" width="180">
            <template #default="{ row }">
              {{ row.last_scanned ? formatDateTime(row.last_scanned) : '-' }}
            </template>
          </el-table-column>
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="!row.is_managed"
                type="primary"
                size="small"
                @click="handleManageTable(row)"
                :loading="managingTableId === row.id"
              >
                纳管
              </el-button>
              <el-button
                v-else
                type="warning"
                size="small"
                @click="handleUnmanageTable(row)"
                :loading="managingTableId === row.id"
              >
                取消纳管
              </el-button>
              <el-button
                v-if="row.is_managed"
                type="info"
                size="small"
                @click="handleViewMetadata(row)"
              >
                查看元数据
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 对象存储管理（暂未实现） -->
      <div v-if="selectedDataSource && selectedDataSource.resource_type === 'minio'" class="minio-section">
        <el-empty description="对象存储元数据管理功能开发中">
          <el-tag type="info">MinIO 元数据扫描功能即将推出</el-tag>
        </el-empty>
      </div>

      <!-- 未选择数据源 -->
      <div v-if="!selectedDataSourceId" class="empty-state">
        <el-empty description="请先选择一个数据源进行元数据管理" />
      </div>
    </el-card>

    <!-- 元数据详情对话框 -->
    <el-dialog
      v-model="metadataDialogVisible"
      title="表元数据详情"
      width="80%"
      destroy-on-close
    >
      <div v-if="selectedTable" class="metadata-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="完整表名">{{ selectedTable.full_name }}</el-descriptions-item>
          <el-descriptions-item label="表类型">{{ selectedTable.table_type }}</el-descriptions-item>
          <el-descriptions-item label="Schema">{{ selectedTable.schema_name }}</el-descriptions-item>
          <el-descriptions-item label="表名">{{ selectedTable.table_name }}</el-descriptions-item>
          <el-descriptions-item label="行数">{{ selectedTable.row_count?.toLocaleString() || '-' }}</el-descriptions-item>
          <el-descriptions-item label="大小">{{ formatBytes(selectedTable.table_size) }}</el-descriptions-item>
          <el-descriptions-item label="纳管时间" :span="2">
            {{ selectedTable.last_managed ? formatDateTime(selectedTable.last_managed) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="注释" :span="2">
            {{ selectedTable.comment || '无' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-divider>字段信息</el-divider>
        <el-table
          v-if="selectedTable.schema"
          :data="parseSchema(selectedTable.schema)"
          border
          style="width: 100%"
        >
          <el-table-column prop="name" label="字段名" width="200" />
          <el-table-column prop="data_type" label="数据类型" width="150" />
          <el-table-column prop="is_nullable" label="可空" width="80">
            <template #default="{ row }">
              <el-tag :type="row.is_nullable ? 'info' : 'warning'" size="small">
                {{ row.is_nullable ? '是' : '否' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="is_primary_key" label="主键" width="80">
            <template #default="{ row }">
              <el-icon v-if="row.is_primary_key" color="#67C23A"><Key /></el-icon>
            </template>
          </el-table-column>
          <el-table-column prop="default_value" label="默认值" min-width="150">
            <template #default="{ row }">
              {{ row.default_value || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="comment" label="注释" min-width="200">
            <template #default="{ row }">
              {{ row.comment || '-' }}
            </template>
          </el-table-column>
        </el-table>

        <el-divider>采样数据 (前10行)</el-divider>
        <div v-if="selectedTable.sample_data" class="sample-data">
          <el-table
            :data="parseSampleData(selectedTable.sample_data)"
            border
            max-height="400"
            style="width: 100%"
          >
            <el-table-column
              v-for="column in getSampleDataColumns(selectedTable.sample_data)"
              :key="column"
              :prop="column"
              :label="column"
              min-width="120"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ formatCellValue(row[column]) }}
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { managerAPI } from '../api/manager'

const dataSources = ref([])
const selectedDataSourceId = ref(null)
const selectedDataSource = computed(() => {
  return dataSources.value.find(ds => ds.id === selectedDataSourceId.value)
})

const scanning = ref(false)
const scanResult = ref(null)

const tableFilter = ref('all')
const tables = ref([])
const loadingTables = ref(false)
const managingTableId = ref(null)

const metadataDialogVisible = ref(false)
const selectedTable = ref(null)

// 加载数据源列表
const loadDataSources = async () => {
  try {
    const response = await managerAPI.getDataSources(1, 100)
    dataSources.value = response.data.data || []

    // 如果只有一个数据源，自动选中
    if (dataSources.value.length === 1) {
      selectedDataSourceId.value = dataSources.value[0].id
      handleDataSourceChange()
    }
  } catch (error) {
    console.error('加载数据源失败:', error)
    ElMessage.error('加载数据源失败: ' + (error.response?.data?.error || error.message))
  }
}

// 数据源切换
const handleDataSourceChange = () => {
  scanResult.value = null
  tables.value = []
  tableFilter.value = 'all'

  if (selectedDataSourceId.value) {
    loadTables()
  }
}

// 扫描元数据
const handleScanMetadata = async () => {
  if (!selectedDataSourceId.value) return

  scanning.value = true
  try {
    const response = await managerAPI.scanDataSource(selectedDataSourceId.value)
    scanResult.value = response.data

    ElMessage.success(`扫描完成! 共发现 ${response.data.total_items} 个表`)

    // 重新加载表列表
    loadTables()
  } catch (error) {
    console.error('扫描元数据失败:', error)
    ElMessage.error('扫描失败: ' + (error.response?.data?.error || error.message))
  } finally {
    scanning.value = false
  }
}

// 加载表列表
const loadTables = async () => {
  if (!selectedDataSourceId.value) return

  loadingTables.value = true
  try {
    const isManaged = tableFilter.value === 'all' ? null : tableFilter.value === 'managed'
    const response = await managerAPI.getTables(selectedDataSourceId.value, isManaged)
    tables.value = response.data.data || []

    // 更新统计信息
    if (!scanResult.value) {
      const allTables = await managerAPI.getTables(selectedDataSourceId.value, null)
      const managedTables = await managerAPI.getTables(selectedDataSourceId.value, true)
      scanResult.value = {
        total_items: allTables.data.total,
        managed_items: managedTables.data.total,
        unmanaged_items: allTables.data.total - managedTables.data.total
      }
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    ElMessage.error('加载失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingTables.value = false
  }
}

// 纳管表
const handleManageTable = async (table) => {
  managingTableId.value = table.id
  try {
    await managerAPI.manageTable(table.id)
    ElMessage.success('表已纳管，正在提取详细元数据...')

    // 重新加载表列表
    await loadTables()
  } catch (error) {
    console.error('纳管表失败:', error)
    ElMessage.error('纳管失败: ' + (error.response?.data?.error || error.message))
  } finally {
    managingTableId.value = null
  }
}

// 取消纳管表
const handleUnmanageTable = async (table) => {
  managingTableId.value = table.id
  try {
    await managerAPI.unmanageTable(table.id)
    ElMessage.success('已取消纳管')

    // 重新加载表列表
    await loadTables()
  } catch (error) {
    console.error('取消纳管失败:', error)
    ElMessage.error('取消纳管失败: ' + (error.response?.data?.error || error.message))
  } finally {
    managingTableId.value = null
  }
}

// 查看元数据详情
const handleViewMetadata = (table) => {
  selectedTable.value = table
  metadataDialogVisible.value = true
}

// 格式化字节数
const formatBytes = (bytes) => {
  if (!bytes) return '-'
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  if (bytes === 0) return '0 B'
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + ' ' + sizes[i]
}

// 格式化日期时间
const formatDateTime = (datetime) => {
  if (!datetime) return '-'
  return new Date(datetime).toLocaleString('zh-CN')
}

// 解析 schema JSON
const parseSchema = (schemaJson) => {
  if (!schemaJson) return []
  try {
    if (typeof schemaJson === 'string') {
      return JSON.parse(schemaJson)
    }
    return schemaJson
  } catch (e) {
    console.error('解析 schema 失败:', e)
    return []
  }
}

// 解析采样数据
const parseSampleData = (sampleDataJson) => {
  if (!sampleDataJson) return []
  try {
    if (typeof sampleDataJson === 'string') {
      return JSON.parse(sampleDataJson)
    }
    return sampleDataJson
  } catch (e) {
    console.error('解析采样数据失败:', e)
    return []
  }
}

// 获取采样数据的列名
const getSampleDataColumns = (sampleDataJson) => {
  const data = parseSampleData(sampleDataJson)
  if (!data || data.length === 0) return []
  return Object.keys(data[0])
}

// 格式化单元格值
const formatCellValue = (value) => {
  if (value === null) return 'NULL'
  if (value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

onMounted(() => {
  loadDataSources()
})
</script>

<style scoped>
.metadata-container {
  padding: 20px;
}

.card-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-header h2 {
  margin: 0;
  font-size: 20px;
  color: #303133;
}

.subtitle {
  margin: 0;
  font-size: 14px;
  color: #909399;
}

.datasource-selector {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-bottom: 24px;
}

.table-section,
.minio-section {
  margin-top: 24px;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.section-header h3 {
  margin: 0;
  font-size: 16px;
  color: #303133;
}

.filter-group {
  display: flex;
  gap: 8px;
  align-items: center;
}

.empty-state {
  margin-top: 60px;
}

.metadata-detail {
  padding: 20px 0;
}

.sample-data {
  max-height: 400px;
  overflow: auto;
}
</style>
