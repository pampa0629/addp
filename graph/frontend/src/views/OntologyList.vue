<template>
  <div class="page-container">
    <div class="page-header">
      <h2>本体管理</h2>
      <el-button type="primary" @click="$router.push('/ontologies/create')">
        <el-icon><Plus /></el-icon> 新建本体
      </el-button>
    </div>

    <el-table v-loading="loading" :data="ontologies" border style="width:100%">
      <el-table-column prop="name" label="名称" min-width="150">
        <template #default="{ row }">
          <el-link type="primary" @click="$router.push(`/ontologies/${row.id}`)">{{ row.name }}</el-link>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
            {{ row.status === 'active' ? '启用' : '归档' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="$router.push(`/ontologies/${row.id}`)">查看</el-button>
          <el-button link size="small" @click="$router.push(`/ontologies/${row.id}/edit`)">编辑</el-button>
          <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'

const loading = ref(false)
const ontologies = ref([])

const load = async () => {
  loading.value = true
  try {
    const res = await ontologyAPI.list()
    ontologies.value = res || []
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async (row) => {
  await ElMessageBox.confirm(`确认删除本体「${row.name}」？`, '提示', { type: 'warning' })
  try {
    await ontologyAPI.delete(row.id)
    ElMessage.success('删除成功')
    load()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

const formatDate = (str) => str ? new Date(str).toLocaleString('zh-CN') : '-'

onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0; font-size: 18px; }
</style>
