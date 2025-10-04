<template>
  <el-container style="height: calc(100vh - 100px)">
    <!-- 左侧树形结构 -->
    <el-aside width="350px" style="border-right: 1px solid #dcdfe6; overflow: auto;">
      <div style="padding: 15px; border-bottom: 1px solid #dcdfe6;">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索表/字段"
          :prefix-icon="Search"
          clearable
          @keyup.enter="handleSearch"
        />
      </div>

      <el-tree
        :data="treeData"
        :props="treeProps"
        node-key="id"
        :expand-on-click-node="false"
        @node-click="handleNodeClick"
        :load="loadNode"
        lazy
        style="padding: 10px;"
      >
        <template #default="{ node, data }">
          <span class="custom-tree-node">
            <el-icon style="margin-right: 5px">
              <DataLine v-if="data.type === 'datasource'" />
              <Files v-else-if="data.type === 'database'" />
              <Document v-else-if="data.type === 'table'" />
            </el-icon>
            <span>{{ node.label }}</span>
            <span v-if="data.row_count" style="color: #909399; margin-left: 5px">
              ({{ data.row_count }} 行)
            </span>
          </span>
        </template>
      </el-tree>
    </el-aside>

    <!-- 右侧详情 -->
    <el-main>
      <el-card v-if="!selectedNode" shadow="never" style="text-align: center; padding: 100px 0;">
        <el-empty description="请从左侧选择数据源、数据库或表进行查看" />
      </el-card>

      <!-- 数据源详情 -->
      <div v-else-if="selectedNode.type === 'datasource'">
        <h2>数据源: {{ selectedNode.label }}</h2>
        <el-descriptions :column="2" border style="margin-top: 20px">
          <el-descriptions-item label="数据源类型">{{ selectedNode.datasource_type }}</el-descriptions-item>
          <el-descriptions-item label="同步状态">
            <el-tag :type="selectedNode.sync_status === 'success' ? 'success' : 'info'">
              {{ selectedNode.sync_status || '未同步' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="最后同步时间">
            {{ selectedNode.last_sync_at || '无' }}
          </el-descriptions-item>
        </el-descriptions>
        <el-button type="primary" @click="syncDatasource" style="margin-top: 20px">
          <el-icon><Refresh /></el-icon> 同步数据源
        </el-button>
      </div>

      <!-- 数据库详情 -->
      <div v-else-if="selectedNode.type === 'database'">
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <h2>数据库: {{ selectedNode.label }}</h2>
          <el-button type="primary" @click="scanDatabase" :loading="scanning">
            <el-icon><Search /></el-icon> 深度扫描
          </el-button>
        </div>
        <el-descriptions :column="2" border style="margin-top: 20px">
          <el-descriptions-item label="字符集">{{ selectedNode.charset || '-' }}</el-descriptions-item>
          <el-descriptions-item label="排序规则">{{ selectedNode.collation || '-' }}</el-descriptions-item>
          <el-descriptions-item label="表数量">{{ selectedNode.table_count || 0 }}</el-descriptions-item>
          <el-descriptions-item label="总大小">
            {{ formatBytes(selectedNode.total_size_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item label="是否已扫描">
            <el-tag :type="selectedNode.is_scanned ? 'success' : 'info'">
              {{ selectedNode.is_scanned ? '是' : '否' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="最后扫描时间">
            {{ selectedNode.last_scan_at || '无' }}
          </el-descriptions-item>
        </el-descriptions>
      </div>

      <!-- 表详情和字段列表 -->
      <div v-else-if="selectedNode.type === 'table'">
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <h2>表: {{ selectedNode.label }}</h2>
          <el-button type="primary" @click="scanTable" :loading="scanning">
            <el-icon><Search /></el-icon> 扫描字段
          </el-button>
        </div>

        <el-descriptions :column="3" border style="margin-top: 20px">
          <el-descriptions-item label="表类型">{{ selectedNode.table_type || 'TABLE' }}</el-descriptions-item>
          <el-descriptions-item label="引擎">{{ selectedNode.engine || '-' }}</el-descriptions-item>
          <el-descriptions-item label="行数">{{ selectedNode.row_count || 0 }}</el-descriptions-item>
          <el-descriptions-item label="数据大小">
            {{ formatBytes(selectedNode.data_size_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item label="索引大小">
            {{ formatBytes(selectedNode.index_size_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item label="表注释">{{ selectedNode.table_comment || '-' }}</el-descriptions-item>
        </el-descriptions>

        <h3 style="margin-top: 30px">字段列表</h3>
        <el-table :data="fields" border style="margin-top: 15px" v-loading="loadingFields">
          <el-table-column prop="field_name" label="字段名" width="200" />
          <el-table-column prop="column_type" label="类型" width="150" />
          <el-table-column prop="is_nullable" label="可空" width="80">
            <template #default="{ row }">
              <el-tag :type="row.is_nullable ? 'info' : 'danger'" size="small">
                {{ row.is_nullable ? 'YES' : 'NO' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="column_key" label="键" width="80">
            <template #default="{ row }">
              <el-tag v-if="row.column_key === 'PRI'" type="danger" size="small">PRI</el-tag>
              <el-tag v-else-if="row.column_key === 'UNI'" type="warning" size="small">UNI</el-tag>
              <el-tag v-else-if="row.column_key === 'MUL'" type="info" size="small">MUL</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="column_default" label="默认值" width="120" />
          <el-table-column prop="extra" label="额外" width="120" />
          <el-table-column prop="field_comment" label="注释" show-overflow-tooltip />
        </el-table>
      </div>
    </el-main>
  </el-container>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, DataLine, Files, Document, Refresh } from '@element-plus/icons-vue'
import metaApi from '../api/meta'

const searchKeyword = ref('')
const treeData = ref([])
const treeProps = {
  label: 'label',
  children: 'children',
  isLeaf: 'leaf'
}

const selectedNode = ref(null)
const fields = ref([])
const loadingFields = ref(false)
const scanning = ref(false)

// 懒加载节点
const loadNode = async (node, resolve) => {
  if (node.level === 0) {
    // 加载数据源列表
    try {
      const res = await metaApi.getDatasources()
      const datasources = res.data.map(ds => ({
        id: `ds-${ds.id}`,
        label: ds.datasource_name,
        type: 'datasource',
        leaf: false,
        ...ds
      }))
      resolve(datasources)
    } catch (error) {
      ElMessage.error('加载数据源失败')
      resolve([])
    }
  } else if (node.data.type === 'datasource') {
    // 加载数据库列表
    try {
      const res = await metaApi.getDatabases(node.data.id.replace('ds-', ''))
      const databases = res.data.map(db => ({
        id: `db-${db.id}`,
        label: db.database_name,
        type: 'database',
        leaf: false,
        ...db
      }))
      resolve(databases)
    } catch (error) {
      ElMessage.error('加载数据库失败')
      resolve([])
    }
  } else if (node.data.type === 'database') {
    // 加载表列表
    try {
      const res = await metaApi.getTables(node.data.id.replace('db-', ''))
      const tables = res.data.map(table => ({
        id: `table-${table.id}`,
        label: table.table_name,
        type: 'table',
        leaf: true,
        ...table
      }))
      resolve(tables)
    } catch (error) {
      ElMessage.error('加载表列表失败')
      resolve([])
    }
  }
}

// 节点点击
const handleNodeClick = async (data) => {
  selectedNode.value = data

  // 如果是表,加载字段列表
  if (data.type === 'table') {
    loadFields(data.id.replace('table-', ''))
  }
}

// 加载字段列表
const loadFields = async (tableId) => {
  loadingFields.value = true
  try {
    const res = await metaApi.getFields(tableId)
    fields.value = res.data
  } catch (error) {
    ElMessage.error('加载字段列表失败')
    fields.value = []
  } finally {
    loadingFields.value = false
  }
}

// 同步数据源
const syncDatasource = async () => {
  try {
    const resourceId = selectedNode.value.resource_id
    await metaApi.syncResource(resourceId)
    ElMessage.success('同步请求已发送')
  } catch (error) {
    ElMessage.error('同步失败')
  }
}

// 深度扫描数据库
const scanDatabase = async () => {
  scanning.value = true
  try {
    const dbId = selectedNode.value.id.replace('db-', '')
    await metaApi.deepScanDatabase(dbId)
    ElMessage.success('扫描完成')
  } catch (error) {
    ElMessage.error('扫描失败')
  } finally {
    scanning.value = false
  }
}

// 扫描表字段
const scanTable = async () => {
  scanning.value = true
  try {
    const tableId = selectedNode.value.id.replace('table-', '')
    await metaApi.deepScanTable(tableId)
    ElMessage.success('扫描完成')
    // 重新加载字段
    await loadFields(tableId)
  } catch (error) {
    ElMessage.error('扫描失败')
  } finally {
    scanning.value = false
  }
}

// 搜索
const handleSearch = async () => {
  if (!searchKeyword.value) {
    ElMessage.warning('请输入搜索关键词')
    return
  }
  try {
    const res = await metaApi.searchTables(searchKeyword.value)
    ElMessage.success(`找到 ${res.data.length} 个结果`)
  } catch (error) {
    ElMessage.error('搜索失败')
  }
}

// 格式化字节
const formatBytes = (bytes) => {
  if (!bytes || bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i]
}
</script>

<style scoped>
.custom-tree-node {
  display: flex;
  align-items: center;
  font-size: 14px;
}

:deep(.el-tree-node__content) {
  height: 36px;
}
</style>
