<template>
  <div class="build-manager">
    <div class="page-header">
      <h2>{{ t('graph.build.title') }}</h2>
      <el-button type="primary" @click="showCreateDialog = true">+ {{ t('graph.build.newTask') }}</el-button>
    </div>

    <div v-if="loading" class="loading-wrap"><el-icon class="is-loading"><Loading /></el-icon></div>

    <div v-else-if="tasks.length === 0" class="empty-tip">
      <el-empty :description="t('graph.build.emptyTip')" />
    </div>

    <div v-else class="task-list">
      <div v-for="task in tasks" :key="task.id" class="task-card" @click="goDetail(task.id)">
        <div class="task-header">
          <span class="task-name">{{ task.name }}</span>
          <el-tag :type="statusTagType(task.status)" size="small">{{ statusLabel(task.status) }}</el-tag>
        </div>
        <div v-if="task.description" class="task-desc">{{ task.description }}</div>
        <div class="task-stats" v-if="task.stats && task.stats.total_materials">
          <span>{{ t('graph.build.statMaterials') }} {{ task.stats.total_materials }}</span>
          <span>{{ t('graph.build.statAutoWritten') }} {{ task.stats.auto_written }}</span>
          <span>{{ t('graph.build.statPendingReview') }} {{ task.stats.pending_review }}</span>
        </div>
        <div class="task-footer">
          <span class="task-date">{{ formatDate(task.created_at) }}</span>
          <div class="task-actions" @click.stop>
            <el-button v-if="task.status === 'pending' || task.status === 'failed'" size="small" type="primary" @click="handleRun(task)">{{ t('graph.build.run') }}</el-button>
            <el-button v-if="task.status === 'running'" size="small" type="warning" @click="handleCancel(task)">{{ t('graph.build.cancel') }}</el-button>
            <el-button v-if="task.status === 'completed' || task.status === 'cancelled'" size="small" type="primary" plain @click="handleRerun(task)">{{ t('graph.build.rerun') }}</el-button>
            <el-button size="small" @click="goReview(task)">
              {{ t('graph.build.review') }}
              <el-badge v-if="pendingCounts[task.id]" :value="pendingCounts[task.id]" class="review-badge" />
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(task.id)">{{ t('graph.common.delete') }}</el-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 新建任务弹窗 -->
    <el-dialog v-model="showCreateDialog" :title="t('graph.build.createDialogTitle')" width="480px">
      <el-form :model="createForm" label-width="120px" @submit.prevent>
        <el-form-item :label="t('graph.build.taskName')" required>
          <el-input v-model="createForm.name" :placeholder="t('graph.build.taskNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="createForm.description" type="textarea" rows="2" />
        </el-form-item>
        <el-form-item :label="t('graph.build.confidenceThreshold')">
          <el-slider v-model="createForm.confidence_threshold" :min="0.1" :max="1.0" :step="0.05" show-input />
        </el-form-item>
        <el-collapse>
          <el-collapse-item :title="t('graph.build.advancedChunk')">
            <el-form-item :label="t('graph.build.chunkSize')">
              <el-input-number v-model="createForm.chunk_size" :min="200" :max="4000" :step="100" />
              <span class="form-hint">{{ t('graph.build.chunkSizeHint') }}</span>
            </el-form-item>
            <el-form-item :label="t('graph.build.chunkOverlap')">
              <el-input-number v-model="createForm.chunk_overlap" :min="0" :max="500" :step="50" />
              <span class="form-hint">{{ t('graph.build.chunkOverlapHint') }}</span>
            </el-form-item>
            <el-form-item :label="t('graph.build.docContext')">
              <el-input-number v-model="createForm.doc_context_size" :min="0" :max="500" :step="50" />
              <span class="form-hint">{{ t('graph.build.docContextHint') }}</span>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" @click="handleCreate">{{ t('graph.common.create') }}</el-button>
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
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
    ElMessage.error(t('graph.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  if (!createForm.value.name.trim()) {
    ElMessage.warning(t('graph.build.taskNameRequired'))
    return
  }
  creating.value = true
  try {
    await buildAPI.createTask(props.graphId, createForm.value)
    showCreateDialog.value = false
    createForm.value = { name: '', description: '', confidence_threshold: 0.7, chunk_size: 1000, chunk_overlap: 200, doc_context_size: 200 }
    await loadTasks()
    ElMessage.success(t('graph.common.createSuccess'))
  } catch (e) {
    ElMessage.error(t('graph.common.createFailed'))
  } finally {
    creating.value = false
  }
}

async function handleRun(task) {
  try {
    await buildAPI.runTask(props.graphId, task.id)
    ElMessage.success(t('graph.build.taskStarted'))
    await loadTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.build.startFailed'))
  }
}

async function handleCancel(task) {
  try {
    await buildAPI.cancelTask(props.graphId, task.id)
    ElMessage.success(t('graph.build.taskCancelled'))
    await loadTasks()
  } catch (e) {
    ElMessage.error(t('graph.build.cancelFailed'))
  }
}

async function handleRerun(task) {
  try {
    await ElMessageBox.confirm(t('graph.build.confirmRerun'), t('graph.build.confirmRerunTitle'), { type: 'warning' })
    await buildAPI.rerunTask(props.graphId, task.id)
    ElMessage.success(t('graph.build.taskRestarted'))
    await loadTasks()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || t('graph.build.startFailed'))
  }
}

async function handleDelete(taskId) {
  await ElMessageBox.confirm(t('graph.build.confirmDelete'), t('graph.common.confirmDelete'), { type: 'warning' })
  try {
    await buildAPI.deleteTask(props.graphId, taskId)
    ElMessage.success(t('graph.common.deleteSuccess'))
    await loadTasks()
  } catch (e) {
    ElMessage.error(t('graph.common.deleteFailed'))
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
  const map = {
    pending: t('graph.build.statusPending'),
    running: t('graph.build.statusRunning'),
    completed: t('graph.build.statusCompleted'),
    failed: t('graph.build.statusFailed'),
    cancelled: t('graph.build.statusCancelled')
  }
  return map[status] || status
}

function formatDate(d) {
  return d ? new Date(d).toLocaleString() : '-'
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
