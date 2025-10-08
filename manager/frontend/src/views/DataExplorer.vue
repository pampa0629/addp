<template>
  <div class="data-explorer">
    <el-row :gutter="20">
      <el-col :span="8" class="tree-panel">
        <el-card shadow="never">
          <template #header>
            <div class="panel-header">
              <span>存储引擎</span>
              <el-button size="small" :loading="loadingTree" @click="loadTree">
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
          </template>
          <el-tree
            :data="treeData"
            :props="treeProps"
            node-key="id"
            :highlight-current="true"
            :expand-on-click-node="false"
            @node-click="handleNodeClick"
            v-loading="loadingTree"
          >
            <template #default="{ data }">
              <span class="tree-node" :class="data.type">
                <el-icon v-if="data.type === 'resource'"><Collection /></el-icon>
                <el-icon v-else-if="data.type === 'schema'"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span class="label" :title="data.label">{{ data.label }}</span>
              </span>
            </template>
          </el-tree>
        </el-card>
      </el-col>

      <el-col :span="16">
        <el-card shadow="never" class="preview-panel">
          <template #header>
            <div class="panel-header">
              <span v-if="selectedTableLabel">{{ selectedTableLabel }} - 数据预览</span>
              <span v-else>请选择一张表</span>
            </div>
          </template>

          <div v-if="!selectedTable" class="empty-state">
            <el-empty description="从左侧选择一张表查看数据" />
          </div>

          <div v-else>
            <el-table :data="preview.rows" v-loading="loadingPreview" height="520">
              <el-table-column
                v-for="col in preview.columns"
                :key="col"
                :prop="col"
                :label="col"
                show-overflow-tooltip
              />
            </el-table>
            <div class="pagination" v-if="preview.total > 0">
              <el-pagination
                background
                layout="prev, pager, next"
                :total="preview.total"
                :page-size="pageSize"
                :current-page="currentPage"
                @current-change="handlePageChange"
              />
              <div class="tip">最多展示前 50 行数据</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Folder, Collection, Document } from '@element-plus/icons-vue'
import dataExplorerAPI from '../api/dataExplorer'

const treeData = ref([])
const treeProps = {
  label: 'label',
  children: 'children'
}
const loadingTree = ref(false)

const selectedTable = ref(null)
const selectedTableLabel = computed(() => {
  if (!selectedTable.value) return ''
  return `${selectedTable.value.schema}.${selectedTable.value.table}`
})

const preview = reactive({
  columns: [],
  rows: [],
  total: 0
})
const loadingPreview = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)

const buildTreeData = (resources) => {
  return resources.map((res) => {
    const schemas = (res.schemas || []).map((schema) => {
      const tables = (schema.tables || []).map((table) => ({
        id: `table-${res.id}-${schema.name}-${table.name}`,
        type: 'table',
        label: table.name,
        resourceId: res.id,
        schema: schema.name,
        table: table.name
      }))
      return {
        id: `schema-${res.id}-${schema.name}`,
        type: 'schema',
        label: schema.name,
        resourceId: res.id,
        schema: schema.name,
        children: tables
      }
    })
    return {
      id: `resource-${res.id}`,
      type: 'resource',
      label: res.name,
      resourceId: res.id,
      children: schemas
    }
  })
}

const loadTree = async () => {
  loadingTree.value = true
  try {
    const response = await dataExplorerAPI.getTree()
    treeData.value = buildTreeData(response.data.data || [])
  } catch (error) {
    ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingTree.value = false
  }
}

const loadPreview = async () => {
  if (!selectedTable.value) return
  loadingPreview.value = true
  try {
    const params = {
      resource_id: selectedTable.value.resourceId,
      schema: selectedTable.value.schema,
      table: selectedTable.value.table,
      page: currentPage.value,
      page_size: pageSize.value
    }
    const response = await dataExplorerAPI.getPreview(params)
    preview.columns = response.data.columns || []
    preview.rows = response.data.rows || []
    preview.total = response.data.total || 0
  } catch (error) {
    ElMessage.error('加载数据预览失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingPreview.value = false
  }
}

const handleNodeClick = (nodeData) => {
  if (nodeData.type !== 'table') return
  selectedTable.value = nodeData
  currentPage.value = 1
  loadPreview()
}

const handlePageChange = (page) => {
  currentPage.value = page
  loadPreview()
}

onMounted(() => {
  loadTree()
})
</script>

<style scoped>
.data-explorer {
  padding: 10px;
}

.tree-panel {
  max-height: 636px;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tree-node .label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-panel {
  min-height: 600px;
}

.empty-state {
  height: 520px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.pagination {
  margin-top: 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination .tip {
  font-size: 12px;
  color: #909399;
}
</style>
