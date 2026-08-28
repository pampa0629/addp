<template>
  <div class="category-page">
    <!-- 左侧资产分类树 -->
    <div class="sidebar" v-loading="categoryLoading">
      <div class="sidebar-header">
        <h4>{{ t('portal.category.title') }}</h4>
      </div>
      <el-scrollbar class="category-scroll">
        <el-tree
          :data="categoryTree"
          :props="treeProps"
          node-key="id"
          :default-expanded-keys="[currentCategoryId]"
          :current-node-key="currentCategoryId"
          highlight-current
          @node-click="handleNodeClick"
        >
          <template #default="{ node, data }">
            <span class="tree-node">
              <el-icon><FolderOpened v-if="node.expanded" /><Folder v-else /></el-icon>
              <span class="node-label">{{ data.name }}</span>
              <el-tag size="small" type="info" class="node-count" v-if="data.count > 0">{{ data.count }}</el-tag>
            </span>
          </template>
        </el-tree>
      </el-scrollbar>
    </div>

    <!-- 右侧资产列表 -->
    <div class="main-content">
      <div class="content-header">
        <h3 class="category-name">{{ currentCategoryName }}</h3>
        <span class="total-count" v-if="!assetLoading">{{ t('portal.category.totalAssets', { count: total }) }}</span>
      </div>

      <div v-loading="assetLoading">
        <el-empty v-if="!assetLoading && assets.length === 0" :description="t('portal.category.noAssets')" />
        <el-row :gutter="16" v-else>
          <el-col
            v-for="asset in assets"
            :key="asset.id"
            :xs="24" :sm="12" :md="8"
            class="asset-col"
          >
            <asset-card :asset="asset" />
          </el-col>
        </el-row>

        <div class="pagination-wrapper" v-if="total > pageSize">
          <el-pagination
            v-model:current-page="page"
            :page-size="pageSize"
            :total="total"
            layout="prev, pager, next"
            @current-change="handlePageChange"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Folder, FolderOpened } from '@element-plus/icons-vue'
import { categoryAPI } from '../api/portal'
import AssetCard from '../components/AssetCard.vue'
import { resolveCategoryRouteState } from '../utils/routeState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const categoryLoading = ref(false)
const assetLoading = ref(false)
const categoryTree = ref([])
const assets = ref([])
const total = ref(0)
const page = ref(resolveCategoryRouteState(route.query).page)
const pageSize = ref(12)

const currentCategoryId = computed(() => Number(route.params.id))
const currentCategoryName = computed(() => {
  return findCategoryName(categoryTree.value, currentCategoryId.value) || t('portal.category.browse')
})

const treeProps = {
  children: 'children',
  label: 'name'
}

function findCategoryName(nodes, id) {
  for (const node of nodes) {
    if (node.id === id) return node.name
    if (node.children?.length) {
      const found = findCategoryName(node.children, id)
      if (found) return found
    }
  }
  return null
}

function handleNodeClick(data) {
  router.push(`/portal/categories/${data.id}`)
}

function handlePageChange(nextPage) {
  page.value = nextPage
  router.replace({
    name: 'Category',
    params: { id: String(route.params.id) },
    query: nextPage > 1 ? { page: String(nextPage) } : {}
  })
}

async function fetchCategories() {
  categoryLoading.value = true
  try {
    const resp = await categoryAPI.list()
    categoryTree.value = resp || []
  } catch (err) {
    console.error('获取资产分类失败:', err)
  } finally {
    categoryLoading.value = false
  }
}

async function fetchAssets() {
  assetLoading.value = true
  try {
    const resp = await categoryAPI.getAssets(currentCategoryId.value, {
      page: page.value,
      page_size: pageSize.value
    })
    assets.value = resp.data || []
    total.value = resp.total || 0
  } catch (err) {
    console.error('获取资产列表失败:', err)
  } finally {
    assetLoading.value = false
  }
}

watch(() => [route.params.id, route.query], async () => {
  const routeState = resolveCategoryRouteState(route.query)
  page.value = routeState.page
  if (routeState.changed) {
    await router.replace({
      name: 'Category',
      params: { id: String(route.params.id) },
      query: routeState.query
    })
    return
  }
  await fetchAssets()
}, { immediate: true })

onMounted(() => {
  fetchCategories()
})
</script>

<style scoped>
.category-page {
  display: flex;
  gap: 0;
}

.sidebar {
  width: 240px;
  flex-shrink: 0;
  background: var(--el-fill-color-blank);
  border-right: 1px solid var(--el-border-color);
  display: flex;
  flex-direction: column;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.sidebar-header h4 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.category-scroll {
  flex: 1;
  padding: 8px 0;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.node-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.node-count {
  font-size: 11px;
}

.main-content {
  flex: 1;
  padding: 20px;
}

.content-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.category-name {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: var(--el-text-color-primary);
}

.total-count {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.asset-col {
  margin-bottom: 16px;
}

.pagination-wrapper {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}
</style>
