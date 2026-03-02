<template>
  <el-card class="section-card">
    <template #header>
      <div class="card-header">
        <h3><el-icon class="header-icon"><Document /></el-icon>关联文档</h3>
        <div class="header-actions">
          <el-button size="small" @click="openLinkDialog">关联已有文档</el-button>
          <el-button size="small" type="primary" @click="openUploadDialog">上传新文档</el-button>
        </div>
      </div>
    </template>

    <div v-if="docs.length === 0 && !loading" class="empty-tip">
      <el-empty description="暂无关联文档" :image-size="60" />
    </div>

    <el-table v-else :data="docs" v-loading="loading" size="small">
      <el-table-column label="文档名称" prop="name" min-width="160" show-overflow-tooltip />
      <el-table-column label="类型" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="docTypeTagType(row.doc_type)">{{ docTypeLabel(row.doc_type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="来源/标准号" prop="source_org" width="140" show-overflow-tooltip />
      <el-table-column label="附件" width="100">
        <template #default="{ row }">
          <span v-if="row.file_name" class="file-tag">
            <el-icon style="vertical-align:middle;margin-right:2px"><Paperclip /></el-icon>
            <span class="file-name-text" :title="row.file_name">{{ row.file_name }}</span>
          </span>
          <span v-else class="no-file">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120" align="center" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.file_name" link size="small" type="primary" @click="downloadDoc(row)">下载</el-button>
          <el-button link size="small" type="danger" @click="unlinkDoc(row)">解除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 上传新文档对话框 -->
    <el-dialog v-model="showUploadDialog" title="上传并关联文档" width="500px" @closed="resetUploadForm">
      <el-form :model="uploadForm" label-width="90px" size="default">
        <el-form-item label="文档名称" required>
          <el-input v-model="uploadForm.name" placeholder="如：GB/T 35273-2020 个人信息安全规范" />
        </el-form-item>
        <el-form-item label="文档类型">
          <el-select v-model="uploadForm.doc_type" style="width:100%">
            <el-option label="国家标准" value="national" />
            <el-option label="行业标准" value="industry" />
            <el-option label="企业内部" value="internal" />
            <el-option label="参考资料" value="reference" />
          </el-select>
        </el-form-item>
        <el-form-item label="来源/标准号">
          <el-input v-model="uploadForm.source_org" placeholder="如：国家市场监督管理总局" />
        </el-form-item>
        <el-form-item label="版本号">
          <el-input v-model="uploadForm.version" placeholder="如：2020-01" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="uploadForm.description" type="textarea" :rows="2" placeholder="可选" />
        </el-form-item>
        <el-form-item label="上传文件">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            :on-change="onFileChange"
            :on-exceed="() => ElMessage.warning('只能上传一个文件')"
            accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt"
          >
            <el-button size="small">选择文件</el-button>
            <template #tip>
              <div class="el-upload__tip">支持 PDF、Word、Excel、PPT 等格式（可选）</div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showUploadDialog = false">取消</el-button>
        <el-button type="primary" @click="submitUpload" :loading="uploading" :disabled="!uploadForm.name">
          {{ selectedFile ? '上传并关联' : '仅录入元数据' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关联已有文档对话框 -->
    <el-dialog v-model="showLinkDialog" title="关联已有文档" width="500px" @open="onLinkDialogOpen">
      <div class="link-tip">搜索已有文档，选择后将关联到当前标准项</div>
      <el-select
        v-model="selectedDocIds"
        multiple
        filterable
        remote
        :remote-method="searchDocs"
        :loading="docSearchLoading"
        placeholder="输入名称或来源搜索文档"
        style="width: 100%; margin-top: 12px"
        size="large"
      >
        <el-option
          v-for="doc in searchedDocs"
          :key="doc.id"
          :label="`${doc.name}${doc.source_org ? '（' + doc.source_org + '）' : ''}`"
          :value="doc.id"
          :disabled="isAlreadyLinked(doc.id)"
        />
      </el-select>
      <template #footer>
        <el-button @click="showLinkDialog = false">取消</el-button>
        <el-button type="primary" @click="confirmLink" :disabled="selectedDocIds.length === 0" :loading="linking">
          确认关联
        </el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Paperclip } from '@element-plus/icons-vue'
import { elementDocumentAPI, glossaryDocumentAPI, metricDocumentAPI, documentAPI } from '../api/standard'

const props = defineProps({
  entityType: {
    type: String,
    required: true,
    validator: (v) => ['element', 'glossary', 'metric'].includes(v)
  },
  entityId: {
    type: Number,
    default: null
  }
})

// 根据实体类型选择对应的 API
const api = {
  element: elementDocumentAPI,
  glossary: glossaryDocumentAPI,
  metric: metricDocumentAPI
}

const docs = ref([])
const loading = ref(false)

// 上传新文档
const showUploadDialog = ref(false)
const uploading = ref(false)
const selectedFile = ref(null)
const uploadRef = ref(null)
const uploadForm = ref({
  name: '',
  doc_type: 'reference',
  source_org: '',
  version: '',
  description: ''
})

// 关联已有文档
const showLinkDialog = ref(false)
const linking = ref(false)
const docSearchLoading = ref(false)
const searchedDocs = ref([])
const selectedDocIds = ref([])

const docTypeLabel = (t) => ({ national: '国家标准', industry: '行业标准', internal: '企业内部', reference: '参考资料' }[t] || t)
const docTypeTagType = (t) => ({ national: 'danger', industry: 'warning', internal: 'success', reference: '' }[t] || '')

const getToken = () => localStorage.getItem('token') || ''

const loadDocs = async () => {
  if (!props.entityId) return
  loading.value = true
  try {
    const res = await api[props.entityType].list(props.entityId)
    docs.value = res.data || []
  } catch (e) {
    console.error('加载关联文档失败:', e)
  } finally {
    loading.value = false
  }
}

const isAlreadyLinked = (docId) => docs.value.some(d => d.id === docId)

const downloadDoc = (doc) => {
  const url = api[props.entityType].downloadUrl(doc.id)
  const token = getToken()
  window.open(url + (token ? `?token=${encodeURIComponent(token)}` : ''), '_blank')
}

const unlinkDoc = async (doc) => {
  try {
    await ElMessageBox.confirm(`解除与「${doc.name}」的关联？文档本身不会被删除。`, '提示', { type: 'warning' })
    await api[props.entityType].unlink(props.entityId, doc.id)
    ElMessage.success('已解除关联')
    await loadDocs()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
  }
}

const openUploadDialog = () => {
  showUploadDialog.value = true
}

const resetUploadForm = () => {
  uploadForm.value = { name: '', doc_type: 'reference', source_org: '', version: '', description: '' }
  selectedFile.value = null
  uploadRef.value?.clearFiles()
}

const onFileChange = (file) => {
  selectedFile.value = file.raw
  if (!uploadForm.value.name) {
    // 用文件名（去扩展名）作为默认文档名
    uploadForm.value.name = file.name.replace(/\.[^.]+$/, '')
  }
}

const submitUpload = async () => {
  if (!uploadForm.value.name) {
    ElMessage.warning('请填写文档名称')
    return
  }
  uploading.value = true
  try {
    // 1. 创建文档元数据并关联到当前标准项
    const res = await api[props.entityType].create(props.entityId, uploadForm.value)
    const doc = res.data
    // 2. 如果选择了文件则上传
    if (selectedFile.value) {
      const formData = new FormData()
      formData.append('file', selectedFile.value)
      await api[props.entityType].uploadFile(doc.id, formData)
    }
    ElMessage.success('关联成功')
    showUploadDialog.value = false
    await loadDocs()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    uploading.value = false
  }
}

const openLinkDialog = () => {
  showLinkDialog.value = true
}

const onLinkDialogOpen = () => {
  selectedDocIds.value = []
  searchDocs('')
}

const searchDocs = async (keyword) => {
  docSearchLoading.value = true
  try {
    const res = await documentAPI.list({ keyword, page_size: 50 })
    searchedDocs.value = res.data || []
  } catch (e) {
    searchedDocs.value = []
  } finally {
    docSearchLoading.value = false
  }
}

const confirmLink = async () => {
  if (selectedDocIds.value.length === 0) return
  linking.value = true
  try {
    await Promise.all(
      selectedDocIds.value.map(docId => api[props.entityType].link(props.entityId, docId))
    )
    ElMessage.success('关联成功')
    showLinkDialog.value = false
    selectedDocIds.value = []
    await loadDocs()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '关联失败')
  } finally {
    linking.value = false
  }
}

watch(() => props.entityId, (val) => {
  if (val) loadDocs()
})

onMounted(() => {
  if (props.entityId) loadDocs()
})
</script>

<style scoped>
.section-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 15px;
  color: var(--el-text-color-primary);
}

.header-icon {
  color: var(--el-color-primary);
}

.header-actions {
  display: flex;
  gap: 8px;
}

.empty-tip {
  padding: 16px 0;
}

.file-tag {
  display: flex;
  align-items: center;
  font-size: 12px;
  color: var(--el-color-primary);
  max-width: 90px;
  overflow: hidden;
}

.file-name-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.no-file {
  color: var(--el-text-color-placeholder);
}

.link-tip {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
