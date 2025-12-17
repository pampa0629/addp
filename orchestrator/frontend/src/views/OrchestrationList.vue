<template>
  <div class="orchestration-list">
    <div class="header">
      <h2>任务编排</h2>
      <el-button type="primary" @click="handleCreate">创建编排</el-button>
    </div>

    <el-table :data="orchestrations" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="80"></el-table-column>
      <el-table-column prop="name" label="名称" width="200"></el-table-column>
      <el-table-column prop="description" label="描述"></el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="scope">
          <el-tag :type="scope.row.enabled ? 'success' : 'info'">
            {{ scope.row.enabled ? '已启用' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="定时调度" width="200">
        <template #default="scope">
          <el-tag v-if="scope.row.schedule" type="success" size="small">
            {{ describeCron(scope.row.schedule) }}
          </el-tag>
          <el-tag v-else type="info" size="small">手动触发</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="步骤数" width="100">
        <template #default="scope">
          {{ scope.row.steps?.length || 0 }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="300">
        <template #default="scope">
          <el-button size="small" @click="handleEdit(scope.row)">编辑</el-button>
          <el-button size="small" type="success" @click="handleExecute(scope.row)">执行</el-button>
          <el-button size="small" type="info" @click="handleViewExecutions(scope.row)">记录</el-button>
          <el-button size="small" type="danger" @click="handleDelete(scope.row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import orchestrationAPI from '../api/orchestration'
import { describeCron } from '@common-ui'

const router = useRouter()
const orchestrations = ref([])
const loading = ref(false)

onMounted(() => {
  loadOrchestrations()
})

async function loadOrchestrations() {
  loading.value = true
  try {
    orchestrations.value = await orchestrationAPI.list()
  } catch (error) {
    ElMessage.error('加载编排列表失败')
  } finally {
    loading.value = false
  }
}

function handleCreate() {
  router.push('/orchestrations/new')
}

function handleEdit(row) {
  router.push(`/orchestrations/${row.id}/edit`)
}

async function handleExecute(row) {
  try {
    await orchestrationAPI.execute(row.id)
    ElMessage.success('编排已触发执行')
  } catch (error) {
    ElMessage.error('触发执行失败')
  }
}

function handleViewExecutions(row) {
  router.push(`/orchestrations/${row.id}/executions`)
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm('确定要删除此编排吗？', '警告', {
      type: 'warning'
    })

    await orchestrationAPI.delete(row.id)
    ElMessage.success('删除成功')
    loadOrchestrations()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}
</script>

<style scoped>
.orchestration-list {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

h2 {
  margin: 0;
}
</style>
