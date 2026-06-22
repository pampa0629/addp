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
        <el-input :value="targetEngineName" disabled />
      </el-form-item>

      <el-form-item :label="t('manager.import.targetSchema')">
        <el-input :value="schemaName" disabled />
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
          :value="t('manager.import.encodingFromCpg', { encoding: detectedEncoding })"
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
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution } from '@addp/common-frontend'
import { importData, getTransferExecutionStatus, getMetaScanExecutionStatus } from '@/api/import'
import JSZip from 'jszip'

const { t } = useI18n()

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

const sleep = (ms) => new Promise(resolve => window.setTimeout(resolve, ms))

const metaScanExecutionIdOf = (execution) => {
  const scan = execution?.metadata?.metadata_scan
  return String(scan?.execution_id || scan?.executionId || '').trim()
}

const waitForMetaScanExecution = async (executionId) => {
  if (!executionId) return
  progress.value = 95
  progressText.value = t('manager.import.scanningMetadata')

  for (let i = 0; i < 120; i += 1) {
    const execution = await getMetaScanExecutionStatus(executionId)
    const status = String(execution?.status || '').toLowerCase()
    if (status === 'success') {
      return
    }
    if (['failed', 'timeout', 'cancelled', 'canceled'].includes(status)) {
      throw new Error(execution?.error_message || execution?.error || execution?.current_step || status)
    }
    await sleep(2000)
  }
  throw new Error('metadata scan timeout')
}

const handleImport = async () => {
  if (form.value.files.length === 0) {
    ElMessage.warning(t('manager.import.selectFiles'))
    return
  }

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
    transferExecutionId.value = result.transfer_execution_id

    progress.value = 30
    progressText.value = t('manager.import.uploadSuccess')

    startPolling()
  } catch (error) {
    importing.value = false
    const errorMsg = error.response?.data?.error || error.message
    ElMessage.error(t('manager.import.importFailed', { msg: errorMsg }))
  }
}

const startPolling = () => {
  pollingTimer.value = setInterval(async () => {
    try {
      const execution = await getTransferExecutionStatus(transferExecutionId.value)

      if (execution.status === 'pending' || execution.status === 'running') {
        progress.value = execution.status === 'running' ? 60 : 40
        progressText.value = t('manager.import.importingData')
      } else if (execution.status === 'success') {
        const scanExecutionId = metaScanExecutionIdOf(execution)
        metaScanExecutionId.value = scanExecutionId
        if (scanExecutionId) {
          stopPolling()
          try {
            await waitForMetaScanExecution(scanExecutionId)
          } catch (error) {
            progressStatus.value = 'exception'
            progressText.value = t('manager.import.importFailed', { msg: error.message })
            importing.value = false
            ElMessage.error(t('manager.import.importFailed', { msg: error.message }))
            return
          }
        }
        progress.value = 100
        progressStatus.value = 'success'
        progressText.value = t('manager.import.importSuccess')
        stopPolling()
        setTimeout(() => {
          importing.value = false
          ElMessage.success(t('manager.import.importSuccessMsg'))
          emit('success')
          handleClose()
          void openMonitorExecution(transferExecutionId.value)
        }, 1500)
      } else if (execution.status === 'failed') {
        progressStatus.value = 'exception'
        progressText.value = t('manager.import.importFailed', { msg: execution.error_msg || '' })
        stopPolling()
        importing.value = false
        ElMessage.error(t('manager.import.importFailedMsg'))
      }
    } catch (error) {
      console.error('轮询任务状态失败:', error)
    }
  }, 2000)
}

const stopPolling = () => {
  if (pollingTimer.value) {
    clearInterval(pollingTimer.value)
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
  if (uploadRef.value) {
    uploadRef.value.clearFiles()
  }
  visible.value = false
}
</script>

<style scoped>
.import-progress {
  margin-top: 20px;
}

.progress-text {
  margin-top: 10px;
  text-align: center;
  color: #606266;
  font-size: 14px;
}

:deep(.el-upload-dragger) {
  padding: 40px;
}
</style>
