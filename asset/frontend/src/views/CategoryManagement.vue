<template>
  <div class="category-management">
    <div class="page-header">
      <h2>{{ t('asset.category.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreate(null)">{{ t('asset.category.newRootCategory') }}</el-button>
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
                    :title="t('asset.category.addSubCategory')"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Edit"
                    @click.stop="openEdit(data)"
                    :title="t('asset.category.edit')"
                  />
                  <el-button
                    link
                    size="small"
                    :icon="Delete"
                    type="danger"
                    @click.stop="handleDelete(data)"
                    :title="t('asset.category.delete')"
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
      :title="isEdit ? t('asset.category.editDialog') : (parentNode ? `${t('asset.category.addSubCategory')}（${parentNode.name}）` : t('asset.category.newRootCategory'))"
      width="480px"
      @closed="resetForm"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item :label="t('asset.category.name')" prop="name">
          <el-input v-model="form.name" :placeholder="t('asset.category.namePlaceholder')" maxlength="200" show-word-limit />
        </el-form-item>
        <el-form-item :label="t('asset.category.sortOrder')">
          <el-input-number v-model="form.sort_order" :min="0" :max="9999" style="width:160px" />
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { categoryAPI } from '../api/asset'
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
  name: [{ required: true, message: t('asset.category.nameRequired'), trigger: 'blur' }]
}))

async function loadTree() {
  loading.value = true
  try {
    const res = await categoryAPI.tree()
    treeData.value = res || []
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
  form.value = { name: '', description: '', sort_order: 0 }
  dialogVisible.value = true
}

function openEdit(data) {
  isEdit.value = true
  parentNode.value = null
  form.value = {
    id: data.id,
    version: data.version,
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
      await categoryAPI.update(form.value.id, {
        version: form.value.version,
        name: form.value.name,
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
    ElMessage.error(e.response?.data?.error || (isEdit.value ? t('asset.category.updateFailed') : t('asset.category.createFailed')))
  } finally {
    submitting.value = false
  }
}

async function handleDelete(data) {
  try {
    await ElMessageBox.confirm(
      `${t('asset.category.deleteConfirmTitle')}「${data.name}」？`,
      t('asset.category.deleteConfirmTitle'),
      { type: 'warning', confirmButtonText: t('asset.category.deleteButton'), confirmButtonClass: 'el-button--danger' }
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
  return new Date(t).toLocaleString('zh-CN')
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
