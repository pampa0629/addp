<template>
  <div class="build-task-detail">
    <div class="page-header">
      <el-button text @click="$router.back()">← {{ t('graph.common.back') }}</el-button>
      <h2>{{ task?.name || t('graph.build.taskDetail') }}</h2>
      <div class="header-actions">
        <el-button v-if="canRun" type="primary" :loading="running" @click="handleRun">{{ t('graph.build.runTask') }}</el-button>
        <el-button v-if="task?.status === 'running'" type="warning" @click="handleCancel">{{ t('graph.build.cancel') }}</el-button>
        <el-button @click="$router.push(`/graphs/${graphId}/review`)">
          {{ t('graph.build.reviewQueue') }}
          <el-badge v-if="pendingCount > 0" :value="pendingCount" style="margin-left:6px" />
        </el-button>
      </div>
    </div>

    <div v-if="!task" class="loading-wrap"><el-icon class="is-loading"><Loading /></el-icon></div>

    <template v-else>
      <!-- 任务信息 -->
      <el-card class="info-card">
        <div class="info-grid">
          <div class="info-item"><span class="label">{{ t('graph.common.status') }}</span><el-tag :type="statusTagType(task.status)">{{ statusLabel(task.status) }}</el-tag></div>
          <div class="info-item"><span class="label">{{ t('graph.build.confidenceThreshold') }}</span>{{ task.confidence_threshold }}</div>
          <div class="info-item"><span class="label">{{ t('graph.build.chunkSize') }}</span>{{ task.chunk_size }} {{ t('graph.build.chars') }}</div>
          <div class="info-item"><span class="label">Overlap</span>{{ task.chunk_overlap }} {{ t('graph.build.chars') }}</div>
          <div class="info-item" v-if="task.execution_id">
            <span class="label">Monitor ID</span>
            <el-text type="info" size="small">{{ task.execution_id }}</el-text>
          </div>
        </div>
        <div v-if="task.stats && task.stats.total_materials" class="stats-row">
          <el-statistic :title="t('graph.build.statTotalMaterials')" :value="task.stats.total_materials" />
          <el-statistic :title="t('graph.build.statProcessed')" :value="task.stats.processed" />
          <el-statistic :title="t('graph.build.statAutoWritten')" :value="task.stats.auto_written" />
          <el-statistic :title="t('graph.build.statPendingReview')" :value="task.stats.pending_review" />
        </div>
        <div v-if="task.error_message" class="error-msg">{{ task.error_message }}</div>
      </el-card>

      <!-- 材料上传 -->
      <el-card class="materials-card">
        <template #header>
          <div class="card-header">
            <span>{{ t('graph.build.materials') }}</span>
            <el-upload
              v-if="canUpload"
              :action="''"
              :auto-upload="false"
              :on-change="handleFileChange"
              :show-file-list="false"
              accept=".txt,.md,.csv"
              multiple
            >
              <el-button size="small">{{ t('graph.build.uploadFile') }}</el-button>
            </el-upload>
          </div>
        </template>

        <div v-if="uploadingFiles.length > 0" class="uploading-list">
          <div v-for="f in uploadingFiles" :key="f.name" class="uploading-item">
            <span>{{ f.name }}</span>
            <el-progress :percentage="f.progress" style="width:200px" />
          </div>
        </div>

        <el-table :data="materials" size="small" v-if="materials.length > 0">
          <el-table-column prop="file_name" :label="t('graph.build.fileName')" />
          <el-table-column :label="t('graph.build.fileSize')" width="100">
            <template #default="{ row }">{{ formatSize(row.file_size) }}</template>
          </el-table-column>
          <el-table-column :label="t('graph.common.status')" width="100">
            <template #default="{ row }">
              <el-tag :type="matStatusType(row.status)" size="small">{{ matStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.build.progress')" width="160">
            <template #default="{ row }">
              <span v-if="row.total_chunks > 0">{{ row.processed_chunks }}/{{ row.total_chunks }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.common.actions')" width="80">
            <template #default="{ row }">
              <el-button size="small" type="danger" text @click="handleDeleteMaterial(row.id)">{{ t('graph.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else :description="t('graph.build.noMaterials')" />
      </el-card>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { buildAPI } from '../api/graphBuild'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const graphId = route.params.id
const taskId = route.params.tid

const task = ref(null)
const materials = ref([])
const pendingCount = ref(0)
const running = ref(false)
const uploadingFiles = ref([])
let pollTimer = null

const canRun = computed(() => task.value && ['pending', 'failed', 'cancelled'].includes(task.value.status))
const canUpload = computed(() => task.value && task.value.status !== 'running')

async function loadTask() {
  try {
    const res = await buildAPI.getTask(graphId, taskId)
    task.value = res
    materials.value = task.value.materials || []
    const countRes = await buildAPI.getPendingCount(graphId)
    pendingCount.value = countRes.count || 0
  } catch (e) {
    ElMessage.error(t('graph.common.loadFailed'))
  }
}

async function handleRun() {
  running.value = true
  try {
    await buildAPI.runTask(graphId, taskId)
    ElMessage.success(t('graph.build.taskStarted'))
    await loadTask()
    startPolling()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.build.startFailed'))
  } finally {
    running.value = false
  }
}

async function handleCancel() {
  try {
    await buildAPI.cancelTask(graphId, taskId)
    ElMessage.success(t('graph.build.taskCancelled'))
    stopPolling()
    await loadTask()
  } catch (e) {
    ElMessage.error(t('graph.build.cancelFailed'))
  }
}

async function handleFileChange(file) {
  const formData = new FormData()
  formData.append('files', file.raw)
  uploadingFiles.value.push({ name: file.name, progress: 0 })

  try {
    await buildAPI.uploadMaterials(graphId, taskId, formData, (e) => {
      const idx = uploadingFiles.value.findIndex(f => f.name === file.name)
      if (idx >= 0) uploadingFiles.value[idx].progress = Math.round(e.loaded / e.total * 100)
    })
    ElMessage.success(t('graph.build.uploadSuccess', { name: file.name }))
    await loadTask()
  } catch (e) {
    ElMessage.error(t('graph.build.uploadFailed', { name: file.name }))
  } finally {
    uploadingFiles.value = uploadingFiles.value.filter(f => f.name !== file.name)
  }
}

async function handleDeleteMaterial(materialId) {
  await ElMessageBox.confirm(t('graph.build.confirmDeleteMaterial'), t('graph.common.confirmDelete'), { type: 'warning' })
  try {
    await buildAPI.deleteMaterial(graphId, taskId, materialId)
    await loadTask()
  } catch (e) {
    ElMessage.error(t('graph.common.deleteFailed'))
  }
}

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    await loadTask()
    if (task.value && task.value.status !== 'running') {
      stopPolling()
    }
  }, 3000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function statusTagType(s) {
  return { pending: 'info', running: 'warning', success: 'success', failed: 'danger', cancelled: '' }[s] || 'info'
}
function statusLabel(s) {
  return {
    pending: t('graph.build.statusPending'),
    running: t('graph.build.statusRunning'),
    success: t('graph.build.statusSuccess'),
    failed: t('graph.build.statusFailed'),
    cancelled: t('graph.build.statusCancelled')
  }[s] || s
}
function matStatusType(s) {
  return { pending: 'info', processing: 'warning', completed: 'success', failed: 'danger' }[s] || 'info'
}
function matStatusLabel(s) {
  return {
    pending: t('graph.build.matPending'),
    processing: t('graph.build.matProcessing'),
    completed: t('graph.build.matCompleted'),
    failed: t('graph.build.statusFailed')
  }[s] || s
}
function formatSize(bytes) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

onMounted(async () => {
  await loadTask()
  if (task.value?.status === 'running') startPolling()
})
onUnmounted(stopPolling)
</script>

<style scoped>
.build-task-detail { padding: 20px; }
.page-header { display: flex; align-items: center; gap: 12px; margin-bottom: 20px; }
.page-header h2 { margin: 0; flex: 1; }
.header-actions { display: flex; gap: 8px; }
.loading-wrap { text-align: center; padding: 60px; font-size: 24px; }
.info-card, .materials-card { margin-bottom: 16px; }
.info-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 16px; }
.info-item { display: flex; align-items: center; gap: 8px; }
.info-item .label { color: #666; font-size: 13px; min-width: 70px; }
.stats-row { display: flex; gap: 32px; padding: 12px 0; border-top: 1px solid #f0f0f0; }
.error-msg { color: #f56c6c; font-size: 13px; margin-top: 8px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.uploading-list { margin-bottom: 12px; }
.uploading-item { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
</style>
