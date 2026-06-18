<template>
  <div class="catalog-management">
    <div class="page-header">
      <h2>{{ t('asset.catalog.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreate(null)">{{ t('asset.catalog.newRootCatalog') }}</el-button>
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
            <template #default="{ node, data }">
              <span class="tree-node">
                <span class="node-label">{{ data.name }}</span>
                <span class="node-actions">
                  <el-button
                    link
                    size="small"
                    :icon="Plus"
                    @click.stop="openCreate(data)"
                    title="添加子目录"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Edit"
                    @click.stop="openEdit(data)"
                    title="编辑"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Delete"
                    type="danger"
                    @click.stop="handleDelete(data)"
                    title="删除"
                  />
                </span>
              </span>
            </template>
          </el-tree>
          <el-empty v-else-if="!loading" :description="t('asset.catalog.emptyDesc')">
            <el-button type="primary" :icon="Plus" @click="openCreate(null)">{{ t('asset.catalog.newRootCatalog') }}</el-button>
          </el-empty>
        </el-card>
      </el-col>

      <!-- 右侧详情 -->
      <el-col :span="16">
        <el-card v-if="selected" class="detail-card">
          <template #header>
            <span>{{ t('asset.catalog.detailTitle') }}</span>
          </template>
          <el-descriptions :column="1" border>
            <el-descriptions-item :label="t('asset.catalog.name')">{{ selected.name }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.catalog.description')">{{ selected.description || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.catalog.sortOrder')">{{ selected.sort_order }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.catalog.createdAt')">{{ formatTime(selected.created_at) }}</el-descriptions-item>
            <el-descriptions-item :label="t('asset.catalog.updatedAt')">{{ formatTime(selected.updated_at) }}</el-descriptions-item>
          </el-descriptions>
          <div class="detail-actions">
            <el-button :icon="Plus" @click="openCreate(selected)">{{ t('asset.catalog.addSubCatalog') }}</el-button>
            <el-button :icon="Edit" @click="openEdit(selected)">{{ t('asset.catalog.edit') }}</el-button>
            <el-button :icon="Delete" type="danger" @click="handleDelete(selected)">{{ t('asset.catalog.delete') }}</el-button>
          </div>
        </el-card>
        <el-empty v-else :description="t('asset.catalog.selectHint')" />
      </el-col>
    </el-row>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? t('asset.catalog.editDialog') : (parentNode ? t('asset.catalog.newRootCatalog') + `（${parentNode.name}）` : t('asset.catalog.newRootCatalog'))"
      width="480px"
      @closed="resetForm"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item :label="t('asset.catalog.name')" prop="name">
          <el-input v-model="form.name" :placeholder="t('asset.catalog.namePlaceholder')" maxlength="200" show-word-limit />
        </el-form-item>
        <el-form-item :label="t('asset.catalog.sortOrder')">
          <el-input-number v-model="form.sort_order" :min="0" :max="9999" style="width:160px" />
          <span class="input-hint">{{ t('asset.catalog.sortOrderHint') }}</span>
        </el-form-item>
        <el-form-item :label="t('asset.catalog.description')">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            :placeholder="t('asset.catalog.descriptionPlaceholder')"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('asset.catalog.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ t('asset.catalog.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { catalogAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const loading = ref(false)
const treeData = ref([])
const selected = ref(null)

const treeProps = { children: 'children', label: 'name' }

const dialogVisible = ref(false)
const isEdit = ref(false)
const parentNode = ref(null)
const submitting = ref(false)
const formRef = ref(null)

const form = ref({ name: '', description: '', sort_order: 0 })
const rules = computed(() => ({
  name: [{ required: true, message: t('asset.catalog.nameRequired'), trigger: 'blur' }]
}))

async function loadTree() {
  loading.value = true
  try {
    const res = await catalogAPI.tree()
    treeData.value = res || []
  } catch (e) {
    ElMessage.error(t('asset.catalog.loadFailed'))
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
  form.value = { name: '', description: '', sort_order: 0 }
  dialogVisible.value = true
}

function openEdit(data) {
  isEdit.value = true
  parentNode.value = null
  form.value = {
    id: data.id,
    name: data.name,
    description: data.description || '',
    sort_order: data.sort_order || 0
  }
  dialogVisible.value = true
}

function resetForm() {
  formRef.value?.resetFields()
}

async function handleSubmit() {
  await formRef.value?.validate()
  submitting.value = true
  try {
    if (isEdit.value) {
      await catalogAPI.update(form.value.id, {
        name: form.value.name,
        description: form.value.description,
        sort_order: form.value.sort_order
      })
      ElMessage.success(t('asset.catalog.updateSuccess'))
    } else {
      await catalogAPI.create({
        name: form.value.name,
        description: form.value.description,
        sort_order: form.value.sort_order,
        parent_id: parentNode.value?.id || null
      })
      ElMessage.success(t('asset.catalog.createSuccess'))
    }
    dialogVisible.value = false
    selected.value = null
    await loadTree()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || (isEdit.value ? t('asset.catalog.updateFailed') : t('asset.catalog.createFailed')))
  } finally {
    submitting.value = false
  }
}

async function handleDelete(data) {
  await ElMessageBox.confirm(
    `${t('asset.catalog.deleteConfirmTitle')}「${data.name}」？`,
    t('asset.catalog.deleteConfirmTitle'),
    { type: 'warning', confirmButtonText: t('asset.catalog.deleteButton'), confirmButtonClass: 'el-button--danger' }
  )
  try {
    await catalogAPI.delete(data.id)
    ElMessage.success(t('asset.catalog.deleteSuccess'))
    if (selected.value?.id === data.id) selected.value = null
    await loadTree()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.catalog.deleteFailed'))
  }
}

function formatTime(t) {
  if (!t) return '—'
  return new Date(t).toLocaleString('zh-CN')
}

onMounted(loadTree)
</script>

<style scoped>
.catalog-management { padding: 24px; }

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-header h2 { margin: 0; font-size: 18px; font-weight: 600; }

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

.input-hint { margin-left: 8px; font-size: 12px; color: var(--el-text-color-secondary); }
</style>
