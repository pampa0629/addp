<template>
  <div class="catalog-management">
    <div class="page-header">
      <h2>目录管理</h2>
      <el-button type="primary" :icon="Plus" @click="openCreate(null)">新建根目录</el-button>
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
          <el-empty v-else-if="!loading" description="暂无目录，点击右上角新建">
            <el-button type="primary" :icon="Plus" @click="openCreate(null)">新建根目录</el-button>
          </el-empty>
        </el-card>
      </el-col>

      <!-- 右侧详情 -->
      <el-col :span="16">
        <el-card v-if="selected" class="detail-card">
          <template #header>
            <span>目录详情</span>
          </template>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="名称">{{ selected.name }}</el-descriptions-item>
            <el-descriptions-item label="描述">{{ selected.description || '—' }}</el-descriptions-item>
            <el-descriptions-item label="排序">{{ selected.sort_order }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatTime(selected.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ formatTime(selected.updated_at) }}</el-descriptions-item>
          </el-descriptions>
          <div class="detail-actions">
            <el-button :icon="Plus" @click="openCreate(selected)">添加子目录</el-button>
            <el-button :icon="Edit" @click="openEdit(selected)">编辑</el-button>
            <el-button :icon="Delete" type="danger" @click="handleDelete(selected)">删除</el-button>
          </div>
        </el-card>
        <el-empty v-else description="选中左侧目录查看详情" />
      </el-col>
    </el-row>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑目录' : (parentNode ? `新建子目录（父：${parentNode.name}）` : '新建根目录')"
      width="480px"
      @closed="resetForm"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入目录名称" maxlength="200" show-word-limit />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" :max="9999" style="width:160px" />
          <span class="input-hint">数值越小越靠前</span>
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            placeholder="请输入目录描述（可选）"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { catalogAPI } from '../api/asset'

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
const rules = {
  name: [{ required: true, message: '请输入目录名称', trigger: 'blur' }]
}

async function loadTree() {
  loading.value = true
  try {
    const res = await catalogAPI.tree()
    treeData.value = res || []
  } catch (e) {
    ElMessage.error('加载目录失败')
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
      ElMessage.success('更新成功')
    } else {
      await catalogAPI.create({
        name: form.value.name,
        description: form.value.description,
        sort_order: form.value.sort_order,
        parent_id: parentNode.value?.id || null
      })
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    selected.value = null
    await loadTree()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || (isEdit.value ? '更新失败' : '创建失败'))
  } finally {
    submitting.value = false
  }
}

async function handleDelete(data) {
  await ElMessageBox.confirm(
    `确定删除目录「${data.name}」？删除前请确保该目录下无子目录且无关联资产。`,
    '删除确认',
    { type: 'warning', confirmButtonText: '删除', confirmButtonClass: 'el-button--danger' }
  )
  try {
    await catalogAPI.delete(data.id)
    ElMessage.success('删除成功')
    if (selected.value?.id === data.id) selected.value = null
    await loadTree()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '删除失败')
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

.tree-card, .detail-card { height: calc(100vh - 180px); overflow-y: auto; }

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
