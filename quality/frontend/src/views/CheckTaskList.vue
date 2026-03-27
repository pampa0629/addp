<template>
  <div>
    <div class="page-header">
      <h2>检查任务</h2>
      <el-button type="primary" :icon="Plus" @click="showCreateDialog = true">新建任务</el-button>
    </div>

    <el-table :data="tasks" v-loading="loading" border>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="任务名称" />
      <el-table-column prop="engine_id" label="引擎 ID" width="100" />
      <el-table-column prop="schema_name" label="Schema" width="120" />
      <el-table-column prop="table_name" label="数据表" width="150" />
      <el-table-column prop="enabled" label="启用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_run_at" label="最近执行" width="180">
        <template #default="{ row }">{{ row.last_run_at ? new Date(row.last_run_at).toLocaleString() : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="200">
        <template #default="{ row }">
          <el-button size="small" type="primary" @click="runTask(row.id)">执行</el-button>
          <el-button size="small" @click="deleteTask(row.id)" type="danger">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="showCreateDialog" title="新建检查任务" width="500px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="任务名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" type="textarea" /></el-form-item>
        <el-form-item label="引擎 ID"><el-input-number v-model="form.engine_id" :min="1" /></el-form-item>
        <el-form-item label="Schema"><el-input v-model="form.schema_name" placeholder="可选" /></el-form-item>
        <el-form-item label="数据表"><el-input v-model="form.table_name" placeholder="可选，留空则检查所有表" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="createTask">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { checkTaskAPI } from '../api/quality'

const tasks = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const form = ref({ name: '', description: '', engine_id: 1, schema_name: '', table_name: '' })

const fetchTasks = async () => {
  loading.value = true
  try {
    const res = await checkTaskAPI.list()
    tasks.value = res || []
  } finally {
    loading.value = false
  }
}

const createTask = async () => {
  try {
    await checkTaskAPI.create(form.value)
    ElMessage.success('创建成功')
    showCreateDialog.value = false
    form.value = { name: '', description: '', engine_id: 1, schema_name: '', table_name: '' }
    await fetchTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  }
}

const runTask = async (id) => {
  try {
    const res = await checkTaskAPI.run(id)
    ElMessage.success(`检查已启动，执行 ID: ${res.execution_id}`)
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '执行失败')
  }
}

const deleteTask = async (id) => {
  await ElMessageBox.confirm('确认删除该任务？', '提示', { type: 'warning' })
  await checkTaskAPI.delete(id)
  ElMessage.success('已删除')
  await fetchTasks()
}

onMounted(fetchTasks)
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
