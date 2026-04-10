<template>
  <div class="data-retrieval">
    <el-card class="search-card">
      <div class="search-header">
        <h2>{{ t('manager.retrieval.title') }}</h2>
        <p class="search-subtitle">{{ t('manager.retrieval.subtitle') }}</p>
      </div>
      <el-form class="search-form" @submit.prevent="handleSearch">
        <el-form-item class="search-input">
          <el-input
            v-model="keyword"
            :placeholder="t('manager.retrieval.searchPlaceholder')"
            clearable
            @keyup.enter="handleSearch"
          >
            <template #append>
              <div class="search-actions">
                <el-popover
                  placement="bottom-end"
                  trigger="click"
                  width="300"
                  v-model:visible="historyVisible"
                  @show="handleHistoryPopoverShow"
                >
                  <template #reference>
                    <el-button class="history-trigger" :loading="historyVisible && historyLoading" circle>
                      <el-icon>
                        <Clock />
                      </el-icon>
                    </el-button>
                  </template>
                  <div class="history-popover">
                    <div class="history-header">
                      <span>{{ t('manager.retrieval.searchHistory') }}</span>
                      <el-button
                        text
                        size="small"
                        :disabled="historyItems.length === 0 || historyLoading"
                        @click="handleClearHistory"
                      >
                        {{ t('manager.retrieval.clearAll') }}
                      </el-button>
                    </div>
                    <div v-if="historyLoading" class="history-loading">{{ t('manager.retrieval.loading') }}</div>
                    <el-scrollbar v-else class="history-list">
                      <template v-if="historyItems.length > 0">
                        <div
                          v-for="item in historyItems"
                          :key="item.id"
                          class="history-item"
                        >
                          <span class="history-query" @click="applyHistory(item)">
                            {{ item.query }}
                          </span>
                          <el-button text size="small" @click.stop="removeHistoryItem(item)">
                            {{ t('manager.retrieval.deleteHistory') }}
                          </el-button>
                        </div>
                      </template>
                      <el-empty v-else description="暂无历史记录" />
                    </el-scrollbar>
                  </div>
                </el-popover>
                <el-button class="search-button" type="primary" size="large" :loading="loading" @click="handleSearch">
                  <el-icon><Search /></el-icon>
                  <span>{{ t('manager.retrieval.searchBtn') }}</span>
                </el-button>
              </div>
            </template>
          </el-input>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="results-card" v-loading="loading">
      <template #header>
        <div class="results-header">
          <span>{{ t('manager.retrieval.results') }}</span>
          <span v-if="hasSearched" class="results-count">
            {{ t('manager.retrieval.resultsCount', { total }) }}
          </span>
        </div>
      </template>

      <template v-if="!hasSearched">
        <el-empty :description="t('manager.retrieval.emptyHint')" />
      </template>

      <template v-else-if="results.length === 0">
        <el-empty :description="t('manager.retrieval.noResults')">
          <el-button type="primary" @click="resetSearch">{{ t('manager.retrieval.researchBtn') }}</el-button>
        </el-empty>
      </template>

      <div v-else class="results-list">
        <div v-for="item in results" :key="item.document_id" class="result-item">
          <div class="result-header">
            <div class="result-title">
              <el-icon class="result-icon">
                <Document />
              </el-icon>
              <span class="result-name">
                {{ item.title || item.file_name || t('manager.retrieval.unnamed') }}
              </span>
              <el-tag v-if="item.document_type" type="success" size="small">
                {{ item.document_type }}
              </el-tag>
              <el-tag v-if="isHybridMatch(item)" type="danger" size="small" effect="dark">
                🔥 关键词+向量
              </el-tag>
              <el-tag v-else-if="isVectorMatch(item)" type="warning" size="small" effect="dark">
                🔍 向量匹配
              </el-tag>
            </div>
            <div class="result-actions">
              <el-button
                size="small"
                type="primary"
                @click="navigateToDocument(item)"
              >
                {{ t('manager.retrieval.locating') }}
              </el-button>
            </div>
          </div>

          <div class="result-meta">
            <span>{{ t('manager.retrieval.engineLabel') }}{{ formatResource(item) }}</span>
            <span v-if="item.schema">{{ t('manager.retrieval.bucketLabel') }}{{ item.schema }}</span>
            <span v-if="item.relative_path">{{ t('manager.retrieval.pathLabel') }}{{ item.relative_path }}</span>
            <span v-if="item.last_modified">{{ t('manager.retrieval.updatedLabel') }}{{ formatDate(item.last_modified) }}</span>
            <span v-if="item.file_size">{{ t('manager.retrieval.sizeLabel') }}{{ formatSize(item.file_size) }}</span>
            <span v-if="hasVectorMatch(item)" class="vector-score">
              <el-tag type="warning" size="small" effect="plain">
                {{ t('manager.retrieval.vectorScore') }}{{ formatVectorScore(item.vector_distance) }}
              </el-tag>
            </span>
          </div>

          <div v-if="getSnippet(item)" class="result-snippet" v-html="getSnippet(item)" />

          <div class="result-extra" v-if="item.metadata && item.metadata.description">
            简介：{{ item.metadata.description }}
          </div>
        </div>

        <el-pagination
          v-if="total > pageSize"
          background
          layout="total, prev, pager, next, sizes"
          :total="total"
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 30, 50]"
          @current-change="handlePageChange"
          @size-change="handlePageSizeChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Clock, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import searchAPI from '@/api/search'

