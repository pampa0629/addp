<template>
  <div class="document-list">
    <div class="page-header">
      <div>
        <h2>全局文档库</h2>
        <p class="page-subtitle">集中查看所有已录入的标准文档。如需关联到具体标准项，请前往对应标准项的详情页操作。</p>
      </div>
      <el-button type="primary" @click="openCreateDialog">录入文档</el-button>
    </div>

    <el-card>
      <div class="toolbar">
        <el-input v-model="keyword" placeholder="搜索文档名称/来源机构" clearable @change="loadDocuments" style="width:280px" />
        <el-select v-model="filterType" placeholder="文档类型" clearable @change="loadDocuments" style="width:140px">
          <el-option label="国家标准" value="national" />
          <el-option label="行业标准" value="industry" />
          <el-option label="企业内部" value="internal" />
          <el-option label="参考资料" value="reference" />
        </el-select>
      </div>

      <el-table :data="documents" v-loading="loading" size="small" @row-click="openDetail">
        <el-table-column label="文档名称" prop="name" min-width="200" show-overflow-tooltip />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="docTypeTagType(row.doc_type)">{{ docTypeLabel(row.doc_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源/标准号" prop="source_org" width="160" show-overflow-tooltip />
        <el-table-column label="版本" prop="version" width="80" />
        <el-table-column label="附件" width="120">
          <template #default="{ row }">
            <span v-if="row.file_name" class="file-name" :title="row.file_name">
              <el-icon style="vertical-align:middle;margin-right:2px"><Document /></el-icon>{{ row.file_name }}
            </span>
            <span v-else class="no-file">—</span>
          </template>
        </el-table-column>
        <el-table-column label="录入时间" width="110">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button link size="small" type="primary" v-if="row.file_name" @click.stop="downloadFile(row)">下载</el-button>
            <el-button link size="small" type="danger" @click.stop="deleteDocument(row)">删除</el-button>
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
          @change="loadDocuments"
        />
      </div>
    </el-card>

    <!-- 录入文档对话框 -->
    <el-dialog v-model="showCreateDialog" title="录入标准文档" width="520px" @closed="resetForm">
      <el-form :model="form" label-width="100px">
        <el-form-item label="文档名称" required>
          <el-input v-model="form.name" placeholder="如：GB/T 35273-2020 个人信息安全规范" />
        </el-form-item>
        <el-form-item label="文档类型">
          <el-select v-model="form.doc_type" style="width:100%">
            <el-option label="国家标准" value="national" />
            <el-option label="行业标准" value="industry" />
            <el-option label="企业内部" value="internal" />
            <el-option label="参考资料" value="reference" />
          </el-select>
        </el-form-item>
        <el-form-item label="来源/标准号">
          <el-input v-model="form.source_org" placeholder="如：国家市场监督管理总局" />
        </el-form-item>
        <el-form-item label="版本号">
          <el-input v-model="form.version" placeholder="如：2020-01" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="文档文件">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            :on-exceed="() => ElMessage.warning('只能上传一个文件')"
            :on-change="onFileChange"
            :on-remove="() => { selectedFile = null }"
          >
            <el-button size="small">选择文件</el-button>
            <template #tip>
              <div class="upload-tip">支持 PDF、Word、Excel 等格式，可选</div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="createDocument" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 文档详情抽屉（只读） -->
    <el-drawer v-model="showDetail" :title="currentDoc?.name" size="480px">
      <div v-if="currentDoc" class="doc-detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="类型">
            <el-tag size="small" :type="docTypeTagType(currentDoc.doc_type)">{{ docTypeLabel(currentDoc.doc_type) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="来源">{{ currentDoc.source_org || '-' }}</el-descriptions-item>
          <el-descriptions-item label="版本">{{ currentDoc.version || '-' }}</el-descriptions-item>
          <el-descriptions-item label="录入时间">{{ formatTime(currentDoc.created_at) }}</el-descriptions-item>
          <el-descriptions-item label="描述" :span="2">{{ currentDoc.description || '-' }}</el-descriptions-item>
        </el-descriptions>

        <!-- 附件区域 -->
        <el-divider>附件</el-divider>
        <div v-if="currentDoc.file_name" class="file-section">
          <el-icon><Document /></el-icon>
          <span class="file-info">{{ currentDoc.file_name }}（{{ formatFileSize(currentDoc.file_size) }}）</span>
          <el-button link type="primary" size="small" @click="downloadFile(currentDoc)">下载</el-button>
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            :on-change="(f) => uploadToExisting(currentDoc, f)"
            style="display:inline-block;margin-left:8px"
          >
            <el-button link size="small">重新上传</el-button>
          </el-upload>
        </div>
        <div v-else class="file-section no-file-section">
          <span style="color:var(--el-text-color-secondary);font-size:13px">暂无附件</span>
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            :on-change="(f) => uploadToExisting(currentDoc, f)"
            style="display:inline-block;margin-left:12px"
          >
            <el-button size="small" type="primary" plain>上传文件</el-button>
          </el-upload>
        </div>

        <!-- 关联标准项（只读展示） -->
        <el-divider>已关联的标准项</el-divider>
        <el-alert
          title="如需修改关联，请前往对应标准项的详情页操作"
          type="info"
          :closable="false"
          show-icon
          style="margin-bottom: 12px"
        />
        <el-tabs>
          <el-tab-pane label="数据元" name="elements">
            <el-tag v-for="m in mappings.elements" :key="m.element_id" size="small" style="margin:4px">
              数据元 #{{ m.element_id }}{{ m.reference_location ? '（' + m.reference_location + '）' : '' }}
            </el-tag>
            <el-empty v-if="!mappings.elements?.length" description="暂无关联" :image-size="60" />
          </el-tab-pane>
          <el-tab-pane label="业务术语" name="glossaries">
            <el-tag v-for="m in mappings.glossaries" :key="m.glossary_id" size="small" style="margin:4px">
              术语 #{{ m.glossary_id }}
            </el-tag>
            <el-empty v-if="!mappings.glossaries?.length" description="暂无关联" :image-size="60" />
          </el-tab-pane>
          <el-tab-pane label="指标" name="metrics">
            <el-tag v-for="m in mappings.metrics" :key="m.metric_id" size="small" style="margin:4px">
              指标 #{{ m.metric_id }}
            </el-tag>
            <el-empty v-if="!mappings.metrics?.length" description="暂无关联" :image-size="60" />
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document } from '@element-plus/icons-vue'
import { documentAPI } from '../api/standard'

const documents = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')
const filterType = ref('')

const showCreateDialog = ref(false)
const showDetail = ref(false)
const currentDoc = ref(null)
const mappings = ref({ elements: [], glossaries: [], metrics: [] })
let selectedFile = null
const uploadRef = ref(null)

const form = ref({ name: '', doc_type: 'reference', source_org: '', version: '', description: '' })

const docTypeLabel = (t) => ({ national: '国家标准', industry: '行业标准', internal: '企业内部', reference: '参考资料' }[t] || t)
const docTypeTagType = (t) => ({ national: 'danger', industry: 'warning', internal: 'primary', reference: 'info' }[t] || 'info')
const formatTime = (t) => t ? new Date(t).toLocaleDateString('zh-CN') : '-'
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
  } finally {
    loading.value = false
  }
}

const createDocument = async () => {
  if (!form.value.name) {
    ElMessage.warning('请填写文档名称')
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
    ElMessage.success('创建成功')
    showCreateDialog.value = false
    loadDocuments()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
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

const downloadFile = (row) => {
  const token = localStorage.getItem('token') || ''
  const url = documentAPI.downloadUrl(row.id) + (token ? `?token=${encodeURIComponent(token)}` : '')
  const a = document.createElement('a')
  a.href = url
  a.download = row.file_name || 'document'
  a.click()
}

const uploadToExisting = async (doc, file) => {
  const fd = new FormData()
  fd.append('file', file.raw)
  try {
    await documentAPI.uploadFile(doc.id, fd)
    ElMessage.success('文件上传成功')
    const res = await documentAPI.get(doc.id)
    currentDoc.value = res
    const idx = documents.value.findIndex(d => d.id === doc.id)
    if (idx !== -1) documents.value[idx] = res
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '上传失败')
  }
}

const deleteDocument = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除文档"${row.name}"？此操作不可恢复。`, '提示', { type: 'warning' })
    await documentAPI.delete(row.id)
    ElMessage.success('删除成功')
    loadDocuments()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

onMounted(loadDocuments)
</script>

<style scoped>
.document-list { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
.page-header h2 { margin: 0 0 4px; font-size: 18px; color: var(--el-text-color-primary); }
.page-subtitle { margin: 0; font-size: 13px; color: var(--el-text-color-secondary); }
.toolbar { display: flex; gap: 10px; margin-bottom: 16px; }
.pagination { margin-top: 16px; display: flex; justify-content: flex-end; }
.doc-detail { padding: 0 4px; }
.upload-tip { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.file-name { font-size: 12px; color: var(--el-text-color-regular); max-width: 100px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; vertical-align: middle; }
.no-file { color: var(--el-text-color-placeholder); }
.file-section { display: flex; align-items: center; gap: 8px; padding: 8px 0; font-size: 13px; }
.file-info { color: var(--el-text-color-regular); }
.no-file-section { padding: 8px 0; }
</style>
