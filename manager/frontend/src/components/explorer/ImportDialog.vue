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
        <el-input :value="engineName" disabled />
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

      <el-form-item :label="t('manager.import.encoding')">
        <el-select v-model="form.encoding" :placeholder="t('manager.import.encodingPlaceholder')">
          <el-option label="UTF-8" value="UTF-8" />
          <el-option label="GBK" value="GBK" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('manager.import.file')">
        <el-upload
          ref="uploadRef"
          :auto-upload="false"
          :limit="1"
          :on-change="handleFileChange"
          :on-exceed="handleExceed"
          accept=".zip"
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
        :disabled="!form.file"
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
import { importData, getTransferExecutionStatus } from '@/api/import'

const { t } = useI18n()

const props = defineProps({
  modelValue: Boolean,
  engineId: Number,
  engineName: String,
  schemaName: String
})

const emit = defineEmits(['update:modelValue', 'success'])

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const uploadRef = ref(null)
const form = ref({
  file: null,
  targetTable: '',
  encoding: 'UTF-8'
})

const importing = ref(false)
const progress = ref(0)
const progressStatus = ref('')
const progressText = ref('')
const transferExecutionId = ref(null)
const pollingTimer = ref(null)

const handleFileChange = (file) => {
  form.value.file = file.raw
}

const handleExceed = () => {
  ElMessage.warning(t('manager.import.fileLimitExceeded'))
}

const handleImport = async () => {
  if (!form.value.file) {
    ElMessage.warning(t('manager.import.selectFile'))
    return
  }

  importing.value = true
  progress.value = 0
  progressStatus.value = ''
  progressText.value = t('manager.import.uploading')

  try {
    const formData = new FormData()
    formData.append('file', form.value.file)
    formData.append('target_engine_id', props.engineId)
    formData.append('target_schema', props.schemaName)
    if (form.value.targetTable) {
      formData.append('target_table', form.value.targetTable)
    }
    if (form.value.encoding) {
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
        progress.value = 100
        progressStatus.value = 'success'
        progressText.value = t('manager.import.importSuccess')
        stopPolling()
        setTimeout(() => {
          importing.value = false
          ElMessage.success(t('manager.import.importSuccessMsg'))
          emit('success')
          handleClose()
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
    file: null,
    targetTable: '',
    encoding: 'UTF-8'
  }
  importing.value = false
  progress.value = 0
  progressStatus.value = ''
  progressText.value = ''
  transferExecutionId.value = null
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