const { t } = useI18n()
const router = useRouter()

const keyword = ref('')
const results = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const hasSearched = ref(false)
const historyVisible = ref(false)
const historyLoading = ref(false)
const historyItems = ref([])
const historyLimit = 10

const highlightOrder = [
  'content',
  'content_preview',
  'metadata.summary',
  'metadata.tags',
  'title',
  'file_name'
]

const loadHistory = async () => {
  historyLoading.value = true
  try {
    const response = await searchAPI.history({ limit: historyLimit })
    // API client 已经自动提取了第一层 data，所以这里直接用 response.data
    const items = response.data?.items
    if (Array.isArray(items)) {
      historyItems.value = items.map((item) => ({
        id: item.id,
        query: item.query,
        createdAt: item.created_at,
        updatedAt: item.updated_at
      }))
    } else {
      historyItems.value = []
    }
  } catch (error) {
    console.error('[DataRetrieval] 加载搜索历史失败', error)
    if (historyVisible.value) {
      ElMessage.error(t('manager.retrieval.loadHistoryFailed'))
    }
  } finally {
    historyLoading.value = false
  }
}

const handleHistoryPopoverShow = async () => {
  if (!historyLoading.value) {
    await loadHistory()
  }
}

const applyHistory = async (item = {}) => {
  if (!item.query) {
    return
  }
  keyword.value = item.query
  historyVisible.value = false
  await handleSearch()
}

const removeHistoryItem = async (item = {}) => {
  if (!item.id) {
    return
  }
  try {
    await searchAPI.deleteHistoryItem(item.id)
    ElMessage.success(t('manager.retrieval.deleteHistorySuccess'))
    await loadHistory()
  } catch (error) {
    console.error('[DataRetrieval] 删除搜索历史失败', error)
    ElMessage.error('删除历史失败')
  }
}

