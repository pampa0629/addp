<template>
  <div class="metadata-scan">
    <el-card>
      <template #header>
        <div class="header">
          <span>元数据扫描</span>
          <el-button type="primary" @click="handleAutoScan" :loading="autoScanning">
            <el-icon>
              <Search />
            </el-icon>
            一键扫描未扫描资源
          </el-button>
        </div>
      </template>

      <div class="scan-container">
        <div class="left-panel">
          <div class="panel-header">
            <h3>存储引擎列表</h3>
          </div>
          <el-table
            :data="resources"
            v-loading="loadingResources"
            highlight-current-row
            @row-click="handleSelectResource"
            height="600"
          >
            <el-table-column type="index" label="#" width="50" />
            <el-table-column prop="name" label="名称" width="150" />
            <el-table-column prop="resource_type" label="类型" width="110">
              <template #default="{ row }">
                <el-tag>{{ row.resource_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Schema统计" width="140">
              <template #default="{ row }">
                <div>总数: {{ row.total_schemas || 0 }}</div>
                <div style="color: #67C23A">已扫: {{ row.scanned_schemas || 0 }}</div>
                <div style="color: #E6A23C">未扫: {{ row.unscanned_schemas || 0 }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="last_scan_at" label="上次扫描" width="170" />
          </el-table>
        </div>

        <div class="right-panel">
          <div class="panel-header">
            <h3>
              Schema列表
              <span v-if="selectedResource"> - {{ selectedResource.name }}</span>
            </h3>
            <div v-if="selectedResource">
              <el-button @click="loadSchemas" :loading="loadingSchemas">
                <el-icon>
                  <Refresh />
                </el-icon>
                刷新
              </el-button>
              <el-button
                type="primary"
                @click="handleBatchScan"
                :disabled="!selectedSchemas.length"
                :loading="scanning"
              >
                <el-icon>
                  <Search />
                </el-icon>
                批量扫描选中 Schema ({{ selectedSchemas.length }})
              </el-button>
            </div>
          </div>

          <div v-if="!selectedResource" class="empty-state">
            <el-empty description="请从左侧选择一个存储引擎" />
          </div>

          <el-table
            v-else
            :data="schemas"
            v-loading="loadingSchemas"
            height="600"
            @selection-change="handleSchemaSelectionChange"
          >
            <el-table-column type="selection" width="55" />
            <el-table-column prop="name" label="Schema名称" width="220" />
            <el-table-column label="扫描状态" width="140">
              <template #default="{ row }">
                <el-tag
                  :type="row.scan_status === '已扫描' ? 'success' : row.scan_status === '扫描中' ? 'warning' : 'info'"
                >
                  {{ row.scan_status }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="table_count" label="表数量" width="120" />
            <el-table-column prop="last_scan_at" label="上次扫描" width="180" />
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button
                  size="small"
                  @click.stop="handleScanSchema(row)"
                  :loading="scanningSchemas[row.name]"
                >
                  {{ row.scan_status === '已扫描' ? '重新扫描' : '扫描' }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
    </el-card>

    <el-dialog v-model="showScanDialog" title="扫描进度" width="500px" :close-on-click-modal="false">
      <div v-if="scanning">
        <el-progress :percentage="scanProgress" :status="scanProgress === 100 ? 'success' : undefined" />
        <p class="scan-message">{{ scanMessage }}</p>
      </div>
      <div v-else-if="scanResult">
        <el-result :icon="scanResult.status === 'success' ? 'success' : 'error'">
          <template #title>
            {{ scanResult.status === 'success' ? '扫描完成' : '扫描失败' }}
          </template>
          <template #sub-title>
            <div>扫描了 {{ scanResult.schemas_scanned }} 个 Schema</div>
            <div>发现 {{ scanResult.tables_scanned }} 个表</div>
            <div>扫描 {{ scanResult.fields_scanned }} 个字段</div>
            <div>耗时: {{ scanResult.duration_ms }} ms</div>
          </template>
        </el-result>
      </div>
      <template #footer>
        <el-button @click="closeScanDialog">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import metaApi from '../api/meta'

const resources = ref([])
const loadingResources = ref(false)
const selectedResource = ref(null)

const schemas = ref([])
const loadingSchemas = ref(false)
const selectedSchemas = ref([])

const autoScanning = ref(false)
const scanning = ref(false)
const scanningSchemas = reactive({})
const showScanDialog = ref(false)
const scanProgress = ref(0)
const scanMessage = ref('')
const scanResult = ref(null)

const loadResources = async () => {
  loadingResources.value = true
  try {
    resources.value = await metaApi.getResources()
  } catch (error) {
    ElMessage.error('加载资源列表失败: ' + (error.message || '未知错误'))
  } finally {
    loadingResources.value = false
  }
}

const handleSelectResource = row => {
  selectedResource.value = row
  loadSchemas()
}

const mergeSchemas = (availableSchemas, scannedSchemas) => {
  const scannedMap = new Map(
    scannedSchemas.map(item => [item.schema_name || item.name, item])
  )

  const merged = availableSchemas.map(item => {
    const scanned = scannedMap.get(item.name)
    return {
      id: scanned?.id ?? 0,
      name: item.name,
      scan_status: scanned?.scan_status || '未扫描',
      table_count: scanned?.table_count || 0,
      last_scan_at: scanned?.last_scan_at || ''
    }
  })

  scannedSchemas.forEach(scanned => {
    const name = scanned.schema_name || scanned.name
    if (!merged.find(item => item.name === name)) {
      merged.push({
        id: scanned.id ?? 0,
        name,
        scan_status: scanned.scan_status || '未扫描',
        table_count: scanned.table_count || 0,
        last_scan_at: scanned.last_scan_at || ''
      })
    }
  })

  return merged
}

const loadSchemas = async () => {
  if (!selectedResource.value) return
  loadingSchemas.value = true
  try {
    const [available, scanned] = await Promise.all([
      metaApi.listAvailableSchemas(selectedResource.value.id),
      metaApi.getSchemas(selectedResource.value.id)
    ])
    schemas.value = mergeSchemas(
      available.map(item => ({ name: item.name || item.schema_name })),
      scanned
    )
  } catch (error) {
    ElMessage.error('加载 Schema 列表失败: ' + (error.message || '未知错误'))
  } finally {
    loadingSchemas.value = false
  }
}

const handleSchemaSelectionChange = selection => {
  selectedSchemas.value = selection
}

const handleAutoScan = async () => {
  try {
    await ElMessageBox.confirm(
      '将自动扫描所有未扫描的资源，这可能需要一些时间。是否继续？',
      '确认自动扫描',
      { type: 'warning' }
    )

    autoScanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = '正在扫描...'
    scanResult.value = null

    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.autoScan()
    clearInterval(progressInterval)
    scanProgress.value = 100
    scanResult.value = res
    ElMessage.success('自动扫描完成')

    await loadResources()
    if (selectedResource.value) {
      await loadSchemas()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('自动扫描失败: ' + (error.response?.data?.error || error.message || '未知错误'))
    }
  } finally {
    autoScanning.value = false
  }
}

const handleBatchScan = async () => {
  if (!selectedSchemas.value.length) return
  try {
    await ElMessageBox.confirm(
      `将扫描 ${selectedSchemas.value.length} 个 Schema，是否继续？`,
      '确认批量扫描',
      { type: 'warning' }
    )

    scanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = '正在扫描...'
    scanResult.value = null

    const schemaNames = selectedSchemas.value.map(s => s.name)

    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.scanResource(selectedResource.value.id, schemaNames)
    clearInterval(progressInterval)
    scanProgress.value = 100
    scanResult.value = res
    ElMessage.success('批量扫描完成')

    await loadSchemas()
    await loadResources()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('批量扫描失败: ' + (error.response?.data?.error || error.message || '未知错误'))
    }
  } finally {
    scanning.value = false
  }
}

const handleScanSchema = async schema => {
  scanningSchemas[schema.name] = true
  try {
    const res = await metaApi.scanResource(selectedResource.value.id, [schema.name])
    ElMessage.success(`Schema "${schema.name}" 扫描完成`)
    scanResult.value = res
    await loadSchemas()
    await loadResources()
  } catch (error) {
    ElMessage.error('扫描失败: ' + (error.response?.data?.error || error.message || '未知错误'))
  } finally {
    scanningSchemas[schema.name] = false
  }
}

const closeScanDialog = () => {
  showScanDialog.value = false
  scanProgress.value = 0
  scanMessage.value = ''
  scanResult.value = null
}

onMounted(() => {
  loadResources()
})
</script>

<style scoped>
.metadata-scan {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.scan-container {
  display: flex;
  gap: 20px;
}

.left-panel {
  flex: 0 0 450px;
  border-right: 1px solid #eee;
  padding-right: 20px;
}

.right-panel {
  flex: 1;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
}

.panel-header h3 {
  margin: 0;
  font-size: 16px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 600px;
}

.scan-message {
  margin-top: 20px;
  text-align: center;
  color: #999;
}
</style>
