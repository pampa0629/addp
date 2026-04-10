<template>
  <div class="catalog-page">
    <!-- 左侧目录树 -->
    <div class="sidebar" v-loading="catalogLoading">
      <div class="sidebar-header">
        <h4>{{ t('portal.catalog.title') }}</h4>
      </div>
      <el-scrollbar class="catalog-scroll">
        <el-tree
          :data="catalogTree"
          :props="treeProps"
          node-key="id"
          :default-expanded-keys="[currentCatalogId]"
          :current-node-key="currentCatalogId"
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
        <h3 class="catalog-name">{{ currentCatalogName }}</h3>
        <span class="total-count" v-if="!assetLoading">{{ t('portal.catalog.totalAssets', { count: total }) }}</span>
      </div>

      <div v-loading="assetLoading">
        <el-empty v-if="!assetLoading && assets.length === 0" :description="t('portal.catalog.noAssets')" />
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
            @current-change="fetchAssets"
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
import { catalogAPI } from '../api/portal'
import AssetCard from '../components/AssetCard.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const catalogLoading = ref(false)
const assetLoading = ref(false)
const catalogTree = ref([])
const assets = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(12)

const currentCatalogId = computed(() => Number(route.params.id))
const currentCatalogName = computed(() => {
  return findCatalogName(catalogTree.value, currentCatalogId.value) || t('portal.catalog.browse')
})

const treeProps = {
  children: 'children',
  label: 'name'
}

function findCatalogName(nodes, id) {
  for (const node of nodes) {
    if (node.id === id) return node.name
    if (node.children?.length) {
      const found = findCatalogName(node.children, id)
      if (found) return found
    }
  }
  return null
}

function handleNodeClick(data) {
  router.push(`/portal/catalogs/${data.id}`)
}

async function fetchCatalogs() {
  catalogLoading.value = true
  try {
    const resp = await catalogAPI.list()
    catalogTree.value = resp || []
  } catch (err) {
    console.error('获取目录失败:', err)
  } finally {
    catalogLoading.value = false
  }
}

async function fetchAssets() {
  assetLoading.value = true
  try {
    const resp = await catalogAPI.getAssets(currentCatalogId.value, {
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

watch(() => route.params.id, () => {
  page.value = 1
  fetchAssets()
})

onMounted(() => {
  fetchCatalogs()
  fetchAssets()
})
</script>

<style scoped>
.catalog-page {
  display: flex;
  height: 100%;
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

.catalog-scroll {
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
  overflow-y: auto;
}

.content-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.catalog-name {
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
