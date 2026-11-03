<template>
  <el-dialog
    :model-value="modelValue"
    :title="t('manager.upload.title')"
    width="520px"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="reset"
  >
    <el-form label-width="120px">
      <el-form-item :label="t('manager.upload.targetNode')">
        <el-input :model-value="targetLabel" disabled />
      </el-form-item>
      <el-form-item :label="t('manager.upload.files')" required>
        <el-upload
          ref="uploadRef"
          drag
          multiple
          :auto-upload="false"
          :file-list="fileList"
          :on-change="handleFileChange"
          :on-remove="handleFileRemove"
        >
          <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
          <div class="el-upload__text">
            {{ t('manager.upload.uploadHint') }}
          </div>
          <template #tip>
            <div class="el-upload__tip">{{ t('manager.upload.uploadTip') }}</div>
          </template>
        </el-upload>
      </el-form-item>
      <el-alert
        v-if="progressText"
        :title="progressText"
        type="info"
        :closable="false"
        show-icon
      />
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('manager.upload.cancel') }}</el-button>
      <el-button type="primary" :loading="uploading" @click="handleUpload">
        {{ uploading ? t('manager.upload.uploading') : t(uploadId ? 'manager.upload.checkStatus' : 'manager.upload.start') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { dataExplorerAPI } from '@/api/dataExplorer'
import { getMetaScanExecutionStatus } from '@/api/import'

const { t } = useI18n()
const ACTIVE_SCAN_STATUSES = new Set(['pending', 'running'])
const SUCCESS_SCAN_STATUSES = new Set(['success'])
const FAILED_SCAN_STATUSES = new Set(['failed', 'timeout', 'cancelled', 'canceled'])
const SCAN_POLL_INTERVAL_MS = 2000

const props = defineProps({
  modelValue: Boolean,
  targetNodeLocator: {
    type: String,
    default: ''
  },
  targetLabel: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:modelValue', 'success'])

const uploadRef = ref(null)
const fileList = ref([])
const uploading = ref(false)
const progressText = ref('')
const uploadId = ref('')
let pollGeneration = 0
let committedPayload = null

const handleFileChange = (_file, files) => {
  fileList.value = files
}

const handleFileRemove = (_file, files) => {
  fileList.value = files
}

const handleUpload = async () => {
  if (uploadId.value) { await checkUploadedStatus(); return }
  const files = fileList.value.map(file => file.raw).filter(Boolean)
  if (!files.length) {
    ElMessage.warning(t('manager.upload.selectFiles'))
    return
  }
  uploading.value = true
  progressText.value = t('manager.upload.uploading')
  try {
    const formData = new FormData()
    formData.append('target_node_locator', props.targetNodeLocator)
    files.forEach(file => formData.append('files', file))
    const payload = await dataExplorerAPI.uploadFiles(formData)
    if (payload.upload_id) {
      uploadId.value = payload.upload_id
      committedPayload = payload
      fileList.value = []
      uploadRef.value?.clearFiles()
      progressText.value = t(payload.failed_files?.length ? 'manager.upload.partialCommitted' : 'manager.upload.initialPending')
      await checkUploadedStatus()
      return
    }
    const scanExecutionId = payload.scan_execution_id
    progressText.value = t('manager.upload.scanningMetadata')
    await waitForMetaScanExecution(scanExecutionId)
    progressText.value = t('manager.upload.uploadSuccess')
    ElMessage.success(t('manager.upload.uploadSuccess'))
    emit('success', payload)
    emit('update:modelValue', false)
  } catch (error) {
    progressText.value = ''
    const message = error.response?.data?.error || error.message || String(error)
    ElMessage.error(t('manager.upload.uploadFailed', { msg: message }))
  } finally {
    uploading.value = false
  }
}

const checkUploadedStatus = async () => {
  const generation = ++pollGeneration
  uploading.value = true
  try {
    for (let attempt = 0; attempt < 120; attempt += 1) {
      const state = await dataExplorerAPI.getOwnUpload(uploadId.value)
      if (generation !== pollGeneration) return
      if (state.initial_status === 'settled' || state.initial_status === 'denied') {
        const outcomes = Array.isArray(state.outcomes) ? state.outcomes : []
        const issued = outcomes.filter(item => item.outcome === 'issued')
        const hasRead = issued.some(item => item.action === 'read')
        const key = state.initial_status === 'denied' || !issued.length
          ? 'manager.upload.initialNotGranted'
          : hasRead ? 'manager.upload.initialReadIssued' : 'manager.upload.initialMetadataIssued'
        progressText.value = t(key)
        ElMessage.info(t(key))
        emit('success', { ...committedPayload, ...state, metadataReady: state.status !== 'failed' && Boolean(state.scan_execution_id) })
        emit('update:modelValue', false)
        return
      }
      if (state.initial_status === 'pending_receipt') break
      progressText.value = t('manager.upload.initialPending')
      await sleep(SCAN_POLL_INTERVAL_MS)
      if (generation !== pollGeneration) return
    }
    progressText.value = t('manager.upload.initialPending')
  } catch (error) {
    // The physical upload was acknowledged. A status failure must never invite
    // another upload POST or label the confirmed bytes as failed.
    if (generation === pollGeneration) progressText.value = t('manager.upload.initialStatusUnavailable')
  } finally {
    if (generation === pollGeneration) uploading.value = false
  }
}
onBeforeUnmount(() => { pollGeneration += 1 })

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms))

const waitForMetaScanExecution = async (executionId) => {
  for (let i = 0; i < 120; i += 1) {
    const execution = await getMetaScanExecutionStatus(executionId)
    const status = String(execution?.status || '').toLowerCase()
    if (SUCCESS_SCAN_STATUSES.has(status)) {
      return execution
    }
    if (FAILED_SCAN_STATUSES.has(status)) {
      throw new Error(execution?.error_message || execution?.error || execution?.current_step || status)
    }
    if (status && !ACTIVE_SCAN_STATUSES.has(status)) {
      return execution
    }
    await sleep(SCAN_POLL_INTERVAL_MS)
  }
  throw new Error('metadata scan timeout')
}

const reset = () => {
  pollGeneration += 1
  uploadId.value = ''
  committedPayload = null
  fileList.value = []
  progressText.value = ''
  uploading.value = false
  uploadRef.value?.clearFiles()
}
</script>
