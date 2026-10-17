<template>
  <el-dialog
    v-model="visible"
    :title="t('manager.import.title')"
    width="600px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form :model="form" label-width="120px">
      <el-form-item :label="t('manager.import.targetEngine')">
        <el-input :model-value="targetEngineName" disabled />
      </el-form-item>

      <el-form-item :label="t('manager.import.targetSchema')">
        <el-input :model-value="schemaName" disabled />
      </el-form-item>

      <el-form-item :label="t('manager.import.targetTable')">
        <el-input
          v-model="form.targetTable"
          :placeholder="t('manager.import.targetTablePlaceholder')"
          clearable
        />
      </el-form-item>

      <el-form-item v-if="detectedEncoding" :label="t('manager.import.encoding')">
        <el-input
          :model-value="t('manager.import.encodingFromCpg', { encoding: detectedEncoding })"
          disabled
        />
      </el-form-item>

      <el-form-item v-else :label="t('manager.import.encoding')">
        <el-select v-model="form.encoding" :placeholder="t('manager.import.encodingPlaceholder')">
          <el-option label="UTF-8" value="UTF-8" />
          <el-option label="GBK" value="GBK" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('manager.import.file')">
        <el-upload
          ref="uploadRef"
          :auto-upload="false"
          multiple
          :on-change="handleFileChange"
          :on-exceed="handleExceed"
          :on-remove="handleFileRemove"
          accept=".zip,.shp,.shx,.dbf,.prj,.qpj,.cpg"
          drag
        >
          <el-icon class="el-icon--upload"><upload-filled /></el-icon>
          <div class="el-upload__text">
            {{ t('manager.import.uploadHint') }}
          </div>
          <template #tip>
            <div class="el-upload__tip">
              {{ t('manager.import.uploadTip') }}
            </div>
          </template>
        </el-upload>
      </el-form-item>
    </el-form>

    <!-- 进度显示 -->
    <div v-if="importing" class="import-progress">
      <el-progress :percentage="progress" :status="progressStatus" />
      <div class="progress-text">{{ progressText }}</div>
    </div>

    <p v-if="failedExecution?.phase === 'meta'" class="progress-text">
      {{ t('manager.import.metadataSyncIncomplete') }}
    </p>
    <ExecutionFailure
      v-if="failedExecution"
      :status="failedExecution.status"
      :error-details="failedExecution.error_details || {}"
      :execution-id="failedExecution.execution_id"
      :show-monitor-link="authStore.hasPermission('monitor.execution.read')"
    />

    <template #footer>
      <el-button @click="handleClose" :disabled="importing">{{ t('manager.import.cancel') }}</el-button>
      <el-button
        type="primary"
        @click="handleImport"
        :loading="importing"
        :disabled="form.files.length === 0"
      >
        {{ importing ? t('manager.import.importing') : t('manager.import.start') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { ExecutionFailure, openMonitorExecution } from '@addp/common-frontend'
import { useAuthStore } from '@/store/auth'
import { importData, getTransferExecutionStatus, getMetaScanExecutionStatus } from '@/api/import'
import JSZip from 'jszip'

const { t } = useI18n()
const authStore = useAuthStore()

const props = defineProps({
  modelValue: Boolean,
  engineId: Number,
  engineName: String,
  schemaName: String,
  targetNodeLocator: String
})

const emit = defineEmits(['update:modelValue', 'success'])

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const uploadRef = ref(null)
const form = ref({
  files: [],
  targetTable: '',
  encoding: 'UTF-8'
})
const detectedEncoding = ref('')

const importing = ref(false)
const progress = ref(0)
const progressStatus = ref('')
const progressText = ref('')
const transferExecutionId = ref(null)
const pollingTimer = ref(null)
const metaScanExecutionId = ref(null)
const failedExecution = ref(null)
let pollingRun = 0
let successTimer = null

const targetEngineName = computed(() => {
  const name = String(props.engineName || '').trim()
  if (name) return name
  return props.engineId ? `#${props.engineId}` : '-'
})

const syncUploadFiles = (uploadFiles = []) => {
  form.value.files = uploadFiles
    .map(item => item.raw)
    .filter(Boolean)
}

const normalizeEncoding = (text) => String(text || '').trim()

const fileExtension = (name) => {
  const index = String(name || '').lastIndexOf('.')
  return index >= 0 ? String(name).slice(index).toLowerCase() : ''
}

const readFileText = (file) => new Promise((resolve, reject) => {
  const reader = new FileReader()
  reader.onload = () => resolve(normalizeEncoding(reader.result))
  reader.onerror = () => reject(reader.error)
  reader.readAsText(file)
})

const detectCpgEncodingFromZip = async (file) => {
  const zip = await JSZip.loadAsync(file)
  const cpgEntry = Object.values(zip.files).find(entry =>
    !entry.dir && fileExtension(entry.name) === '.cpg'
  )
  if (!cpgEntry) return ''
  return normalizeEncoding(await cpgEntry.async('text'))
}

const detectCpgEncoding = async (files) => {
  const zipFile = files.length === 1 && fileExtension(files[0].name) === '.zip' ? files[0] : null
  if (zipFile) {
    return detectCpgEncodingFromZip(zipFile)
  }
  const cpgFile = files.find(file => fileExtension(file.name) === '.cpg')
  if (!cpgFile) return ''
  return readFileText(cpgFile)
}

const updateDetectedEncoding = async () => {
  const files = form.value.files
  if (files.length === 0) {
    detectedEncoding.value = ''
    return
  }
  try {
    detectedEncoding.value = await detectCpgEncoding(files)
  } catch (error) {
    console.warn('读取 Shapefile CPG 编码声明失败:', error)
    detectedEncoding.value = ''
  }
}

const handleFileChange = async (_file, uploadFiles) => {
  syncUploadFiles(uploadFiles)
  await updateDetectedEncoding()
}

const handleFileRemove = async (_file, uploadFiles) => {
  syncUploadFiles(uploadFiles)
  await updateDetectedEncoding()
}

const handleExceed = () => {
  ElMessage.warning(t('manager.import.fileLimitExceeded'))
}

const handleImport = async () => {
  if (form.value.files.length === 0) {
    ElMessage.warning(t('manager.import.selectFiles'))
    return
  }

  stopPolling()
  const run = pollingRun
  failedExecution.value = null
  metaScanExecutionId.value = null
  importing.value = true
  progress.value = 0
  progressStatus.value = ''
  progressText.value = t('manager.import.uploading')

  try {
    const formData = new FormData()
    form.value.files.forEach(file => formData.append('files', file))
    formData.append('target_node_locator', props.targetNodeLocator || '')
    if (form.value.targetTable) {
      formData.append('target_table', form.value.targetTable)
    }
    if (!detectedEncoding.value && form.value.encoding) {
      formData.append('encoding', form.value.encoding)
    }

    const result = await importData(formData)
    if (run !== pollingRun) return
    transferExecutionId.value = result.transfer_execution_id

    progress.value = 30
    progressText.value = t('manager.import.uploadSuccess')

    startPolling(run)
  } catch (error) {
    if (run !== pollingRun) return
    importing.value = false
    const errorMsg = error.response?.data?.error || error.message
    ElMessage.error(t('manager.import.importFailed', { msg: errorMsg }))
  }
}

const failExecution = (execution, executionId, phase = 'transfer') => {
  failedExecution.value = {
    status: execution.status,
    error_details: execution.error_details,
    execution_id: executionId,
    phase
  }
  progressStatus.value = 'exception'
  stopPolling()
  importing.value = false
  ElMessage.error(t('manager.import.importFailedMsg'))
}

const startPolling = (run) => {
  const poll = async () => {
    try {
      const scanId = metaScanExecutionId.value
      const executionId = scanId || transferExecutionId.value
      const execution = await (scanId ? getMetaScanExecutionStatus(executionId) : getTransferExecutionStatus(executionId))
      if (run !== pollingRun) return

      if (execution.status === 'pending' || execution.status === 'running') {
        progress.value = scanId ? 95 : execution.status === 'running' ? 60 : 40
        progressText.value = t(scanId ? 'manager.import.scanningMetadata' : 'manager.import.importingData')
      } else if (execution.status === 'success') {
        if (!scanId) {
          const scan = execution.metadata?.metadata_scan
          if (scan?.status !== 'submitted' || !scan.execution_id) {
            failExecution({ status: 'failed', error_details: { category: 'execution_failed', code: scan?.error_code } }, executionId, 'meta')
            return
          }
          metaScanExecutionId.value = scan.execution_id
          progress.value = 95
          progressText.value = t('manager.import.scanningMetadata')
          pollingTimer.value = setTimeout(poll, 2000)
          return
        }
        progress.value = 100
        progressStatus.value = 'success'
        progressText.value = t('manager.import.importSuccess')
        successTimer = setTimeout(() => {
          if (run !== pollingRun) return
          importing.value = false
          ElMessage.success(t('manager.import.importSuccessMsg'))
          const completedId = transferExecutionId.value
          emit('success')
          handleClose()
          if (authStore.hasPermission('monitor.execution.read')) void openMonitorExecution(completedId)
        }, 1500)
        return
      } else if (['failed', 'timeout', 'cancelled'].includes(execution.status)) {
        failExecution(execution, executionId, scanId ? 'meta' : 'transfer')
        return
      }
    } catch (error) {
      console.error('轮询任务状态失败:', error)
    }
    if (run === pollingRun) pollingTimer.value = setTimeout(poll, 2000)
  }
  pollingTimer.value = setTimeout(poll, 2000)
}

const stopPolling = () => {
  pollingRun += 1
  clearTimeout(successTimer)
  successTimer = null
  if (pollingTimer.value) {
    clearTimeout(pollingTimer.value)
    pollingTimer.value = null
  }
}

const handleClose = () => {
  stopPolling()
  form.value = {
    files: [],
    targetTable: '',
    encoding: 'UTF-8'
  }
  detectedEncoding.value = ''
  importing.value = false
  progress.value = 0
  progressStatus.value = ''
  progressText.value = ''
  transferExecutionId.value = null
  metaScanExecutionId.value = null
  failedExecution.value = null
  if (uploadRef.value) {
    uploadRef.value.clearFiles()
  }
  visible.value = false
}

watch(() => props.modelValue, value => { if (!value) handleClose() })
onBeforeUnmount(stopPolling)
</script>

<style scoped>
.import-progress {
  margin-top: 20px;
}

.progress-text {
  margin-top: 10px;
  text-align: center;
  color: var(--el-text-color-regular);
  font-size: 14px;
}

:deep(.el-upload-dragger) {
  padding: 40px;
}
</style>
