<template>
  <div class="asset-manager">
    <div class="manager-body">
      <!-- 左侧分类目录树 -->
      <div class="category-panel">
        <div class="category-tree-wrap">
          <!-- 未分类虚拟节点 -->
          <div
            class="category-item uncategorized"
            :class="{ active: selectedCategoryId === null }"
            @click="selectCategory(null)"
          >
            <el-icon><FolderOpened /></el-icon>
            <span>{{ t('asset.assetManager.uncategorized') }}</span>
            <span class="count">{{ uncategorizedCount }}</span>
          </div>

          <!-- 分隔线 + 目录标题 -->
          <div class="catalog-section-header">
            <span>{{ t('asset.assetManager.catalog') }}</span>
            <el-button link type="primary" size="small" @click="openAddRootCategory">{{ t('asset.assetManager.newButton') }}</el-button>
          </div>

          <!-- 分类树 -->
          <el-tree
            v-if="categories.length"
            ref="treeRef"
            :data="categoryTree"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            highlight-current
            :current-node-key="selectedCategoryId"
            @node-click="onTreeNodeClick"
          >
            <template #default="{ node, data }">
              <div class="tree-node">
                <el-icon><Folder /></el-icon>
                <span class="node-label">{{ data.name }}</span>
                <div class="node-actions" @click.stop>
                  <el-button link size="small" @click="openAddSubCategory(data)">+</el-button>
                  <el-button link size="small" @click="openRenameCategory(data)">改</el-button>
                  <el-button link size="small" type="danger" @click="deleteCategory(data)">删</el-button>
                </div>
                <span class="count">{{ data.count || 0 }}</span>
              </div>
            </template>
          </el-tree>
        </div>

      </div>

      <!-- 右侧资产列表 -->
      <div class="asset-panel">
        <!-- 过滤栏 -->
        <div class="filter-bar">
          <el-input
            v-model="filters.keyword"
            :placeholder="t('asset.assetManager.searchPlaceholder')"
            clearable
            style="width: 220px"
            @change="loadAssets"
          />
          <el-select v-model="filters.typeId" :placeholder="t('asset.assetManager.assetType')" clearable style="width: 130px" @change="loadAssets">
            <el-option v-for="t in typeOptions" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
          <el-select v-model="filters.status" :placeholder="t('asset.assetManager.status')" clearable style="width: 110px" @change="loadAssets">
            <el-option :label="t('asset.assetManager.draft')" value="draft" />
            <el-option :label="t('asset.assetManager.published')" value="published" />
            <el-option :label="t('asset.assetManager.offline')" value="offline" />
          </el-select>
          <span class="total-label">{{ t('asset.assetManager.total', { count: total }) }}</span>
          <el-button
            type="primary"
            :loading="syncing"
            @click="handleSync"
          >
            {{ t('asset.assetManager.syncButton') }}
          </el-button>
        </div>

        <!-- 批量操作栏 -->
        <div v-if="selectedIds.length > 0" class="batch-bar">
          <span class="selected-label">{{ t('asset.assetManager.selectedCount', { count: selectedIds.length }) }}</span>
          <el-button size="small" type="success" @click="batchPublish">{{ t('asset.assetManager.batchPublish') }}</el-button>
          <el-button size="small" @click="batchOffline">{{ t('asset.assetManager.batchOffline') }}</el-button>
          <el-dropdown @command="batchCategorize">
            <el-button size="small">{{ t('asset.assetManager.assignCatalog') }} <el-icon><ArrowDown /></el-icon></el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item :command="null">{{ t('asset.assetManager.clearCatalog') }}</el-dropdown-item>
                <el-dropdown-item v-for="cat in flatCategories" :key="cat.id" :command="cat.id">
                  {{ cat.name }}
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>

        <!-- 资产列表表格 -->
        <el-table
          v-loading="loading"
          :data="assets"
          row-key="id"
          @selection-change="onSelectionChange"
          style="width: 100%"
        >
          <el-table-column type="selection" width="44" />
          <el-table-column prop="name" :label="t('asset.assetManager.name')" min-width="180" show-overflow-tooltip />
          <el-table-column prop="type_name" :label="t('asset.assetManager.type')" width="90" />
          <el-table-column prop="source_module" :label="t('asset.assetManager.source')" width="90" />
          <el-table-column prop="catalog_name" :label="t('asset.assetManager.catalog')" width="120" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="category-text">{{ row.catalog_name || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="status" :label="t('asset.assetManager.status')" width="90">
            <template #default="{ row }">
              <el-tag v-if="!row.source_available" type="danger" size="small">{{ t('asset.assetManager.sourceUnavailable') }}</el-tag>
              <el-tag v-else-if="row.status === 'draft'" type="info" size="small">{{ t('asset.assetManager.draft') }}</el-tag>
              <el-tag v-else-if="row.status === 'published'" type="success" size="small">{{ t('asset.assetManager.published') }}</el-tag>
              <el-tag v-else-if="row.status === 'offline'" type="warning" size="small">{{ t('asset.assetManager.offline') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('asset.assetManager.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="row.status !== 'published'"
                link
                type="primary"
                size="small"
                @click="publishOne(row)"
              >{{ t('asset.assetManager.publish') }}</el-button>
              <el-button
                v-if="row.status === 'published'"
                link
                type="warning"
                size="small"
                @click="offlineOne(row)"
              >{{ t('asset.assetManager.takeOffline') }}</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- 分页 -->
        <div class="pagination-wrap">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            @change="loadAssets"
          />
        </div>
      </div>
    </div>

    <!-- 新建/重命名分类弹窗 -->
    <el-dialog v-model="categoryDialogVisible" :title="categoryDialogTitle" width="400px">
      <el-form>
        <el-form-item :label="t('asset.assetManager.catalogName')">
          <el-input v-model="categoryForm.name" :placeholder="t('asset.assetManager.catalogNamePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="categoryDialogVisible = false">{{ t('asset.assetManager.cancel') }}</el-button>
        <el-button type="primary" @click="submitCategory">{{ t('asset.assetManager.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Folder, FolderOpened, ArrowDown } from '@element-plus/icons-vue'
import { assetAPI, catalogAPI, typeDefinitionAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// ===== 状态 =====
const syncing = ref(false)
const loading = ref(false)
const assets = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selectedIds = ref([])

const categories = ref([])
const typeOptions = ref([])
const selectedCategoryId = ref(null) // null = 未分类, number = 分类ID
const uncategorizedCount = ref(0)

const filters = reactive({
  keyword: '',
  typeId: null,
  status: ''
})

// 分类弹窗
const categoryDialogVisible = ref(false)
const categoryDialogTitle = ref('')
const categoryForm = reactive({ name: '', parentId: null, editId: null })

// ===== 计算属性 =====
const categoryTree = computed(() => buildTree(categories.value))

const flatCategories = computed(() => {
  const result = []
  const flatten = (nodes, prefix = '') => {
    for (const n of nodes) {
      result.push({ id: n.id, name: prefix + n.name })
      if (n.children) flatten(n.children, prefix + n.name + ' / ')
    }
  }
  flatten(categoryTree.value)
  return result
})

// ===== 生命周期 =====
onMounted(async () => {
  await Promise.all([loadCategories(), loadTypes()])
  await handleSync()
})

// ===== 同步 =====
async function handleSync() {
  syncing.value = true
  try {
    const res = await assetAPI.sync()
    const created = res.created ?? 0
    if (created > 0) {
      ElMessage.success(t('asset.assetManager.syncSuccess', { count: created }))
    }
  } catch (e) {
    ElMessage.warning(t('asset.assetManager.syncError'))
  } finally {
    syncing.value = false
    await loadAssets()
    await loadUncategorizedCount()
  }
}

// ===== 资产列表 =====
async function loadAssets() {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value
    }
    if (filters.keyword) params.keyword = filters.keyword
    if (filters.typeId) params.type_id = filters.typeId
    if (filters.status) params.status = filters.status

    // 分类过滤：null 时传 -1 表示"未分类"，有 id 时传 id
    if (selectedCategoryId.value === null) {
      params.catalog_id = -1
    } else if (selectedCategoryId.value !== undefined) {
      params.catalog_id = selectedCategoryId.value
    }

    const res = await assetAPI.list(params)
    assets.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    ElMessage.error(t('asset.assetManager.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadUncategorizedCount() {
  try {
    const res = await assetAPI.list({ catalog_id: -1, page: 1, page_size: 1 })
    uncategorizedCount.value = res.total || 0
  } catch {
    // ignore
  }
}

// ===== 状态操作 =====
async function publishOne(row) {
  try {
    await assetAPI.publish(row.id)
    ElMessage.success(t('asset.assetManager.publishSuccess'))
    await loadAssets()
  } catch (e) {
    ElMessage.error(t('asset.assetManager.operationFailed') + ': ' + (e.response?.data?.error || e.message))
  }
}

async function offlineOne(row) {
  try {
    await assetAPI.offline(row.id)
    ElMessage.success(t('asset.assetManager.offlineSuccess'))
    await loadAssets()
  } catch (e) {
    ElMessage.error(t('asset.assetManager.operationFailed') + ': ' + (e.response?.data?.error || e.message))
  }
}

// ===== 批量操作 =====
function onSelectionChange(rows) {
  selectedIds.value = rows.map(r => r.id)
}

async function batchPublish() {
  if (!selectedIds.value.length) return
  try {
    await assetAPI.batchPublish(selectedIds.value)
    ElMessage.success(t('asset.assetManager.batchPublishSuccess'))
    selectedIds.value = []
    await loadAssets()
  } catch (e) {
    ElMessage.error(t('asset.assetManager.operationFailed'))
  }
}

async function batchOffline() {
  if (!selectedIds.value.length) return
  try {
    await assetAPI.batchOffline(selectedIds.value)
    ElMessage.success(t('asset.assetManager.batchOfflineSuccess'))
    selectedIds.value = []
    await loadAssets()
  } catch (e) {
    ElMessage.error(t('asset.assetManager.operationFailed'))
  }
}

async function batchCategorize(categoryId) {
  if (!selectedIds.value.length) return
  try {
    await assetAPI.batchCatalog(selectedIds.value, categoryId)
    ElMessage.success(t('asset.assetManager.catalogAssignSuccess'))
    selectedIds.value = []
    await Promise.all([loadAssets(), loadCategories(), loadUncategorizedCount()])
  } catch (e) {
    ElMessage.error(t('asset.assetManager.operationFailed'))
  }
}

// ===== 分类管理 =====
async function loadCategories() {
  try {
    const res = await catalogAPI.list()
    categories.value = res || []
  } catch {
    categories.value = []
  }
}

async function loadTypes() {
  try {
    const res = await typeDefinitionAPI.list()
    typeOptions.value = res || []
  } catch {
    typeOptions.value = []
  }
}

function selectCategory(id) {
  selectedCategoryId.value = id
  page.value = 1
  loadAssets()
}

function onTreeNodeClick(data) {
  selectedCategoryId.value = data.id
  page.value = 1
  loadAssets()
}

function openAddRootCategory() {
  categoryForm.name = ''
  categoryForm.parentId = null
  categoryForm.editId = null
  categoryDialogTitle.value = t('asset.assetManager.newRootCatalog')
  categoryDialogVisible.value = true
}

function openAddSubCategory(parent) {
  categoryForm.name = ''
  categoryForm.parentId = parent.id
  categoryForm.editId = null
  categoryDialogTitle.value = t('asset.assetManager.newRootCatalog') + `（${parent.name}）`
  categoryDialogVisible.value = true
}

function openRenameCategory(data) {
  categoryForm.name = data.name
  categoryForm.parentId = data.parent_id || null
  categoryForm.editId = data.id
  categoryDialogTitle.value = t('asset.assetManager.renameCatalog')
  categoryDialogVisible.value = true
}

async function submitCategory() {
  if (!categoryForm.name.trim()) {
    ElMessage.warning(t('asset.assetManager.catalogNameRequired'))
    return
  }
  try {
    if (categoryForm.editId) {
      await catalogAPI.update(categoryForm.editId, { name: categoryForm.name })
    } else {
      await catalogAPI.create({ name: categoryForm.name, parent_id: categoryForm.parentId })
    }
    categoryDialogVisible.value = false
    await loadCategories()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.assetManager.operationFailed'))
  }
}

async function deleteCategory(data) {
  try {
    await ElMessageBox.confirm(`${t('asset.assetManager.deleteCategoryConfirm', { name: data.name })}`, t('asset.assetManager.hint'), { type: 'warning' })
    await catalogAPI.delete(data.id)
    await loadCategories()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || t('asset.assetManager.deleteFailed'))
  }
}

// ===== 工具函数 =====
function buildTree(list) {
  const map = {}
  const roots = []
  list.forEach(item => { map[item.id] = { ...item, children: [] } })
  list.forEach(item => {
    if (item.parent_id && map[item.parent_id]) {
      map[item.parent_id].children.push(map[item.id])
    } else {
      roots.push(map[item.id])
    }
  })
  return roots
}
</script>

<style scoped>
.asset-manager {
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color-page, #f5f7fa);
}

.manager-body {
  display: flex;
  align-items: stretch;
  overflow: visible;
}

/* 左侧分类面板 */
.category-panel {
  width: 220px;
  flex-shrink: 0;
  background: var(--el-bg-color, #fff);
  border-right: 1px solid var(--el-border-color-lighter);
  display: flex;
  flex-direction: column;
  overflow: visible;
}

.category-tree-wrap {
  overflow: visible;
  padding: 8px 0;
}

.category-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  cursor: pointer;
  color: var(--el-text-color-regular);
  font-size: 14px;
  transition: background 0.15s;
}

.category-item:hover,
.category-item.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.catalog-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px 6px;
  margin-top: 4px;
  border-top: 1px solid var(--el-border-color-lighter);
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
}

.node-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  padding: 0 4px;
}

.node-actions {
  display: none;
  gap: 2px;
}

.tree-node:hover .node-actions {
  display: flex;
}

/* 右侧资产面板 */
.asset-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: visible;
  padding: 16px;
  gap: 12px;
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.total-label {
  margin-left: auto;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.batch-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--el-color-primary-light-9);
  border-radius: 4px;
  border: 1px solid var(--el-color-primary-light-7);
}

.selected-label {
  font-size: 13px;
  color: var(--el-color-primary);
  font-weight: 500;
}

.category-text {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  margin-top: auto;
}
</style>