const handleClearHistory = async () => {
  if (historyItems.value.length === 0) {
    return
  }
  try {
    await ElMessageBox.confirm(t('manager.retrieval.clearConfirm'), t('common.info'), {
      confirmButtonText: t('manager.retrieval.clearAll'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
  } catch (error) {
    return
  }

  try {
    await searchAPI.clearHistory()
    historyItems.value = []
    ElMessage.success(t('manager.retrieval.clearSuccess'))
  } catch (error) {
    console.error('[DataRetrieval] 清除搜索历史失败', error)
    ElMessage.error('清除历史失败')
  }
}

onMounted(() => {
  loadHistory()
})

const handleSearch = async () => {
  const trimmed = keyword.value.trim()
  if (!trimmed) {
    ElMessage.warning(t('manager.retrieval.searchWarning'))
    return
  }
  page.value = 1
  await fetchResults()
  await loadHistory()
}

const fetchResults = async () => {
  const trimmed = keyword.value.trim()
  if (!trimmed) {
    return
  }

  loading.value = true
  try {
    const params = {
      q: trimmed,
      page: page.value,
      page_size: pageSize.value
    }
    const response = await searchAPI.search(params)
    // API client 已经自动提取了第一层 data，所以这里直接用 response.data
    const payload = response.data || {}
    results.value = Array.isArray(payload.results) ? payload.results : []
    total.value = Number(payload.total || 0)
    page.value = Number(payload.page || page.value || 1)
    pageSize.value = Number(payload.page_size || pageSize.value || 10)
    hasSearched.value = true
  } catch (error) {
    hasSearched.value = true
    console.error('[DataRetrieval] 搜索失败', error)
    const message = error.response?.data?.error || error.message
    ElMessage.error(t('manager.retrieval.searchFailed', { msg: message }))
  } finally {
    loading.value = false
  }
}

const handlePageChange = async (nextPage) => {
  page.value = nextPage
  await fetchResults()
}

const handlePageSizeChange = async (size) => {
  pageSize.value = size
  page.value = 1
  await fetchResults()
}

const resetSearch = () => {
  keyword.value = ''
  results.value = []
  total.value = 0
  page.value = 1
  pageSize.value = 10
  hasSearched.value = false
}

const escapeHtml = (input = '') => {
  return input
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const getSnippet = (item = {}) => {
  const highlights = item.highlights || {}
  for (const key of highlightOrder) {
    const fragments = highlights[key]
    if (Array.isArray(fragments) && fragments.length > 0) {
      return fragments[0]
    }
  }
  if (item.content_preview) {
    return escapeHtml(item.content_preview.slice(0, 200))
  }
  return ''
}

const formatResource = (item = {}) => {
  const name = item.engine_name || ''
  const id = item.engine_id
  if (name) {
    return name
  }
  if (id) {
    return t('manager.retrieval.engineWithId', { id })
  }
  return t('manager.retrieval.unknownEngine')
}

const formatDate = (value) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

const formatSize = (bytes) => {
  if (!bytes || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = 0
  let size = bytes
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index++
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

const formatScore = (score) => {
  if (score === undefined || score === null) return ''
  // 将分数转换为百分比显示（0-1 -> 0-100%）
  const percentage = (score * 100).toFixed(1)
  return `${percentage}%`
}

const formatVectorScore = (distance) => {
  if (distance === undefined || distance === null) return ''
  // 向量距离转换为相似度百分比
  // distance 越小越相似，通常 distance <= 1
  // 使用公式: similarity = (1 - distance) * 100%
  const similarity = Math.max(0, Math.min(100, (1 - distance) * 100))
  return `${similarity.toFixed(1)}%`
}

const isVectorMatch = (item = {}) => {
  // 检查是否包含向量匹配（包括纯向量和混合匹配）
  return Array.isArray(item.match_methods) && item.match_methods.includes('vector')
}

const isHybridMatch = (item = {}) => {
  // 检查是否为混合匹配（同时包含关键词和向量）
  return Array.isArray(item.match_methods) &&
         item.match_methods.includes('keyword') &&
         item.match_methods.includes('vector')
}

const hasVectorMatch = (item = {}) => {
  // 检查是否有向量匹配，并且有 vector_distance 字段
  return isVectorMatch(item) && item.vector_distance !== undefined && item.vector_distance !== null
}

const navigateToDocument = (item = {}) => {
  const fileName = item.file_name || t('manager.retrieval.unnamed')
  // 检查必需字段
  const engineId = item.engine_id || item.resource_id
  if (!engineId) {
    ElMessage.warning({
      message: t('manager.retrieval.missingEngine', { name: fileName }),
      duration: 5000
    })
    return
  }

  // 获取存储桶
  const bucket = item.schema || item.bucket || ''
  if (!bucket) {
    ElMessage.warning({
      message: t('manager.retrieval.missingBucket', { name: fileName }),
      duration: 5000
    })
    return
  }

  let objectPath = item.relative_path || item.object_key || ''

  if (objectPath && item.file_name && !objectPath.endsWith(item.file_name)) {
    objectPath = `${objectPath}/${item.file_name}`.replace(/\/+/g, '/')
  }

  if (!objectPath && item.asset_id && item.asset_id.length < 100) {
    objectPath = item.asset_id
  }

  if (!objectPath) {
    ElMessage.warning({
      message: t('manager.retrieval.missingPath', { name: fileName }),
      duration: 5000
    })
    return
  }

  const query = {
    engineId: String(engineId),
    bucket: bucket,
    objectKey: objectPath
  }

  console.log('[DataRetrieval] 定位到对象:', query)
  router.push({ path: '/data-explorer', query })
}
</script>

<style scoped>
.data-retrieval {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.search-card {
  padding: 8px 12px 16px;
}

.search-header {
  margin-bottom: 12px;
}

.search-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: #1f2d3d;
}

.search-subtitle {
  margin: 4px 0 0;
  color: var(--addp-text-tertiary);
  font-size: 13px;
}

.search-form {
  margin-top: 4px;
}

.search-input :deep(.el-input) {
  font-size: 16px;
}

.search-input :deep(.el-input-group__append) {
  border-left: none;
  background-color: transparent;
  padding: 0 16px;
}

.search-actions {
  display: flex;
  align-items: center;
  gap: 48px;
}

.history-trigger {
  padding: 0;
  width: 36px;
  height: 36px;
  border: none;
  background-color: transparent;
  color: var(--addp-text-secondary);
}

.history-trigger:hover {
  color: var(--el-color-primary);
}

.history-trigger:focus-visible {
  outline: none;
}

.search-button {
  min-width: 100px;
  height: 40px;
  font-size: 16px;
  font-weight: 600;
  box-shadow: 0 2px 8px rgba(64, 158, 255, 0.3);
  transition: all 0.3s ease;
}

.search-button:hover {
  box-shadow: 0 4px 12px rgba(64, 158, 255, 0.5);
  transform: translateY(-1px);
}

.search-button :deep(.el-icon) {
  margin-right: 4px;
  font-size: 18px;
}

.history-popover {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.history-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  color: var(--addp-text-primary);
}

.history-loading {
  text-align: center;
  color: var(--addp-text-tertiary);
  font-size: 13px;
  padding: 12px 0;
}

.history-list {
  max-height: 240px;
}

.history-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 0;
}

.history-query {
  flex: 1;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--addp-text-primary);
}

.history-query:hover {
  color: var(--el-color-primary);
}

.history-list :deep(.el-empty) {
  margin: 8px 0;
}

.results-card {
  min-height: 320px;
}

.results-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: #1f2d3d;
}

.results-count {
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.results-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.result-item {
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.result-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.result-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.result-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.result-name {
  max-width: 520px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.result-icon {
  color: var(--el-color-primary);
}

.result-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.vector-score {
  display: inline-flex;
  align-items: center;
}

.vector-score :deep(.el-tag) {
  font-weight: 600;
  border: 1px solid #f0ad4e;
}

.result-snippet {
  font-size: 14px;
  color: var(--addp-text-primary);
  line-height: 1.6;
  background: var(--addp-bg-secondary);
  padding: 12px;
  border-radius: 6px;
  max-height: 120px;
  overflow: hidden;
}

.result-snippet mark {
  background-color: #ffe58f;
  padding: 0 2px;
}

.result-extra {
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.result-actions {
  display: flex;
  gap: 8px;
}

.el-pagination {
  align-self: flex-end;
  margin-top: 8px;
}
</style>
