<template>
  <div class="code-set-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="$router.back()">
          <el-icon><ArrowLeft /></el-icon>
          返回
        </el-button>
        <span class="code-set-name">{{ codeSet.name || '加载中...' }}</span>
        <el-tag :type="codeSet.type === 'system' ? 'success' : 'info'" size="small">
          {{ codeSet.type === 'system' ? '系统内置' : '自定义' }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">基本信息</span></template>
          <el-form :model="form" label-width="90px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="编码">
                  <el-input :value="codeSet.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="名称">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="类型">
                  <el-select v-model="form.type" style="width:100%">
                    <el-option label="自定义" value="custom" />
                    <el-option label="系统内置" value="system" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item label="描述">
                  <el-input v-model="form.description" type="textarea" :rows="2" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>

      <!-- 码值项列表 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">码值项</span>
              <el-button type="primary" size="small" @click="openItemDialog()">
                <el-icon><Plus /></el-icon>
                添加码值项
              </el-button>
            </div>
          </template>

          <el-table :data="items" v-loading="itemLoading" stripe>
            <el-table-column label="序号" type="index" width="60" />
            <el-table-column label="编码" prop="code" width="150" />
            <el-table-column label="显示值" prop="value" min-width="140" />
            <el-table-column label="排序" prop="sort_order" width="80" />
            <el-table-column label="启用" width="80">
              <template #default="{ row }">
                <el-tag :type="row.is_active ? 'success' : 'info'" size="small">
                  {{ row.is_active ? '是' : '否' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="描述" prop="description" show-overflow-tooltip />
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openItemDialog(row)">编辑</el-button>
                <el-popconfirm title="确定删除该码值项吗？" @confirm="deleteItem(row.id)">
                  <template #reference>
                    <el-button link type="danger">删除</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <!-- 码值项对话框 -->
    <el-dialog
      v-model="itemDialogVisible"
      :title="editingItem ? '编辑码值项' : '添加码值项'"
      width="480px"
    >
      <el-form ref="itemFormRef" :model="itemForm" :rules="itemRules" label-width="100px">
        <el-form-item label="编码" prop="code">
          <el-input v-model="itemForm.code" placeholder="如：1, M, active" :disabled="!!editingItem" />
        </el-form-item>
        <el-form-item label="显示值" prop="value">
          <el-input v-model="itemForm.value" placeholder="如：男、启用" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="itemForm.sort_order" :min="0" style="width:100%" />
        </el-form-item>
        <el-form-item label="是否启用">
          <el-switch v-model="itemForm.is_active" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="itemForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="itemDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleItemSubmit" :loading="itemSubmitting">
          {{ editingItem ? '保存' : '添加' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { codeSetAPI } from '../api/standard'

const route = useRoute()
const router = useRouter()
const codeSetId = parseInt(route.params.id)

const saving = ref(false)
const itemLoading = ref(false)
const itemSubmitting = ref(false)
const itemDialogVisible = ref(false)
const editingItem = ref(null)
const itemFormRef = ref(null)

const codeSet = ref({})
const form = reactive({ name: '', type: 'custom', description: '' })
const items = ref([])

const itemForm = reactive({ code: '', value: '', description: '', sort_order: 0, is_active: true })
const itemRules = {
  code: [{ required: true, message: '请输入编码', trigger: 'blur' }],
  value: [{ required: true, message: '请输入显示值', trigger: 'blur' }]
}

const loadCodeSet = async () => {
  const res = await codeSetAPI.get(codeSetId)
  codeSet.value = res.data || {}
  Object.assign(form, {
    name: codeSet.value.name,
    type: codeSet.value.type || 'custom',
    description: codeSet.value.description || ''
  })
}

const loadItems = async () => {
  itemLoading.value = true
  try {
    const res = await codeSetAPI.getItems(codeSetId)
    items.value = res.data || []
  } finally {
    itemLoading.value = false
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    await codeSetAPI.update(codeSetId, form)
    ElMessage.success('保存成功')
    loadCodeSet()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const openItemDialog = (item = null) => {
  editingItem.value = item
  if (item) {
    Object.assign(itemForm, {
      code: item.code,
      value: item.value,
      description: item.description || '',
      sort_order: item.sort_order || 0,
      is_active: item.is_active
    })
  } else {
    Object.assign(itemForm, { code: '', value: '', description: '', sort_order: 0, is_active: true })
  }
  itemDialogVisible.value = true
}

const handleItemSubmit = async () => {
  await itemFormRef.value.validate()
  itemSubmitting.value = true
  try {
    if (editingItem.value) {
      await codeSetAPI.updateItem(codeSetId, editingItem.value.id, itemForm)
      ElMessage.success('更新成功')
    } else {
      await codeSetAPI.createItem(codeSetId, itemForm)
      ElMessage.success('添加成功')
    }
    itemDialogVisible.value = false
    loadItems()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    itemSubmitting.value = false
  }
}

const deleteItem = async (itemId) => {
  try {
    await codeSetAPI.deleteItem(codeSetId, itemId)
    ElMessage.success('删除成功')
    loadItems()
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(async () => {
  loadCodeSet()
  loadItems()
})
</script>

<style scoped>
.code-set-detail {
  padding: 20px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.code-set-name {
  font-size: 18px;
  font-weight: 600;
}

.info-card {
  margin-bottom: 0;
}

.card-title {
  font-weight: 600;
}

.card-header-with-action {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
