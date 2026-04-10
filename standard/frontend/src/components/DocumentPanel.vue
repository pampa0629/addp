<template>
  <el-card class="section-card">
    <template #header>
      <div class="card-header">
        <h3><el-icon class="header-icon"><Document /></el-icon>{{ $t('standard.documentPanel.title') }}</h3>
        <div class="header-actions">
          <el-button size="small" @click="openLinkDialog">{{ $t('standard.documentPanel.linkExisting') }}</el-button>
          <el-button size="small" type="primary" @click="openUploadDialog">{{ $t('standard.documentPanel.uploadNew') }}</el-button>
        </div>
      </div>
    </template>

    <div v-if="docs.length === 0 && !loading" class="empty-tip">
      <el-empty :description="$t('standard.documentPanel.noDocuments')" :image-size="60" />
    </div>

    <el-table v-else :data="docs" v-loading="loading" size="small">
      <el-table-column :label="$t('standard.documentPanel.docName')" prop="name" min-width="160" show-overflow-tooltip />
      <el-table-column :label="$t('standard.documentPanel.docType')" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="docTypeTagType(row.doc_type)">{{ docTypeLabel(row.doc_type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="$t('standard.documentPanel.docSource')" prop="source_org" width="140" show-overflow-tooltip />
      <el-table-column :label="$t('standard.documentPanel.docAttachment')" width="100">
        <template #default="{ row }">
          <span v-if="row.file_name" class="file-tag">
            <el-icon style="vertical-align:middle;margin-right:2px"><Paperclip /></el-icon>
            <span class="file-name-text" :title="row.file_name">{{ row.file_name }}</span>
          </span>
          <span v-else class="no-file">—</span>
        </template>
      </el-table-column>
      <el-table-column :label="$t('standard.documentPanel.docActions')" width="120" align="center" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.file_name" link size="small" type="primary" @click="downloadDoc(row)">{{ $t('standard.documentPanel.download') }}</el-button>
          <el-button link size="small" type="danger" @click="unlinkDoc(row)">{{ $t('standard.documentPanel.unlink') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 上传新文档对话框 -->
    <el-dialog v-model="showUploadDialog" :title="$t('standard.documentPanel.uploadTitle')" width="500px" @closed="resetUploadForm">
      <el-form :model="uploadForm" label-width="90px" size="default">
        <el-form-item :label="$t('standard.documentPanel.nameLabel')" required>
          <el-input v-model="uploadForm.name" :placeholder="$t('standard.document.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.documentPanel.typeLabel')">
          <el-select v-model="uploadForm.doc_type" style="width:100%">
            <el-option :label="$t('standard.document.national')" value="national" />
            <el-option :label="$t('standard.document.industry')" value="industry" />
            <el-option :label="$t('standard.document.internal')" value="internal" />
            <el-option :label="$t('standard.document.reference')" value="reference" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.documentPanel.sourceLabel')">
          <el-input v-model="uploadForm.source_org" :placeholder="$t('standard.document.sourcePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.documentPanel.versionLabel')">
          <el-input v-model="uploadForm.version" :placeholder="$t('standard.document.versionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.documentPanel.descriptionLabel')">
          <el-input v-model="uploadForm.description" type="textarea" :rows="2" :placeholder="$t('standard.document.descriptionLabel')" />
        </el-form-item>
        <el-form-item :label="$t('standard.documentPanel.fileLabel')">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            :on-change="onFileChange"
            :on-exceed="() => ElMessage.warning(t('standard.documentPanel.fileExceedWarning'))"
            accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt"
          >
            <el-button size="small">{{ $t('standard.documentPanel.fileSelectBtn') }}</el-button>
            <template #tip>
              <div class="el-upload__tip">{{ $t('standard.documentPanel.fileTip') }}</div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showUploadDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="submitUpload" :loading="uploading" :disabled="!uploadForm.name">
          {{ selectedFile ? $t('standard.documentPanel.uploadAndLink') : $t('standard.documentPanel.metadataOnly') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关联已有文档对话框 -->
    <el-dialog v-model="showLinkDialog" :title="$t('standard.documentPanel.linkTitle')" width="500px" @open="onLinkDialogOpen">
      <div class="link-tip">{{ $t('standard.documentPanel.linkTip') }}</div>
      <el-select
        v-model="selectedDocIds"
        multiple
        filterable
        remote
        :remote-method="searchDocs"
        :loading="docSearchLoading"
        :placeholder="$t('standard.documentPanel.searchPlaceholder')"
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
        <el-button @click="showLinkDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmLink" :disabled="selectedDocIds.length === 0" :loading="linking">
          {{ $t('standard.documentPanel.confirmLink') }}
        </el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Paperclip } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { elementDocumentAPI, glossaryDocumentAPI, metricDocumentAPI, documentAPI } from '../api/standard'

const { t } = useI18n()

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

const docTypeLabel = (type) => ({
  national: t('standard.document.national'),
  industry: t('standard.document.industry'),
  internal: t('standard.document.internal'),
  reference: t('standard.document.reference')
}[type] || type)
const docTypeTagType = (type) => ({ national: 'danger', industry: 'warning', internal: 'success', reference: '' }[type] || '')

const getToken = () => localStorage.getItem('token') || ''

const loadDocs = async () => {
  if (!props.entityId) return
  loading.value = true
  try {
    const res = await api[props.entityType].list(props.entityId)
    docs.value = res || []
  } catch (e) {
    console.error('loadDocs error:', e)
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
    await ElMessageBox.confirm(t('standard.documentPanel.confirmUnlink', { name: doc.name }), t('standard.common.hint'), { type: 'warning' })
    await api[props.entityType].unlink(props.entityId, doc.id)
    ElMessage.success(t('standard.documentPanel.unlinkSuccess'))
    await loadDocs()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.operationFailed'))
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
    uploadForm.value.name = file.name.replace(/\.[^.]+$/, '')
  }
}

const submitUpload = async () => {
  if (!uploadForm.value.name) {
    ElMessage.warning(t('standard.documentPanel.nameRequired'))
    return
  }
  uploading.value = true
  try {
    const res = await api[props.entityType].create(props.entityId, uploadForm.value)
    const doc = res
    if (selectedFile.value) {
      const formData = new FormData()
      formData.append('file', selectedFile.value)
      await api[props.entityType].uploadFile(doc.id, formData)
    }
    ElMessage.success(t('standard.documentPanel.linkSuccess'))
    showUploadDialog.value = false
    await loadDocs()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
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
    ElMessage.success(t('standard.documentPanel.linkSuccess'))
    showLinkDialog.value = false
    selectedDocIds.value = []
    await loadDocs()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
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
