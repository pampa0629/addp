<template>
  <div class="explorer-search">
    <!-- 搜索输入框 -->
    <el-input
      v-model="keyword"
      :placeholder="t('manager.explorer.searchPlaceholder')"
      clearable
      :prefix-icon="Search"
      @input="handleSearchInput"
      @clear="handleClear"
      class="search-input"
    >
      <template #append>
        <el-button :icon="Search" @click="handleSearchClick" />
      </template>
    </el-input>

    <!-- 搜索结果 -->
    <div v-if="showResults" class="search-results">
      <!-- 加载状态 -->
      <div v-if="searching" class="search-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>{{ t('manager.explorer.searching') }}</span>
      </div>

      <!-- 无结果 -->
      <div v-else-if="results.length === 0 && keyword.length >= 2" class="no-results">
        <el-empty :description="t('manager.explorer.noMatchingDataSources')" :image-size="80" />
      </div>

      <!-- 结果列表 -->
      <div v-else-if="results.length > 0" class="results-list">
        <div class="results-header">
          <span class="results-count">{{ t('manager.explorer.foundResults', { n: total }) }}</span>
          <el-button text size="small" @click="handleClear">{{ t('manager.explorer.clear') }}</el-button>
        </div>

        <el-scrollbar max-height="400px">
          <div
            v-for="(result, index) in results"
            :key="index"
            class="result-item"
            @click="handleSelectResult(result)"
          >
            <!-- 节点信息 -->
            <div class="result-node">
              <el-icon class="node-icon">
                <component :is="getNodeIcon(result.node)" />
              </el-icon>
              <span class="node-label">{{ result.node.label }}</span>
              <el-tag size="small" :type="getNodeTypeColor(result.node.type)">
                {{ result.node.type }}
              </el-tag>
            </div>

            <!-- 完整路径 -->
            <div class="result-path">
              <el-icon><FolderOpened /></el-icon>
              <span
                v-for="(segment, idx) in result.path"
                :key="idx"
                class="path-segment"
              >
                {{ segment.label }}
                <el-icon v-if="idx < result.path.length - 1"><ArrowRight /></el-icon>
              </span>
            </div>

            <!-- 匹配信息 -->
            <div class="result-meta">
              <el-tag size="small" effect="plain">
                {{ getMatchTypeLabel(result.match_type) }}
              </el-tag>
              <span class="score">{{ t('manager.explorer.score') }}: {{ (result.score * 100).toFixed(0) }}%</span>
            </div>
          </div>
        </el-scrollbar>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { Search, Loading, FolderOpened, ArrowRight, Document, Coin, Collection, Folder } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useExplorerStore } from '@/stores/explorer'
import { debounce } from 'lodash-es'

const { t } = useI18n()

// Props
const props = defineProps({
  engineId: {
    type: Number,
    required: false,
    default: null
  },
  nodeTypes: {
    type: Array,
    default: () => null
  }
})

// Emits
const emit = defineEmits(['select-result'])

const store = useExplorerStore()

// 状态
const keyword = ref('')
const results = ref([])
const total = ref(0)
const searching = ref(false)
const showResults = ref(false)

// 搜索逻辑（防抖 500ms）
const performSearch = debounce(async () => {
  if (!keyword.value || keyword.value.length < 2) {
    results.value = []
    total.value = 0
    showResults.value = false
    return
  }

  searching.value = true
  showResults.value = true

  try {
    // 如果指定了 engineId，只搜索该引擎；否则搜索所有引擎
    if (props.engineId) {
      const searchResults = await store.searchNodes(
        props.engineId,
        keyword.value,
        props.nodeTypes
      )
      results.value = searchResults
      total.value = searchResults.length
    } else {
      // 搜索所有引擎
      const allResults = []
      for (const engine of store.engines) {
        const engineResults = await store.searchNodes(
          engine.id,
          keyword.value,
          props.nodeTypes
        )
        allResults.push(...engineResults)
      }
      results.value = allResults
      total.value = allResults.length
    }
  } catch (error) {
    console.error('搜索失败:', error)
    ElMessage.error(t('manager.explorer.searchFailed', { error: error.message }))
    results.value = []
    total.value = 0
  } finally {
    searching.value = false
  }
}, 500)

// 事件处理：输入变化
const handleSearchInput = () => {
  performSearch()
}

// 事件处理：点击搜索按钮
const handleSearchClick = () => {
  performSearch.flush() // 立即执行搜索，不等待防抖
}

// 事件处理：清除
const handleClear = () => {
  keyword.value = ''
  results.value = []
  total.value = 0
  showResults.value = false
}

// 事件处理：选择搜索结果
const handleSelectResult = (result) => {
  console.log('[ExplorerSearch] 选择搜索结果:', result)

  // 触发父组件事件
  emit('select-result', result)

  // 清除搜索
  handleClear()
}

// 工具函数：获取节点图标
const getNodeIcon = (node) => {
  const iconMap = {
    table: Document,
    collection: Collection,
    schema: Folder,
    database: Coin,
    bucket: Folder,
    directory: Folder,
    object: Document
  }
  return iconMap[node.type] || Document
}

// 工具函数：获取节点类型颜色
const getNodeTypeColor = (type) => {
  const colorMap = {
    table: 'primary',
    collection: 'success',
    schema: 'info',
    database: 'warning',
    bucket: 'info',
    directory: 'info',
    object: 'info'
  }
  return colorMap[type] || 'info'
}

// 工具函数：获取匹配类型标签
const getMatchTypeLabel = (matchType) => {
  const labelMap = {
    exact: t('manager.explorer.matchExact'),
    prefix: t('manager.explorer.matchPrefix'),
    contains: t('manager.explorer.matchContains'),
    metadata: t('manager.explorer.matchMetadata')
  }
  return labelMap[matchType] || matchType
}

// 监听 engineId 变化，重新搜索
watch(() => props.engineId, () => {
  if (keyword.value) {
    performSearch()
  }
})
</script>

<style scoped>
.explorer-search {
  padding: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.search-input {
  width: 100%;
}

.search-results {
  margin-top: 12px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  overflow: hidden;
}

.search-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px;
  color: var(--el-text-color-secondary);
}

.no-results {
  padding: 24px;
}

.results-list {
  padding: 0;
}

.results-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.results-count {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.result-item {
  padding: 12px;
  cursor: pointer;
  border-bottom: 1px solid var(--el-border-color-lighter);
  transition: background-color 0.2s;
}

.result-item:hover {
  background: var(--el-fill-color-light);
}

.result-item:last-child {
  border-bottom: none;
}

.result-node {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.node-icon {
  font-size: 16px;
  color: var(--el-color-primary);
}

.node-label {
  font-weight: 500;
  font-size: 14px;
  flex: 1;
}

.result-path {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.path-segment {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.result-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.score {
  color: var(--el-text-color-secondary);
}
</style>
