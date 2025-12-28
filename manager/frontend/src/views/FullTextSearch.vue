<template>
  <div class="fulltext-search">
    <el-card class="search-card">
      <div class="search-header">
        <h2>全文检索</h2>
        <p class="search-subtitle">支持按标题、文件名、正文内容等关键词快速定位文档</p>
      </div>
      <el-form class="search-form" @submit.prevent="handleSearch">
        <el-form-item class="search-input">
          <el-input
            v-model="keyword"
            placeholder="请输入搜索关键词，例如：季度报表 / 销售数据"
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
                      <span>搜索历史</span>
                      <el-button
                        type="text"
                        size="small"
                        :disabled="historyItems.length === 0 || historyLoading"
                        @click="handleClearHistory"
                      >
                        清除全部
                      </el-button>
                    </div>
                    <div v-if="historyLoading" class="history-loading">加载中...</div>
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
                          <el-button type="text" size="small" @click.stop="removeHistoryItem(item)">
                            删除
                          </el-button>
                        </div>
                      </template>
                      <el-empty v-else description="暂无历史记录" />
                    </el-scrollbar>
                  </div>
                </el-popover>
                <el-button class="search-button" type="primary" :loading="loading" @click="handleSearch">
                  搜索
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
          <span>检索结果</span>
          <span v-if="hasSearched" class="results-count">
            共 {{ total }} 条记录
          </span>
        </div>
      </template>

      <template v-if="!hasSearched">
        <el-empty description="输入关键词后开始搜索" />
      </template>

      <template v-else-if="results.length === 0">
        <el-empty description="未找到匹配的文档或引擎">
          <el-button type="primary" @click="resetSearch">重新搜索</el-button>
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
                {{ item.title || item.file_name || '未命名文档' }}
              </span>
              <el-tag v-if="item.document_type" type="success" size="small">
                {{ item.document_type }}
              </el-tag>
              <el-tag v-if="isVectorMatch(item)" type="warning" size="small" effect="dark">
                🔍 向量匹配
              </el-tag>
            </div>
            <div class="result-actions">
              <el-button
                size="small"
                type="primary"
                @click="navigateToDocument(item)"
              >
                定位
              </el-button>
            </div>
          </div>

          <div class="result-meta">
            <span>引擎：{{ formatResource(item) }}</span>
            <span v-if="item.schema">桶/Schema：{{ item.schema }}</span>
            <span v-if="item.relative_path">路径：{{ item.relative_path }}</span>
            <span v-if="item.last_modified">更新：{{ formatDate(item.last_modified) }}</span>
            <span v-if="item.file_size">大小：{{ formatSize(item.file_size) }}</span>
            <span v-if="item.score !== undefined && item.score > 0" class="vector-score">
              <el-tag type="warning" size="small" effect="plain">
                向量相似度：{{ formatScore(item.score) }}
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
import { Document, Clock } from '@element-plus/icons-vue'
import searchAPI from '@/api/search'

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
    const items = response.data?.data?.items
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
    console.error('[FullTextSearch] 加载搜索历史失败', error)
    if (historyVisible.value) {
      ElMessage.error('加载搜索历史失败')
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
    ElMessage.success('已删除所选历史')
    await loadHistory()
  } catch (error) {
    console.error('[FullTextSearch] 删除搜索历史失败', error)
    ElMessage.error('删除历史失败')
  }
}

const handleClearHistory = async () => {
  if (historyItems.value.length === 0) {
    return
  }
  try {
    await ElMessageBox.confirm('确认清除全部搜索历史？', '提示', {
      confirmButtonText: '清除',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch (error) {
    return
  }

  try {
    await searchAPI.clearHistory()
    historyItems.value = []
    ElMessage.success('已清除全部历史')
  } catch (error) {
    console.error('[FullTextSearch] 清除搜索历史失败', error)
    ElMessage.error('清除历史失败')
  }
}

onMounted(() => {
  loadHistory()
})

const handleSearch = async () => {
  const trimmed = keyword.value.trim()
  if (!trimmed) {
    ElMessage.warning('请输入搜索关键词')
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
    const response = await searchAPI.fullText(params)
    // API client 已经自动提取了第一层 data，所以这里直接用 response.data
    const payload = response.data || {}
    results.value = Array.isArray(payload.results) ? payload.results : []
    total.value = Number(payload.total || 0)
    page.value = Number(payload.page || page.value || 1)
    pageSize.value = Number(payload.page_size || pageSize.value || 10)
    hasSearched.value = true
  } catch (error) {
    hasSearched.value = true
    console.error('[FullTextSearch] 搜索失败', error)
    const message = error.response?.data?.error || error.message
    ElMessage.error(`搜索失败：${message}`)
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
  const name = item.resource_name || ''
  const id = item.engine_id
  if (name) {
    return name
  }
  if (id) {
    return `引擎 ${id}`
  }
  return '未知引擎'
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

const isVectorMatch = (item = {}) => {
  // 检查是否有向量相似度分数
  return item.score !== undefined && item.score > 0
}

const navigateToDocument = (item = {}) => {
  if (!item.engine_id) {
    ElMessage.warning('缺少引擎信息，无法定位文档')
    return
  }
  const schema = item.schema || item.bucket || ''
  const objectPath = item.relative_path || ''
  const query = {
    engineId: String(item.engine_id),
    schema,
    objectPath
  }
  router.push({ path: '/data-explorer', query })
}
</script>

<style scoped>
.fulltext-search {
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
  color: #909399;
  font-size: 13px;
}

.search-form {
  margin-top: 4px;
}

.search-input :deep(.el-input) {
  font-size: 16px;
}

.search-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.history-trigger {
  padding: 0;
  width: 36px;
  height: 36px;
  border: none;
  background-color: transparent;
  color: #606266;
}

.history-trigger:hover {
  color: #409eff;
}

.history-trigger:focus-visible {
  outline: none;
}

.search-button {
  margin-left: 4px;
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
  color: #303133;
}

.history-loading {
  text-align: center;
  color: #909399;
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
  color: #303133;
}

.history-query:hover {
  color: #409eff;
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
  color: #909399;
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
  color: #303133;
}

.result-name {
  max-width: 520px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.result-icon {
  color: #409eff;
}

.result-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  font-size: 13px;
  color: #606266;
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
  color: #303133;
  line-height: 1.6;
  background: #f5f7fa;
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
  color: #909399;
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
