<template>
  <div class="document-list">
    <div class="page-header">
      <div>
        <h2>{{ $t('standard.document.title') }}</h2>
        <p class="page-subtitle">{{ $t('standard.document.subtitle') }}</p>
      </div>
      <el-button type="primary" @click="openCreateDialog">{{ $t('standard.document.create') }}</el-button>
    </div>

    <el-card>
      <div class="toolbar">
        <el-input v-model="keyword" :placeholder="$t('standard.document.searchPlaceholder')" clearable @change="handleFilterChange" style="width:280px" />
        <el-select v-model="filterType" :placeholder="$t('standard.document.filterTypePlaceholder')" clearable @change="handleFilterChange" style="width:140px">
          <el-option :label="$t('standard.document.national')" value="national" />
          <el-option :label="$t('standard.document.industry')" value="industry" />
          <el-option :label="$t('standard.document.internal')" value="internal" />
          <el-option :label="$t('standard.document.reference')" value="reference" />
        </el-select>
      </div>

      <el-table :data="documents" v-loading="loading" size="small" @row-click="openDetail">
        <el-table-column :label="$t('standard.document.nameLabel')" prop="name" min-width="200" show-overflow-tooltip />
        <el-table-column :label="$t('standard.common.type')" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="docTypeTagType(row.doc_type)">{{ docTypeLabel(row.doc_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.document.sourceLabel')" prop="source_org" width="160" show-overflow-tooltip />
        <el-table-column :label="$t('standard.document.versionLabel')" prop="version" width="80" />
        <el-table-column :label="$t('standard.document.attachment')" width="120">
          <template #default="{ row }">
            <span v-if="row.file_name" class="file-name" :title="row.file_name">
              <el-icon style="vertical-align:middle;margin-right:2px"><Document /></el-icon>{{ row.file_name }}
            </span>
            <span v-else class="no-file">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.document.entryTime')" width="110">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
            <el-button link size="small" type="primary" v-if="row.file_name" @click.stop="downloadFile(row)">{{ $t('standard.document.download') }}</el-button>
            <el-button link size="small" type="danger" @click.stop="deleteDocument(row)">{{ $t('standard.common.delete') }}</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[20, 50]"
          layout="total, sizes, prev, pager, next"
          @change="handlePageChange"
        />
      </div>
    </el-card>

    <!-- 录入文档对话框 -->
    <el-dialog v-model="showCreateDialog" :title="$t('standard.document.createTitle')" width="520px" @closed="resetForm">
      <el-form :model="form" label-width="100px">
        <el-form-item :label="$t('standard.document.nameLabel')" required>
          <el-input v-model="form.name" :placeholder="$t('standard.document.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.document.typeLabel')">
          <el-select v-model="form.doc_type" style="width:100%">
            <el-option :label="$t('standard.document.national')" value="national" />
            <el-option :label="$t('standard.document.industry')" value="industry" />
            <el-option :label="$t('standard.document.internal')" value="internal" />
            <el-option :label="$t('standard.document.reference')" value="reference" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.document.sourceLabel')">
          <el-input v-model="form.source_org" :placeholder="$t('standard.document.sourcePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.document.versionLabel')">
          <el-input v-model="form.version" :placeholder="$t('standard.document.versionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.document.descriptionLabel')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.document.fileLabel')">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            :on-exceed="() => ElMessage.warning(t('standard.document.fileTip'))"
            :on-change="onFileChange"
            :on-remove="() => { selectedFile = null }"
          >
            <el-button size="small">{{ $t('standard.document.fileSelectBtn') }}</el-button>
            <template #tip>
              <div class="upload-tip">{{ $t('standard.document.fileTip') }}</div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="createDocument" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 文档详情抽屉（只读） -->
    <el-drawer v-model="showDetail" :title="currentDoc?.name" size="480px">
      <div v-if="currentDoc" class="doc-detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item :label="$t('standard.common.type')">
            <el-tag size="small" :type="docTypeTagType(currentDoc.doc_type)">{{ docTypeLabel(currentDoc.doc_type) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('standard.document.source')">{{ currentDoc.source_org || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('standard.document.version')">{{ currentDoc.version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('standard.document.entryTime')">{{ formatTime(currentDoc.created_at) }}</el-descriptions-item>
          <el-descriptions-item :label="$t('standard.document.descriptionLabel')" :span="2">{{ currentDoc.description || '-' }}</el-descriptions-item>
        </el-descriptions>

        <!-- 附件区域 -->
        <el-divider>{{ $t('standard.document.attachment') }}</el-divider>
        <div v-if="currentDoc.file_name" class="file-section">
          <el-icon><Document /></el-icon>
          <span class="file-info">{{ currentDoc.file_name }}（{{ formatFileSize(currentDoc.file_size) }}）</span>
          <el-button link type="primary" size="small" @click="downloadFile(currentDoc)">{{ $t('standard.document.download') }}</el-button>
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            :on-change="(f) => uploadToExisting(currentDoc, f)"
            style="display:inline-block;margin-left:8px"
          >
            <el-button link size="small">{{ $t('standard.document.reupload') }}</el-button>
          </el-upload>
        </div>
        <div v-else class="file-section no-file-section">
          <span style="color:var(--el-text-color-secondary);font-size:13px">{{ $t('standard.document.noAttachment') }}</span>
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            :on-change="(f) => uploadToExisting(currentDoc, f)"
            style="display:inline-block;margin-left:12px"
          >
            <el-button size="small" type="primary" plain>{{ $t('standard.document.uploadFile') }}</el-button>
          </el-upload>
        </div>

        <!-- 关联标准项（只读展示） -->
        <el-divider>{{ $t('standard.document.relatedItems') }}</el-divider>
        <el-alert
          :title="$t('standard.document.relatedItemsHint')"
          type="info"
          :closable="false"
          show-icon
          style="margin-bottom: 12px"
        />
        <el-tabs>
          <el-tab-pane :label="$t('standard.document.relatedElements')" name="elements">
            <el-tag v-for="m in mappings.elements" :key="m.element_id" size="small" class="mapping-tag" @click="openRelated('elements', m.element_id)">
              {{ m.name || $t('standard.document.elementRef', { id: m.element_id }) }}{{ m.reference_location ? ` (${m.reference_location})` : '' }}
            </el-tag>
            <el-empty v-if="!mappings.elements?.length" :description="$t('standard.document.noRelated')" :image-size="60" />
          </el-tab-pane>
          <el-tab-pane :label="$t('standard.document.relatedGlossaries')" name="glossaries">
            <el-tag v-for="m in mappings.glossaries" :key="m.glossary_id" size="small" class="mapping-tag" @click="openRelated('glossaries', m.glossary_id)">
              {{ m.name || $t('standard.document.glossaryRef', { id: m.glossary_id }) }}
            </el-tag>
            <el-empty v-if="!mappings.glossaries?.length" :description="$t('standard.document.noRelated')" :image-size="60" />
          </el-tab-pane>
          <el-tab-pane :label="$t('standard.document.relatedMetrics')" name="metrics">
            <el-tag v-for="m in mappings.metrics" :key="m.metric_id" size="small" class="mapping-tag" @click="openRelated('metrics', m.metric_id)">
              {{ m.name || $t('standard.document.metricRef', { id: m.metric_id }) }}
            </el-tag>
            <el-empty v-if="!mappings.metrics?.length" :description="$t('standard.document.noRelated')" :image-size="60" />
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document } from '@element-plus/icons-vue'
import { documentAPI } from '../api/standard'
import { saveBlob } from '../utils/download'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDate } from '../utils/dateTime'

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()

const documents = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(Number(route.query.page) > 0 ? Number(route.query.page) : 1)
const pageSize = ref(Number(route.query.page_size) > 0 ? Number(route.query.page_size) : 20)
const total = ref(0)
const keyword = ref(typeof route.query.keyword === 'string' ? route.query.keyword : '')
const filterType = ref(typeof route.query.doc_type === 'string' ? route.query.doc_type : '')

const showCreateDialog = ref(false)
const showDetail = ref(false)
const currentDoc = ref(null)
const mappings = ref({ elements: [], glossaries: [], metrics: [] })
let selectedFile = null
const uploadRef = ref(null)

const form = ref({ name: '', doc_type: 'reference', source_org: '', version: '', description: '' })

const docTypeLabel = (type) => ({
  national: t('standard.document.national'),
  industry: t('standard.document.industry'),
  internal: t('standard.document.internal'),
  reference: t('standard.document.reference')
}[type] || type)
const docTypeTagType = (type) => ({ national: 'danger', industry: 'warning', internal: 'primary', reference: 'info' }[type] || 'info')
const formatTime = time => formatStandardDate(time, locale.value)
const formatFileSize = (bytes) => {
  if (!bytes) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

const openCreateDialog = () => {
  selectedFile = null
  showCreateDialog.value = true
}

const resetForm = () => {
  form.value = { name: '', doc_type: 'reference', source_org: '', version: '', description: '' }
  selectedFile = null
  uploadRef.value?.clearFiles()
}

const onFileChange = (file) => {
  selectedFile = file.raw
}

const loadDocuments = async () => {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value, keyword: keyword.value, doc_type: filterType.value }
    const res = await documentAPI.list(params)
    documents.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    documents.value = []
    total.value = 0
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const syncQuery = () => {
  const query = {}
  if (keyword.value) query.keyword = keyword.value
  if (filterType.value) query.doc_type = filterType.value
  if (page.value !== 1) query.page = String(page.value)
  if (pageSize.value !== 20) query.page_size = String(pageSize.value)
  navigateStandardRoute(router, { path: '/documents', query }, { history: 'replace' })
}

const handleFilterChange = () => {
  page.value = 1
  syncQuery()
  loadDocuments()
}

const handlePageChange = () => {
  syncQuery()
  loadDocuments()
}

const createDocument = async () => {
  if (!form.value.name) {
    ElMessage.warning(t('standard.document.nameRequired'))
    return
  }
  saving.value = true
  try {
    const res = await documentAPI.create(form.value)
    const docId = res?.id
    if (selectedFile && docId) {
      const fd = new FormData()
      fd.append('file', selectedFile)
      await documentAPI.uploadFile(docId, fd)
    }
    ElMessage.success(t('standard.common.createSuccess'))
    showCreateDialog.value = false
    loadDocuments()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    saving.value = false
  }
}

const openDetail = async (row) => {
  currentDoc.value = row
  showDetail.value = true
  try {
    const res = await documentAPI.getMappings(row.id)
    mappings.value = res || { elements: [], glossaries: [], metrics: [] }
  } catch (e) {
    mappings.value = { elements: [], glossaries: [], metrics: [] }
  }
}

const downloadFile = async (row) => {
  try {
    const blob = await documentAPI.download(row.id)
    saveBlob(blob, row.file_name || row.name)
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.document.downloadFailed'))
  }
}

const openRelated = (resource, id) => navigateStandardRoute(router, `/${resource}/${id}`)

const uploadToExisting = async (doc, file) => {
  const fd = new FormData()
  fd.append('file', file.raw)
  try {
    await documentAPI.uploadFile(doc.id, fd)
    ElMessage.success(t('standard.document.uploadSuccess'))
    const res = await documentAPI.get(doc.id)
    currentDoc.value = res
    const idx = documents.value.findIndex(d => d.id === doc.id)
    if (idx !== -1) documents.value[idx] = res
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  }
}

const deleteDocument = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.document.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await documentAPI.delete(row.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    loadDocuments()
  } catch (e) {
    if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
  }
}

onMounted(loadDocuments)
</script>

<style scoped>
.document-list { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
.page-header h2 { margin: 0 0 4px; font-size: 18px; color: var(--addp-text-primary); }
.page-subtitle { margin: 0; font-size: 13px; color: var(--addp-text-secondary); }
.document-list :deep(.el-card) { background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }
.toolbar { display: flex; gap: 10px; margin-bottom: 16px; }
.pagination { margin-top: 16px; display: flex; justify-content: flex-end; }
.doc-detail { padding: 0 4px; }
.upload-tip { font-size: 12px; color: var(--addp-text-secondary); margin-top: 4px; }
.file-name { font-size: 12px; color: var(--el-text-color-regular); max-width: 100px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; vertical-align: middle; }
.no-file { color: var(--el-text-color-placeholder); }
.file-section { display: flex; align-items: center; gap: 8px; padding: 8px 0; font-size: 13px; }
.file-info { color: var(--el-text-color-regular); }
.no-file-section { padding: 8px 0; }
.mapping-tag { margin: 4px; cursor: pointer; }
.table-actions { display: inline-flex; align-items: center; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }

@media (max-width: 768px) {
  .document-list { padding: 12px; }
  .page-header { flex-wrap: wrap; gap: 10px; }
  .toolbar { flex-wrap: wrap; }
  .toolbar :deep(.el-input), .toolbar :deep(.el-select) { width: 100% !important; }
}
</style>
