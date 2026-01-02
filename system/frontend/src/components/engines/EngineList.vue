<template>
  <div>
    <el-table :data="engines" v-loading="loading" stripe>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="名称" min-width="150" />
      <el-table-column label="类型" width="150">
        <template #default="{ row }">
          <el-tag>{{ getEngineTypeText(row.engine_type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="连接状态" width="120">
        <template #default="{ row }">
          <el-tag :type="getConnectionStatus(row.connection_status).type">
            {{ getConnectionStatus(row.connection_status).text }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="primary" :icon="View" @click="$emit('view', row)">
            查看
          </el-button>
          <el-button size="small" type="success" :icon="Connection" @click="$emit('test', row)">
            测试
          </el-button>
          <el-button size="small" type="primary" :icon="Edit" @click="$emit('edit', row)">
            编辑
          </el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="$emit('delete', row)">
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      :current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      layout="total, prev, pager, next"
      style="margin-top: 20px; justify-content: flex-end"
      @current-change="$emit('page-change', $event)"
    />
  </div>
</template>

<script setup>
import { View, Edit, Delete, Connection } from '@element-plus/icons-vue'
import { formatDate } from '@common-ui/utils/formatters'

defineProps({
  engines: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  currentPage: {
    type: Number,
    default: 1
  },
  pageSize: {
    type: Number,
    default: 10
  },
  total: {
    type: Number,
    default: 0
  },
  getEngineTypeText: {
    type: Function,
    required: true
  },
  getConnectionStatus: {
    type: Function,
    required: true
  }
})

defineEmits(['view', 'test', 'edit', 'delete', 'page-change'])
</script>
