<template>
  <div class="build-manager">
    <div class="page-header">
      <h2>图谱构建</h2>
      <el-button type="primary" @click="showCreateDialog = true">+ 新建任务</el-button>
    </div>

    <div v-if="loading" class="loading-wrap"><el-icon class="is-loading"><Loading /></el-icon></div>

    <div v-else-if="tasks.length === 0" class="empty-tip">
      <el-empty description="暂无构建任务，点击右上角新建" />
    </div>

    <div v-else class="task-list">
      <div v-for="task in tasks" :key="task.id" class="task-card" @click="goDetail(task.id)">
        <div class="task-header">
          <span class="task-name">{{ task.name }}</span>
          <el-tag :type="statusTagType(task.status)" size="small">{{ statusLabel(task.status) }}</el-tag>
        </div>
        <div v-if="task.description" class="task-desc">{{ task.description }}</div>
        <div class="task-stats" v-if="task.stats && task.stats.total_materials">
          <span>材料 {{ task.stats.total_materials }}</span>
          <span>自动写入 {{ task.stats.auto_written }}</span>
          <span>待审核 {{ task.stats.pending_review }}</span>
        </div>
        <div class="task-footer">
          <span class="task-date">{{ formatDate(task.created_at) }}</span>
          <div class="task-actions" @click.stop>
            <el-button v-if="task.status === 'pending' || task.status === 'failed'" size="small" type="primary" @click="handleRun(task)">运行</el-button>
            <el-button v-if="task.status === 'running'" size="small" type="warning" @click="handleCancel(task)">取消</el-button>
            <el-button v-if="task.status === 'completed' || task.status === 'cancelled'" size="small" type="primary" plain @click="handleRerun(task)">重新运行</el-button>
            <el-button size="small" @click="goReview(task)">
              审核
              <el-badge v-if="pendingCounts[task.id]" :value="pendingCounts[task.id]" class="review-badge" />
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(task.id)">删除</el-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 新建任务弹窗 -->
    <el-dialog v-model="showCreateDialog" title="新建构建任务" width="480px">
      <el-form :model="createForm" label-width="120px" @submit.prevent>
        <el-form-item label="任务名称" required>
          <el-input v-model="createForm.name" placeholder="请输入任务名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" rows="2" />
        </el-form-item>
        <el-form-item label="置信度阈值">
          <el-slider v-model="createForm.confidence_threshold" :min="0.1" :max="1.0" :step="0.05" show-input />
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="高级分块设置">
            <el-form-item label="Chunk 大小">
              <el-input-number v-model="createForm.chunk_size" :min="200" :max="4000" :step="100" />
              <span class="form-hint">字符数（默认 1000）</span>
            </el-form-item>
            <el-form-item label="Overlap 大小">
              <el-input-number v-model="createForm.chunk_overlap" :min="0" :max="500" :step="50" />
              <span class="form-hint">字符数（默认 200）</span>
            </el-form-item>
            <el-form-item label="文档上下文">
              <el-input-number v-model="createForm.doc_context_size" :min="0" :max="500" :step="50" />
              <span class="form-hint">头部字符数（默认 200）</span>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { buildAPI } from '../api/graphBuild'

const props = defineProps({ graphId: { type: [String, Number], required: true } })
const router = useRouter()

const tasks = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const creating = ref(false)
const pendingCounts = ref({})

const createForm = ref({
  name: '',
  description: '',
  confidence_threshold: 0.7,
  chunk_size: 1000,
  chunk_overlap: 200,
  doc_context_size: 200
})

async function loadTasks() {
  loading.value = true
  try {
    const res = await buildAPI.listTasks(props.graphId)
    tasks.value = res || []
    const countRes = await buildAPI.getPendingCount(props.graphId)
    const total = countRes.count || 0
    // 简单地将 pending 数显示在第一个 running/completed 任务上
    if (total > 0 && tasks.value.length > 0) {
      pendingCounts.value[tasks.value[0].id] = total
    }
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  if (!createForm.value.name.trim()) {
    ElMessage.warning('请输入任务名称')
    return
  }
  creating.value = true
  try {
    await buildAPI.createTask(props.graphId, createForm.value)
    showCreateDialog.value = false
    createForm.value = { name: '', description: '', confidence_threshold: 0.7, chunk_size: 1000, chunk_overlap: 200, doc_context_size: 200 }
    await loadTasks()
    ElMessage.success('创建成功')
  } catch (e) {
    ElMessage.error('创建失败')
  } finally {
    creating.value = false
  }
}

async function handleRun(task) {
  try {
    await buildAPI.runTask(props.graphId, task.id)
    ElMessage.success('任务已启动')
    await loadTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '启动失败')
  }
}

async function handleCancel(task) {
  try {
    await buildAPI.cancelTask(props.graphId, task.id)
    ElMessage.success('任务已取消')
    await loadTasks()
  } catch (e) {
    ElMessage.error('取消失败')
  }
}

async function handleRerun(task) {
  try {
    await ElMessageBox.confirm('重新运行将清空本次待审核队列并从头重新处理所有材料，确认？', '确认重新运行', { type: 'warning' })
    await buildAPI.rerunTask(props.graphId, task.id)
    ElMessage.success('任务已重新启动')
    await loadTasks()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '启动失败')
  }
}

async function handleDelete(taskId) {
  await ElMessageBox.confirm('删除任务将同时删除所有材料和审核记录，确认？', '确认删除', { type: 'warning' })
  try {
    await buildAPI.deleteTask(props.graphId, taskId)
    ElMessage.success('删除成功')
    await loadTasks()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

function goDetail(taskId) {
  router.push(`/graphs/${props.graphId}/build/tasks/${taskId}`)
}

function goReview(task) {
  router.push(`/graphs/${props.graphId}/review`)
}

function statusTagType(status) {
  const map = { pending: 'info', running: 'warning', completed: 'success', failed: 'danger', cancelled: '' }
  return map[status] || 'info'
}

function statusLabel(status) {
  const map = { pending: '待运行', running: '运行中', completed: '已完成', failed: '失败', cancelled: '已取消' }
  return map[status] || status
}

function formatDate(d) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

onMounted(loadTasks)
</script>

<style scoped>
.build-manager { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.page-header h2 { margin: 0; }
.loading-wrap { text-align: center; padding: 60px; font-size: 24px; }
.task-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(360px, 1fr)); gap: 16px; }
.task-card { background: var(--addp-bg-primary); border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 16px; cursor: pointer; transition: box-shadow 0.2s; }
.task-card:hover { box-shadow: var(--addp-shadow-hover); }
.task-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.task-name { font-weight: 600; font-size: 15px; }
.task-desc { font-size: 13px; color: var(--addp-text-secondary); margin-bottom: 8px; }
.task-stats { display: flex; gap: 16px; font-size: 12px; color: var(--addp-text-secondary); margin-bottom: 8px; }
.task-footer { display: flex; justify-content: space-between; align-items: center; }
.task-date { font-size: 12px; color: var(--addp-text-tertiary); }
.task-actions { display: flex; gap: 8px; align-items: center; }
.review-badge { margin-left: 4px; }
.form-hint { font-size: 12px; color: var(--addp-text-tertiary); margin-left: 8px; }
</style>
