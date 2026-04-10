<template>
  <div class="search-page">
    <!-- 搜索栏 -->
    <div class="search-bar">
      <el-input
        v-model="keyword"
        :placeholder="t('portal.search.placeholder')"
        size="large"
        clearable
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      >
        <template #append>
          <el-button type="primary" :icon="Search" @click="handleSearch">{{ t('portal.search.searchBtn') }}</el-button>
        </template>
      </el-input>
    </div>

    <!-- 结果区 -->
    <div class="result-header" v-if="!loading">
      <span class="result-count">{{ t('portal.search.resultCount', { count: total }) }}</span>
      <span class="keyword-hint" v-if="keyword">
        {{ t('portal.search.keywordHint', { keyword }) }}
      </span>
    </div>

    <div v-loading="loading">
      <el-empty v-if="!loading && assets.length === 0" :description="t('portal.search.noResults')" />
      <div v-else class="result-list">
        <div
          v-for="asset in assets"
          :key="asset.id"
          class="result-item"
          @click="$router.push(`/portal/assets/${asset.id}`)"
        >
          <div class="result-item-header">
            <el-tag size="small" class="type-tag">{{ getTypeName(asset.type_code, asset.type_name) }}</el-tag>
            <span class="asset-name">{{ asset.name }}</span>
            <span class="catalog-path" v-if="asset.catalog_name">
              <el-icon><FolderOpened /></el-icon>
              {{ asset.catalog_name }}
            </span>
          </div>
          <p class="asset-description">{{ asset.description || t('portal.common.noDescription') }}</p>
          <div class="result-item-footer">
            <div class="tags" v-if="asset.tags?.length">
              <el-tag
                v-for="tag in asset.tags.slice(0, 4)"
                :key="tag"
                size="small"
                type="info"
              >{{ tag }}</el-tag>
              <span v-if="asset.tags.length > 4" class="more-tags">+{{ asset.tags.length - 4 }}</span>
            </div>
            <span class="date">{{ formatDate(asset.updated_at) }}</span>
          </div>
        </div>
      </div>

      <div class="pagination-wrapper" v-if="total > pageSize">
        <el-pagination
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next, total"
          @current-change="fetchAssets"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Search, FolderOpened } from '@element-plus/icons-vue'
import { formatDate } from '@common-ui'
import { searchAPI } from '../api/portal'
import { useAssetType } from '../composables/useAssetType'

const { t } = useI18n()
const { getTypeName } = useAssetType()
const route = useRoute()
const router = useRouter()

const keyword = ref(route.query.keyword || '')
const typeId = ref(route.query.type_id ? Number(route.query.type_id) : 0)
const loading = ref(false)
const assets = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(15)

function handleSearch() {
  page.value = 1
  router.push({ path: '/portal/search', query: keyword.value ? { keyword: keyword.value } : {} })
}

async function fetchAssets() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (keyword.value) params.keyword = keyword.value
    if (typeId.value) params.type_id = typeId.value
    const resp = await searchAPI.search(params)
    assets.value = resp.data || []
    total.value = resp.total || 0
  } catch (err) {
    console.error('搜索失败:', err)
  } finally {
    loading.value = false
  }
}

watch(() => route.query, (query) => {
  keyword.value = query.keyword || ''
  typeId.value = query.type_id ? Number(query.type_id) : 0
  page.value = 1
  fetchAssets()
})

onMounted(fetchAssets)
</script>

<style scoped>
.search-page {
  padding: 24px;
  max-width: 960px;
  margin: 0 auto;
}

.search-bar {
  margin-bottom: 20px;
}

.result-header {
  margin-bottom: 16px;
  font-size: 14px;
  color: var(--el-text-color-secondary);
}

.result-count strong {
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.keyword-hint {
  color: var(--el-color-primary);
}

.result-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.result-item {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.15s;
}

.result-item:hover {
  border-color: var(--el-color-primary);
  box-shadow: var(--addp-shadow-card);
}

.result-item-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}

.type-tag {
  font-size: 11px;
  flex-shrink: 0;
}

.asset-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  flex: 1;
}

.catalog-path {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}

.asset-description {
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin: 0 0 10px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  line-height: 1.5;
}

.result-item-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.more-tags {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  line-height: 20px;
}

.date {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}

.pagination-wrapper {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}
</style>
