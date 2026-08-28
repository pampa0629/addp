<template>
  <div class="category-management">
    <div class="page-header">
      <h2>{{ t('asset.category.title') }}</h2>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="loadTree">{{ t('asset.category.refresh') }}</el-button>
        <el-button type="primary" :icon="Plus" @click="openCreate(null)">{{ t('asset.category.newRootCategory') }}</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 左侧树 -->
      <el-col :span="8">
        <el-card class="tree-card" v-loading="loading">
          <el-tree
            v-if="treeData.length > 0"
            :data="treeData"
            :props="treeProps"
            node-key="id"
            default-expand-all
            highlight-current
            @current-change="handleSelect"
          >
            <template #default="{ data }">
              <span class="tree-node">
                <span class="node-label">{{ data.name }}</span>
                <span class="node-actions">
                  <el-button
                    link
                    size="small"
                    :icon="Plus"
                    @click.stop="openCreate(data)"
                    :title="t('asset.category.addSubCategory')"
                    :aria-label="t('asset.category.addSubCategory')"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Edit"
                    @click.stop="openEdit(data)"
                    :title="t('asset.category.edit')"
                    :aria-label="t('asset.category.edit')"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Delete"
                    type="danger"
                    @click.stop="handleDelete(data)"
                    :title="t('asset.category.delete')"
                    :aria-label="t('asset.category.delete')"
                  />
                </span>
              </span>
            </template>
          </el-tree>
          <el-empty v-else-if="!loading" :description="t('asset.category.emptyDesc')">
            <el-button type="primary" :icon="Plus" @click="openCreate(null)">{{ t('asset.category.newRootCategory') }}</el-button>
          </el-empty>
        </el-card>
      </el-col>

      <!-- 右侧详情 -->
      <el-col :span="16">
        <el-card v-if="selected" class="detail-card">
          <template #header>
            <span>{{ t('asset.category.detailTitle') }}</span>
          </template>
          <el-descriptions :column="1" border>
            <el-descriptions-item :label="t('asset.category.name')">{{ selected.name }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.category.parent')">{{ selectedParentName }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.category.description')">{{ selected.description || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.category.sortOrder')">{{ selected.sort_order }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.category.createdAt')">{{ formatTime(selected.created_at) }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.category.updatedAt')">{{ formatTime(selected.updated_at) }}</el-descriptions-item>
          </el-descriptions>
          <div class="detail-actions">
            <el-button :icon="Plus" @click="openCreate(selected)">{{ t('asset.category.addSubCategory') }}</el-button>
            <el-button :icon="Edit" @click="openEdit(selected)">{{ t('asset.category.edit') }}</el-button>
            <el-button :icon="Delete" type="danger" @click="handleDelete(selected)">{{ t('asset.category.delete') }}</el-button>
          </div>
        </el-card>
        <el-empty v-else :description="t('asset.category.selectHint')" />
      </el-col>
    </el-row>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      class="addp-dialog"
      :title="dialogTitle"
      width="min(560px, calc(100vw - 24px))"
      @opened="focusNameInput"
      @closed="resetForm"
    >
      <div v-if="versionConflict" class="conflict-notice" role="alert">
        <span>{{ t('asset.category.versionConflict') }}</span>
        <el-button link type="primary" :loading="reloading" @click="reloadEditBaseline">
          {{ t('asset.category.reloadLatest') }}
        </el-button>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="t('asset.category.name')" prop="name">
          <el-input ref="nameInputRef" v-model="form.name" :placeholder="t('asset.category.namePlaceholder')" maxlength="200" show-word-limit />
        </el-form-item>
        <el-form-item v-if="isEdit" :label="t('asset.category.parent')" prop="parent_value">
          <el-select v-model="form.parent_value" class="parent-select" :placeholder="t('asset.category.parentPlaceholder')" filterable>
            <el-option :label="t('asset.category.rootCategory')" :value="ROOT_CATEGORY_PARENT" />
            <el-option
              v-for="option in parentOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('asset.category.sortOrder')">
          <el-input-number v-model="form.sort_order" :min="0" :max="9999" class="sort-order-input" />
          <span class="input-hint">{{ t('asset.category.sortOrderHint') }}</span>
        </el-form-item>
        <el-form-item :label="t('asset.category.description')">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            :placeholder="t('asset.category.descriptionPlaceholder')"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('asset.category.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ t('asset.category.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Refresh } from '@element-plus/icons-vue'
import { categoryAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'
import {
  ROOT_CATEGORY_PARENT,
  buildCategoryParentOptions,
  collectCategorySubtreeIds,
  findCategoryNode
} from '../utils/categoryTree'

const { t, locale } = useI18n()

const loading = ref(false)
const treeData = ref([])
const selected = ref(null)

const treeProps = { children: 'children', label: 'name' }

const dialogVisible = ref(false)
const isEdit = ref(false)
const parentNode = ref(null)
const submitting = ref(false)
const reloading = ref(false)
const versionConflict = ref(false)
const formRef = ref(null)
const nameInputRef = ref(null)

const form = ref({ name: '', parent_value: ROOT_CATEGORY_PARENT, description: '', sort_order: 0 })
const rules = computed(() => ({
  name: [{ required: true, message: t('asset.category.nameRequired'), trigger: 'blur' }]
}))
const dialogTitle = computed(() => {
  if (isEdit.value) return t('asset.category.editDialog')
  if (parentNode.value) return t('asset.category.addSubCategoryOf', { name: parentNode.value.name })
  return t('asset.category.newRootCategory')
})
const selectedParentName = computed(() => {
  if (!selected.value?.parent_id) return t('asset.category.rootCategory')
  return findCategoryNode(treeData.value, selected.value.parent_id)?.name || t('asset.category.parentUnavailable')
})
const parentOptions = computed(() => {
  const edited = findCategoryNode(treeData.value, form.value.id)
  return buildCategoryParentOptions(treeData.value, collectCategorySubtreeIds(edited))
})

async function loadTree() {
  loading.value = true
  try {
    const res = await categoryAPI.tree()
    treeData.value = res || []
    if (selected.value) selected.value = findCategoryNode(treeData.value, selected.value.id)
  } catch (e) {
    ElMessage.error(t('asset.category.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleSelect(data) {
  selected.value = data
}

function openCreate(parent) {
  isEdit.value = false
  parentNode.value = parent || null
  versionConflict.value = false
  form.value = { name: '', parent_value: ROOT_CATEGORY_PARENT, description: '', sort_order: 0 }
  dialogVisible.value = true
}

function openEdit(data) {
  isEdit.value = true
  parentNode.value = null
  versionConflict.value = false
  applyEditForm(data)
  dialogVisible.value = true
}

function applyEditForm(data) {
  form.value = {
    id: data.id,
    version: data.version,
    name: data.name,
    parent_value: data.parent_id ?? ROOT_CATEGORY_PARENT,
    description: data.description || '',
    sort_order: data.sort_order || 0
  }
}

function resetForm() {
  versionConflict.value = false
  formRef.value?.resetFields()
}

async function focusNameInput() {
  await nextTick()
  nameInputRef.value?.focus()
}

async function reloadEditBaseline() {
  reloading.value = true
  try {
    const latest = await categoryAPI.get(form.value.id)
    await loadTree()
    applyEditForm(findCategoryNode(treeData.value, latest.id) || latest)
    versionConflict.value = false
    ElMessage.success(t('asset.category.reloaded'))
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.category.loadFailed'))
  } finally {
    reloading.value = false
  }
}

async function handleSubmit() {
  await formRef.value?.validate()
  submitting.value = true
  versionConflict.value = false
  try {
    if (isEdit.value) {
      await categoryAPI.update(form.value.id, {
        version: form.value.version,
        name: form.value.name,
        parent_id: form.value.parent_value === ROOT_CATEGORY_PARENT ? null : form.value.parent_value,
        description: form.value.description,
        sort_order: form.value.sort_order
      })
      ElMessage.success(t('asset.category.updateSuccess'))
    } else {
      await categoryAPI.create({
        name: form.value.name,
        description: form.value.description,
        sort_order: form.value.sort_order,
        parent_id: parentNode.value?.id || null
      })
      ElMessage.success(t('asset.category.createSuccess'))
    }
    dialogVisible.value = false
    selected.value = null
    await loadTree()
  } catch (e) {
    if (isEdit.value && e.response?.status === 409 && e.response?.data?.error_code === 'asset_category_version_conflict') {
      versionConflict.value = true
    }
    ElMessage.error(e.response?.data?.error || (isEdit.value ? t('asset.category.updateFailed') : t('asset.category.createFailed')))
  } finally {
    submitting.value = false
  }
}

async function handleDelete(data) {
  try {
    await ElMessageBox.confirm(
      t('asset.category.deleteConfirm', { name: data.name }),
      t('asset.category.deleteConfirmTitle'),
      {
        type: 'warning',
        confirmButtonText: t('asset.category.deleteButton'),
        cancelButtonText: t('asset.category.cancel'),
        confirmButtonClass: 'el-button--danger',
        customClass: 'addp-message-box'
      }
    )
  } catch {
    return
  }
  try {
    await categoryAPI.delete(data.id, data.version)
    ElMessage.success(t('asset.category.deleteSuccess'))
    if (selected.value?.id === data.id) selected.value = null
    await loadTree()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.category.deleteFailed'))
  }
}

function formatTime(t) {
  if (!t) return '—'
  return new Date(t).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN')
}

onMounted(loadTree)
</script>

<style scoped>
.category-management { padding: 24px; }

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-header h2 { margin: 0; font-size: 18px; font-weight: 600; }

.header-actions { display: flex; gap: 8px; flex-wrap: wrap; }

.tree-card, .detail-card { overflow-y: visible; }

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding-right: 4px;
}

.node-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.node-actions { display: none; gap: 2px; flex-shrink: 0; }
.tree-node:hover .node-actions { display: flex; }

.detail-actions { margin-top: 20px; display: flex; gap: 8px; }

.sort-order-input { width: 160px; }
.parent-select { width: 100%; }

.input-hint { margin-left: 8px; font-size: 12px; color: var(--addp-text-secondary); }

.conflict-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  padding: 12px;
  color: var(--el-color-warning);
  background: var(--addp-bg-secondary);
  border: 1px solid var(--el-color-warning);
  border-radius: 4px;
}

@media (max-width: 768px) {
  .conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
