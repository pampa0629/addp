<template>
  <el-dialog
    v-model="visible"
    title="导入数据"
    width="600px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form :model="form" label-width="120px">
      <el-form-item label="目标引擎">
        <el-input :value="engineName" disabled />
      </el-form-item>

      <el-form-item label="目标 Schema">
        <el-input :value="schemaName" disabled />
      </el-form-item>

      <el-form-item label="目标表名">
        <el-input
          v-model="form.targetTable"
          placeholder="留空则使用文件名"
          clearable
        />
      </el-form-item>

      <el-form-item label="文件编码">
        <el-select v-model="form.encoding" placeholder="选择编码">
          <el-option label="UTF-8" value="UTF-8" />
          <el-option label="GBK" value="GBK" />
        </el-select>
      </el-form-item>

      <el-form-item label="选择文件">
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
            拖拽文件到此处或 <em>点击上传</em>
          </div>
          <template #tip>
            <div class="el-upload__tip">
              仅支持 Shapefile ZIP 包（包含 .shp/.dbf/.shx 文件）
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
      <el-button @click="handleClose" :disabled="importing">取消</el-button>
      <el-button
        type="primary"
        @click="handleImport"
        :loading="importing"
        :disabled="!form.file"
      >
        {{ importing ? '导入中...' : '开始导入' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { importData, getTransferExecutionStatus } from '@/api/import'

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

// 文件选择
const handleFileChange = (file) => {
  form.value.file = file.raw
}

// 文件数量超限
const handleExceed = () => {
  ElMessage.warning('只能上传一个文件')
}

// 开始导入
const handleImport = async () => {
  if (!form.value.file) {
    ElMessage.warning('请选择文件')
    return
  }

  importing.value = true
  progress.value = 0
  progressStatus.value = ''
  progressText.value = '正在上传文件...'

  try {
    // 构建 FormData
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

    // 调用导入 API
    const result = await importData(formData)
    transferExecutionId.value = result.transfer_execution_id

    progress.value = 30
    progressText.value = '文件上传成功，正在导入数据...'

    // 开始轮询任务状态
    startPolling()
  } catch (error) {
    importing.value = false
    const errorMsg = error.response?.data?.error || error.message
    ElMessage.error('导入失败: ' + errorMsg)
  }
}

// 轮询执行状态
const startPolling = () => {
  pollingTimer.value = setInterval(async () => {
    try {
      const execution = await getTransferExecutionStatus(transferExecutionId.value)

      if (execution.status === 'pending' || execution.status === 'running') {
        progress.value = execution.status === 'running' ? 60 : 40
        progressText.value = '正在导入数据...'
      } else if (execution.status === 'success') {
        progress.value = 100
        progressStatus.value = 'success'
        progressText.value = '导入成功！'
        stopPolling()
        setTimeout(() => {
          importing.value = false
          ElMessage.success('数据导入成功')
          emit('success')
          handleClose()
        }, 1500)
      } else if (execution.status === 'failed') {
        progressStatus.value = 'exception'
        progressText.value = '导入失败: ' + (execution.error_msg || '未知错误')
        stopPolling()
        importing.value = false
        ElMessage.error('导入失败')
      }
    } catch (error) {
      console.error('轮询任务状态失败:', error)
    }
  }, 2000) // 每 2 秒轮询一次
}

// 停止轮询
const stopPolling = () => {
  if (pollingTimer.value) {
    clearInterval(pollingTimer.value)
    pollingTimer.value = null
  }
}

// 关闭对话框
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
